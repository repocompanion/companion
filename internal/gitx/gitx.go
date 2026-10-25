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
	"net/url"
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
	return fmt.Sprintf("git %s: %s", RedactRemote(strings.Join(e.Args, " ")), RedactRemote(msg))
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

var (
	scpRemote  = regexp.MustCompile(`^([^@/:]+@)?([^:/]+):(.+)$`)
	drivePath  = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
	repoPartRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// SplitRemote parses a Git remote into its host and a GitHub-style owner/repo
// path. The host is returned separately so callers never silently treat a
// GitLab or arbitrary host as github.com.
//
// Supported forms:
//
//	git@host:owner/repo.git
//	ssh://git@host/owner/repo.git
//	https://host/owner/repo.git
//	git://host/owner/repo.git
func SplitRemote(remote string) (host, owner, repo string, err error) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", "", "", fmt.Errorf("empty git remote")
	}
	if filepath.IsAbs(remote) || drivePath.MatchString(remote) {
		return "", "", "", fmt.Errorf("not a hosted git remote: %s", RedactRemote(remote))
	}

	pathPart := ""
	if i := strings.Index(remote, "://"); i >= 0 {
		u, perr := url.Parse(remote)
		if perr != nil || u.Hostname() == "" {
			return "", "", "", fmt.Errorf("invalid git remote: %s", RedactRemote(remote))
		}
		switch u.Scheme {
		case "http", "https", "ssh", "git":
		default:
			return "", "", "", fmt.Errorf("unsupported git remote scheme %q", u.Scheme)
		}
		host = u.Hostname()
		pathPart = u.Path
	} else if m := scpRemote.FindStringSubmatch(remote); m != nil {
		host = m[2]
		pathPart = m[3]
	} else {
		return "", "", "", fmt.Errorf("not a hosted git remote: %s", RedactRemote(remote))
	}

	pathPart = strings.Trim(pathPart, "/")
	pathPart = strings.TrimSuffix(pathPart, ".git")
	parts := strings.Split(pathPart, "/")
	if len(parts) != 2 || !validRepoPart(parts[0]) || !validRepoPart(parts[1]) {
		return "", "", "", fmt.Errorf("remote must be host/owner/repo, got %s", RedactRemote(remote))
	}
	return strings.ToLower(host), parts[0], parts[1], nil
}

func validRepoPart(p string) bool {
	if p == "" || p == "." || p == ".." || strings.HasPrefix(p, "-") {
		return false
	}
	return repoPartRE.MatchString(p)
}

// IsGitHubHost reports whether host is github.com (including www).
func IsGitHubHost(host string) bool {
	switch strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), ".")) {
	case "github.com", "www.github.com":
		return true
	default:
		return false
	}
}

// SplitGitHubRemote normalizes the common GitHub remote forms to owner/repo.
// Non-GitHub hosts are rejected; callers that support enterprise hosts must use
// SplitRemote and validate the host against their configured API URL.
func SplitGitHubRemote(remote string) (owner, repo string, err error) {
	host, owner, repo, err := SplitRemote(remote)
	if err != nil {
		return "", "", err
	}
	if !IsGitHubHost(host) {
		return "", "", fmt.Errorf("not a GitHub remote: host %q is not github.com", host)
	}
	return owner, repo, nil
}

var urlUserInfo = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/\s@]+@`)

// RedactRemote removes credentials from a URL or command line. It is used on
// error text and display paths so a token embedded in a remote URL never lands
// in logs or diagnostic output.
func RedactRemote(s string) string {
	return urlUserInfo.ReplaceAllString(s, "${1}***@")
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
