package cli

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"ok", nil, ExitOK},
		{"failure", errors.New("boom"), ExitFailure},
		{"pending", ErrPending, ExitPending},
	}
	for _, c := range cases {
		if got := ExitCode(c.err); got != c.want {
			t.Errorf("ExitCode(%s) = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestDoctorStrictFailsWhenChecksFail(t *testing.T) {
	t.Setenv("COMPANION_HOME", t.TempDir())
	t.Setenv("COMPANION_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))

	cmd := NewRootCommand()
	cmd.SetArgs([]string{"doctor", "--strict"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("doctor --strict should fail when the store is missing")
	}
	if code := ExitCode(err); code != ExitFailure {
		t.Fatalf("exit code = %d, want %d", code, ExitFailure)
	}
}
