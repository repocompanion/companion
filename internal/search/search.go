// Package search implements a small, dependency-free BM25 index over the
// documents visible to companion. It is intentionally in-memory: the active
// changeset overlay is cheap to materialize, and no SQLite cache is needed yet.
package search

import (
	"errors"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/repocompanion/companion/internal/store"
)

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

type Result struct {
	Path         string   `json:"path"`
	ID           string   `json:"id,omitempty"`
	Title        string   `json:"title,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	Status       string   `json:"status,omitempty"`
	Score        float64  `json:"score"`
	Snippet      string   `json:"snippet,omitempty"`
	Line         int      `json:"line,omitempty"`
	MatchedTerms []string `json:"matched_terms,omitempty"`
	Why          string   `json:"why,omitempty"`
	Source       string   `json:"source,omitempty"`
	State        string   `json:"state,omitempty"`
}

type fieldDoc struct {
	counts map[string]int
}

type indexedDoc struct {
	doc    *store.Doc
	fields map[string]fieldDoc
	length int
}

type Index struct {
	docs   []indexedDoc
	df     map[string]int
	avgLen float64
}

var fieldWeights = map[string]float64{
	"title":    6,
	"topics":   5,
	"headings": 3,
	"body":     1,
	"owners":   2,
	"refs":     1.5,
}

var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "do": true, "does": true, "for": true, "from": true,
	"how": true, "in": true, "is": true, "it": true, "of": true, "on": true,
	"or": true, "our": true, "that": true, "the": true, "this": true, "to": true,
	"we": true, "what": true, "when": true, "where": true, "with": true,
}

// New builds an index over the supplied documents.
func New(docs []*store.Doc) *Index {
	idx := &Index{df: map[string]int{}}
	var totalLen int
	for _, d := range docs {
		if d == nil {
			continue
		}
		fields := map[string]fieldDoc{}
		docLen := 0
		for name := range fieldWeights {
			text := fieldText(d, name)
			tokens := tokenizeAll(text)
			counts := map[string]int{}
			for _, t := range tokens {
				counts[t]++
			}
			fields[name] = fieldDoc{counts: counts}
			docLen += len(tokens)
		}
		totalLen += docLen
		idx.docs = append(idx.docs, indexedDoc{doc: d, fields: fields, length: docLen})
		seen := map[string]bool{}
		for _, f := range fields {
			for term := range f.counts {
				if !seen[term] {
					idx.df[term]++
					seen[term] = true
				}
			}
		}
	}
	if len(idx.docs) > 0 {
		idx.avgLen = float64(totalLen) / float64(len(idx.docs))
	}
	return idx
}

// Search ranks docs for query and returns at most limit results.
func (idx *Index) Search(query string, limit int) ([]Result, error) {
	terms := Tokenize(query)
	if len(terms) == 0 {
		return nil, errors.New("search query is required")
	}
	if limit <= 0 {
		limit = 50
	}
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	var results []Result
	for _, d := range idx.docs {
		score, matched, matchedFields := idx.score(d, terms)
		if score <= 0 {
			continue
		}
		title := strings.ToLower(d.doc.Frontmatter.Title)
		if lowerQuery != "" && strings.Contains(title, lowerQuery) {
			score += 6
		}
		if lowerQuery != "" && strings.Contains(strings.ToLower(d.doc.Body), lowerQuery) {
			score += 1.5
		}
		switch d.doc.Frontmatter.Status {
		case "approved":
			score *= 1.05
		case "deprecated", "superseded":
			score *= 0.35
		}
		snippet, line := bestSnippet(d.doc, terms)
		why := "matched"
		if len(matchedFields) > 0 {
			why += " in " + strings.Join(matchedFields, ", ")
		}
		results = append(results, Result{
			Path: d.doc.Path, ID: d.doc.Frontmatter.ID, Title: d.doc.Frontmatter.Title,
			Kind: d.doc.Frontmatter.Kind, Status: d.doc.Frontmatter.Status,
			Score: math.Round(score*100) / 100, Snippet: snippet, Line: line,
			MatchedTerms: matched, Why: why,
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Path < results[j].Path
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// Search is a convenience for one-shot queries over a document set.
func Search(docs []*store.Doc, query string, limit int) ([]Result, error) {
	return New(docs).Search(query, limit)
}

func (idx *Index) score(d indexedDoc, terms []string) (float64, []string, []string) {
	var score float64
	matched := make([]string, 0, len(terms))
	fieldSet := map[string]bool{}
	for _, term := range terms {
		df := idx.df[term]
		if df == 0 {
			continue
		}
		idf := math.Log(1 + (float64(len(idx.docs)-df)+0.5)/(float64(df)+0.5))
		weightedTF := 0.0
		for name, f := range d.fields {
			if tf := f.counts[term]; tf > 0 {
				weightedTF += float64(tf) * fieldWeights[name]
				fieldSet[name] = true
			}
		}
		if weightedTF == 0 {
			continue
		}
		lengthNorm := 1.0
		if idx.avgLen > 0 {
			lengthNorm = 1 - bm25B + bm25B*float64(d.length)/idx.avgLen
		}
		score += idf * (weightedTF * (bm25K1 + 1)) / (weightedTF + bm25K1*lengthNorm)
		matched = append(matched, term)
	}
	return score, matched, sortedFields(fieldSet)
}

func fieldText(d *store.Doc, field string) string {
	f := d.Frontmatter
	switch field {
	case "title":
		return f.Title + " " + f.ID + " " + d.Path
	case "topics":
		return strings.Join(f.Topics, " ")
	case "headings":
		return strings.Join(headings(d.Body), " ")
	case "body":
		return d.Body
	case "owners":
		return strings.Join(f.Owners, " ")
	case "refs":
		var b strings.Builder
		for _, r := range f.ReferenceImpls {
			b.WriteString(r.Repo)
			b.WriteByte(' ')
			b.WriteString(r.Path)
			b.WriteByte(' ')
			b.WriteString(r.Ref)
			b.WriteByte(' ')
			b.WriteString(r.Commit)
			b.WriteByte(' ')
		}
		return b.String()
	default:
		return ""
	}
}

func headings(body string) []string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// Tokenize lowercases and splits text into unique, searchable terms. Queries
// use this form so a repeated word does not weight the query more heavily.
func Tokenize(s string) []string {
	tokens := tokenizeAll(s)
	seen := make(map[string]bool, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

// tokenizeAll is the indexing form: it keeps every occurrence so term
// frequency and document length are measured correctly by BM25.
func tokenizeAll(s string) []string {
	var out []string
	for _, word := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(word) < 2 || stopwords[word] {
			continue
		}
		out = append(out, word)
	}
	return out
}

func bestSnippet(d *store.Doc, terms []string) (string, int) {
	best, bestLine, bestHits := strings.TrimSpace(d.Frontmatter.Title), 0, 0
	for i, line := range strings.Split(d.Body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		hits := 0
		lower := strings.ToLower(line)
		for _, t := range terms {
			if strings.Contains(lower, t) {
				hits++
			}
		}
		if hits > bestHits {
			best, bestLine, bestHits = line, i+1, hits
		}
	}
	if runes := []rune(best); len(runes) > 240 {
		best = string(runes[:237]) + "..."
	}
	return best, bestLine
}

func sortedFields(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
