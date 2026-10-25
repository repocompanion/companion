package changeset

import (
	"os"
	"testing"
)

// TestMain pins a git author and committer for the whole package.
//
// Commits made by the changeset manager shell out to `git commit` without an
// explicit author, so they inherit the ambient git configuration. Hosted CI
// runners have no global user.name/user.email, and git refuses to guess there,
// which made tests fail only on CI. Tests that assert on a specific identity
// still override these defaults with t.Setenv.
func TestMain(m *testing.M) {
	defaults := map[string]string{
		"GIT_AUTHOR_NAME":     "Companion Test",
		"GIT_AUTHOR_EMAIL":    "test@companion.invalid",
		"GIT_COMMITTER_NAME":  "Companion Test",
		"GIT_COMMITTER_EMAIL": "test@companion.invalid",
	}
	for key, value := range defaults {
		if os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}
