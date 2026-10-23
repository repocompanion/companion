package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bolacha/docsync/internal/config"
)

func TestStatusIncludesChecksAndReviewThreads(t *testing.T) {
	var requests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/acme/docs/pulls/1", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 1, "title": "Docs", "state": "open", "draft": false,
			"html_url": "https://example.test/pull/1", "node_id": "NODE",
			"updated_at":      "2026-01-01T00:00:00Z",
			"head":            map[string]any{"ref": "docs/test", "sha": "abc"},
			"base":            map[string]any{"ref": "main", "sha": "def"},
			"user":            map[string]any{"login": "ana"},
			"mergeable":       true,
			"mergeable_state": "clean",
		})
	})
	mux.HandleFunc("/api/v3/repos/acme/docs/commits/abc/check-runs", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 2,
			"check_runs": []any{
				map[string]any{"name": "lint", "status": "completed", "conclusion": "success", "html_url": "https://example.test/check/1"},
				map[string]any{"name": "test", "status": "in_progress", "conclusion": nil},
			},
		})
	})
	mux.HandleFunc("/api/graphql", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		threads := map[string]any{
			"totalCount": 2,
			"nodes": []any{
				map[string]any{"isResolved": true},
				map[string]any{"isResolved": false},
			},
		}
		pr := map[string]any{"reviewDecision": "APPROVED", "reviewThreads": threads}
		repo := map[string]any{"pullRequest": pr}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": repo}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, err := NewFromRemote("git@github.com:acme/docs.git", config.ForgeConfig{Token: "test", APIURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	status, err := c.Status(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if status.CheckConclusion != "pending" || len(status.Checks) != 2 {
		t.Fatalf("checks = %+v", status)
	}
	if status.ReviewDecision != "APPROVED" || status.UnresolvedThreads != 1 {
		t.Fatalf("review = %+v", status)
	}
	if status.PR == nil || status.PR.HeadSHA != "abc" || status.Mergeable == nil || !*status.Mergeable {
		t.Fatalf("pr = %+v", status.PR)
	}
	before := requests.Load()
	if _, err := c.Status(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != before {
		t.Fatalf("status cache was not used: requests=%d before=%d", requests.Load(), before)
	}
}
