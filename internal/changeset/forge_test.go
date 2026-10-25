package changeset

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/repocompanion/companion/internal/config"
	"github.com/repocompanion/companion/internal/forge"
	"github.com/repocompanion/companion/internal/state"
	"github.com/repocompanion/companion/internal/store"
)

func TestSyncAndSubmitWithFakeGitHub(t *testing.T) {
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	remote := filepath.Join(root, "remote.git")
	home := filepath.Join(root, "home")
	writeFile(t, seed, "projects/acme/api/README.md", "---\nid: readme\nkind: project-doc\ntitle: Acme API\nstatus: approved\n---\n# Acme API\n")
	git(t, seed, "init", "-q", "-b", "main")
	git(t, seed, "add", ".")
	git(t, seed, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "seed")
	git(t, root, "init", "-q", "--bare", remote)
	git(t, seed, "remote", "add", "origin", remote)
	git(t, seed, "push", "-q", "origin", "main")

	t.Setenv("GIT_AUTHOR_NAME", "Tester")
	t.Setenv("GIT_AUTHOR_EMAIL", "tester@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Tester")
	t.Setenv("GIT_COMMITTER_EMAIL", "tester@example.com")

	fake := newFakeGitHub(t)
	defer fake.Close()
	fc, err := forge.NewFromRemote("git@github.com:acme/docs.git", config.ForgeConfig{Token: "test", APIURL: fake.URL()})
	if err != nil {
		t.Fatal(err)
	}
	if !fc.Available() {
		t.Fatal("fake forge not available")
	}

	st := store.New(home, remote)
	if err := st.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.MakeWritable() })
	ctx := context.Background()
	cfg := config.UserConfig{}
	cfg.Changesets.BranchPrefix = "docs"
	cc := &config.Context{Project: "acme/api", UserName: "Ana", Machine: "test"}
	m := New(st, state.NewStore(st.StatePath), cfg, cc, fc)

	c, err := m.Start(ctx, "refund flow", "projects/acme/api")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/refund.md", "---\nid: refund\nkind: flow\ntitle: Refund\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Refund\n", "", "test"); err != nil {
		t.Fatal(err)
	}
	res, err := m.Sync(ctx, c.ID, "test")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if res.Change.PR == 0 || res.PR == nil {
		t.Fatalf("no PR created: %+v", res)
	}
	if res.Change.PRState != "draft" {
		t.Fatalf("state = %s, want draft", res.Change.PRState)
	}
	// A preflight lint error keeps the PR as a draft.
	broken := "---\nid: broken\nkind: flow\ntitle: Broken\nstatus: approved\nowners: [payments]\nlast_reviewed: 2026-01-01\n---\n# Broken\n\n[missing](does-not-exist.md)\n"
	if _, _, err := m.WriteFile(ctx, c.Area, "flows/broken.md", broken, "", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Submit(ctx, c.ID, "document refunds", "test"); err == nil {
		t.Fatal("submit should fail preflight lint")
	} else {
		var lintErr *LintError
		if !errors.As(err, &lintErr) {
			t.Fatalf("submit error = %T %v, want LintError", err, err)
		}
	}
	if !fake.Draft() {
		t.Fatal("failed preflight marked the PR ready")
	}
	// A human override can mark it ready despite lint errors.
	if _, err := m.SubmitWithOptions(ctx, c.ID, "document refunds", "test", SubmitOptions{SkipLint: true}); err != nil {
		t.Fatalf("skip-lint submit: %v", err)
	}
	if fake.Draft() {
		t.Fatal("skip-lint submit did not mark the PR ready")
	}
	if !strings.Contains(fake.body, "Companion-Source: test") {
		t.Fatalf("submit did not write squash trailers: %s", fake.body)
	}
	if _, err := m.Reopen(ctx, c.ID); err != nil {
		t.Fatalf("reopen after skip-lint submit: %v", err)
	}
	hash, err := m.HashFile(ctx, c, "flows/broken.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.DeleteFile(ctx, c.Area, "flows/broken.md", hash, "test"); err != nil {
		t.Fatal(err)
	}

	// Advance main; submit must rebase before marking ready.
	writeFile(t, seed, "projects/acme/api/main-only.md", "---\nid: main-only\nkind: flow\ntitle: Main only\nstatus: approved\n---\n# Main\n")
	git(t, seed, "add", ".")
	git(t, seed, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "advance main")
	git(t, seed, "push", "-q", "origin", "main")
	res, err = m.Submit(ctx, c.ID, "document refunds", "test")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.Change.PRState != "ready" || !res.Change.Frozen {
		t.Fatalf("submit state = %+v", res.Change)
	}
	if got := fake.Draft(); got {
		t.Fatal("fake PR is still draft after submit")
	}
	reopened, err := m.Reopen(ctx, c.ID)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.Frozen || reopened.PRState != "draft" {
		t.Fatalf("reopen state = %+v", reopened)
	}
	if !fake.Draft() {
		t.Fatal("fake PR was not returned to draft")
	}
}

type fakeGitHub struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	exists bool
	number int
	title  string
	body   string
	draft  bool
	head   string
	state  string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{t: t, number: 1, draft: true, state: "open"}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/acme/docs/pulls", f.pulls)
	mux.HandleFunc("/api/v3/repos/acme/docs/pulls/1", f.pull)
	mux.HandleFunc("/api/v3/repos/acme/docs/pulls/1/files", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]any{})
	})
	mux.HandleFunc("/api/v3/user", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "ana"})
	})
	mux.HandleFunc("/api/graphql", f.graphql)
	f.srv = httptest.NewServer(mux)
	return f
}

func (f *fakeGitHub) URL() string { return f.srv.URL }
func (f *fakeGitHub) Close()      { f.srv.Close() }

func (f *fakeGitHub) Draft() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.draft
}

func (f *fakeGitHub) prJSON() map[string]any {
	return map[string]any{
		"number": f.number, "title": f.title, "body": f.body, "state": f.state,
		"draft": f.draft, "merged": false, "node_id": "NODE", "html_url": "https://example.test/acme/docs/pull/1",
		"user":       map[string]any{"login": "ana"},
		"head":       map[string]any{"ref": f.head, "sha": "abc"},
		"base":       map[string]any{"ref": "main", "sha": "def"},
		"updated_at": "2026-01-01T00:00:00Z",
	}
}

func (f *fakeGitHub) pulls(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == http.MethodPost {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.exists = true
		f.title, _ = body["title"].(string)
		f.body, _ = body["body"].(string)
		f.head, _ = body["head"].(string)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(f.prJSON())
		return
	}
	if !f.exists {
		_ = json.NewEncoder(w).Encode([]any{})
		return
	}
	_ = json.NewEncoder(w).Encode([]any{f.prJSON()})
}

func (f *fakeGitHub) pull(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method == http.MethodPatch {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if v, ok := body["title"].(string); ok {
			f.title = v
		}
		if v, ok := body["body"].(string); ok {
			f.body = v
		}
	}
	_ = json.NewEncoder(w).Encode(f.prJSON())
}

func (f *fakeGitHub) graphql(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body struct {
		Query string `json:"query"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if len(body.Query) > 0 {
		if strings.Contains(body.Query, "markPullRequestReadyForReview") {
			f.draft = false
		}
		if strings.Contains(body.Query, "convertPullRequestToDraft") {
			f.draft = true
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"markPullRequestReadyForReview": map[string]any{"pullRequest": map[string]any{"isDraft": f.draft}}}})
}
