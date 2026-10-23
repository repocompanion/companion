// Package docrules owns the canonical document model shared by MCP, lint,
// templates and CLI helpers. Keeping the rules here prevents MCP and store CI
// from drifting apart.
package docrules

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/bolacha/docsync/internal/store"
)

// Kinds are the canonical document kinds. "architecture" is intentionally not
// a kind; architecture.md is a project-doc.
const (
	KindProjectDoc = "project-doc"
	KindADR        = "adr"
	KindFlow       = "flow"
	KindRunbook    = "runbook"
	KindPattern    = "pattern"
	KindStandard   = "standard"
)

const (
	StatusProposed   = "proposed"
	StatusApproved   = "approved"
	StatusDeprecated = "deprecated"
	StatusSuperseded = "superseded"
)

var ValidKinds = map[string]bool{
	KindProjectDoc: true,
	KindADR:        true,
	KindFlow:       true,
	KindRunbook:    true,
	KindPattern:    true,
	KindStandard:   true,
}

var ValidStatuses = map[string]bool{
	StatusProposed:   true,
	StatusApproved:   true,
	StatusDeprecated: true,
	StatusSuperseded: true,
}

var adrFile = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-[a-z0-9][a-z0-9-]*\.md$`)
var fingerprintRE = regexp.MustCompile(`^sha256:[a-fA-F0-9]{64}$`)

var ValidReferenceKinds = map[string]bool{
	"function": true, "method": true, "type": true, "package": true,
	"route": true, "job": true, "migration": true, "config": true, "test": true,
}

// Structure describes the canonical project layout for agents.
type Structure struct {
	Layout             []StructureItem `json:"layout"`
	Frontmatter        []string        `json:"frontmatter_required"`
	FrontmatterExample string          `json:"frontmatter_example"`
	Rules              []string        `json:"rules"`
}

type StructureItem struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Use  string `json:"use"`
}

// CanonicalStructure returns the machine-readable structure exposed by
// docs_get_context.
func CanonicalStructure() Structure {
	return Structure{
		Layout: []StructureItem{
			{Path: "README.md", Kind: KindProjectDoc, Use: "project overview and documentation map"},
			{Path: "architecture.md", Kind: KindProjectDoc, Use: "components, data flow, dependencies and trade-offs"},
			{Path: "adr/YYYY-MM-DD-<slug>.md", Kind: KindADR, Use: "one file per decision"},
			{Path: "flows/<slug>.md", Kind: KindFlow, Use: "one file per runtime/business flow"},
			{Path: "runbooks/<slug>.md", Kind: KindRunbook, Use: "one file per operational procedure"},
			{Path: "patterns/<slug>.md", Kind: KindPattern, Use: "reusable implementation patterns"},
			{Path: "standards/<slug>.md", Kind: KindStandard, Use: "rules and conventions"},
		},
		Frontmatter: []string{"id", "kind", "title", "status", "owners", "last_reviewed"},
		FrontmatterExample: "---\n" +
			"id: refund-flow\n" +
			"kind: flow\n" +
			"title: Refund flow\n" +
			"status: approved\n" +
			"topics: [refunds, payments]\n" +
			"languages: [go]\n" +
			"related: []\n" +
			"owners: [payments]\n" +
			"last_reviewed: 2026-08-31\n" +
			"---\n",
		Rules: []string{
			"Every .md document needs YAML frontmatter.",
			"ADRs are one dated file per decision under adr/.",
			"Flows belong under flows/, runbooks under runbooks/, patterns under patterns/, standards under standards/.",
			"Project root documents are README.md and architecture.md only.",
			"Do not collapse multiple ADRs, flows or runbooks into an aggregate Markdown file.",
			"Use related: to link documents; use reference_impls: for code examples.",
		},
	}
}

// Problem is one canonical-rule violation. Lint converts these to its own
// Issue type; MCP returns the first as an error.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// IsMarkdown reports whether path is a Markdown document that should carry
// frontmatter. Mermaid files are agent-writable but are not docs.
func IsMarkdown(p string) bool {
	return strings.EqualFold(path.Ext(strings.ReplaceAll(p, "\\", "/")), ".md")
}

// IsAgentDocumentPath reports whether agents may create, replace or delete a
// file through the document tools.
func IsAgentDocumentPath(p string) bool {
	switch strings.ToLower(path.Ext(strings.ReplaceAll(p, "\\", "/"))) {
	case ".md", ".mmd":
		return true
	default:
		return false
	}
}

// ValidateArea accepts org/**, teams/<team>/** and projects/<owner>/<repo>/**.
func ValidateArea(area string) error {
	area = strings.Trim(area, "/")
	parts := strings.Split(area, "/")
	switch {
	case area == ".docsync" || strings.HasPrefix(area, ".docsync/"):
		return nil
	case area == "org" || (len(parts) > 1 && parts[0] == "org"):
		return nil
	case len(parts) >= 2 && parts[0] == "teams" && parts[1] != "":
		return nil
	case len(parts) >= 3 && parts[0] == "projects" && parts[1] != "" && parts[2] != "":
		return nil
	default:
		return fmt.Errorf("area must be org/..., teams/<team>/... or projects/<owner>/<repo>/...: %q", area)
	}
}

// AreaForPath returns the changeset area for a store-relative path.
func AreaForPath(p string) (string, error) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	switch {
	case len(parts) >= 1 && parts[0] == ".docsync":
		return ".docsync", nil
	case len(parts) >= 1 && parts[0] == "org":
		return "org", nil
	case len(parts) >= 2 && parts[0] == "teams":
		return "teams/" + parts[1], nil
	case len(parts) >= 3 && parts[0] == "projects":
		return "projects/" + parts[1] + "/" + parts[2], nil
	default:
		return "", fmt.Errorf("cannot derive an area from %q", p)
	}
}

// ScopeOf returns the uniqueness scope for a document path.
func ScopeOf(p string) string {
	area, err := AreaForPath(p)
	if err != nil {
		return "."
	}
	return area
}

// ValidateFrontmatter checks required fields and their values.
func ValidateFrontmatter(rel string, f store.Frontmatter) []Problem {
	var out []Problem
	add := func(msg string) {
		out = append(out, Problem{Path: rel, Message: msg})
	}
	if f.ID == "" {
		add("missing frontmatter id")
	}
	if f.Kind == "" {
		add("missing frontmatter kind")
	} else if !ValidKinds[f.Kind] {
		add(fmt.Sprintf("unknown frontmatter kind %q; expected project-doc, adr, pattern, standard, flow or runbook", f.Kind))
	}
	if f.Title == "" {
		add("missing frontmatter title")
	}
	if f.Status == "" {
		add("missing frontmatter status")
	} else if !ValidStatuses[f.Status] {
		add(fmt.Sprintf("unknown frontmatter status %q", f.Status))
	}
	if len(f.Owners) == 0 {
		add(fmt.Sprintf("document %s needs a non-empty owners list in frontmatter", rel))
	}
	if f.LastReviewed == "" {
		add(fmt.Sprintf("document %s needs last_reviewed: YYYY-MM-DD", rel))
	} else if _, err := time.Parse("2006-01-02", f.LastReviewed); err != nil {
		add(fmt.Sprintf("last_reviewed must be YYYY-MM-DD, got %q", f.LastReviewed))
	}
	return out
}

// ValidateDoc checks frontmatter and canonical layout for a parsed document.
func ValidateDoc(rel string, f store.Frontmatter) []Problem {
	out := ValidateFrontmatter(rel, f)
	if msg := LayoutError(rel, f.Kind); msg != "" {
		out = append(out, Problem{Path: rel, Message: msg})
	}
	out = append(out, ValidateReferences(rel, f.ReferenceImpls)...)
	return out
}

// ValidateReferences checks the reference_impls shape. Path existence and
// symbol resolution are intentionally handled by the code index because they
// require a code checkout and optional providers.
func ValidateReferences(rel string, refs []store.ReferenceImpl) []Problem {
	var out []Problem
	add := func(msg string) { out = append(out, Problem{Path: rel, Message: msg}) }
	for i, r := range refs {
		where := fmt.Sprintf("reference_impls[%d]", i)
		if strings.TrimSpace(r.Repo) == "" {
			add(where + " requires repo")
		}
		if strings.TrimSpace(r.Path) == "" {
			add(where + " requires path")
		}
		if r.Kind != "" && !ValidReferenceKinds[r.Kind] {
			add(fmt.Sprintf("%s has unknown kind %q", where, r.Kind))
		}
		if r.Symbol != "" && strings.ContainsAny(r.Symbol, " \t\r\n") {
			add(where + " symbol must not contain whitespace")
		}
		if r.Fingerprint != "" && !fingerprintRE.MatchString(r.Fingerprint) {
			add(where + " fingerprint must be sha256:<64 hex characters>")
		}
	}
	return out
}

// ValidateMarkdown parses and validates a Markdown document. Non-Markdown
// files are ignored so Mermaid and image writes keep their existing behavior.
func ValidateMarkdown(rel, content string) ([]Problem, error) {
	if !IsMarkdown(rel) {
		return nil, nil
	}
	doc, err := store.ParseDoc(rel, content)
	if err != nil {
		return nil, err
	}
	return ValidateDoc(rel, doc.Frontmatter), nil
}

// LayoutError returns a canonical-layout violation, or "" when the path is
// valid for the kind.
func LayoutError(p, kind string) string {
	area, err := AreaForPath(p)
	if err != nil {
		return ""
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(strings.Trim(p, "/"), area), "/")
	base := path.Base(rel)
	switch kind {
	case KindProjectDoc:
		if path.Dir(rel) != "." || (base != "README.md" && base != "architecture.md") {
			return fmt.Sprintf("project-doc files must be README.md or architecture.md at the area root, not %s", p)
		}
	case KindADR:
		if !strings.HasPrefix(rel, "adr/") || !adrFile.MatchString(base) {
			return fmt.Sprintf("ADRs must be one dated file under adr/, e.g. adr/2026-08-31-my-decision.md; got %s", p)
		}
	case KindFlow:
		if !strings.HasPrefix(rel, "flows/") {
			return fmt.Sprintf("flow documents must live under flows/, got %s", p)
		}
	case KindRunbook:
		if !strings.HasPrefix(rel, "runbooks/") {
			return fmt.Sprintf("runbook documents must live under runbooks/, got %s", p)
		}
	case KindPattern:
		if !strings.HasPrefix(rel, "patterns/") {
			return fmt.Sprintf("pattern documents must live under patterns/, got %s", p)
		}
	case KindStandard:
		if !strings.HasPrefix(rel, "standards/") {
			return fmt.Sprintf("standard documents must live under standards/, got %s", p)
		}
	}
	return ""
}
