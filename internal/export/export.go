// Package export renders companion context for agents that do not use MCP.
package export

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/repocompanion/companion/internal/gitx"
	"github.com/repocompanion/companion/internal/runtime"
	"github.com/repocompanion/companion/internal/search"
	"github.com/repocompanion/companion/internal/store"
)

const (
	ManagedBegin = "<!-- companion:begin -->"
	ManagedEnd   = "<!-- companion:end -->"
)

type Snapshot struct {
	GeneratedAt      time.Time `json:"generated_at"`
	MainCommitTime   time.Time `json:"main_commit_time,omitempty"`
	MainRevision     string    `json:"main_revision,omitempty"`
	ActiveChangesets []string  `json:"active_changesets,omitempty"`
}

type DocRef struct {
	Path   string `json:"path"`
	ID     string `json:"id,omitempty"`
	Title  string `json:"title,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Status string `json:"status,omitempty"`
	Source string `json:"source,omitempty"`
	State  string `json:"state,omitempty"`
	URL    string `json:"url,omitempty"`
	Read   string `json:"read,omitempty"`

	doc *store.Doc
}

type RequiredDoc struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
	State   string `json:"state,omitempty"`
	Source  string `json:"source,omitempty"`
}

type Group struct {
	Kind  string   `json:"kind"`
	Title string   `json:"title"`
	Docs  []DocRef `json:"docs,omitempty"`
}

type TaskPick struct {
	Task string   `json:"task"`
	Docs []DocRef `json:"docs,omitempty"`
}

type Bundle struct {
	Project      string        `json:"project"`
	Area         string        `json:"area"`
	Team         string        `json:"team,omitempty"`
	Languages    []string      `json:"languages,omitempty"`
	Summary      string        `json:"summary"`
	Snapshot     Snapshot      `json:"snapshot"`
	Required     []RequiredDoc `json:"required_docs,omitempty"`
	Tasks        []TaskPick    `json:"common_tasks,omitempty"`
	Groups       []Group       `json:"groups,omitempty"`
	Docs         []DocRef      `json:"docs,omitempty"`
	TotalDocs    int           `json:"total_docs"`
	ReturnedDocs int           `json:"returned_docs"`
	Truncated    bool          `json:"truncated,omitempty"`
	MaxDocs      int           `json:"max_docs"`
}

// Build assembles a bounded, overlay-aware export bundle for a session.
func Build(ctx context.Context, sess *runtime.Session) (*Bundle, error) {
	if sess == nil || sess.Context == nil {
		return nil, fmt.Errorf("export requires a resolved companion session")
	}
	if sess.Context.ProjectArea() == "" {
		return nil, fmt.Errorf("project is unresolved; pass --project owner/repo")
	}
	areas := []string{sess.Context.ProjectArea(), sess.Context.TeamArea(), sess.Context.OrgArea()}
	seenArea := map[string]bool{}
	byPath := map[string]DocRef{}
	for _, area := range areas {
		if area == "" || seenArea[area] {
			continue
		}
		seenArea[area] = true
		o, err := sess.Manager.Overlay(ctx, area)
		if err != nil {
			return nil, err
		}
		for _, d := range o.Docs {
			if _, exists := byPath[d.Path]; exists {
				continue
			}
			url := docURL(sess.Runtime.Store.Remote, d.Path, o.States[d.Path])
			byPath[d.Path] = DocRef{
				Path: d.Path, ID: d.Frontmatter.ID, Title: d.Frontmatter.Title,
				Kind: d.Frontmatter.Kind, Status: d.Frontmatter.Status,
				Source: o.Sources[d.Path], State: o.States[d.Path], URL: url,
				Read: "companion cat " + d.Path, doc: d,
			}
		}
	}
	all := make([]DocRef, 0, len(byPath))
	for _, d := range byPath {
		all = append(all, d)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Path < all[j].Path })
	total := len(all)
	maxDocs := 200
	truncated := false
	if len(all) > maxDocs {
		all = all[:maxDocs]
		truncated = true
	}

	b := &Bundle{
		Project: sess.Context.Project, Area: sess.Context.ProjectArea(),
		Team: sess.Context.Team, Languages: sess.Context.Languages,
		Summary: summaryFor(all, sess.Context.ProjectArea()),
		Docs:    all, TotalDocs: total, ReturnedDocs: len(all),
		Truncated: truncated, MaxDocs: maxDocs,
	}
	if rev, t, err := sess.Runtime.Store.MirrorRev(ctx); err == nil {
		b.Snapshot.MainRevision = rev
		b.Snapshot.MainCommitTime = t.UTC()
	}
	b.Snapshot.GeneratedAt = time.Now().UTC()
	if list, err := sess.Manager.List(); err == nil {
		for _, c := range list {
			if c.IsOpen() && (c.Area == b.Area || c.Area == sess.Context.TeamArea() || c.Area == "org") {
				b.Snapshot.ActiveChangesets = append(b.Snapshot.ActiveChangesets, c.ID+" ("+c.Area+")")
			}
		}
		sort.Strings(b.Snapshot.ActiveChangesets)
	}
	b.Required = requiredDocs(sess, all)
	b.Tasks = commonTasks(all)
	b.Groups = groups(all)
	return b, nil
}

func summaryFor(docs []DocRef, area string) string {
	for _, d := range docs {
		if d.Path != path.Join(area, "README.md") {
			continue
		}
		if d.doc != nil {
			for _, line := range strings.Split(d.doc.Body, "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				if len(line) > 320 {
					line = line[:317] + "..."
				}
				return line
			}
		}
		if d.Title != "" {
			return d.Title + " — " + d.Status + " project documentation."
		}
	}
	return "Project context exported from companion."
}

func requiredDocs(sess *runtime.Session, docs []DocRef) []RequiredDoc {
	required := sess.Repo.Docs.Required
	if len(required) == 0 {
		required = []string{"README.md", "architecture.md"}
	}
	byPath := map[string]DocRef{}
	for _, d := range docs {
		byPath[d.Path] = d
	}
	out := make([]RequiredDoc, 0, len(required))
	for _, req := range required {
		rel := store.JoinArea(sess.Context.ProjectArea(), req)
		d, ok := byPath[rel]
		out = append(out, RequiredDoc{
			Path: rel, Present: ok, State: d.State, Source: d.Source,
		})
	}
	return out
}

func commonTasks(docs []DocRef) []TaskPick {
	tasks := []string{
		"implement an integration, queue consumer or API",
		"change data, schema or a database migration",
		"operate, deploy, roll back or troubleshoot",
		"understand an end-to-end business flow",
	}
	var out []TaskPick
	for _, task := range tasks {
		results, err := search.Search(docsAsDocs(docs), task, 2)
		if err != nil {
			continue
		}
		pick := TaskPick{Task: task}
		for _, r := range results {
			if d, ok := findRef(docs, r.Path); ok && len(pick.Docs) < 2 {
				pick.Docs = append(pick.Docs, d)
			}
		}
		if len(pick.Docs) > 0 {
			out = append(out, pick)
		}
	}
	return out
}

// docsAsDocs builds minimal store.Doc values for the BM25 helper. The refs
// already contain the fields the index uses.
func docsAsDocs(refs []DocRef) []*store.Doc {
	out := make([]*store.Doc, 0, len(refs))
	for _, r := range refs {
		if r.doc != nil {
			out = append(out, r.doc)
			continue
		}
		out = append(out, &store.Doc{
			Path: r.Path,
			Frontmatter: store.Frontmatter{
				ID: r.ID, Kind: r.Kind, Title: r.Title, Status: r.Status,
			},
		})
	}
	return out
}

func findRef(refs []DocRef, p string) (DocRef, bool) {
	for _, r := range refs {
		if r.Path == p {
			return r, true
		}
	}
	return DocRef{}, false
}

func groups(docs []DocRef) []Group {
	defs := []struct{ kind, title string }{
		{"project-doc", "Project docs"},
		{"adr", "Decisions"},
		{"flow", "Flows"},
		{"runbook", "Runbooks"},
		{"pattern", "Patterns"},
		{"standard", "Standards"},
	}
	var out []Group
	for _, def := range defs {
		g := Group{Kind: def.kind, Title: def.title}
		for _, d := range docs {
			if d.Kind == def.kind && len(g.Docs) < 20 {
				g.Docs = append(g.Docs, d)
			}
		}
		if len(g.Docs) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func docURL(remote, rel, state string) string {
	if state != "" && state != "main" {
		return ""
	}
	host, owner, repo, err := gitx.SplitRemote(remote)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("https://%s/%s/%s/blob/main/%s", host, owner, repo, rel)
}

func snapshotLine(b *Bundle) string {
	rev := b.Snapshot.MainRevision
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if rev == "" {
		rev = "unknown"
	}
	line := "Snapshot generated by companion from `main@" + rev + "`."
	if !b.Snapshot.MainCommitTime.IsZero() {
		line = "Snapshot generated by companion from `main@" + rev + "` (commit time " + b.Snapshot.MainCommitTime.Format(time.RFC3339) + ")."
	}
	if len(b.Snapshot.ActiveChangesets) > 0 {
		line += " Active changesets: " + strings.Join(b.Snapshot.ActiveChangesets, ", ") + "."
	}
	if b.Truncated {
		line += fmt.Sprintf(" Showing %d of %d docs.", b.ReturnedDocs, b.TotalDocs)
	}
	return line + " Re-run the export after `companion pull`."
}

func docLink(d DocRef) string {
	label := d.TitleOrPath()
	if label == "" || label == d.Path {
		return fmt.Sprintf("`%s`", d.Path)
	}
	if d.URL != "" {
		return fmt.Sprintf("[%s](%s) (`%s`)", label, d.URL, d.Path)
	}
	return fmt.Sprintf("%s (`%s`)", label, d.Path)
}

func (d DocRef) TitleOrPath() string {
	if strings.TrimSpace(d.Title) != "" {
		return d.Title
	}
	return d.Path
}

// RenderAgentsMD renders a managed AGENTS.md block.
func RenderAgentsMD(b *Bundle) string {
	var s strings.Builder
	s.WriteString(ManagedBegin + "\n")
	fmt.Fprintf(&s, "# %s — agent context\n\n", b.Project)
	s.WriteString("> " + snapshotLine(b) + "\n\n")
	if b.Summary != "" {
		s.WriteString(b.Summary + "\n\n")
	}
	s.WriteString("## Start here\n\n")
	for _, d := range b.Docs {
		if d.Path == path.Join(b.Area, "README.md") || d.Path == path.Join(b.Area, "architecture.md") {
			fmt.Fprintf(&s, "- %s\n", docLink(d))
		}
	}
	if !strings.Contains(s.String(), "`"+path.Join(b.Area, "README.md")+"`") {
		s.WriteString("- Use `companion context` (or `docs_get_context` via MCP) for the current project index.\n")
	}
	s.WriteString("\n## Common tasks\n\n")
	if len(b.Tasks) == 0 {
		s.WriteString("Use `companion guidance \"<task>\"` for ranked patterns, ADRs and standards.\n")
	}
	for _, t := range b.Tasks {
		fmt.Fprintf(&s, "### %s\n\n", titleCase(t.Task))
		for _, d := range t.Docs {
			fmt.Fprintf(&s, "- %s\n", docLink(d))
		}
		s.WriteString("\n")
	}
	s.WriteString("## Required documentation\n\n")
	for _, r := range b.Required {
		mark := "x"
		if !r.Present {
			mark = " "
		}
		fmt.Fprintf(&s, "- [%s] `%s`\n", mark, r.Path)
	}
	s.WriteString("\n## Documentation index\n\n")
	for _, g := range b.Groups {
		fmt.Fprintf(&s, "### %s\n\n", g.Title)
		for _, d := range g.Docs {
			fmt.Fprintf(&s, "- %s", docLink(d))
			if d.State != "" && d.State != "main" {
				fmt.Fprintf(&s, " (%s)", d.State)
			}
			s.WriteString("\n")
		}
		s.WriteString("\n")
	}
	s.WriteString("## How to use companion\n\n")
	s.WriteString("- Read a document with `companion cat <path>`.\n")
	s.WriteString("- Find guidance with `companion guidance \"<task>\"`.\n")
	s.WriteString("- Refresh this snapshot with `companion pull` and re-run the export.\n")
	s.WriteString("- Do not edit this managed block by hand; edit docs in the companion store through a changeset.\n")
	s.WriteString(ManagedEnd + "\n")
	return s.String()
}

// RenderLLMsTxt renders a compact llms.txt-style discovery file.
func RenderLLMsTxt(b *Bundle) string {
	var s strings.Builder
	fmt.Fprintf(&s, "# %s\n\n", b.Project)
	fmt.Fprintf(&s, "> %s\n\n", snapshotLine(b))
	if b.Summary != "" {
		s.WriteString(b.Summary + "\n\n")
	}
	s.WriteString("## Documentation\n\n")
	for _, d := range b.Docs {
		fmt.Fprintf(&s, "- %s: `%s`\n", docLink(d), d.Path)
	}
	s.WriteString("\n## Common tasks\n\n")
	for _, t := range b.Tasks {
		fmt.Fprintf(&s, "- %s: ", titleCase(t.Task))
		for i, d := range t.Docs {
			if i > 0 {
				s.WriteString("; ")
			}
			s.WriteString(docLink(d))
		}
		s.WriteString("\n")
	}
	s.WriteString("\n## Required documentation\n\n")
	for _, r := range b.Required {
		status := "present"
		if !r.Present {
			status = "missing"
		}
		fmt.Fprintf(&s, "- `%s`: %s\n", r.Path, status)
	}
	return s.String()
}

// RenderContextJSON returns the full bounded export payload.
func RenderContextJSON(b *Bundle) (string, error) {
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

// RenderContextMarkdown returns a human-readable context bundle.
func RenderContextMarkdown(b *Bundle) string {
	var s strings.Builder
	fmt.Fprintf(&s, "# %s — context bundle\n\n", b.Project)
	s.WriteString("> " + snapshotLine(b) + "\n\n")
	fmt.Fprintf(&s, "- Project: `%s`\n- Area: `%s`\n", b.Project, b.Area)
	if b.Team != "" {
		fmt.Fprintf(&s, "- Team: `%s`\n", b.Team)
	}
	if len(b.Languages) > 0 {
		fmt.Fprintf(&s, "- Languages: %s\n", strings.Join(b.Languages, ", "))
	}
	s.WriteString("\n## Required documentation\n\n")
	for _, r := range b.Required {
		status := "present"
		if !r.Present {
			status = "missing"
		}
		fmt.Fprintf(&s, "- `%s`: %s\n", r.Path, status)
	}
	s.WriteString("\n## Documentation\n\n")
	for _, g := range b.Groups {
		fmt.Fprintf(&s, "### %s\n\n", g.Title)
		for _, d := range g.Docs {
			fmt.Fprintf(&s, "- %s", docLink(d))
			if d.State != "" && d.State != "main" {
				fmt.Fprintf(&s, " (%s)", d.State)
			}
			s.WriteString("\n")
		}
		s.WriteString("\n")
	}
	return s.String()
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// WriteManaged writes content, replacing an existing companion-managed block.
func WriteManaged(filename, content string, force bool) error {
	if filename == "" || filename == "-" {
		fmt.Print(content)
		return nil
	}
	existing, err := os.ReadFile(filename)
	if err == nil {
		old := string(existing)
		if start, end := strings.Index(old, ManagedBegin), strings.Index(old, ManagedEnd); start >= 0 && end > start {
			content = old[:start] + content + old[end+len(ManagedEnd):]
		} else if !force {
			return fmt.Errorf("%s exists and has no companion managed block; use --force to overwrite", filename)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return atomicWrite(filename, content)
}

// WriteFile writes a generated export, refusing to overwrite unless forced.
func WriteFile(filename, content string, force bool) error {
	if filename == "" || filename == "-" {
		fmt.Print(content)
		return nil
	}
	if _, err := os.Stat(filename); err == nil && !force {
		return fmt.Errorf("%s exists; use --force to overwrite", filename)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return atomicWrite(filename, content)
}

func atomicWrite(filename, content string) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".companion-export-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, filename); err != nil {
		// os.Rename cannot replace an existing file on Windows. Retry after
		// removing the generated target, keeping the temp-file write itself.
		if _, statErr := os.Stat(filename); statErr == nil {
			if removeErr := os.Remove(filename); removeErr == nil {
				return os.Rename(tmp, filename)
			}
		}
		return err
	}
	return nil
}

// SnapshotWarning is used by callers that need just the warning text.
func SnapshotWarning(b *Bundle) string { return snapshotLine(b) }
