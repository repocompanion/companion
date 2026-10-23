package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/go-github/v68/github"
)

const statusCacheTTL = 30 * time.Second

// Check is a compact CI check result for status displays.
type Check struct {
	Name       string `json:"name"`
	Status     string `json:"status,omitempty"`
	Conclusion string `json:"conclusion,omitempty"`
	URL        string `json:"url,omitempty"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// PRStatus combines PR metadata with checks and review-thread state.
type PRStatus struct {
	PR                *PR       `json:"pr,omitempty"`
	Checks            []Check   `json:"checks,omitempty"`
	CheckConclusion   string    `json:"check_conclusion,omitempty"`
	ReviewDecision    string    `json:"review_decision,omitempty"`
	UnresolvedThreads int       `json:"unresolved_threads"`
	Mergeable         *bool     `json:"mergeable,omitempty"`
	MergeableState    string    `json:"mergeable_state,omitempty"`
	FetchedAt         time.Time `json:"fetched_at"`
	Warnings          []string  `json:"warnings,omitempty"`
}

type cachedPRStatus struct {
	status PRStatus
	at     time.Time
}

func (c *Client) statusCacheFor(number int) (PRStatus, bool) {
	if c.statusCache == nil {
		return PRStatus{}, false
	}
	cached, ok := c.statusCache[number]
	if !ok || time.Since(cached.at) > statusCacheTTL {
		return PRStatus{}, false
	}
	return cloneStatus(cached.status), true
}

func cloneStatus(in PRStatus) PRStatus {
	out := in
	out.Checks = append([]Check(nil), in.Checks...)
	out.Warnings = append([]string(nil), in.Warnings...)
	return out
}

func (c *Client) cacheStatus(number int, status PRStatus) {
	if c.statusCache == nil {
		c.statusCache = map[int]cachedPRStatus{}
	}
	c.statusCache[number] = cachedPRStatus{status: cloneStatus(status), at: time.Now()}
}

func (c *Client) invalidateStatus(number int) {
	if c.statusCache != nil {
		delete(c.statusCache, number)
	}
}

// Status returns PR metadata, CI checks and review-thread information. Partial
// failures are returned as warnings because branch protection remains the
// source of truth.
func (c *Client) Status(ctx context.Context, number int) (*PRStatus, error) {
	if err := c.require(); err != nil {
		return nil, err
	}
	c.statusMu.Lock()
	if cached, ok := c.statusCacheFor(number); ok {
		c.statusMu.Unlock()
		return &cached, nil
	}
	c.statusMu.Unlock()

	pr, err := c.Get(ctx, number)
	if err != nil {
		return nil, err
	}
	out := &PRStatus{
		PR: pr, Mergeable: pr.Mergeable, MergeableState: pr.MergeableState,
		FetchedAt: time.Now().UTC(),
	}
	if pr.HeadSHA != "" {
		runs, _, err := c.gh.Checks.ListCheckRunsForRef(ctx, c.Owner, c.Repo, pr.HeadSHA, &github.ListCheckRunsOptions{
			ListOptions: github.ListOptions{PerPage: 100},
		})
		if err != nil {
			out.Warnings = append(out.Warnings, "could not load checks: "+err.Error())
		} else if runs != nil {
			for _, run := range runs.CheckRuns {
				out.Checks = append(out.Checks, Check{
					Name:       run.GetName(),
					Status:     run.GetStatus(),
					Conclusion: run.GetConclusion(),
					URL:        run.GetHTMLURL(),
					StartedAt:  formatTime(run.StartedAt),
					FinishedAt: formatTime(run.CompletedAt),
				})
			}
			out.CheckConclusion = combineChecks(out.Checks)
		}
	}
	info, err := c.reviewInfo(ctx, number)
	if err != nil {
		out.Warnings = append(out.Warnings, "could not load review state: "+err.Error())
	} else {
		out.ReviewDecision = info.ReviewDecision
		out.UnresolvedThreads = info.UnresolvedThreads
		out.Warnings = append(out.Warnings, info.Warnings...)
	}
	c.statusMu.Lock()
	c.cacheStatus(number, *out)
	c.statusMu.Unlock()
	return out, nil
}

func formatTime(t *github.Timestamp) string {
	if t == nil || t.Time.IsZero() {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func combineChecks(checks []Check) string {
	if len(checks) == 0 {
		return ""
	}
	pending := false
	for _, check := range checks {
		conclusion := check.Conclusion
		if check.Status != "" && check.Status != "completed" {
			pending = true
			continue
		}
		switch conclusion {
		case "failure", "timed_out", "cancelled", "action_required":
			return "failure"
		case "success", "neutral", "skipped":
		default:
			pending = true
		}
	}
	if pending {
		return "pending"
	}
	return "success"
}

type reviewInfo struct {
	ReviewDecision    string
	UnresolvedThreads int
	Warnings          []string
}

func (c *Client) reviewInfo(ctx context.Context, number int) (reviewInfo, error) {
	query := `query($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $number) {
      reviewDecision
      reviewThreads(first: 100) {
        totalCount
        nodes { isResolved }
      }
    }
  }
}`
	payload := map[string]any{
		"query": query,
		"variables": map[string]any{
			"owner": c.Owner, "repo": c.Repo, "number": number,
		},
	}
	b, _ := json.Marshal(payload)
	apiURL, err := c.graphqlURL()
	if err != nil {
		return reviewInfo{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(b))
	if err != nil {
		return reviewInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return reviewInfo{}, err
	}
	defer resp.Body.Close()
	var result struct {
		Data struct {
			Repository struct {
				PullRequest *struct {
					ReviewDecision string `json:"reviewDecision"`
					ReviewThreads  struct {
						TotalCount int `json:"totalCount"`
						Nodes      []struct {
							IsResolved bool `json:"isResolved"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return reviewInfo{}, err
	}
	if len(result.Errors) > 0 {
		return reviewInfo{}, fmt.Errorf("GitHub GraphQL: %s", result.Errors[0].Message)
	}
	if resp.StatusCode >= 300 {
		return reviewInfo{}, fmt.Errorf("GitHub GraphQL returned HTTP %d", resp.StatusCode)
	}
	pr := result.Data.Repository.PullRequest
	if pr == nil {
		return reviewInfo{}, fmt.Errorf("pull request #%d not found", number)
	}
	var out reviewInfo
	out.ReviewDecision = pr.ReviewDecision
	for _, thread := range pr.ReviewThreads.Nodes {
		if !thread.IsResolved {
			out.UnresolvedThreads++
		}
	}
	if pr.ReviewThreads.TotalCount > len(pr.ReviewThreads.Nodes) {
		out.Warnings = append(out.Warnings, fmt.Sprintf("review threads truncated: showing %d of %d", len(pr.ReviewThreads.Nodes), pr.ReviewThreads.TotalCount))
	}
	return out, nil
}
