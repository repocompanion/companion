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

func TestApprovalsUsesLatestReviewPerUserAndCache(t *testing.T) {
	var requests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/acme/docs/pulls/1/reviews", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"id": 1, "user": map[string]any{"login": "ana"}, "state": "CHANGES_REQUESTED", "submitted_at": "2026-01-01T00:00:00Z"},
			map[string]any{"id": 2, "user": map[string]any{"login": "ana"}, "state": "APPROVED", "submitted_at": "2026-01-02T00:00:00Z"},
			map[string]any{"id": 3, "user": map[string]any{"login": "ben"}, "state": "COMMENTED", "submitted_at": "2026-01-03T00:00:00Z"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, err := NewFromRemote("git@github.com:acme/docs.git", config.ForgeConfig{Token: "test", APIURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	names, err := c.Approvals(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "ana" {
		t.Fatalf("approvals = %v", names)
	}
	if _, err := c.Approvals(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("review cache was not used: requests=%d", requests.Load())
	}
}
