package store

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/repocompanion/companion/internal/gitx"
)

type Revision struct {
	Version  int       `json:"version"`
	Commit   string    `json:"commit"`
	Date     time.Time `json:"date"`
	Author   string    `json:"author"`
	Email    string    `json:"email"`
	Subject  string    `json:"subject"`
	PR       int       `json:"pr,omitempty"`
	Source   string    `json:"source,omitempty"`
	Reviewer string    `json:"reviewer,omitempty"`
}

var prInSubject = regexp.MustCompile(`\(#(\d+)\)`)
var revPattern = regexp.MustCompile(`^[A-Za-z0-9_./~^@{}-]+$`)

func validateRev(rev string) error {
	if rev == "" {
		return nil
	}
	if strings.HasPrefix(rev, "-") || !revPattern.MatchString(rev) {
		return fmt.Errorf("invalid revision %q", rev)
	}
	return nil
}

// Log returns merged history for a document, newest first.
func (s *Store) Log(ctx context.Context, rel string, wip bool) ([]Revision, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return nil, err
	}
	dir := s.Mirror
	args := []string{"log", "--follow", "--format=%H%x1f%aI%x1f%an%x1f%ae%x1f%s%x1f%b%x1e", "--", clean}
	if wip {
		// Caller may pass a changeset worktree path in rel. Try it first.
		if strings.HasPrefix(clean, s.Changes+"/") {
			dir = s.Home
		}
	}
	out, err := gitx.Run(ctx, dir, args...)
	if err != nil {
		return nil, err
	}
	records := strings.Split(out, "\x1e")
	var revs []Revision
	for _, rec := range records {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, "\x1f")
		if len(fields) < 5 {
			continue
		}
		t, _ := time.Parse(time.RFC3339, fields[1])
		rev := Revision{
			Commit:  fields[0],
			Date:    t,
			Author:  fields[2],
			Email:   fields[3],
			Subject: fields[4],
		}
		if len(fields) > 5 {
			for _, line := range strings.Split(fields[5], "\n") {
				switch {
				case strings.HasPrefix(line, "Companion-Source:"):
					rev.Source = strings.TrimSpace(strings.TrimPrefix(line, "Companion-Source:"))
				case strings.HasPrefix(line, "Reviewed-by:"):
					rev.Reviewer = strings.TrimSpace(strings.TrimPrefix(line, "Reviewed-by:"))
				}
			}
		}
		if m := prInSubject.FindStringSubmatch(rev.Subject); m != nil {
			rev.PR, _ = strconv.Atoi(m[1])
		}
		revs = append(revs, rev)
	}
	for i := range revs {
		revs[i].Version = len(revs) - i
	}
	return revs, nil
}

// ResolveRevision maps friendly version labels such as v2 to the corresponding
// merged commit. Other revisions pass through unchanged.
func (s *Store) ResolveRevision(ctx context.Context, rel, rev string) (string, error) {
	rev = strings.TrimSpace(rev)
	if rev == "" {
		return "", nil
	}
	lower := strings.ToLower(rev)
	if strings.HasPrefix(lower, "v") {
		n, err := strconv.Atoi(rev[1:])
		if err == nil {
			if n <= 0 {
				return "", fmt.Errorf("invalid version %q", rev)
			}
			revs, err := s.Log(ctx, rel, false)
			if err != nil {
				return "", err
			}
			for _, r := range revs {
				if r.Version == n {
					return r.Commit, nil
				}
			}
			return "", fmt.Errorf("version %d of %s not found", n, rel)
		}
	}
	return rev, nil
}

// ReadAtRev reads a file from a git revision in the mirror.
func (s *Store) ReadAtRev(ctx context.Context, rel, rev string) (string, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	if rev == "" {
		rev = "HEAD"
	}
	if err := validateRev(rev); err != nil {
		return "", err
	}
	return gitx.Run(ctx, s.Mirror, "show", rev+":"+clean)
}

// ReadPR fetches and reads a file from a pull request head.
func (s *Store) ReadPR(ctx context.Context, rel string, pr int) (string, error) {
	if pr <= 0 {
		return "", fmt.Errorf("invalid PR number")
	}
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	ref := "refs/companion/pr/" + strconv.Itoa(pr)
	spec := fmt.Sprintf("+refs/pull/%d/head:%s", pr, ref)
	if _, err := s.git(ctx, "fetch", "origin", spec); err != nil {
		return "", fmt.Errorf("fetch PR #%d: %w", pr, err)
	}
	return s.git(ctx, "show", ref+":"+clean)
}

// Diff returns a git diff for a document.
func (s *Store) Diff(ctx context.Context, rel, a, b string) (string, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	if a == "" && b == "" {
		a, b = "HEAD~1", "HEAD"
	}
	if a == "" {
		a = "HEAD"
	}
	if b == "" {
		b = "HEAD"
	}
	if err := validateRev(a); err != nil {
		return "", err
	}
	if err := validateRev(b); err != nil {
		return "", err
	}
	return gitx.Run(ctx, s.Mirror, "diff", "--no-ext-diff", a, b, "--", clean)
}

func (s *Store) Blame(ctx context.Context, rel string) (string, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	return gitx.Run(ctx, s.Mirror, "blame", "--line-porcelain", "--", clean)
}

// ReadRange returns old content for restore.
func (s *Store) ReadRange(ctx context.Context, rel, rev string) (string, error) {
	return s.ReadAtRev(ctx, rel, rev)
}

// DiffPR shows the changes in a PR relative to origin/main.
func (s *Store) DiffPR(ctx context.Context, rel string, pr int) (string, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	ref := "refs/companion/pr/" + strconv.Itoa(pr)
	spec := fmt.Sprintf("+refs/pull/%d/head:%s", pr, ref)
	if _, err := s.git(ctx, "fetch", "origin", spec); err != nil {
		return "", fmt.Errorf("fetch PR #%d: %w", pr, err)
	}
	return s.git(ctx, "diff", "--no-ext-diff", "origin/main..."+ref, "--", clean)
}

// Hash returns the blob hash that base_rev uses. The path is passed to Git so
// attributes such as text/eol normalization are applied exactly as they are for
// the working-tree file. Hashing raw stdin would produce a different object for
// CRLF checkouts and break every subsequent optimistic write.
func (s *Store) Hash(ctx context.Context, rel, content string) (string, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return "", err
	}
	out, err := gitx.RunInput(ctx, s.Mirror, []byte(content), "hash-object", "--stdin", "--path="+clean)
	return strings.TrimSpace(out), err
}

// LogWIP returns commits in a changeset worktree for a file.
func (s *Store) LogWIP(ctx context.Context, worktree, rel string) ([]Revision, error) {
	clean, err := CleanRel(rel)
	if err != nil {
		return nil, err
	}
	out, err := gitx.Run(ctx, worktree, "log", "--follow", "--format=%H%x1f%aI%x1f%an%x1f%ae%x1f%s%x1f%b%x1e", "--", clean)
	if err != nil {
		return nil, err
	}
	records := strings.Split(out, "\x1e")
	var revs []Revision
	for _, rec := range records {
		rec = strings.Trim(rec, "\n")
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, "\x1f")
		if len(fields) < 5 {
			continue
		}
		t, _ := time.Parse(time.RFC3339, fields[1])
		rev := Revision{Commit: fields[0], Date: t, Author: fields[2], Email: fields[3], Subject: fields[4]}
		if len(fields) > 5 {
			for _, line := range strings.Split(fields[5], "\n") {
				if strings.HasPrefix(line, "Companion-Source:") {
					rev.Source = strings.TrimSpace(strings.TrimPrefix(line, "Companion-Source:"))
				}
			}
		}
		revs = append(revs, rev)
	}
	for i := range revs {
		revs[i].Version = len(revs) - i
	}
	return revs, nil
}
