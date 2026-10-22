package gitx

import "testing"

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
