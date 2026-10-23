// Package lint implements the local subset of the store CI checks.
package lint

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/bolacha/docsync/internal/docrules"
	"github.com/bolacha/docsync/internal/store"
)

type Issue struct {
	Level   string `json:"level"` // error | warning
	Path    string `json:"path"`
	Message string `json:"message"`
}

type Result struct {
	Issues []Issue `json:"issues"`
	Docs   int     `json:"docs"`
}

var (
	linkRE   = regexp.MustCompile(`\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	secretRE = regexp.MustCompile(`(?i)(aws_secret_access_key\s*=|-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----|ghp_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{20,}|xox[baprs]-[A-Za-z0-9-]{10,})`)
)

func Run(s *store.Store) (*Result, error) {
	return RunDir(s, "")
}

// RunDir lints a subtree of the store.
func RunDir(s *store.Store, dir string) (*Result, error) {
	docs, err := s.WalkDocs(dir)
	if err != nil {
		return nil, err
	}
	return runDocs(s, docs), nil
}

// RunFiles lints a specific set of store-relative files. Missing files are
// skipped so deleted documents do not fail a submit preflight.
func RunFiles(root string, rels []string) (*Result, error) {
	s := &store.Store{Mirror: root}
	seen := map[string]bool{}
	var docs []*store.Doc
	for _, rel := range rels {
		clean, err := store.CleanRel(rel)
		if err != nil {
			return nil, err
		}
		if seen[clean] || !docrules.IsMarkdown(clean) {
			continue
		}
		seen[clean] = true
		raw, err := s.Read(clean)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		d, err := store.ParseDoc(clean, raw)
		if err != nil {
			return &Result{Docs: 1, Issues: []Issue{{Level: "error", Path: clean, Message: err.Error()}}}, nil
		}
		docs = append(docs, d)
	}
	return runDocs(s, docs), nil
}

func runDocs(s *store.Store, docs []*store.Doc) *Result {
	res := &Result{Docs: len(docs)}
	ids := map[string]string{}    // id -> path
	scoped := map[string]string{} // scope+id -> path
	overrides := map[string]string{}
	for _, d := range docs {
		f := d.Frontmatter
		for _, p := range docrules.ValidateDoc(d.Path, f) {
			res.add("error", d.Path, p.Message)
		}
		scope := docrules.ScopeOf(d.Path)
		if f.ID != "" {
			key := scope + "\x00" + f.ID
			if prev, ok := scoped[key]; ok {
				res.add("error", d.Path, fmt.Sprintf("duplicate id %q in %s (also %s)", f.ID, scope, prev))
			} else {
				scoped[key] = d.Path
			}
			ids[f.ID] = d.Path
		}
		if f.Overrides != "" {
			overrides[f.Overrides] = d.Path
		}
		if f.LastReviewed != "" {
			if t, err := time.Parse("2006-01-02", f.LastReviewed); err == nil && time.Since(t) > 180*24*time.Hour {
				res.add("warning", d.Path, "last_reviewed is older than 6 months")
			}
		}
		for _, r := range f.ReferenceImpls {
			if r.Commit != "" && r.Ref != "" && r.Ref != "main" {
				res.add("warning", d.Path, "reference impl pins a feature-branch ref; prefer ref: main")
			}
		}
		checkLinks(s, d, res)
		if secretRE.MatchString(d.Raw) {
			res.add("error", d.Path, "possible secret detected")
		}
	}
	for target, p := range overrides {
		if ids[target] == "" {
			res.add("error", p, fmt.Sprintf("overrides target %q does not exist", target))
		}
	}
	return res
}

func (r *Result) add(level, p, msg string) {
	r.Issues = append(r.Issues, Issue{Level: level, Path: p, Message: msg})
}

func checkLinks(s *store.Store, d *store.Doc, res *Result) {
	for _, m := range linkRE.FindAllStringSubmatch(d.Raw, -1) {
		target := strings.TrimSpace(m[1])
		if target == "" || strings.HasPrefix(target, "#") ||
			strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
			strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "tel:") {
			continue
		}
		if i := strings.IndexAny(target, "#?"); i >= 0 {
			target = target[:i]
		}
		if target == "" {
			continue
		}
		var rel string
		if strings.HasPrefix(target, "/") {
			rel = strings.TrimPrefix(target, "/")
		} else {
			rel = path.Join(path.Dir(d.Path), target)
		}
		if strings.Contains(rel, "{{") || strings.Contains(rel, "<") {
			continue
		}
		if !s.ExistsPath(rel) {
			// Also accept links relative to the repository root.
			if !s.ExistsPath(strings.TrimPrefix(target, "/")) {
				res.add("error", d.Path, fmt.Sprintf("broken internal link %q", target))
			}
		}
	}
}

// Format renders issues for humans.
func Format(r *Result) string {
	if len(r.Issues) == 0 {
		return fmt.Sprintf("ok: %d documents, no issues\n", r.Docs)
	}
	var b strings.Builder
	for _, i := range r.Issues {
		fmt.Fprintf(&b, "%-7s %s: %s\n", strings.ToUpper(i.Level), i.Path, i.Message)
	}
	errors, warnings := 0, 0
	for _, i := range r.Issues {
		if i.Level == "error" {
			errors++
		} else {
			warnings++
		}
	}
	fmt.Fprintf(&b, "\n%d documents, %d errors, %d warnings\n", r.Docs, errors, warnings)
	return b.String()
}
