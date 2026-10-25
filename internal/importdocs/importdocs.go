// Package importdocs scans legacy Markdown trees and proposes canonical
// companion destinations. It never writes to the store; the CLI decides whether
// to apply a report.
package importdocs

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/repocompanion/companion/internal/docrules"
	"github.com/repocompanion/companion/internal/gitx"
	"github.com/repocompanion/companion/internal/store"
	"gopkg.in/yaml.v3"
)

const (
	StatusReady      = "ready"
	StatusUnresolved = "unresolved"
	StatusCollision  = "collision"

	// maxImportFileSize bounds a single legacy document. Imports are an
	// explicit local action, but an unbounded read of an untrusted tree should
	// not be able to exhaust memory.
	maxImportFileSize = 2 << 20
)

type Options struct {
	Owners       []string
	Status       string
	LastReviewed string
	Mapping      map[string]string
	Existing     map[string]bool
	ExistingIDs  map[string]string
	Now          time.Time
}

type Entry struct {
	Source               string   `json:"source"`
	Destination          string   `json:"destination,omitempty"`
	Kind                 string   `json:"kind,omitempty"`
	ID                   string   `json:"id,omitempty"`
	Title                string   `json:"title,omitempty"`
	Confidence           string   `json:"confidence,omitempty"`
	Status               string   `json:"status"`
	Reason               string   `json:"reason,omitempty"`
	Notes                []string `json:"notes,omitempty"`
	FrontmatterGenerated bool     `json:"frontmatter_generated,omitempty"`
	Content              string   `json:"-"`
}

type Report struct {
	Source     string  `json:"source"`
	Area       string  `json:"area"`
	Entries    []Entry `json:"entries"`
	Ready      int     `json:"ready"`
	Unresolved int     `json:"unresolved"`
	Collisions int     `json:"collisions"`
}

var adrName = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})-(.+)\.md$`)

// Scan walks a Markdown tree and returns a deterministic migration report.
func Scan(source, area string, opts Options) (*Report, error) {
	if err := docrules.ValidateArea(area); err != nil {
		return nil, err
	}
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("--from must be a directory: %s", source)
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	absSource, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	report := &Report{Source: source, Area: strings.Trim(area, "/")}
	err = filepath.WalkDir(absSource, func(p string, d os.DirEntry, err error) error {
		if err != nil {
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
		rel, err := filepath.Rel(absSource, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 {
			report.Entries = append(report.Entries, Entry{
				Source: rel, Status: StatusUnresolved, Reason: "symlinks are not imported",
			})
			return nil
		}
		if info, err := d.Info(); err == nil && info.Size() > maxImportFileSize {
			report.Entries = append(report.Entries, Entry{
				Source: rel, Status: StatusUnresolved,
				Reason: fmt.Sprintf("file exceeds the %d-byte import limit", maxImportFileSize),
			})
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			report.Entries = append(report.Entries, Entry{Source: rel, Status: StatusUnresolved, Reason: err.Error()})
			return nil
		}
		e := classify(rel, string(raw), report.Area, opts, opts.Mapping[rel])
		report.Entries = append(report.Entries, e)
		return nil
	})
	if err != nil {
		return nil, err
	}
	finalize(report, opts)
	return report, nil
}

func classify(rel, raw, area string, opts Options, mapped string) Entry {
	e := Entry{Source: rel, Status: StatusUnresolved}
	doc, err := store.ParseDoc(rel, raw)
	if err != nil {
		e.Reason = "invalid frontmatter: " + err.Error()
		return e
	}
	f := doc.Frontmatter
	e.ID = strings.TrimSpace(f.ID)
	e.Title = strings.TrimSpace(f.Title)
	e.Kind = f.Kind
	if !docrules.ValidKinds[e.Kind] {
		e.Kind = ""
	}

	base := path.Base(rel)
	dir := path.Dir(rel)
	lower := strings.ToLower(rel)
	if mapped != "" {
		mapped = strings.Trim(strings.ReplaceAll(mapped, "\\", "/"), "/")
		if mapped != area && !strings.HasPrefix(mapped, area+"/") {
			e.Reason = fmt.Sprintf("mapped destination %s is outside area %s", mapped, area)
			return e
		}
		if k := kindForDestination(mapped, area); k != "" {
			e.Kind, e.Reason = k, "explicit --map"
		} else {
			e.Reason = fmt.Sprintf("cannot infer a kind from mapped destination %s", mapped)
			return e
		}
	} else if e.Kind == "" {
		e.Kind, e.Reason = detectKind(rel, base, dir, lower, doc.Body)
	} else {
		e.Reason = "frontmatter kind"
	}
	if e.Kind == "" {
		return e
	}
	if e.Title == "" {
		e.Title = titleFromPath(base)
	}
	if e.ID == "" {
		e.ID = gitx.Slug(strings.TrimSuffix(base, path.Ext(base)))
	}

	dest, reason, note := destinationFor(e.Kind, rel, base, dir, e.ID, area)
	if mapped != "" {
		mapped = strings.Trim(strings.ReplaceAll(mapped, "\\", "/"), "/")
		if mapped != area && !strings.HasPrefix(mapped, area+"/") {
			e.Reason = fmt.Sprintf("mapped destination %s is outside area %s", mapped, area)
			return e
		}
		if k := kindForDestination(mapped, area); k != "" {
			e.Kind = k
		} else {
			e.Reason = fmt.Sprintf("cannot infer a kind from mapped destination %s", mapped)
			return e
		}
		dest, reason = mapped, "explicit --map"
	}
	if note != "" {
		e.Notes = append(e.Notes, note)
	}
	if dest == "" {
		e.Reason = reason
		return e
	}
	e.Destination = dest
	if e.Reason == "" || reason == "explicit --map" {
		e.Reason = reason
	}
	if e.Confidence == "" {
		e.Confidence = confidenceFor(e.Reason)
	}
	if e.ID == "" {
		e.Reason = "cannot derive a stable id"
		e.Destination = ""
		return e
	}
	content, generated, err := canonicalContent(doc, e, opts)
	if err != nil {
		e.Reason = err.Error()
		e.Destination = ""
		return e
	}
	e.Content = content
	e.FrontmatterGenerated = generated
	if problems, err := docrules.ValidateMarkdown(e.Destination, content); err != nil {
		e.Reason = err.Error()
		e.Destination = ""
		return e
	} else if len(problems) > 0 {
		e.Reason = problems[0].Message
		e.Destination = ""
		return e
	}
	e.Status = StatusReady
	return e
}

func finalize(report *Report, opts Options) {
	destCounts := map[string]int{}
	idCounts := map[string]int{}
	for _, e := range report.Entries {
		if e.Destination != "" {
			destCounts[e.Destination]++
		}
		if e.ID != "" {
			idCounts[e.ID]++
		}
	}
	for i := range report.Entries {
		e := &report.Entries[i]
		if e.Status == StatusReady && opts.Existing[e.Destination] {
			e.Status = StatusCollision
			e.Reason = "destination already exists"
			e.Notes = append(e.Notes, "use --map to choose another destination or remove the existing document")
		}
		if e.Status == StatusReady && opts.ExistingIDs[e.ID] != "" && opts.ExistingIDs[e.ID] != e.Destination {
			e.Status = StatusCollision
			e.Reason = fmt.Sprintf("id %q already exists at %s", e.ID, opts.ExistingIDs[e.ID])
		}
		if e.Status == StatusReady && destCounts[e.Destination] > 1 {
			e.Status = StatusCollision
			e.Reason = "multiple source files map to the same destination"
		}
		if e.Status == StatusReady && idCounts[e.ID] > 1 {
			e.Status = StatusCollision
			e.Reason = "multiple source files use the same id"
		}
		switch e.Status {
		case StatusReady:
			report.Ready++
		case StatusCollision:
			report.Collisions++
		default:
			e.Status = StatusUnresolved
			report.Unresolved++
		}
	}
	sort.SliceStable(report.Entries, func(i, j int) bool { return report.Entries[i].Source < report.Entries[j].Source })
}

func detectKind(rel, base, dir, lower, body string) (string, string) {
	if base == "decisions.md" || base == "operations.md" {
		return "", "aggregate files must be split or mapped explicitly"
	}
	if strings.EqualFold(base, "README.md") {
		if dir == "." {
			return docrules.KindProjectDoc, "root README"
		}
		return "", "nested README is not an area-level project-doc"
	}
	if strings.EqualFold(base, "architecture.md") {
		if dir == "." {
			return docrules.KindProjectDoc, "architecture project-doc"
		}
		return "", "nested architecture file is not an area-level project-doc"
	}
	if hasSeg(lower, "adr") || hasSeg(lower, "decisions") || adrName.MatchString(base) {
		return docrules.KindADR, "ADR path or date-prefixed filename"
	}
	if hasSeg(lower, "flows") || hasSeg(lower, "flow") {
		return docrules.KindFlow, "flow path"
	}
	if hasSeg(lower, "runbooks") || hasSeg(lower, "operations") || hasSeg(lower, "playbooks") {
		return docrules.KindRunbook, "runbook path"
	}
	if hasSeg(lower, "patterns") {
		return docrules.KindPattern, "pattern path"
	}
	if hasSeg(lower, "standards") || hasSeg(lower, "conventions") {
		return docrules.KindStandard, "standard path"
	}
	if k := headingKind(body); k != "" {
		return k, "document headings"
	}
	_ = rel
	return "", "no reliable kind signal"
}

func hasSeg(p, seg string) bool {
	for _, part := range strings.Split(p, "/") {
		if part == seg {
			return true
		}
	}
	return false
}

func headingKind(body string) string {
	score := map[string]int{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.ToLower(strings.TrimSpace(strings.TrimLeft(line, "#")))
		switch {
		case strings.Contains(line, "context") || strings.Contains(line, "consequences"):
			score[docrules.KindADR]++
		case strings.Contains(line, "trigger") || strings.Contains(line, "failure modes"):
			score[docrules.KindFlow]++
		case strings.Contains(line, "symptoms") || strings.Contains(line, "mitigation") || strings.Contains(line, "escalation"):
			score[docrules.KindRunbook]++
		case strings.Contains(line, "when to use") || strings.Contains(line, "reference implementations") || strings.Contains(line, "pitfalls"):
			score[docrules.KindPattern]++
		case strings.Contains(line, "rationale") || strings.Contains(line, "exceptions"):
			score[docrules.KindStandard]++
		}
	}
	best, bestScore := "", 0
	for kind, n := range score {
		if n > bestScore {
			best, bestScore = kind, n
		}
	}
	if bestScore >= 2 {
		return best
	}
	return ""
}

func destinationFor(kind, rel, base, dir, id, area string) (dest, reason, note string) {
	switch kind {
	case docrules.KindProjectDoc:
		if strings.EqualFold(base, "README.md") {
			return path.Join(area, "README.md"), "area README", ""
		}
		if strings.EqualFold(base, "architecture.md") {
			return path.Join(area, "architecture.md"), "area architecture", ""
		}
		return "", "project-doc must be README.md or architecture.md", ""
	case docrules.KindADR:
		m := adrName.FindStringSubmatch(base)
		if len(m) != 3 {
			return "", "ADR filename must start with YYYY-MM-DD-", "supply a destination with --map if the decision date is not in the filename"
		}
		slug := gitx.Slug(m[2])
		return path.Join(area, "adr", m[1]+"-"+slug+".md"), "dated ADR filename", ""
	case docrules.KindFlow, docrules.KindRunbook, docrules.KindStandard:
		slug := gitx.Slug(strings.TrimSuffix(base, path.Ext(base)))
		sub := map[string]string{docrules.KindFlow: "flows", docrules.KindRunbook: "runbooks", docrules.KindStandard: "standards"}[kind]
		return path.Join(area, sub, slug+".md"), "kind and filename", ""
	case docrules.KindPattern:
		slug := gitx.Slug(strings.TrimSuffix(base, path.Ext(base)))
		lower := strings.ToLower(rel)
		if strings.HasPrefix(lower, "patterns/") {
			rest := rel[len("patterns/"):]
			category := path.Dir(rest)
			if category != "." && category != "" {
				return path.Join(area, "patterns", category, slug+".md"), "pattern path with category", ""
			}
		} else if i := strings.Index(lower, "/patterns/"); i >= 0 {
			rest := rel[i+len("/patterns/"):]
			category := path.Dir(rest)
			if category != "." && category != "" {
				return path.Join(area, "patterns", category, slug+".md"), "pattern path with category", ""
			}
		}
		return path.Join(area, "patterns", slug+".md"), "pattern filename", ""
	}
	return "", "unknown kind", ""
}

func kindForDestination(dest, area string) string {
	rel := strings.TrimPrefix(strings.TrimPrefix(dest, area), "/")
	switch {
	case rel == "README.md" || rel == "architecture.md":
		return docrules.KindProjectDoc
	case strings.HasPrefix(rel, "adr/"):
		return docrules.KindADR
	case strings.HasPrefix(rel, "flows/"):
		return docrules.KindFlow
	case strings.HasPrefix(rel, "runbooks/"):
		return docrules.KindRunbook
	case strings.HasPrefix(rel, "patterns/"):
		return docrules.KindPattern
	case strings.HasPrefix(rel, "standards/"):
		return docrules.KindStandard
	default:
		return ""
	}
}

func confidenceFor(reason string) string {
	switch reason {
	case "frontmatter kind", "root README", "area README", "architecture project-doc":
		return "high"
	case "ADR path or date-prefixed filename", "flow path", "runbook path", "pattern path", "pattern filename", "standard path", "dated ADR filename", "kind and filename":
		return "medium"
	case "explicit --map":
		return "high"
	case "document headings":
		return "low"
	default:
		return "low"
	}
}

func canonicalContent(doc *store.Doc, e Entry, opts Options) (string, bool, error) {
	f := doc.Frontmatter
	if completeFrontmatter(f) {
		return doc.Raw, false, nil
	}
	status := f.Status
	if !docrules.ValidStatuses[status] {
		status = opts.Status
	}
	if !docrules.ValidStatuses[status] {
		status = "approved"
	}
	owners := f.Owners
	if len(owners) == 0 {
		owners = opts.Owners
	}
	if len(owners) == 0 {
		owners = []string{"platform-team"}
	}
	last := f.LastReviewed
	if _, err := time.Parse("2006-01-02", last); err != nil {
		last = opts.LastReviewed
	}
	if _, err := time.Parse("2006-01-02", last); err != nil {
		last = opts.Now.Format("2006-01-02")
	}
	fm := map[string]any{
		"id": e.ID, "kind": e.Kind, "title": e.Title, "status": status,
		"superseded_by": nil, "overrides": nil,
		"topics": f.Topics, "languages": f.Languages, "related": f.Related,
		"reference_impls": f.ReferenceImpls, "owners": owners, "last_reviewed": last,
	}
	b, err := yaml.Marshal(fm)
	if err != nil {
		return "", false, err
	}
	return "---\n" + string(b) + "---\n\n" + strings.TrimLeft(doc.Body, "\r\n"), true, nil
}

func completeFrontmatter(f store.Frontmatter) bool {
	if f.ID == "" || !docrules.ValidKinds[f.Kind] || f.Title == "" || !docrules.ValidStatuses[f.Status] || len(f.Owners) == 0 || f.LastReviewed == "" {
		return false
	}
	_, err := time.Parse("2006-01-02", f.LastReviewed)
	return err == nil
}

func titleFromPath(base string) string {
	name := strings.TrimSuffix(base, path.Ext(base))
	if m := adrName.FindStringSubmatch(base); len(m) == 3 {
		name = m[2]
	}
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	if name == "" {
		return "Document"
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// Format returns a reviewable text report.
func Format(r *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "import report: %s -> %s\n", r.Source, r.Area)
	fmt.Fprintf(&b, "%d ready, %d unresolved, %d collisions\n", r.Ready, r.Unresolved, r.Collisions)
	for _, e := range r.Entries {
		fmt.Fprintf(&b, "%-10s %s", e.Status, e.Source)
		if e.Destination != "" {
			fmt.Fprintf(&b, " -> %s", e.Destination)
		}
		if e.Kind != "" {
			fmt.Fprintf(&b, " [%s", e.Kind)
			if e.Confidence != "" {
				fmt.Fprintf(&b, "/%s", e.Confidence)
			}
			b.WriteString("]")
		}
		if e.Reason != "" {
			fmt.Fprintf(&b, "\n  reason: %s", e.Reason)
		}
		for _, n := range e.Notes {
			fmt.Fprintf(&b, "\n  note: %s", n)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// WriteReport writes the migration report as JSON.
func WriteReport(r *Report, filename string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if dir := filepath.Dir(filename); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(filename, b, 0o644)
}
