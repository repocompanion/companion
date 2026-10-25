// Package gitx is a small wrapper around the system git binary.
//
// companion deliberately uses the developer's git installation instead of a
// library. That keeps SSH keys, credential helpers and worktree behaviour
// identical to the commands developers already use.
package gitx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// CommandError carries the command and its stderr for useful error messages.
type CommandError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), msg)
}

func (e *CommandError) Unwrap() error { return e.Err }

// Run executes git in dir and returns trimmed stdout.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := RunRaw(ctx, dir, args...)
	return strings.TrimSpace(out), err
}

// RunRaw executes git in dir and returns stdout without trimming.
func RunRaw(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), &CommandError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.String(), nil
}

// RunInput executes git with stdin.
func RunInput(ctx context.Context, dir string, input []byte, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), &CommandError{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.String(), nil
}

// Detected is best-effort information found in the current code repository.
type Detected struct {
	Root    string
	Remote  string
	Project string // owner/repo
	Owner   string
	Repo    string
	Name    string
	Email   string
	Branch  string
}

// Detect inspects the repository containing cwd. It returns an empty Detected
// (and no error) when cwd is not inside a git repository.
func Detect(ctx context.Context, cwd string) Detected {
	var d Detected
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	root, err := Run(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return d
	}
	d.Root = root

	d.Remote, _ = Run(ctx, root, "remote", "get-url", "origin")
	d.Owner, d.Repo, _ = SplitGitHubRemote(d.Remote)
	if d.Owner != "" && d.Repo != "" {
		d.Project = d.Owner + "/" + d.Repo
	}
	d.Name, _ = Run(ctx, root, "config", "--get", "user.name")
	d.Email, _ = Run(ctx, root, "config", "--get", "user.email")
	d.Branch, _ = Run(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if d.Branch == "HEAD" {
		d.Branch = ""
	}
	return d
}

var scpRemote = regexp.MustCompile(`^[^@]+@[^:]+:(.+)$`)

// SplitGitHubRemote normalizes the common GitHub remote forms to owner/repo.
// Supported:
//
//	git@github.com:owner/repo.git
//	ssh://git@github.com/owner/repo.git
//	https://github.com/owner/repo.git
//	git://github.com/owner/repo.git
func SplitGitHubRemote(remote string) (owner, repo string, err error) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", "", fmt.Errorf("empty git remote")
	}
	path := ""
	switch {
	case strings.HasPrefix(remote, "http://"), strings.HasPrefix(remote, "https://"), strings.HasPrefix(remote, "ssh://"), strings.HasPrefix(remote, "git://"):
		// Strip scheme and optional user, then take the path after the host.
		s := remote
		if i := strings.Index(s, "://"); i >= 0 {
			s = s[i+3:]
		}
		if i := strings.Index(s, "@"); i >= 0 {
			s = s[i+1:]
		}
		if i := strings.Index(s, "/"); i >= 0 {
			path = s[i+1:]
		}
	default:
		if m := scpRemote.FindStringSubmatch(remote); m != nil {
			path = m[1]
		}
	}
	path = strings.Trim(path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("not a GitHub owner/repo remote: %s", remote)
	}
	owner, repo = parts[0], parts[1]
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("not a GitHub owner/repo remote: %s", remote)
	}
	return owner, repo, nil
}

var (
	unsafeSlug = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	dotRun     = regexp.MustCompile(`\.{2,}`)
)

// Slug makes a branch/path-safe token. Git rejects ref components that
// contain ".." or end in ".lock", so those are rewritten; single dots are
// kept so existing slugs such as "v1.2" stay stable.
func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = unsafeSlug.ReplaceAllString(s, "-")
	s = dotRun.ReplaceAllString(s, ".")
	s = strings.Trim(s, "-._")
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-._")
	}
	if strings.HasSuffix(s, ".lock") {
		s = strings.TrimSuffix(s, ".lock") + "-lock"
	}
	if s == "" {
		return "untitled"
	}
	return s
}

// AreaSlug converts projects/acme/api into projects-acme-api.
func AreaSlug(area string) string {
	area = strings.Trim(area, "/")
	area = strings.ReplaceAll(area, "/", "-")
	return Slug(area)
}

// RepoName returns the final path element, useful for local/<name> fallbacks.
func RepoName(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Abs returns an absolute, cleaned path.
func Abs(p string) (string, error) {
	if p == "" {
		return os.Getwd()
	}
	return filepath.Abs(p)
}
