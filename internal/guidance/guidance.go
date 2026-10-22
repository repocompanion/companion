// Package guidance ranks patterns, ADRs and standards for a coding task.
package guidance

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/bolacha/docsync/internal/config"
	"github.com/bolacha/docsync/internal/search"
	"github.com/bolacha/docsync/internal/store"
)

type Result struct {
	ID             string                `json:"id"`
	Title          string                `json:"title"`
	Path           string                `json:"path"`
	Kind           string                `json:"kind"`
	Status         string                `json:"status"`
	Scope          string                `json:"scope"`
	Score          float64               `json:"score"`
	Snippet        string                `json:"snippet,omitempty"`
	MatchedTerms   []string              `json:"matched_terms,omitempty"`
	Why            string                `json:"why,omitempty"`
	Topics         []string              `json:"topics,omitempty"`
	Languages      []string              `json:"languages,omitempty"`
	SupersededBy   string                `json:"superseded_by,omitempty"`
	Overrides      string                `json:"overrides,omitempty"`
	ReferenceImpls []store.ReferenceImpl `json:"reference_impls,omitempty"`
	Note           string                `json:"note,omitempty"`
}

type candidate struct {
	doc          *store.Doc
	scope        string
	scopeRank    int
	score        float64
	snippet      string
	matchedTerms []string
	why          string
}

var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "for": true, "from": true, "how": true, "in": true,
	"is": true, "it": true, "of": true, "on": true, "or": true, "our": true,
	"that": true, "the": true, "this": true, "to": true, "we": true, "with": true,
	"implement": true, "implementing": true, "add": true, "new": true, "use": true,
	"using": true, "write": true, "create": true,
}

// Lookup returns ranked guidance for task. maxResults <= 0 means 5.
func Lookup(s *store.Store, ctx *config.Context, task, language string, maxResults int) ([]Result, error) {
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 20 {
		maxResults = 20
	}
	tokens := tokenize(task)
	lang := strings.ToLower(strings.TrimSpace(language))
	if lang == "" && ctx != nil && len(ctx.Languages) > 0 {
		lang = ctx.Languages[0]
	}

	scopes := []struct {
		path string
		rank int
	}{
		{"", 0},
		{"", 1},
		{"org", 2},
	}
	if ctx != nil {
		scopes[0].path = ctx.ProjectArea()
		scopes[1].path = ctx.TeamArea()
	}
	seenScope := map[string]bool{}
	var candidates []candidate
	for _, sc := range scopes {
		if sc.path == "" || seenScope[sc.path] {
			continue
		}
		seenScope[sc.path] = true
		docs, err := s.WalkDocs(sc.path)
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			if d.Frontmatter.Kind != "pattern" && d.Frontmatter.Kind != "adr" && d.Frontmatter.Kind != "standard" {
				continue
			}
			score := scoreDoc(d, tokens, lang)
			candidates = append(candidates, candidate{doc: d, scope: sc.path, scopeRank: sc.rank, score: float64(score)})
		}
	}

	// Build one BM25 index across the candidate set. The ranking is field
	// weighted; language and status adjustments are applied below.
	scoreDocs := make([]*store.Doc, 0, len(candidates))
	for _, c := range candidates {
		scoreDocs = append(scoreDocs, c.doc)
	}
	scoreByPath := map[string]search.Result{}
	if ranked, rerr := search.Search(scoreDocs, task, len(scoreDocs)+1); rerr == nil {
		for _, r := range ranked {
			scoreByPath[r.Path] = r
		}
	}
	for i := range candidates {
		c := &candidates[i]
		if r, ok := scoreByPath[c.doc.Path]; ok {
			c.score = r.Score
			c.snippet = r.Snippet
			c.matchedTerms = r.MatchedTerms
			c.why = r.Why
		} else {
			c.score = float64(scoreDoc(c.doc, tokens, lang))
		}
		if lang != "" && len(c.doc.Frontmatter.Languages) > 0 {
			match := false
			for _, l := range c.doc.Frontmatter.Languages {
				if strings.EqualFold(strings.TrimSpace(l), lang) {
					match = true
					break
				}
			}
			if match {
				c.score += 2
			} else {
				c.score -= 1
			}
		}
		switch c.doc.Frontmatter.Status {
		case "approved":
			c.score += 0.5
		case "proposed":
			c.score += 0.2
		}
		if c.snippet == "" {
			c.snippet = c.doc.Frontmatter.Title
		}
	}

	// Explicit overrides always suppress the broader document, even if the
	// broader document has a better keyword score.
	overridden := map[string]bool{}
	for _, c := range candidates {
		if c.doc.Frontmatter.Overrides != "" {
			overridden[c.doc.Frontmatter.Overrides] = true
		}
	}

	// De-duplicate by stable ID. The narrower scope wins.
	best := map[string]candidate{}
	for _, c := range candidates {
		id := c.doc.Frontmatter.ID
		if id == "" {
			id = c.doc.Path
		}
		prev, ok := best[id]
		if !ok || c.scopeRank < prev.scopeRank || (c.scopeRank == prev.scopeRank && c.score > prev.score) {
			best[id] = c
		}
	}
	var filtered []candidate
	for id, c := range best {
		if overridden[id] {
			continue
		}
		f := c.doc.Frontmatter
		if f.Status == "deprecated" || f.Status == "superseded" {
			continue
		}
		filtered = append(filtered, c)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].score != filtered[j].score {
			return filtered[i].score > filtered[j].score
		}
		if filtered[i].scopeRank != filtered[j].scopeRank {
			return filtered[i].scopeRank < filtered[j].scopeRank
		}
		return filtered[i].doc.Path < filtered[j].doc.Path
	})

	// If nothing live matched, surface deprecated docs with their redirect so
	// the agent gets a useful answer instead of silence.
	includeDeprecated := len(filtered) == 0
	if includeDeprecated {
		for _, c := range candidates {
			if c.doc.Frontmatter.Status == "deprecated" || c.doc.Frontmatter.Status == "superseded" {
				filtered = append(filtered, c)
			}
		}
		sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].score > filtered[j].score })
	}

	var out []Result
	for _, c := range filtered {
		if len(out) >= maxResults {
			break
		}
		f := c.doc.Frontmatter
		r := Result{
			ID:             f.ID,
			Title:          f.Title,
			Path:           c.doc.Path,
			Kind:           f.Kind,
			Status:         f.Status,
			Scope:          c.scope,
			Score:          c.score,
			Snippet:        c.snippet,
			MatchedTerms:   c.matchedTerms,
			Why:            c.why,
			Topics:         f.Topics,
			Languages:      f.Languages,
			SupersededBy:   f.SupersededBy,
			Overrides:      f.Overrides,
			ReferenceImpls: f.ReferenceImpls,
		}
		if f.Status == "deprecated" || f.Status == "superseded" {
			r.Note = "deprecated"
			if f.SupersededBy != "" {
				r.Note = "deprecated; superseded_by " + f.SupersededBy
			}
		}
		if r.Title == "" {
			r.Title = strings.TrimSuffix(pathBase(r.Path), ".md")
		}
		out = append(out, r)
	}
	return out, nil
}

func scoreDoc(d *store.Doc, tokens []string, language string) int {
	f := d.Frontmatter
	score := 0
	title := strings.ToLower(f.Title + " " + f.ID)
	topics := strings.ToLower(strings.Join(f.Topics, " "))
	body := strings.ToLower(d.Body)
	for _, t := range tokens {
		if strings.Contains(topics, t) {
			score += 6
		}
		if strings.Contains(title, t) {
			score += 4
		}
		if strings.Contains(body, t) {
			score += 1
		}
	}
	return score
}

func tokenize(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, word := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(word) < 2 || stopwords[word] || seen[word] {
			continue
		}
		seen[word] = true
		out = append(out, word)
	}
	return out
}

func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// FormatCompact is used by the CLI for a short, readable result.
func FormatCompact(results []Result) string {
	if len(results) == 0 {
		return "no guidance found\n"
	}
	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s [%s/%s] score=%.2f\n   %s\n", i+1, r.Title, r.Kind, r.Status, r.Score, r.Path)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "   %s\n", r.Snippet)
		}
		if r.Note != "" {
			fmt.Fprintf(&b, "   note: %s\n", r.Note)
		}
		for _, ref := range r.ReferenceImpls {
			refName := ref.Repo + "@main"
			if ref.Ref != "" {
				refName = ref.Repo + "@" + ref.Ref
			}
			if ref.Commit != "" {
				refName = ref.Repo + "@" + ref.Commit
			}
			fmt.Fprintf(&b, "   ref: %s:%s\n", refName, ref.Path)
		}
	}
	return b.String()
}
