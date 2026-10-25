package gitx

import (
	"os/exec"
	"testing"
)

func TestSplitGitHubRemote(t *testing.T) {
	cases := []struct {
		in          string
		owner, repo string
	}{
		{"git@github.com:acme/api.git", "acme", "api"},
		{"ssh://git@github.com/acme/api.git", "acme", "api"},
		{"https://github.com/acme/api.git", "acme", "api"},
		{"https://github.com/acme/api", "acme", "api"},
		{"git://github.com/acme/api.git", "acme", "api"},
	}
	for _, c := range cases {
		owner, repo, err := SplitGitHubRemote(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.in, err)
		}
		if owner != c.owner || repo != c.repo {
			t.Fatalf("%s -> %s/%s", c.in, owner, repo)
		}
	}
	if _, _, err := SplitGitHubRemote("/tmp/store.git"); err == nil {
		t.Fatal("expected error for a local path")
	}
}

func TestSplitGitHubRemoteRejectsOtherHostsAndNestedPaths(t *testing.T) {
	cases := []string{
		"https://gitlab.com/acme/api.git",
		"git@gitlab.com:acme/api.git",
		"https://gitlab.com/group/sub/api.git",
		"ssh://git@github.example.com/acme/api.git",
	}
	for _, in := range cases {
		if _, _, err := SplitGitHubRemote(in); err == nil {
			t.Errorf("SplitGitHubRemote(%q) accepted a non-GitHub remote", in)
		}
	}
}

func TestSplitRemoteKeepsHostForEnterprise(t *testing.T) {
	host, owner, repo, err := SplitRemote("ssh://git@github.example.com/acme/api.git")
	if err != nil {
		t.Fatal(err)
	}
	if host != "github.example.com" || owner != "acme" || repo != "api" {
		t.Fatalf("SplitRemote = %q %q/%q", host, owner, repo)
	}
}

func TestRedactRemoteHidesCredentials(t *testing.T) {
	cases := map[string]string{
		"https://x-access-token:ghp_abcdefghijklmnopqrstuvwxyz@github.com/acme/api.git": "https://***@github.com/acme/api.git",
		"https://github.com/acme/api.git":                                               "https://github.com/acme/api.git",
		"git@github.com:acme/api.git":                                                   "git@github.com:acme/api.git",
		"failed to clone https://user:secret@example.com/store.git":                     "failed to clone https://***@example.com/store.git",
	}
	for in, want := range cases {
		if got := RedactRemote(in); got != want {
			t.Errorf("RedactRemote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlugProducesValidRefComponents(t *testing.T) {
	cases := map[string]string{
		"Refund Flow":  "refund-flow",
		"v1.2 notes":   "v1.2-notes",
		"v1..v2":       "v1.v2",
		"a...b":        "a.b",
		"release.lock": "release-lock",
		"..":           "untitled",
		"   ":          "untitled",
		"--.hidden.--": "hidden",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
		if out, err := exec.Command("git", "check-ref-format", "refs/heads/docs/"+Slug(in)).CombinedOutput(); err != nil {
			t.Errorf("Slug(%q) = %q is not a valid ref: %s", in, Slug(in), out)
		}
	}
}
