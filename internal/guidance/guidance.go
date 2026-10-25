// Package guidance ranks patterns, ADRs and standards for a coding task.
package guidance

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/repocompanion/companion/internal/codeindex"
	"github.com/repocompanion/companion/internal/config"
	"github.com/repocompanion/companion/internal/search"
	"github.com/repocompanion/companion/internal/store"
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
	CodeContext    []CodeContext         `json:"code_context,omitempty"`
	LastReviewed   string                `json:"last_reviewed,omitempty"`
	Warnings       []string              `json:"warnings,omitempty"`
	Note           string                `json:"note,omitempty"`
}

// CodeContext is the compact, validated view of a reference implementation.
// It is intentionally separate from the raw frontmatter so callers can see
// exactly what was resolved without parsing provider-specific data.
type CodeContext struct {
	Repo         string `json:"repo,omitempty"`
	Path         string `json:"path"`
	Symbol       string `json:"symbol,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Ref          string `json:"ref,omitempty"`
	Commit       string `json:"commit,omitempty"`
	PathStatus   string `json:"path_status,omitempty"`
	SymbolStatus string `json:"symbol_status,omitempty"`
	DriftStatus  string `json:"drift_status,omitempty"`
	Provider     string `json:"provider,omitempty"`
	ResolvedPath string `json:"resolved_path,omitempty"`
	Line         int    `json:"line,omitempty"`
	Qualified    string `json:"qualified,omitempty"`
	Signature    string `json:"signature,omitempty"`
	Reason       string `json:"reason,omitempty"`
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
	docs, err := s.WalkDocs("")
	if err != nil {
		return nil, err
	}
	return LookupDocs(docs, ctx, task, language, maxResults)
}

// LookupDocs ranks an already-loaded document set. Callers that have an active
// changeset should pass the overlay documents so pending guidance is visible.
func LookupDocs(docs []*store.Doc, ctx *config.Context, task, language string, maxResults int) ([]Result, error) {
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

	scopes := scopesFor(ctx)
	var candidates []candidate
	for _, d := range docs {
		if d == nil {
			continue
		}
		if d.Frontmatter.Kind != "pattern" && d.Frontmatter.Kind != "adr" && d.Frontmatter.Kind != "standard" {
			continue
		}
		sc, ok := scopeForPath(d.Path, scopes)
		if !ok {
			continue
		}
		score := scoreDoc(d, tokens, lang)
		candidates = append(candidates, candidate{doc: d, scope: sc.path, scopeRank: sc.rank, score: float64(score)})
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
			LastReviewed:   f.LastReviewed,
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

type scopeDef struct {
	path string
	rank int
}

func scopesFor(ctx *config.Context) []scopeDef {
	var scopes []scopeDef
	seen := map[string]bool{}
	add := func(p string, rank int) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		scopes = append(scopes, scopeDef{path: p, rank: rank})
	}
	if ctx != nil {
		add(ctx.ProjectArea(), 0)
		add(ctx.TeamArea(), 1)
	}
	add("org", 2)
	return scopes
}

func scopeForPath(p string, scopes []scopeDef) (scopeDef, bool) {
	for _, sc := range scopes {
		if p == sc.path || strings.HasPrefix(p, sc.path+"/") {
			return sc, true
		}
	}
	return scopeDef{}, false
}

// Enrich attaches validated code context to guidance results. It is a no-op
// when the results have no reference implementations. Missing code roots and
// validation errors become per-result warnings rather than failing guidance.
func Enrich(ctx context.Context, results []Result, root, project string) {
	docs := make([]*store.Doc, 0, len(results))
	byPath := map[string]int{}
	for i := range results {
		if len(results[i].ReferenceImpls) == 0 {
			continue
		}
		if strings.TrimSpace(root) == "" {
			results[i].Warnings = append(results[i].Warnings, "code context unavailable: no code repository detected")
			continue
		}
		docs = append(docs, &store.Doc{
			Path: results[i].Path,
			Frontmatter: store.Frontmatter{
				ReferenceImpls: results[i].ReferenceImpls,
				LastReviewed:   results[i].LastReviewed,
			},
		})
		byPath[results[i].Path] = i
	}
	if len(docs) == 0 {
		return
	}
	validated, err := codeindex.ValidateDocs(ctx, root, project, docs)
	if err != nil {
		for i := range results {
			if len(results[i].ReferenceImpls) > 0 {
				results[i].Warnings = append(results[i].Warnings, "code context unavailable: "+err.Error())
			}
		}
		return
	}
	applyCodeContext(results, byPath, validated)
}

func applyCodeContext(results []Result, byPath map[string]int, validated []codeindex.DocValidation) {
	for _, d := range validated {
		i, ok := byPath[d.Path]
		if !ok {
			continue
		}
		for _, ref := range d.References {
			cc := CodeContext{
				Repo: ref.Repo, Path: ref.Path, Symbol: ref.Symbol, Kind: ref.Kind,
				Ref: ref.Ref, Commit: ref.Commit, PathStatus: ref.PathStatus,
				SymbolStatus: ref.SymbolStatus, DriftStatus: ref.DriftStatus,
				Provider: ref.Provider, ResolvedPath: ref.ResolvedPath,
				Line: ref.ResolvedLine, Qualified: ref.ResolvedQualified,
				Signature: ref.ResolvedSignature, Reason: ref.Reason,
			}
			results[i].CodeContext = append(results[i].CodeContext, cc)
			switch {
			case ref.PathStatus == codeindex.PathMissing:
				results[i].Warnings = append(results[i].Warnings, fmt.Sprintf("missing code reference path %s", ref.Path))
			case ref.SymbolStatus == codeindex.SymbolMissing:
				results[i].Warnings = append(results[i].Warnings, fmt.Sprintf("missing code symbol %s", ref.Symbol))
			case ref.DriftStatus == codeindex.DriftStale:
				results[i].Warnings = append(results[i].Warnings, fmt.Sprintf("stale code reference %s: %s", ref.Path, ref.DriftReason))
			}
		}
	}
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
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "   warning: %s\n", w)
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
		for _, cc := range r.CodeContext {
			loc := cc.Path
			if cc.ResolvedPath != "" {
				loc = cc.ResolvedPath
			}
			if cc.Line > 0 {
				loc = fmt.Sprintf("%s:%d", loc, cc.Line)
			}
			if cc.Qualified != "" {
				loc += " (" + cc.Qualified + ")"
			}
			fmt.Fprintf(&b, "   code: %s [%s] path=%s symbol=%s drift=%s\n", loc, cc.Provider, cc.PathStatus, cc.SymbolStatus, cc.DriftStatus)
			if cc.Signature != "" {
				fmt.Fprintf(&b, "         %s\n", cc.Signature)
			}
		}
	}
	return b.String()
}
