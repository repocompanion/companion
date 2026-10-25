package config

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/repocompanion/companion/internal/gitx"
)

func TestLoadUserAppliesRemoteEnvWithoutConfigFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COMPANION_CONFIG", filepath.Join(dir, "missing.yaml"))
	t.Setenv("COMPANION_REMOTE", "git@github.com:acme/store.git")

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

func TestSetRepoMappingPreservesUnchangedFields(t *testing.T) {
	reg := Registry{Repos: map[string]RepoMapping{
		"acme/api": {Team: "old-team", Domain: "checkout"},
	}}

	reg.SetRepoMapping("acme/api", func(m *RepoMapping) {
		m.Team = "payments"
	})
	if got := reg.Repos["acme/api"]; got.Team != "payments" || got.Domain != "checkout" {
		t.Fatalf("mapping = %+v, want team updated and domain preserved", got)
	}

	reg.SetRepoMapping("acme/new", func(m *RepoMapping) {
		m.Domain = "billing"
	})
	if got := reg.Repos["acme/new"]; got.Domain != "billing" {
		t.Fatalf("new mapping = %+v", got)
	}
}

func TestProjectSettersRejectNonCanonicalValues(t *testing.T) {
	var user UserConfig
	if err := SetUserKey(&user, "project", "acme/api/extra"); err == nil {
		t.Fatal("user project accepted extra path segment")
	}
	if err := SetUserKey(&user, "project", "acme/api"); err != nil {
		t.Fatalf("valid user project rejected: %v", err)
	}
	if user.Project != "acme/api" {
		t.Fatalf("user project = %q", user.Project)
	}

	var repo RepoConfig
	if err := SetRepoKey(&repo, "project", "../api"); err == nil {
		t.Fatal("repo project accepted traversal")
	}
	if err := SetRepoKey(&repo, "project", "acme/api"); err != nil {
		t.Fatalf("valid repo project rejected: %v", err)
	}
}

func TestResolveWarnsOnInvalidProject(t *testing.T) {
	ctx := Resolve(context.Background(), ResolveInput{
		Cwd:  t.TempDir(),
		User: UserConfig{Project: "acme/api/extra"},
		Env:  map[string]string{},
	})
	found := false
	for _, warning := range ctx.Warnings {
		if strings.Contains(warning, "project must be owner/repo") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warnings = %v", ctx.Warnings)
	}
}

func TestLoadRegistryRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	storeRoot := t.TempDir()
	outside := filepath.Join(t.TempDir(), "registry.yaml")
	if err := os.WriteFile(outside, []byte("teams:\n  stolen:\n    domain: secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(storeRoot, ".companion"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(storeRoot, ".companion", "registry.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRegistry(storeRoot); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink escape rejection, got %v", err)
	}
}

func TestLoadRepoRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privileges on Windows")
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(outside, []byte("project: stolen/repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".companion.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRepo(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink escape rejection, got %v", err)
	}
}
