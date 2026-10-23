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
