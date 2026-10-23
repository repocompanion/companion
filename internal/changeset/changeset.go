// Package changeset implements the unit of work: a branch, a worktree and a PR.
package changeset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bolacha/docsync/internal/codeindex"
	"github.com/bolacha/docsync/internal/config"
	"github.com/bolacha/docsync/internal/docrules"
	"github.com/bolacha/docsync/internal/forge"
	"github.com/bolacha/docsync/internal/gitx"
	"github.com/bolacha/docsync/internal/lint"
	"github.com/bolacha/docsync/internal/state"
	"github.com/bolacha/docsync/internal/store"
	"github.com/gofrs/flock"
)

var (
	ErrFrozen          = errors.New("changeset is ready for review and frozen; run `docsync change reopen` first")
	ErrBaseRevRequired = errors.New("base_rev is required when modifying an existing file")
	ErrNoChangeset     = errors.New("no active changeset")
)

type PendingError struct {
	Op  string
	Err error
}

func (e *PendingError) Error() string {
	if e.Err == nil {
		return e.Op + " is pending; local commits were kept"
	}
	return fmt.Sprintf("%s is pending; local commits were kept: %v", e.Op, e.Err)
}
func (e *PendingError) Unwrap() error { return e.Err }

func IsPending(err error) bool {
	var p *PendingError
	return errors.As(err, &p)
}

type JoinRequiredError struct {
	Area string
	PRs  []forge.PR
}

func (e *JoinRequiredError) Error() string {
	var nums []string
	for _, p := range e.PRs {
		nums = append(nums, fmt.Sprintf("#%d (%s, %s)", p.Number, p.Title, p.User))
	}
	return fmt.Sprintf("an open shared PR exists for %s: %s; join it with `docsync change join <PR#>` and then write again", e.Area, strings.Join(nums, ", "))
}

type Manager struct {
	Store *store.Store
	State *state.Store
	Cfg   config.UserConfig
	Ctx   *config.Context
	Forge *forge.Client
}

func New(s *store.Store, st *state.Store, cfg config.UserConfig, ctx *config.Context, f *forge.Client) *Manager {
	return &Manager{Store: s, State: st, Cfg: cfg, Ctx: ctx, Forge: f}
}

type Result struct {
	Change   state.Change              `json:"change"`
	Pushed   bool                      `json:"pushed"`
	PR       *forge.PR                 `json:"pr,omitempty"`
	Pending  bool                      `json:"pending"`
	Warning  string                    `json:"warning,omitempty"`
	Warnings []string                  `json:"warnings,omitempty"`
	Files    []string                  `json:"files,omitempty"`
	CodeRefs []codeindex.DocValidation `json:"code_refs,omitempty"`
}

// SubmitOptions controls the final submit step.
type SubmitOptions struct {
	SkipLint     bool
	SkipCodeRefs bool
}

// LintError is returned when changed documents fail validation. The issues are
// structured so MCP clients can show them without parsing a formatted string.
type LintError struct {
	Result lint.Result `json:"result"`
}

func (e *LintError) Error() string {
	return "submit blocked: documentation lint failed\n" + lint.Format(&e.Result)
}

// CodeRefsError is returned when changed documents reference missing paths or
// symbols. Stale/unverified references are warnings, not errors.
type CodeRefsError struct {
	Issues []codeindex.PreflightIssue `json:"issues"`
	Docs   []codeindex.DocValidation  `json:"docs"`
}

func (e *CodeRefsError) Error() string {
	var b strings.Builder
	b.WriteString("submit blocked: code-reference preflight failed")
	for _, i := range e.Issues {
		if i.Level != "error" {
			continue
		}
		fmt.Fprintf(&b, "\n  %s: %s", i.Doc, i.Message)
	}
	return b.String()
}

// Overlay is a read view of one or more areas with the active changesets
// applied. Deleted documents are absent from Docs but present in States.
type Overlay struct {
	Docs    []*store.Doc      `json:"docs"`
	States  map[string]string `json:"states"`
	Sources map[string]string `json:"sources"`
}

const (
	DocStateMain     = "main"
	DocStateAdded    = "added"
	DocStateModified = "modified"
	DocStateDeleted  = "deleted"
)

func newOverlay() *Overlay {
	return &Overlay{
		States:  map[string]string{},
		Sources: map[string]string{},
	}
}

// Overlay returns the documents visible in an area, with the active changeset
// overlaid on the main mirror.
func (m *Manager) Overlay(ctx context.Context, area string) (*Overlay, error) {
	return m.overlayArea(ctx, area)
}

func (m *Manager) overlayArea(ctx context.Context, area string) (*Overlay, error) {
	area = strings.Trim(area, "/")
	out := newOverlay()
	mirrorDocs, err := m.Store.WalkDocs(area)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]*store.Doc, len(mirrorDocs))
	for _, d := range mirrorDocs {
		byPath[d.Path] = d
		out.States[d.Path] = DocStateMain
		out.Sources[d.Path] = "main"
	}

	c, err := m.Active(area)
	if err != nil || c == nil {
		out.Docs = sortedDocs(byPath)
		return out, err
	}
	changed, err := m.changedPaths(ctx, *c)
	if err != nil {
		// A missing main ref or an old worktree should not make reads fail.
		out.Docs = sortedDocs(byPath)
		return out, nil
	}
	worktreeDocs, err := store.WalkDocsAt(c.Path, area)
	if err != nil {
		out.Docs = sortedDocs(byPath)
		return out, nil
	}
	worktreeByPath := make(map[string]*store.Doc, len(worktreeDocs))
	for _, d := range worktreeDocs {
		worktreeByPath[d.Path] = d
	}
	for p, status := range changed {
		if p != area && !strings.HasPrefix(p, area+"/") {
			continue
		}
		if status == "D" {
			delete(byPath, p)
			out.States[p] = DocStateDeleted
			out.Sources[p] = "changeset " + c.ID
			continue
		}
		d, ok := worktreeByPath[p]
		if !ok {
			continue
		}
		byPath[p] = d
		if status == "A" {
			out.States[p] = DocStateAdded
		} else {
			out.States[p] = DocStateModified
		}
		out.Sources[p] = "changeset " + c.ID
	}
	out.Docs = sortedDocs(byPath)
	return out, nil
}

// OverlayAll applies every active changeset. It is used by scope=all reads.
func (m *Manager) OverlayAll(ctx context.Context) (*Overlay, error) {
	all := newOverlay()
	docs, err := m.Store.WalkDocs("")
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
		all.Docs = append(all.Docs, d)
		all.States[d.Path] = DocStateMain
		all.Sources[d.Path] = "main"
	}

	f, err := m.load()
	if err != nil {
		return all, nil
	}
	for area := range f.Active {
		c := f.ActiveFor(area)
		if c == nil || !c.IsOpen() {
			continue
		}
		o, err := m.overlayArea(ctx, area)
		if err != nil {
			continue
		}
		filtered := all.Docs[:0]
		for _, d := range all.Docs {
			if d.Path == area || strings.HasPrefix(d.Path, area+"/") {
				continue
			}
			filtered = append(filtered, d)
		}
		all.Docs = append(filtered, o.Docs...)
		for p, s := range o.States {
			all.States[p] = s
		}
		for p, s := range o.Sources {
			all.Sources[p] = s
		}
	}
	sort.Slice(all.Docs, func(i, j int) bool { return all.Docs[i].Path < all.Docs[j].Path })
	return all, nil
}

// OverlayDir applies the overlay to the area containing dir and filters the
// result to that subtree. An empty dir means the whole store.
func (m *Manager) OverlayDir(ctx context.Context, dir string) (*Overlay, error) {
	dir = strings.Trim(strings.ReplaceAll(dir, "\\", "/"), "/")
	if dir == "" {
		return m.OverlayAll(ctx)
	}
	area, err := docrules.AreaForPath(dir)
	if err != nil {
		return m.OverlayAll(ctx)
	}
	o, err := m.overlayArea(ctx, area)
	if err != nil || dir == area {
		return o, err
	}
	filtered := newOverlay()
	for _, d := range o.Docs {
		if d.Path == dir || strings.HasPrefix(d.Path, dir+"/") {
			filtered.Docs = append(filtered.Docs, d)
			filtered.States[d.Path] = o.States[d.Path]
			filtered.Sources[d.Path] = o.Sources[d.Path]
		}
	}
	for p, s := range o.States {
		if s == DocStateDeleted && (p == dir || strings.HasPrefix(p, dir+"/")) {
			filtered.States[p] = s
			filtered.Sources[p] = o.Sources[p]
		}
	}
	return filtered, nil
}

func sortedDocs(byPath map[string]*store.Doc) []*store.Doc {
	out := make([]*store.Doc, 0, len(byPath))
	for _, d := range byPath {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (m *Manager) changedPaths(ctx context.Context, c state.Change) (map[string]string, error) {
	out, err := m.wt(ctx, c, "diff", "--name-status", "--no-renames", "main...HEAD")
	if err != nil {
		return nil, err
	}
	changed := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		status := strings.TrimSpace(parts[0])
		p := strings.TrimSpace(parts[1])
		if status == "" || p == "" {
			continue
		}
		changed[p] = status[:1]
	}
	return changed, nil
}

// ReadFileState reads a document with changeset overlay information. A deleted
// state means the file is deleted in the active changeset and should not fall
// back to main.
func (m *Manager) ReadFileState(ctx context.Context, area, rel string) (content, source, state string, err error) {
	area = strings.Trim(area, "/")
	rel = store.RelativeToArea(area, rel)
	clean, err := store.EnsureArea(area, rel)
	if err != nil {
		return "", "", "", err
	}
	o, err := m.overlayArea(ctx, area)
	if err != nil {
		return "", "", "", err
	}
	state = o.States[clean]
	source = o.Sources[clean]
	if state == "" {
		state, source = DocStateMain, "main"
	}
	if state == DocStateDeleted {
		return "", source, state, nil
	}
	for _, d := range o.Docs {
		if d.Path == clean {
			return d.Raw, source, state, nil
		}
	}
	return "", source, state, fmt.Errorf("%s not found", clean)
}

func (m *Manager) git(ctx context.Context, args ...string) (string, error) {
	all := append([]string{"--git-dir", m.Store.Bare}, args...)
	return gitx.Run(ctx, "", all...)
}

func (m *Manager) wt(ctx context.Context, c state.Change, args ...string) (string, error) {
	return gitx.Run(ctx, c.Path, args...)
}

func (m *Manager) load() (*state.File, error) { return m.State.Load() }

func (m *Manager) ensureFetchRefspec(ctx context.Context) {
	_, _ = m.git(ctx, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
}

// lockArea serializes Git work for one area across processes. It is
// deliberately per-area so unrelated changesets can still make progress.
func (m *Manager) lockArea(area string) (func(), error) {
	dir := filepath.Join(m.Store.Home, "locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	l := flock.New(filepath.Join(dir, gitx.Slug(area)+".lock"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ok, err := l.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("timed out waiting for the %s lock", area)
	}
	return func() { _ = l.Unlock() }, nil
}

func (m *Manager) save(c state.Change) error {
	c.UpdatedAt = time.Now().UTC()
	return m.State.With(func(f *state.File) error {
		for i := range f.Changes {
			if f.Changes[i].ID == c.ID {
				f.Changes[i] = c
				return nil
			}
		}
		return fmt.Errorf("changeset %s disappeared from state", c.ID)
	})
}

func (m *Manager) activate(c state.Change) error {
	c.UpdatedAt = time.Now().UTC()
	return m.State.With(func(f *state.File) error {
		for i := range f.Changes {
			if f.Changes[i].ID == c.ID {
				f.Changes[i] = c
				f.SetActive(&f.Changes[i])
				return nil
			}
		}
		return fmt.Errorf("changeset %s disappeared from state", c.ID)
	})
}

func (m *Manager) List() ([]state.Change, error) {
	f, err := m.load()
	if err != nil {
		return nil, err
	}
	out := append([]state.Change(nil), f.Changes...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

// Resolve finds a changeset by ID or, when the argument is empty, by the
// active area. An empty argument with multiple active changesets is an error;
// docsync must not guess.
func (m *Manager) Resolve(idOrArea string) (state.Change, error) {
	f, err := m.load()
	if err != nil {
		return state.Change{}, err
	}
	if idOrArea != "" {
		c, err := f.Find(idOrArea)
		if err != nil {
			return state.Change{}, err
		}
		return *c, nil
	}
	var open []state.Change
	for _, c := range f.Changes {
		if c.IsOpen() {
			open = append(open, c)
		}
	}
	if len(open) == 1 {
		return open[0], nil
	}
	if len(open) == 0 {
		return state.Change{}, ErrNoChangeset
	}
	var names []string
	for _, c := range open {
		names = append(names, fmt.Sprintf("%s (%s, %s)", c.ID, c.Area, c.Title))
	}
	return state.Change{}, fmt.Errorf("multiple changesets are active: %s; pass a changeset ID", strings.Join(names, ", "))
}

func (m *Manager) Active(area string) (*state.Change, error) {
	f, err := m.load()
	if err != nil {
		return nil, err
	}
	c := f.ActiveFor(area)
	if c == nil {
		return nil, nil
	}
	if _, err := os.Stat(c.Path); err != nil {
		return nil, nil
	}
	out := *c
	return &out, nil
}

// reconcile refreshes a changeset's PR state from GitHub when possible.
func (m *Manager) reconcile(ctx context.Context, c state.Change) (state.Change, error) {
	if m.Forge == nil || !m.Forge.Available() {
		return c, nil
	}
	var pr *forge.PR
	var err error
	if c.PR == 0 {
		pr, err = m.Forge.FindByBranch(ctx, c.Branch)
		if err == nil && pr == nil {
			return c, nil
		}
	} else {
		pr, err = m.Forge.Get(ctx, c.PR)
	}
	if err != nil {
		return c, err
	}
	stateName := pr.StateName()
	if c.PRState != stateName || c.Frozen != (stateName == "ready") {
		c.PRState = stateName
		c.Frozen = stateName == "ready"
		if err := m.save(c); err != nil {
			return c, err
		}
	}
	return c, nil
}

// ValidateArea accepts org/**, teams/<team>/** and projects/<owner>/<repo>/**.
func ValidateArea(area string) error {
	return docrules.ValidateArea(area)
}

func AreaForPath(p string) (string, error) {
	return docrules.AreaForPath(p)
}

func (m *Manager) Start(ctx context.Context, title, area string) (state.Change, error) {
	area = strings.Trim(area, "/")
	if err := ValidateArea(area); err != nil {
		return state.Change{}, err
	}
	if title == "" {
		title = "docs updates"
	}
	prefix := m.Cfg.Changesets.BranchPrefix
	if prefix == "" {
		prefix = "docs"
	}
	user := m.Ctx.BranchUser()
	areaSlug := gitx.AreaSlug(area)
	topic := gitx.Slug(title)
	branch := fmt.Sprintf("%s/%s/%s/%s", prefix, user, areaSlug, topic)
	worktree := filepath.Join(m.Store.Changes, areaSlug+"--"+topic)

	if _, err := m.git(ctx, "rev-parse", "--verify", "refs/heads/"+branch); err == nil {
		// `change start` means a new topic, not reattaching to an old branch.
		for i := 2; i < 100; i++ {
			candidate := fmt.Sprintf("%s-%d", branch, i)
			if _, err := m.git(ctx, "rev-parse", "--verify", "refs/heads/"+candidate); err != nil {
				branch = candidate
				break
			}
		}
	}
	for i := 1; ; i++ {
		p := worktree
		if i > 1 {
			p = fmt.Sprintf("%s-%d", worktree, i)
		}
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			worktree = p
			break
		}
	}
	if err := os.MkdirAll(m.Store.Changes, 0o755); err != nil {
		return state.Change{}, err
	}

	if _, err := m.git(ctx, "worktree", "add", "-b", branch, worktree, "main"); err != nil {
		return state.Change{}, fmt.Errorf("create worktree: %w", err)
	}
	now := time.Now().UTC()
	c := state.Change{
		ID:        state.NewID(),
		Title:     title,
		Area:      area,
		Branch:    branch,
		Path:      worktree,
		PRState:   "none",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := m.State.With(func(f *state.File) error {
		f.Add(c)
		f.SetActive(&f.Changes[len(f.Changes)-1])
		return nil
	}); err != nil {
		return state.Change{}, err
	}
	return c, nil
}

// Ensure returns the active changeset for an area, auto-creating one when
// configured. It checks shared PRs first and never silently joins another
// author's PR.
func (m *Manager) Ensure(ctx context.Context, area string) (state.Change, error) {
	return m.EnsureNamed(ctx, area, "docs updates")
}

// EnsureNamed is Ensure with a useful title for the auto-created changeset.
func (m *Manager) EnsureNamed(ctx context.Context, area, title string) (state.Change, error) {
	if c, err := m.Active(area); err != nil {
		return state.Change{}, err
	} else if c != nil {
		c2, err := m.reconcile(ctx, *c)
		if err == nil {
			c = &c2
		}
		if c.IsOpen() {
			if c.IsReady() {
				return state.Change{}, ErrFrozen
			}
			return *c, nil
		}
	}
	auto := true
	if m.Cfg.Changesets.AutoCreate != nil {
		auto = *m.Cfg.Changesets.AutoCreate
	}
	if !auto {
		return state.Change{}, fmt.Errorf("no active changeset for %s and changesets.auto_create is false", area)
	}
	if m.Forge != nil && m.Forge.Available() {
		if prs, err := m.Forge.ListOpenForArea(ctx, area); err == nil && len(prs) > 0 {
			if len(prs) == 1 {
				if login, err := m.Forge.CurrentLogin(ctx); err == nil && login != "" && strings.EqualFold(login, prs[0].User) {
					return m.Join(ctx, prs[0].Number)
				}
			}
			return state.Change{}, &JoinRequiredError{Area: area, PRs: prs}
		}
	}
	if title == "" {
		title = "docs updates"
	}
	if title == "docs updates" && m.Ctx != nil && m.Ctx.Project != "" {
		title = "docs updates for " + m.Ctx.Project
	}
	return m.Start(ctx, title, area)
}

func (m *Manager) Join(ctx context.Context, number int) (state.Change, error) {
	if m.Forge == nil || !m.Forge.Available() {
		return state.Change{}, errors.New("joining a shared PR requires GitHub authentication")
	}
	pr, err := m.Forge.Get(ctx, number)
	if err != nil {
		return state.Change{}, err
	}
	if pr.State != "open" {
		return state.Change{}, fmt.Errorf("PR #%d is %s, not open", number, pr.StateName())
	}
	f, err := m.load()
	if err != nil {
		return state.Change{}, err
	}
	for _, c := range f.Changes {
		if c.PR == number {
			if _, statErr := os.Stat(c.Path); statErr == nil {
				if err := m.activate(c); err != nil {
					return state.Change{}, err
				}
				return c, nil
			}
			_ = m.State.With(func(sf *state.File) error {
				sf.Remove(c.ID)
				return nil
			})
			break
		}
	}
	area := forge.AreaFromBody(pr.Body)
	if area == "" {
		area = m.Ctx.ProjectArea()
	}
	if area == "" {
		return state.Change{}, fmt.Errorf("cannot determine the area for PR #%d; add a Docsync-Area line to its body or pass a project", number)
	}
	if err := ValidateArea(area); err != nil {
		return state.Change{}, err
	}
	if _, err := m.git(ctx, "fetch", "origin", pr.Head+":refs/remotes/origin/"+pr.Head); err != nil {
		return state.Change{}, fmt.Errorf("fetch PR branch: %w", err)
	}
	// If a local branch already exists, attach it; otherwise create it from the
	// fetched remote branch.
	localExists := true
	if _, err := m.git(ctx, "rev-parse", "--verify", "refs/heads/"+pr.Head); err != nil {
		localExists = false
	}
	areaSlug := gitx.AreaSlug(area)
	topic := gitx.Slug(path.Base(pr.Head))
	worktree := filepath.Join(m.Store.Changes, areaSlug+"--"+topic)
	if _, err := os.Stat(worktree); err == nil {
		return state.Change{}, fmt.Errorf("worktree already exists at %s", worktree)
	}
	if err := os.MkdirAll(m.Store.Changes, 0o755); err != nil {
		return state.Change{}, err
	}
	if localExists {
		if _, err := m.git(ctx, "worktree", "add", worktree, pr.Head); err != nil {
			return state.Change{}, err
		}
	} else {
		if _, err := m.git(ctx, "worktree", "add", "-b", pr.Head, worktree, "origin/"+pr.Head); err != nil {
			return state.Change{}, err
		}
	}
	now := time.Now().UTC()
	c := state.Change{
		ID:        state.NewID(),
		Title:     pr.Title,
		Area:      area,
		Branch:    pr.Head,
		Path:      worktree,
		PR:        pr.Number,
		PRState:   pr.StateName(),
		Frozen:    pr.StateName() == "ready",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := m.State.With(func(f *state.File) error {
		f.Add(c)
		f.SetActive(&f.Changes[len(f.Changes)-1])
		return nil
	}); err != nil {
		return state.Change{}, err
	}
	return c, nil
}

func (m *Manager) Switch(idOrArea string) (state.Change, error) {
	f, err := m.load()
	if err != nil {
		return state.Change{}, err
	}
	c, err := f.Find(idOrArea)
	if err != nil {
		return state.Change{}, err
	}
	if _, err := os.Stat(c.Path); err != nil {
		return state.Change{}, fmt.Errorf("changeset worktree is missing: %s", c.Path)
	}
	if err := m.activate(*c); err != nil {
		return state.Change{}, err
	}
	return *c, nil
}

func (m *Manager) files(ctx context.Context, c state.Change) []string {
	out, err := m.wt(ctx, c, "diff", "--name-only", "main...HEAD")
	if err != nil {
		return nil
	}
	var files []string
	for _, f := range strings.Split(out, "\n") {
		if f = strings.TrimSpace(f); f != "" {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	return files
}

func scanSecrets(data string) bool {
	return regexp.MustCompile(`(?i)(aws_secret_access_key\s*=|-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----|ghp_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{20,}|xox[baprs]-[A-Za-z0-9-]{10,})`).MatchString(data)
}

func (m *Manager) commitIfNeeded(ctx context.Context, c state.Change, source string) (bool, error) {
	status, err := m.wt(ctx, c, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(status) == "" {
		return false, nil
	}
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 {
			continue
		}
		rel := strings.TrimSpace(line[3:])
		rel = strings.TrimPrefix(rel, "\"")
		rel = strings.TrimSuffix(rel, "\"")
		full := filepath.Join(c.Path, filepath.FromSlash(rel))
		if b, err := os.ReadFile(full); err == nil {
			if scanSecrets(string(b)) {
				return false, fmt.Errorf("possible secret in %s; refusing to commit", rel)
			}
		}
	}
	if _, err := m.wt(ctx, c, "add", "-A"); err != nil {
		return false, err
	}
	short := path.Base(c.Area)
	if short == "" || short == "." {
		short = c.Area
	}
	msg := fmt.Sprintf("docs(%s): %s\n\nDocsync-Source: %s\nDocsync-Machine: %s", short, c.Title, source, machine(m.Ctx))
	if _, err := m.wt(ctx, c, "commit", "-m", msg); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	return true, nil
}

func machine(ctx *config.Context) string {
	if ctx == nil || ctx.Machine == "" {
		return "unknown"
	}
	return ctx.Machine
}

// validateUniqueIDLocked checks the active worktree and the main mirror while
// the area lock is held. The worktree is checked first so a collision between
// two files in the same changeset is caught before either one is committed.
func (m *Manager) validateUniqueIDLocked(ctx context.Context, c state.Change, area, rel, content string, ignore ...string) error {
	doc, err := store.ParseDoc(rel, content)
	if err != nil || doc.Frontmatter.ID == "" {
		return nil
	}
	ignored := map[string]bool{rel: true}
	for _, p := range ignore {
		ignored[p] = true
	}
	roots := []string{c.Path, m.Store.Mirror}
	for _, root := range roots {
		docs, err := store.WalkDocsAt(root, area)
		if err != nil {
			continue
		}
		for _, existing := range docs {
			if !ignored[existing.Path] && existing.Frontmatter.ID == doc.Frontmatter.ID {
				return fmt.Errorf("id %q already exists at %s; use a unique id or update that document", doc.Frontmatter.ID, existing.Path)
			}
		}
	}
	return nil
}

func (m *Manager) Sync(ctx context.Context, idOrArea, source string) (Result, error) {
	c, err := m.Resolve(idOrArea)
	if err != nil {
		return Result{}, err
	}
	unlock, err := m.lockArea(c.Area)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	return m.syncLocked(ctx, c, source)
}

func (m *Manager) syncLocked(ctx context.Context, c state.Change, source string) (Result, error) {
	if c.ID == "" {
		return Result{}, ErrNoChangeset
	}
	var err error
	if !c.IsOpen() {
		return Result{}, fmt.Errorf("changeset %s is %s; run `docsync change cleanup`", c.ID, c.PRState)
	}
	if c.IsReady() {
		return Result{}, ErrFrozen
	}
	if _, err := os.Stat(c.Path); err != nil {
		return Result{}, fmt.Errorf("changeset worktree is missing: %s", c.Path)
	}
	if _, err := m.commitIfNeeded(ctx, c, source); err != nil {
		return Result{}, err
	}

	res := Result{Change: c, Files: m.files(ctx, c)}

	// Fetch and reconcile the PR before pushing: a collaborator may have marked
	// the PR ready, or a host-side edit may have landed.
	if m.Forge != nil && m.Forge.Available() {
		pr := (*forge.PR)(nil)
		var prErr error
		if c.PR > 0 {
			pr, prErr = m.Forge.Get(ctx, c.PR)
		} else {
			pr, prErr = m.Forge.FindByBranch(ctx, c.Branch)
		}
		if prErr != nil {
			c.NeedsSync = true
			c.Error = prErr.Error()
			_ = m.save(c)
			res.Change, res.Pending = c, true
			return res, &PendingError{Op: "refresh PR", Err: prErr}
		}
		if pr != nil {
			c.PR = pr.Number
			c.PRState = pr.StateName()
			c.Frozen = pr.StateName() == "ready"
			if pr.State != "open" {
				_ = m.save(c)
				return res, fmt.Errorf("PR #%d is %s; run `docsync change cleanup`", pr.Number, pr.StateName())
			}
			if c.IsReady() {
				_ = m.save(c)
				return res, ErrFrozen
			}
		}
	}

	m.ensureFetchRefspec(ctx)
	if _, err := m.git(ctx, "fetch", "--prune", "origin"); err != nil {
		c.NeedsSync = true
		c.Error = err.Error()
		_ = m.save(c)
		res.Change, res.Pending = c, true
		return res, &PendingError{Op: "sync fetch", Err: err}
	}
	remoteRef := "refs/remotes/origin/" + c.Branch
	if _, err := m.git(ctx, "rev-parse", "--verify", remoteRef); err == nil {
		if _, err := m.wt(ctx, c, "rebase", "origin/"+c.Branch); err != nil {
			_, _ = m.wt(ctx, c, "rebase", "--abort")
			c.Error = "needs-rebase: remote branch and local work conflict"
			c.NeedsSync = true
			_ = m.save(c)
			return res, fmt.Errorf("rebase conflict with origin/%s; resolve with `docsync change rebase`", c.Branch)
		}
	}
	remoteExists := true
	if _, err := m.git(ctx, "rev-parse", "--verify", remoteRef); err != nil {
		remoteExists = false
	}
	res.Files = m.files(ctx, c)
	if remoteExists {
		_, err = m.wt(ctx, c, "push", "--force-with-lease", "origin", "HEAD:"+c.Branch)
	} else {
		_, err = m.wt(ctx, c, "push", "-u", "origin", "HEAD:"+c.Branch)
	}
	if err != nil {
		c.NeedsSync = true
		c.Error = err.Error()
		_ = m.save(c)
		res.Change, res.Pending = c, true
		return res, &PendingError{Op: "sync push", Err: err}
	}
	res.Pushed = true
	c.NeedsSync = false
	c.Error = ""

	if m.Forge == nil || !m.Forge.Available() {
		res.Warning = "branch pushed, but no GitHub token is available; draft PR was not created/updated"
		_ = m.save(c)
		res.Change = c
		return res, nil
	}

	pr := (*forge.PR)(nil)
	var prErr error
	if c.PR > 0 {
		pr, prErr = m.Forge.Get(ctx, c.PR)
	} else {
		pr, prErr = m.Forge.FindByBranch(ctx, c.Branch)
	}
	if prErr != nil {
		c.NeedsSync = true
		c.Error = prErr.Error()
		_ = m.save(c)
		res.Change, res.Pending = c, true
		return res, &PendingError{Op: "refresh PR after push", Err: prErr}
	}
	body := forge.ManagedBody("", c.ID, c.Area, res.Files)
	if pr == nil {
		title := fmt.Sprintf("docs(%s): %s", path.Base(c.Area), c.Title)
		pr, err = m.Forge.CreateDraft(ctx, title, c.Branch, "main", body)
		if err != nil {
			c.NeedsSync = true
			c.Error = err.Error()
			_ = m.save(c)
			res.Change, res.Pending = c, true
			return res, &PendingError{Op: "create PR", Err: err}
		}
	} else {
		c.PR = pr.Number
		if pr.State != "open" {
			c.PRState = pr.StateName()
			_ = m.save(c)
			return res, fmt.Errorf("PR #%d is %s", pr.Number, pr.StateName())
		}
		if pr.StateName() == "ready" {
			c.PRState, c.Frozen = "ready", true
			_ = m.save(c)
			return res, ErrFrozen
		}
		body = forge.ManagedBody(pr.Body, c.ID, c.Area, res.Files)
		if _, err := m.Forge.Update(ctx, pr.Number, pr.Title, body); err != nil {
			res.Warning = "PR body update failed: " + err.Error()
		}
	}
	c.PR = pr.Number
	c.PRState = "draft"
	c.Frozen = false
	_ = m.save(c)
	res.Change = c
	res.PR = pr
	return res, nil
}

func (m *Manager) Submit(ctx context.Context, idOrArea, summary, source string) (Result, error) {
	return m.SubmitWithOptions(ctx, idOrArea, summary, source, SubmitOptions{})
}

func (m *Manager) SubmitWithOptions(ctx context.Context, idOrArea, summary, source string, opts SubmitOptions) (Result, error) {
	c, err := m.Resolve(idOrArea)
	if err != nil {
		return Result{}, err
	}
	unlock, err := m.lockArea(c.Area)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	if c.IsReady() {
		return Result{}, ErrFrozen
	}
	res, err := m.syncLocked(ctx, c, source)
	if err != nil {
		return res, err
	}
	c = res.Change
	if !opts.SkipLint {
		if err := m.lintChangeset(ctx, c); err != nil {
			return res, err
		}
	}
	if !opts.SkipCodeRefs {
		refs, warnings, err := m.validateCodeRefs(ctx, c)
		res.CodeRefs = refs
		res.Warnings = append(res.Warnings, warnings...)
		if err != nil {
			return res, err
		}
	}
	if m.Forge == nil || !m.Forge.Available() {
		return res, errors.New("cannot submit without GitHub authentication")
	}
	if c.PR == 0 {
		return res, errors.New("cannot submit: no PR was created")
	}
	pr, err := m.Forge.Get(ctx, c.PR)
	if err != nil {
		return res, err
	}
	if pr.State != "open" {
		return res, fmt.Errorf("PR #%d is %s", pr.Number, pr.StateName())
	}

	// Rebase on main only if needed. Never do this silently during normal sync.
	if _, err := m.git(ctx, "fetch", "origin", "main"); err != nil {
		return res, &PendingError{Op: "submit fetch main", Err: err}
	}
	if _, err := m.wt(ctx, c, "merge-base", "--is-ancestor", "origin/main", "HEAD"); err != nil {
		if _, err := m.wt(ctx, c, "rebase", "origin/main"); err != nil {
			_, _ = m.wt(ctx, c, "rebase", "--abort")
			c.Error = "needs-rebase: conflicts with main"
			c.NeedsSync = true
			_ = m.save(c)
			return res, fmt.Errorf("rebase on main conflicted; branch left untouched, PR remains draft")
		}
		if _, err := m.wt(ctx, c, "push", "--force-with-lease", "origin", "HEAD:"+c.Branch); err != nil {
			c.NeedsSync = true
			c.Error = err.Error()
			_ = m.save(c)
			return res, &PendingError{Op: "submit push", Err: err}
		}
		res.Pushed = true
		res.Files = m.files(ctx, c)
	}

	title := pr.Title
	if strings.TrimSpace(summary) != "" {
		title = fmt.Sprintf("docs(%s): %s", path.Base(c.Area), summary)
	}
	body := forge.ManagedBody(pr.Body, c.ID, c.Area, res.Files)
	if _, err := m.Forge.Update(ctx, c.PR, title, body); err != nil {
		return res, err
	}
	if err := m.Forge.SetDraft(ctx, c.PR, false); err != nil {
		return res, err
	}
	c.PRState = "ready"
	c.Frozen = true
	c.NeedsSync = false
	c.Error = ""
	_ = m.save(c)
	res.Change = c
	return res, nil
}

func (m *Manager) lintChangeset(ctx context.Context, c state.Change) error {
	files := m.files(ctx, c)
	if len(files) == 0 {
		return nil
	}
	res, err := lint.RunFiles(c.Path, files)
	if err != nil {
		return err
	}
	hasErrors := false
	for _, issue := range res.Issues {
		if issue.Level == "error" {
			hasErrors = true
			break
		}
	}
	if hasErrors {
		return &LintError{Result: *res}
	}
	return nil
}

// validateCodeRefs checks changed docs against the current code repository.
// It returns warnings for advisory states and an error for missing paths or
// missing symbols that a provider claimed to support.
func (m *Manager) validateCodeRefs(ctx context.Context, c state.Change) ([]codeindex.DocValidation, []string, error) {
	files := m.files(ctx, c)
	if len(files) == 0 {
		return nil, nil, nil
	}
	docs, err := lint.LoadDocs(c.Path, files)
	if err != nil {
		return nil, nil, err
	}
	hasRefs := false
	for _, d := range docs {
		if d != nil && len(d.Frontmatter.ReferenceImpls) > 0 {
			hasRefs = true
			break
		}
	}
	if !hasRefs {
		return nil, nil, nil
	}
	root, project := "", ""
	if m.Ctx != nil {
		root, project = m.Ctx.RepoRoot, m.Ctx.Project
	}
	if strings.TrimSpace(root) == "" {
		return nil, []string{"code references were not validated: no code repository detected"}, nil
	}
	validated, err := codeindex.ValidateDocs(ctx, root, project, docs)
	if err != nil {
		return nil, nil, err
	}
	issues := codeindex.Preflight(validated)
	if codeindex.HasPreflightErrors(issues) {
		return validated, preflightWarnings(issues), &CodeRefsError{Issues: issues, Docs: validated}
	}
	return validated, preflightWarnings(issues), nil
}

func preflightWarnings(issues []codeindex.PreflightIssue) []string {
	var out []string
	for _, i := range issues {
		if i.Level == "warning" {
			out = append(out, fmt.Sprintf("%s: %s", i.Doc, i.Message))
		}
	}
	return out
}

func (m *Manager) Reopen(ctx context.Context, idOrArea string) (state.Change, error) {
	c, err := m.Resolve(idOrArea)
	if err != nil {
		return state.Change{}, err
	}
	unlock, err := m.lockArea(c.Area)
	if err != nil {
		return state.Change{}, err
	}
	defer unlock()
	if c.PR == 0 {
		return state.Change{}, errors.New("changeset has no PR")
	}
	if m.Forge == nil || !m.Forge.Available() {
		return state.Change{}, errors.New("reopening a PR requires GitHub authentication")
	}
	if err := m.Forge.SetDraft(ctx, c.PR, true); err != nil {
		return state.Change{}, err
	}
	c.PRState = "draft"
	c.Frozen = false
	if err := m.save(c); err != nil {
		return state.Change{}, err
	}
	return c, nil
}

func (m *Manager) Rebase(ctx context.Context, idOrArea string) (state.Change, error) {
	c, err := m.Resolve(idOrArea)
	if err != nil {
		return state.Change{}, err
	}
	unlock, err := m.lockArea(c.Area)
	if err != nil {
		return state.Change{}, err
	}
	defer unlock()
	m.ensureFetchRefspec(ctx)
	if _, err := m.git(ctx, "fetch", "--prune", "origin"); err != nil {
		return state.Change{}, err
	}
	if _, err := m.git(ctx, "rev-parse", "--verify", "refs/remotes/origin/"+c.Branch); err == nil {
		if _, err := m.wt(ctx, c, "rebase", "origin/"+c.Branch); err != nil {
			_, _ = m.wt(ctx, c, "rebase", "--abort")
			c.Error = "needs-rebase"
			_ = m.save(c)
			return state.Change{}, fmt.Errorf("conflict with origin/%s; resolve manually in %s", c.Branch, c.Path)
		}
	}
	if _, err := m.wt(ctx, c, "rebase", "origin/main"); err != nil {
		_, _ = m.wt(ctx, c, "rebase", "--abort")
		c.Error = "needs-rebase with main"
		_ = m.save(c)
		return state.Change{}, fmt.Errorf("conflict with main; resolve manually in %s", c.Path)
	}
	c.Error = ""
	_ = m.save(c)
	return c, nil
}

func (m *Manager) Cleanup(ctx context.Context, idOrArea string, archive, force bool) (state.Change, error) {
	c, err := m.Resolve(idOrArea)
	if err != nil {
		return state.Change{}, err
	}
	unlock, err := m.lockArea(c.Area)
	if err != nil {
		return state.Change{}, err
	}
	defer unlock()
	if c2, err := m.reconcile(ctx, c); err == nil {
		c = c2
	}
	if c.IsOpen() && c.PR != 0 {
		return state.Change{}, fmt.Errorf("PR #%d is still open", c.PR)
	}
	if c.PR == 0 && !force {
		return state.Change{}, errors.New("changeset has no PR; use change abandon --force to discard it")
	}
	unpublished, err := m.unpublished(ctx, c)
	if err != nil {
		return state.Change{}, err
	}
	if unpublished && !archive && !force {
		return state.Change{}, errors.New("changeset has unpushed local work; use --archive to keep a patch or --force to discard")
	}
	if unpublished && archive {
		if err := m.archive(ctx, c); err != nil {
			return state.Change{}, err
		}
	}
	if err := m.remove(ctx, c); err != nil {
		return state.Change{}, err
	}
	return c, nil
}

func (m *Manager) Abandon(ctx context.Context, idOrArea string, archive, force bool) (state.Change, error) {
	c, err := m.Resolve(idOrArea)
	if err != nil {
		return state.Change{}, err
	}
	unlock, err := m.lockArea(c.Area)
	if err != nil {
		return state.Change{}, err
	}
	defer unlock()
	if c2, err := m.reconcile(ctx, c); err == nil {
		c = c2
	}
	unpublished, _ := m.unpublished(ctx, c)
	if unpublished && !archive && !force {
		return state.Change{}, errors.New("changeset has unpushed local work; use --archive to keep a patch or --force to discard")
	}
	if c.PR > 0 && c.IsOpen() {
		if m.Forge == nil || !m.Forge.Available() {
			return state.Change{}, errors.New("closing the PR requires GitHub authentication; the worktree was not removed")
		}
		if err := m.Forge.Close(ctx, c.PR); err != nil {
			return state.Change{}, err
		}
	}
	if unpublished && archive {
		if err := m.archive(ctx, c); err != nil {
			return state.Change{}, err
		}
	}
	if err := m.remove(ctx, c); err != nil {
		return state.Change{}, err
	}
	return c, nil
}

func (m *Manager) unpublished(ctx context.Context, c state.Change) (bool, error) {
	status, err := m.wt(ctx, c, "status", "--porcelain")
	if err != nil {
		return false, nil
	}
	if strings.TrimSpace(status) != "" {
		return true, nil
	}
	if _, err := m.git(ctx, "rev-parse", "--verify", "refs/remotes/origin/"+c.Branch); err == nil {
		out, err := m.wt(ctx, c, "rev-list", "--count", "origin/"+c.Branch+"..HEAD")
		if err != nil {
			return true, nil
		}
		return strings.TrimSpace(out) != "0", nil
	}
	out, err := m.wt(ctx, c, "rev-list", "--count", "main..HEAD")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(out) != "0", nil
}

func (m *Manager) archive(ctx context.Context, c state.Change) error {
	dir := filepath.Join(m.Store.Home, "archive")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	out, err := m.wt(ctx, c, "diff", "--binary", "main...HEAD")
	if err != nil {
		return err
	}
	// Include uncommitted and untracked work in the archive.
	_, _ = m.wt(ctx, c, "add", "-N", ".")
	status, _ := m.wt(ctx, c, "diff", "--binary")
	_, _ = m.wt(ctx, c, "reset")
	return os.WriteFile(filepath.Join(dir, c.ID+".patch"), []byte(out+"\n"+status), 0o600)
}

func (m *Manager) remove(ctx context.Context, c state.Change) error {
	_, _ = m.git(ctx, "worktree", "remove", "--force", c.Path)
	_ = os.RemoveAll(c.Path)
	_, _ = m.git(ctx, "branch", "-D", c.Branch)
	return m.State.With(func(f *state.File) error {
		f.Remove(c.ID)
		if f.Active[c.Area] == "" {
			for i := len(f.Changes) - 1; i >= 0; i-- {
				if f.Changes[i].Area == c.Area && f.Changes[i].IsOpen() {
					f.Active[c.Area] = f.Changes[i].ID
					break
				}
			}
		}
		return nil
	})
}

func (m *Manager) Adopt(ctx context.Context, source string) (state.Change, error) {
	status, err := gitx.Run(ctx, m.Store.Mirror, "status", "--porcelain")
	if err != nil {
		return state.Change{}, err
	}
	if strings.TrimSpace(status) == "" {
		return state.Change{}, errors.New("the main mirror has no changes to adopt")
	}
	if strings.Contains(status, "??") {
		return state.Change{}, errors.New("untracked files in the main mirror cannot be adopted safely; move them manually into a changeset")
	}
	names, err := gitx.Run(ctx, m.Store.Mirror, "diff", "--name-only")
	if err != nil {
		return state.Change{}, err
	}
	var files []string
	var area string
	for _, f := range strings.Split(names, "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		a, err := AreaForPath(f)
		if err != nil {
			return state.Change{}, err
		}
		if area == "" {
			area = a
		} else if area != a {
			return state.Change{}, fmt.Errorf("main mirror changes span multiple areas (%s, %s); adopt one area at a time", area, a)
		}
		files = append(files, f)
	}
	if area == "" {
		return state.Change{}, errors.New("no tracked changes to adopt")
	}
	patch, err := gitx.RunRaw(ctx, m.Store.Mirror, "diff", "--binary")
	if err != nil {
		return state.Change{}, err
	}
	c, err := m.Start(ctx, "adopt main mirror edits", area)
	if err != nil {
		return state.Change{}, err
	}
	if _, err := gitx.RunInput(ctx, c.Path, []byte(patch), "apply", "--3way"); err != nil {
		return state.Change{}, fmt.Errorf("apply patch to changeset: %w", err)
	}
	if err := m.Store.MakeWritable(); err != nil {
		return state.Change{}, err
	}
	_, err = gitx.Run(ctx, m.Store.Mirror, "checkout", "--", ".")
	_ = m.Store.MakeReadOnly()
	if err != nil {
		return state.Change{}, fmt.Errorf("restore main mirror: %w", err)
	}
	if _, err := m.commitIfNeeded(ctx, c, source); err != nil {
		return state.Change{}, err
	}
	return c, nil
}

// WriteFile writes a doc, validates base_rev, and commits it locally.
func (m *Manager) WriteFile(ctx context.Context, area, rel, content, baseRev, source string) (state.Change, bool, error) {
	unlock, err := m.lockArea(area)
	if err != nil {
		return state.Change{}, false, err
	}
	defer unlock()
	c, err := m.Ensure(ctx, area)
	if err != nil {
		return state.Change{}, false, err
	}
	rel = store.RelativeToArea(area, rel)
	clean, err := store.EnsureArea(area, rel)
	if err != nil {
		return state.Change{}, false, err
	}
	full, err := safeWorktreePath(c.Path, clean)
	if err != nil {
		return state.Change{}, false, err
	}
	exists := false
	if _, err := os.Stat(full); err == nil {
		exists = true
	}
	if exists {
		current, err := os.ReadFile(full)
		if err != nil {
			return state.Change{}, false, err
		}
		if baseRev == "" {
			return c, false, &BaseRevError{Path: clean, Content: string(current)}
		}
		hash, err := gitx.Run(ctx, c.Path, "hash-object", "--", full)
		if err != nil {
			return c, false, err
		}
		if hash != baseRev {
			return c, false, &BaseRevError{Path: clean, Content: string(current)}
		}
	} else if baseRev != "" {
		return c, false, fmt.Errorf("base_rev was supplied for a new file: %s", clean)
	}
	if int64(len(content)) > m.Cfg.MCP.MaxFileSize && m.Cfg.MCP.MaxFileSize > 0 {
		return c, false, fmt.Errorf("file exceeds max size of %d bytes", m.Cfg.MCP.MaxFileSize)
	}
	if scanSecrets(content) {
		return c, false, fmt.Errorf("possible secret in %s; refusing to write", clean)
	}
	if err := m.validateUniqueIDLocked(ctx, c, area, clean, content); err != nil {
		return c, false, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return c, false, err
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return c, false, err
	}
	if _, err := m.commitIfNeeded(ctx, c, source); err != nil {
		return c, false, err
	}
	return c, true, nil
}

type BaseRevError struct {
	Path    string
	Content string
}

func (e *BaseRevError) Error() string {
	return fmt.Sprintf("base_rev is required or stale for %s; latest content follows", e.Path)
}

type Edit struct {
	Old string `json:"old"`
	New string `json:"new"`
}

// MoveFile renames a document with git mv so history is preserved. The
// destination must be canonical and must not exist. baseRev guards the source
// against a concurrent edit.
func (m *Manager) MoveFile(ctx context.Context, area, oldRel, newRel, baseRev, source string) (state.Change, error) {
	unlock, err := m.lockArea(area)
	if err != nil {
		return state.Change{}, err
	}
	defer unlock()
	c, err := m.Ensure(ctx, area)
	if err != nil {
		return state.Change{}, err
	}
	oldRel = store.RelativeToArea(area, oldRel)
	newRel = store.RelativeToArea(area, newRel)
	oldClean, err := store.EnsureArea(area, oldRel)
	if err != nil {
		return state.Change{}, err
	}
	newClean, err := store.EnsureArea(area, newRel)
	if err != nil {
		return state.Change{}, err
	}
	if oldClean == newClean {
		return state.Change{}, errors.New("source and destination are the same path")
	}
	oldFull, err := safeWorktreePath(c.Path, oldClean)
	if err != nil {
		return state.Change{}, err
	}
	newFull, err := safeWorktreePath(c.Path, newClean)
	if err != nil {
		return state.Change{}, err
	}
	fi, err := os.Stat(oldFull)
	if err != nil {
		return state.Change{}, fmt.Errorf("%s is not in changeset %s", oldClean, c.ID)
	}
	if fi.IsDir() {
		return state.Change{}, fmt.Errorf("%s is a directory, not a document", oldClean)
	}
	if _, err := os.Lstat(newFull); err == nil {
		return state.Change{}, fmt.Errorf("destination already exists: %s", newClean)
	} else if !errors.Is(err, os.ErrNotExist) {
		return state.Change{}, err
	}
	content, err := os.ReadFile(oldFull)
	if err != nil {
		return state.Change{}, err
	}
	if baseRev == "" {
		return c, &BaseRevError{Path: oldClean, Content: string(content)}
	}
	hash, err := gitx.Run(ctx, c.Path, "hash-object", "--", oldFull)
	if err != nil {
		return c, err
	}
	if hash != baseRev {
		return c, &BaseRevError{Path: oldClean, Content: string(content)}
	}
	problems, err := docrules.ValidateMarkdown(newClean, string(content))
	if err != nil {
		return c, err
	}
	if len(problems) > 0 {
		return c, fmt.Errorf("cannot move to %s: %s", newClean, problems[0].Message)
	}
	if err := m.validateUniqueIDLocked(ctx, c, area, newClean, string(content), oldClean); err != nil {
		return c, err
	}
	if err := os.MkdirAll(filepath.Dir(newFull), 0o755); err != nil {
		return c, err
	}
	if _, err := gitx.Run(ctx, c.Path, "mv", "--", oldClean, newClean); err != nil {
		return c, err
	}
	if _, err := m.commitIfNeeded(ctx, c, source); err != nil {
		return c, err
	}
	return c, nil
}

// PatchFile applies find/replace edits and commits the result.
func (m *Manager) PatchFile(ctx context.Context, area, rel, baseRev string, edits []Edit, source string) (state.Change, error) {
	unlock, err := m.lockArea(area)
	if err != nil {
		return state.Change{}, err
	}
	defer unlock()
	c, err := m.Ensure(ctx, area)
	if err != nil {
		return state.Change{}, err
	}
	rel = store.RelativeToArea(area, rel)
	clean, err := store.EnsureArea(area, rel)
	if err != nil {
		return state.Change{}, err
	}
	full, err := safeWorktreePath(c.Path, clean)
	if err != nil {
		return state.Change{}, err
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return state.Change{}, err
	}
	if baseRev == "" {
		return state.Change{}, &BaseRevError{Path: clean, Content: string(b)}
	}
	hash, err := gitx.Run(ctx, c.Path, "hash-object", "--", full)
	if err != nil {
		return state.Change{}, err
	}
	if hash != baseRev {
		return state.Change{}, &BaseRevError{Path: clean, Content: string(b)}
	}
	content := string(b)
	for _, e := range edits {
		if e.Old == "" {
			return state.Change{}, errors.New("patch edit has an empty old block")
		}
		if !strings.Contains(content, e.Old) {
			return state.Change{}, fmt.Errorf("patch block not found in %s", clean)
		}
		content = strings.Replace(content, e.Old, e.New, 1)
	}
	if err := m.validateUniqueIDLocked(ctx, c, area, clean, content); err != nil {
		return state.Change{}, err
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return state.Change{}, err
	}
	if _, err := m.commitIfNeeded(ctx, c, source); err != nil {
		return state.Change{}, err
	}
	return c, nil
}

// ReadFile reads a file from the active changeset or the main mirror.
func (m *Manager) ReadFile(ctx context.Context, area, rel string) (string, string, error) {
	c, err := m.Active(area)
	if err == nil && c != nil {
		rel = store.RelativeToArea(area, rel)
		clean, err := store.EnsureArea(area, rel)
		if err != nil {
			return "", "", err
		}
		full, err := safeWorktreePath(c.Path, clean)
		if err != nil {
			return "", "", err
		}
		if b, err := os.ReadFile(full); err == nil {
			return string(b), "changeset " + c.ID, nil
		}
	}
	rel = store.RelativeToArea(area, rel)
	clean, err := store.EnsureArea(area, rel)
	if err != nil {
		return "", "", err
	}
	b, err := m.Store.Read(clean)
	if err != nil {
		return "", "", err
	}
	return b, "main", nil
}

func safeWorktreePath(root, rel string) (string, error) {
	clean, err := store.CleanRel(rel)
	if err != nil {
		return "", err
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if abs != rootAbs && !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes the changeset: %s", rel)
	}
	rootResolved, rerr := filepath.EvalSymlinks(rootAbs)
	if rerr != nil {
		rootResolved = rootAbs
	}
	if resolved, serr := filepath.EvalSymlinks(abs); serr == nil {
		if resolved != rootResolved && !strings.HasPrefix(resolved, rootResolved+string(os.PathSeparator)) {
			return "", fmt.Errorf("path escapes the changeset through a symlink: %s", rel)
		}
	}
	parent := filepath.Dir(abs)
	for {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			if resolved != rootResolved && !strings.HasPrefix(resolved, rootResolved+string(os.PathSeparator)) {
				return "", fmt.Errorf("path escapes the changeset through a symlink: %s", rel)
			}
			break
		}
		next := filepath.Dir(parent)
		if next == parent {
			break
		}
		parent = next
	}
	return abs, nil
}

// Reconcile refreshes PR state from GitHub. It is used by status and cleanup.
func (m *Manager) Reconcile(ctx context.Context, c state.Change) (state.Change, error) {
	return m.reconcile(ctx, c)
}

// Dirty returns uncommitted paths in a changeset.
func (m *Manager) Dirty(ctx context.Context, c state.Change) ([]string, error) {
	out, err := m.wt(ctx, c, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) > 3 {
			files = append(files, strings.TrimSpace(line[3:]))
		}
	}
	return files, nil
}

// HashFile returns the base_rev for a file in a changeset.
func (m *Manager) HashFile(ctx context.Context, c state.Change, rel string) (string, error) {
	rel = store.RelativeToArea(c.Area, rel)
	clean, err := store.EnsureArea(c.Area, rel)
	if err != nil {
		return "", err
	}
	full, err := safeWorktreePath(c.Path, clean)
	if err != nil {
		return "", err
	}
	return gitx.Run(ctx, c.Path, "hash-object", "--", full)
}

// DeleteFile removes a document from the active changeset and commits the
// deletion. base_rev must match the current blob to avoid deleting a newer edit.
func (m *Manager) DeleteFile(ctx context.Context, area, rel, baseRev, source string) (state.Change, error) {
	unlock, err := m.lockArea(area)
	if err != nil {
		return state.Change{}, err
	}
	defer unlock()
	c, err := m.Ensure(ctx, area)
	if err != nil {
		return state.Change{}, err
	}
	rel = store.RelativeToArea(area, rel)
	clean, err := store.EnsureArea(area, rel)
	if err != nil {
		return state.Change{}, err
	}
	full, err := safeWorktreePath(c.Path, clean)
	if err != nil {
		return state.Change{}, err
	}
	if _, err := os.Stat(full); err != nil {
		return state.Change{}, fmt.Errorf("%s is not in changeset %s", clean, c.ID)
	}
	if baseRev == "" {
		b, _ := os.ReadFile(full)
		return c, &BaseRevError{Path: clean, Content: string(b)}
	}
	hash, err := gitx.Run(ctx, c.Path, "hash-object", "--", full)
	if err != nil {
		return c, err
	}
	if hash != baseRev {
		b, _ := os.ReadFile(full)
		return c, &BaseRevError{Path: clean, Content: string(b)}
	}
	if err := os.Remove(full); err != nil {
		return c, err
	}
	if _, err := m.commitIfNeeded(ctx, c, source); err != nil {
		return c, err
	}
	return c, nil
}
