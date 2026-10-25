package store

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ReferenceImpl struct {
	Repo        string `yaml:"repo" json:"repo"`
	Path        string `yaml:"path" json:"path"`
	Symbol      string `yaml:"symbol,omitempty" json:"symbol,omitempty"`
	Kind        string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Ref         string `yaml:"ref,omitempty" json:"ref,omitempty"`
	Commit      string `yaml:"commit,omitempty" json:"commit,omitempty"`
	Fingerprint string `yaml:"fingerprint,omitempty" json:"fingerprint,omitempty"`
}

type Frontmatter struct {
	ID             string          `yaml:"id" json:"id"`
	Kind           string          `yaml:"kind" json:"kind"`
	Title          string          `yaml:"title" json:"title"`
	Status         string          `yaml:"status" json:"status"`
	SupersededBy   string          `yaml:"superseded_by" json:"superseded_by,omitempty"`
	Overrides      string          `yaml:"overrides" json:"overrides,omitempty"`
	Topics         []string        `yaml:"topics" json:"topics,omitempty"`
	Languages      []string        `yaml:"languages" json:"languages,omitempty"`
	Related        []string        `yaml:"related" json:"related,omitempty"`
	ReferenceImpls []ReferenceImpl `yaml:"reference_impls" json:"reference_impls,omitempty"`
	Owners         []string        `yaml:"owners" json:"owners,omitempty"`
	LastReviewed   string          `yaml:"last_reviewed" json:"last_reviewed,omitempty"`
}

type Doc struct {
	Path        string      `json:"path"`
	Frontmatter Frontmatter `json:"frontmatter"`
	Body        string      `json:"body"`
	BodyLine    int         `json:"-"` // number of lines before Body in Raw
	Raw         string      `json:"-"`
	ModTime     int64       `json:"mod_time,omitempty"`
}

type parseFrontmatter struct {
	ID             string          `yaml:"id"`
	Kind           string          `yaml:"kind"`
	Title          string          `yaml:"title"`
	Status         string          `yaml:"status"`
	SupersededBy   any             `yaml:"superseded_by"`
	Overrides      any             `yaml:"overrides"`
	Topics         []string        `yaml:"topics"`
	Languages      []string        `yaml:"languages"`
	Related        []string        `yaml:"related"`
	ReferenceImpls []ReferenceImpl `yaml:"reference_impls"`
	Owners         []string        `yaml:"owners"`
	LastReviewed   any             `yaml:"last_reviewed"`
}

func asString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case time.Time:
		return x.Format("2006-01-02")
	case fmt.Stringer:
		return strings.TrimSpace(x.String())
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

// ParseDoc parses YAML frontmatter and a markdown body.
func ParseDoc(rel, raw string) (*Doc, error) {
	d := &Doc{Path: rel, Raw: raw}
	fmText, body, bodyLine, hasFrontmatter, err := splitFrontmatter(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", rel, err)
	}
	d.Body = body
	d.BodyLine = bodyLine
	if !hasFrontmatter {
		return d, nil
	}
	var p parseFrontmatter
	if err := yaml.Unmarshal([]byte(fmText), &p); err != nil {
		return nil, fmt.Errorf("%s: invalid frontmatter: %w", rel, err)
	}
	d.Frontmatter = Frontmatter{
		ID:             p.ID,
		Kind:           p.Kind,
		Title:          p.Title,
		Status:         p.Status,
		SupersededBy:   asString(p.SupersededBy),
		Overrides:      asString(p.Overrides),
		Topics:         p.Topics,
		Languages:      p.Languages,
		Related:        p.Related,
		ReferenceImpls: p.ReferenceImpls,
		Owners:         p.Owners,
		LastReviewed:   asString(p.LastReviewed),
	}
	return d, nil
}

// splitFrontmatter finds a leading YAML frontmatter block using whole-line
// fences. Matching arbitrary "\n---" text is not sufficient: a "---" line
// inside a YAML block scalar is content, not the end of the document. The
// returned bodyLine is the number of lines that precede Body, so search can
// report file-relative line numbers.
func splitFrontmatter(raw string) (fm, body string, bodyLine int, has bool, err error) {
	raw = strings.TrimPrefix(raw, "\ufeff")
	lines := strings.SplitAfter(raw, "\n")

	start := 0
	for start < len(lines) && strings.TrimSpace(trimEOL(lines[start])) == "" {
		start++
	}
	if start == len(lines) || trimEOL(lines[start]) != "---" {
		return "", raw, 0, false, nil
	}

	var b strings.Builder
	for i := start + 1; i < len(lines); i++ {
		if line := trimEOL(lines[i]); line == "---" || line == "..." {
			return b.String(), strings.Join(lines[i+1:], ""), i + 1, true, nil
		}
		b.WriteString(lines[i])
	}
	return "", "", 0, false, errors.New("unclosed frontmatter fence")
}

func trimEOL(line string) string {
	line = strings.TrimSuffix(line, "\n")
	return strings.TrimSuffix(line, "\r")
}

func (s *Store) LoadDoc(rel string) (*Doc, error) {
	raw, err := s.Read(rel)
	if err != nil {
		return nil, err
	}
	d, err := ParseDoc(rel, raw)
	if err != nil {
		return nil, err
	}
	if full, err := s.Abs(rel); err == nil {
		if fi, err := os.Stat(full); err == nil {
			d.ModTime = fi.ModTime().Unix()
		}
	}
	return d, nil
}

// WalkDocs returns all markdown documents under dir. Hidden metadata
// directories are skipped.
func (s *Store) WalkDocs(dir string) ([]*Doc, error) {
	return WalkDocsAt(s.Mirror, dir)
}

// WalkDocsAt walks markdown documents under a store root, such as the main
// mirror or a changeset worktree. Paths in the result are relative to root.
func WalkDocsAt(root, dir string) ([]*Doc, error) {
	root = filepath.Clean(root)
	walkRoot := root
	if dir != "" {
		clean, err := CleanRel(dir)
		if err != nil {
			return nil, err
		}
		walkRoot = filepath.Join(root, filepath.FromSlash(clean))
	}
	if _, err := os.Stat(walkRoot); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	var out []*Doc
	err := filepath.WalkDir(walkRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// Do not follow symlinks. A tracked symlink could otherwise expose a
		// file outside the store to list/search/context and overlay reads.
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == ".companion" || name == ".github" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(name), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		doc, err := ParseDoc(rel, string(raw))
		if err != nil {
			// A malformed doc should not hide all other docs from listing.
			doc = &Doc{Path: rel}
		} else if fi, err := d.Info(); err == nil {
			doc.ModTime = fi.ModTime().Unix()
		}
		out = append(out, doc)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

type SearchHit struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Text   string `json:"text"`
	Source string `json:"source,omitempty"`
	State  string `json:"state,omitempty"`
}

func (s *Store) Search(dir, query string, limit int) ([]SearchHit, error) {
	docs, err := s.WalkDocs(dir)
	if err != nil {
		return nil, err
	}
	return SearchDocs(docs, query, limit)
}

// SearchDocs performs the current substring search over an already-loaded set
// of documents, including documents from a changeset overlay.
func SearchDocs(docs []*Doc, query string, limit int) ([]SearchHit, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil, errors.New("search query is required")
	}
	if limit <= 0 {
		limit = 50
	}
	var hits []SearchHit
	for _, d := range docs {
		if strings.Contains(strings.ToLower(d.Path), query) {
			hits = append(hits, SearchHit{Path: d.Path, Line: 0, Text: d.Frontmatter.Title})
		}
		for i, line := range strings.Split(d.Raw, "\n") {
			if strings.Contains(strings.ToLower(line), query) {
				hits = append(hits, SearchHit{Path: d.Path, Line: i + 1, Text: strings.TrimSpace(line)})
				if len(hits) >= limit {
					return hits, nil
				}
			}
		}
	}
	return hits, nil
}

// RelativeToArea turns an area-relative or store-relative path into a
// store-relative path.
func RelativeToArea(area, p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	p = strings.TrimPrefix(p, "./")
	if p == "" {
		return area
	}
	clean := path.Clean(p)
	if clean == area || strings.HasPrefix(clean, area+"/") {
		return clean
	}
	if strings.HasPrefix(clean, "org/") || strings.HasPrefix(clean, "teams/") || strings.HasPrefix(clean, "projects/") {
		return clean
	}
	return path.Join(area, clean)
}
