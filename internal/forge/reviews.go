package forge

import (
	"context"
	"sort"
	"time"

	"github.com/google/go-github/v68/github"
)

const reviewCacheTTL = 5 * time.Minute

type cachedReviewers struct {
	names []string
	at    time.Time
}

// Approvals returns the users whose latest review on a PR is APPROVED.
// Re-approval after a change request wins because only the latest review per
// user is considered.
func (c *Client) Approvals(ctx context.Context, number int) ([]string, error) {
	if err := c.require(); err != nil {
		return nil, err
	}
	if number <= 0 {
		return nil, nil
	}
	c.reviewMu.Lock()
	if cached, ok := c.reviewCache[number]; ok && time.Since(cached.at) <= reviewCacheTTL {
		names := append([]string(nil), cached.names...)
		c.reviewMu.Unlock()
		return names, nil
	}
	c.reviewMu.Unlock()

	type reviewState struct {
		state string
		at    time.Time
	}
	states := map[string]reviewState{}
	opts := &github.ListOptions{PerPage: 100}
	for {
		reviews, resp, err := c.gh.PullRequests.ListReviews(ctx, c.Owner, c.Repo, number, opts)
		if err != nil {
			return nil, err
		}
		for _, review := range reviews {
			login := review.GetUser().GetLogin()
			if login == "" {
				continue
			}
			at := review.GetSubmittedAt().Time
			prev, exists := states[login]
			if !exists || !at.Before(prev.at) {
				states[login] = reviewState{state: review.GetState(), at: at}
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	var names []string
	for login, state := range states {
		if state.state == "APPROVED" {
			names = append(names, login)
		}
	}
	sort.Strings(names)
	c.reviewMu.Lock()
	if c.reviewCache == nil {
		c.reviewCache = map[int]cachedReviewers{}
	}
	c.reviewCache[number] = cachedReviewers{names: append([]string(nil), names...), at: time.Now()}
	c.reviewMu.Unlock()
	return names, nil
}
