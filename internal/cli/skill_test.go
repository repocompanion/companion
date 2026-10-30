package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// The shipped agent skill names commands and MCP tools. The skill is prose, so
// nothing else keeps those names honest when the CLI or MCP surface changes.
// These tests parse the skill and fail on references that no longer exist.

const skillRelDir = "../../skills/repo-companion"

var (
	skillNameRE     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	skillInlineCode = regexp.MustCompile("`([^`\n]+)`")
	skillToolName   = regexp.MustCompile(`\bdocs_[a-z_]+\b`)
	skillMCPTool    = regexp.MustCompile(`Name:\s*"(docs_[a-z_]+)"`)
	skillCommandRE  = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type skillCommand struct {
	name     string
	children map[string]*skillCommand
}

func skillRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Clean(skillRelDir)
	info, err := os.Stat(filepath.Join(root, "SKILL.md"))
	if err != nil {
		t.Fatalf("skill is missing: %v", err)
	}
	if info.IsDir() {
		t.Fatal("SKILL.md is a directory")
	}
	return root
}

func readSkillFrontmatter(t *testing.T, path string) (skillFrontmatter, string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := strings.TrimPrefix(string(raw), "\ufeff")
	if !strings.HasPrefix(text, "---\n") {
		t.Fatalf("%s: missing opening frontmatter fence", path)
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		t.Fatalf("%s: missing closing frontmatter fence", path)
	}
	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		t.Fatalf("%s: invalid frontmatter: %v", path, err)
	}
	return fm, rest[end+len("\n---\n"):]
}

func skillCommandTree(root *cobra.Command) map[string]*skillCommand {
	out := map[string]*skillCommand{}
	var walk func(parent *cobra.Command, into map[string]*skillCommand)
	walk = func(parent *cobra.Command, into map[string]*skillCommand) {
		for _, c := range parent.Commands() {
			node := &skillCommand{name: c.Name(), children: map[string]*skillCommand{}}
			into[node.name] = node
			walk(c, node.children)
		}
	}
	walk(root, out)
	return out
}

func skillMCPTools(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "mcpserver", "*.go"))
	if err != nil {
		t.Fatalf("glob mcpserver: %v", err)
	}
	out := map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, m := range skillMCPTool.FindAllStringSubmatch(string(raw), -1) {
			out[m[1]] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("no MCP tool names found in internal/mcpserver")
	}
	return out
}

func TestSkillFrontmatterAndLayout(t *testing.T) {
	root := skillRoot(t)
	fm, body := readSkillFrontmatter(t, filepath.Join(root, "SKILL.md"))

	if fm.Name != filepath.Base(root) {
		t.Errorf("name = %q, want folder name %q", fm.Name, filepath.Base(root))
	}
	if !skillNameRE.MatchString(fm.Name) {
		t.Errorf("name %q is not lowercase kebab-case", fm.Name)
	}
	if n := len([]rune(fm.Description)); n < 1 || n > 1024 {
		t.Errorf("description length = %d, want 1..1024", n)
	}
	if lines := strings.Count(body, "\n") + 1; lines > 500 {
		t.Errorf("SKILL.md body is %d lines; the spec recommends under 500", lines)
	}

	for _, rel := range []string{
		"references/workflows.md",
		"references/document-model.md",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("missing referenced file %s: %v", rel, err)
		}
	}
}

func TestSkillReferencesRealSurfaces(t *testing.T) {
	root := skillRoot(t)
	commands := skillCommandTree(NewRootCommand())
	tools := skillMCPTools(t)

	files := []string{filepath.Join(root, "SKILL.md")}
	refs, err := filepath.Glob(filepath.Join(root, "references", "*.md"))
	if err != nil {
		t.Fatalf("glob references: %v", err)
	}
	files = append(files, refs...)

	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(raw)

		for _, m := range skillInlineCode.FindAllStringSubmatch(text, -1) {
			checkSkillCommandSpan(t, filepath.Base(path), m[1], commands)
		}
		for _, name := range skillToolName.FindAllString(text, -1) {
			if !tools[name] {
				t.Errorf("%s: references MCP tool %q, which is not registered", filepath.Base(path), name)
			}
		}
	}
}

func checkSkillCommandSpan(t *testing.T, file, span string, commands map[string]*skillCommand) {
	t.Helper()
	fields := strings.Fields(strings.TrimSpace(span))
	if len(fields) < 2 || fields[0] != "companion" {
		return
	}
	name := fields[1]
	if !skillCommandRE.MatchString(name) {
		return
	}
	node, ok := commands[name]
	if !ok {
		t.Errorf("%s: `%s` references unknown command %q", file, span, name)
		return
	}
	if len(fields) < 3 || len(node.children) == 0 {
		return
	}
	sub := fields[2]
	if !skillCommandRE.MatchString(sub) {
		return
	}
	if _, ok := node.children[sub]; !ok {
		t.Errorf("%s: `%s` references unknown subcommand %q of %q", file, span, sub, name)
	}
}
