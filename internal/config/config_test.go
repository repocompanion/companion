package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bolacha/docsync/internal/gitx"
)

func TestLoadUserAppliesRemoteEnvWithoutConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCSYNC_CONFIG", filepath.Join(dir, "missing.yaml"))
	t.Setenv("DOCSYNC_REMOTE", "git@github.com:acme/store.git")

	cfg, err := LoadUser()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Remote != "git@github.com:acme/store.git" {
		t.Fatalf("remote = %q", cfg.Remote)
	}
}

func TestResolveUsesUserConfigFields(t *testing.T) {
	ctx := Resolve(context.Background(), ResolveInput{
		Cwd: t.TempDir(),
		User: UserConfig{
			Project:   "acme/from-user",
			Team:      "payments",
			Domain:    "checkout",
			Languages: []string{"go"},
			User:      UserDefaults{Name: "Ana", Email: "ana@example.com"},
		},
		Env: map[string]string{},
	})

	if ctx.Project != "acme/from-user" || ctx.Sources["project"] != "user config" {
		t.Fatalf("project = %q source=%q", ctx.Project, ctx.Sources["project"])
	}
	if ctx.Team != "payments" || ctx.Sources["team"] != "user config" {
		t.Fatalf("team = %q source=%q", ctx.Team, ctx.Sources["team"])
	}
	if ctx.Domain != "checkout" || ctx.Sources["domain"] != "user config" {
		t.Fatalf("domain = %q source=%q", ctx.Domain, ctx.Sources["domain"])
	}
	if len(ctx.Languages) != 1 || ctx.Languages[0] != "go" || ctx.Sources["languages"] != "user config" {
		t.Fatalf("languages = %v source=%q", ctx.Languages, ctx.Sources["languages"])
	}
}

func TestResolveRegistryReviewersOverrideCodeOwners(t *testing.T) {
	storeRoot := t.TempDir()
	writeTestFile(t, storeRoot, ".github/CODEOWNERS", "projects/acme/api/ @store/team\n")

	ctx := Resolve(context.Background(), ResolveInput{
		Cwd:   t.TempDir(),
		Flags: Flags{Project: "acme/api"},
		Registry: Registry{
			Reviewers: map[string]string{"acme/api": "@registry/team"},
		},
		Store: storeRoot,
		Env:   map[string]string{},
	})

	if ctx.Reviewers != "@registry/team" || ctx.Sources["reviewers"] != "registry reviewers" {
		t.Fatalf("reviewers = %q source=%q", ctx.Reviewers, ctx.Sources["reviewers"])
	}
}

func TestResolveTeamFromCodeRepoCodeOwners(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".github/CODEOWNERS", "* @acme/payments\n")
	if _, err := gitx.Run(context.Background(), root, "init", "-q"); err != nil {
		t.Fatal(err)
	}

	ctx := Resolve(context.Background(), ResolveInput{
		Cwd: root,
		Env: map[string]string{},
	})

	if ctx.Team != "payments" || ctx.Sources["team"] != "code repo CODEOWNERS root" {
		t.Fatalf("team = %q source=%q", ctx.Team, ctx.Sources["team"])
	}
}

func TestResolveUserLanguagesBeatManifestDetection(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", "module example.com/api\n\ngo 1.25.0\n")
	if _, err := gitx.Run(context.Background(), root, "init", "-q"); err != nil {
		t.Fatal(err)
	}

	ctx := Resolve(context.Background(), ResolveInput{
		Cwd:  root,
		User: UserConfig{Languages: []string{"php"}},
		Env:  map[string]string{},
	})

	if len(ctx.Languages) != 1 || ctx.Languages[0] != "php" || ctx.Sources["languages"] != "user config" {
		t.Fatalf("languages = %v source=%q", ctx.Languages, ctx.Sources["languages"])
	}
}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
