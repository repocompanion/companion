// Package forge is the GitHub PR/review client. Git transport stays on SSH;
// this package only talks to the REST/GraphQL APIs.
package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bolacha/docsync/internal/config"
	"github.com/bolacha/docsync/internal/gitx"
	"github.com/google/go-github/v68/github"
)

type PR struct {
	Number         int       `json:"number"`
	Title          string    `json:"title"`
	Body           string    `json:"body"`
	State          string    `json:"state"`
	Draft          bool      `json:"draft"`
	Merged         bool      `json:"merged"`
	Head           string    `json:"head"`
	Base           string    `json:"base"`
	HeadSHA        string    `json:"head_sha"`
	BaseSHA        string    `json:"base_sha"`
	URL            string    `json:"url"`
	User           string    `json:"user"`
	UpdatedAt      time.Time `json:"updated_at"`
	Mergeable      *bool     `json:"mergeable,omitempty"`
	MergeableState string    `json:"mergeable_state,omitempty"`
	NodeID         string    `json:"node_id,omitempty"`
}

func (p PR) StateName() string {
	if p.Merged {
		return "merged"
	}
	if p.State == "closed" {
		return "closed"
	}
	if p.Draft {
		return "draft"
	}
	return "ready"
}

type Client struct {
	Owner   string
	Repo    string
	Token   string
	BaseURL string
	gh      *github.Client
	enabled bool
}

// NewFromRemote builds a client for a GitHub remote. It never fails just
// because a token is missing: read-only store operations still work and the
// caller can report that PR actions are unavailable.
func NewFromRemote(remote string, cfg config.ForgeConfig) (*Client, error) {
	owner, repo, err := gitx.SplitGitHubRemote(remote)
	if err != nil {
		return &Client{enabled: false}, nil
	}
	token := strings.TrimSpace(os.Getenv("DOCSYNC_FORGE_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(cfg.Token)
	}
	if token == "" {
		token = ghAuthToken()
	}
	c := &Client{Owner: owner, Repo: repo, Token: token, enabled: token != ""}
	base := strings.TrimRight(cfg.APIURL, "/")
	if base == "" {
		base = "https://api.github.com/"
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	c.BaseURL = base
	gh := github.NewClient(nil)
	if c.enabled {
		gh = gh.WithAuthToken(token)
	}
	if cfg.APIURL != "" {
		// WithEnterpriseURLs appends /api/v3/ when needed. Keep the effective
		// base URL so GraphQL uses the matching /api/graphql endpoint.
		if u, err := gh.WithEnterpriseURLs(base, base); err == nil {
			gh = u
			c.BaseURL = gh.BaseURL.String()
		}
	}
	if hc := gh.Client(); hc != nil {
		hc.Timeout = 30 * time.Second
	}
	c.gh = gh
	return c, nil
}

func ghAuthToken() string {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (c *Client) Available() bool { return c != nil && c.enabled && c.gh != nil }

func (c *Client) RepoName() string {
	if c == nil {
		return ""
	}
	return c.Owner + "/" + c.Repo
}

func (c *Client) require() error {
	if !c.Available() {
		return errors.New("GitHub token unavailable; set DOCSYNC_FORGE_TOKEN or run `gh auth login`")
	}
	return nil
}

func toPR(p *github.PullRequest) PR {
	if p == nil {
		return PR{}
	}
	out := PR{
		Number:         p.GetNumber(),
		Title:          p.GetTitle(),
		Body:           p.GetBody(),
		State:          p.GetState(),
		Draft:          p.GetDraft(),
		Merged:         p.GetMerged(),
		URL:            p.GetHTMLURL(),
		UpdatedAt:      p.GetUpdatedAt().Time,
		Mergeable:      p.Mergeable,
		MergeableState: p.GetMergeableState(),
		NodeID:         p.GetNodeID(),
	}
	if h := p.GetHead(); h != nil {
		out.Head = h.GetRef()
		out.HeadSHA = h.GetSHA()
	}
	if b := p.GetBase(); b != nil {
		out.Base = b.GetRef()
		out.BaseSHA = b.GetSHA()
	}
	if u := p.GetUser(); u != nil {
		out.User = u.GetLogin()
	}
	return out
}

func (c *Client) Get(ctx context.Context, number int) (*PR, error) {
	if err := c.require(); err != nil {
		return nil, err
	}
	p, _, err := c.gh.PullRequests.Get(ctx, c.Owner, c.Repo, number)
	if err != nil {
		return nil, err
	}
	out := toPR(p)
	return &out, nil
}

func (c *Client) FindByBranch(ctx context.Context, branch string) (*PR, error) {
	if err := c.require(); err != nil {
		return nil, err
	}
	opts := &github.PullRequestListOptions{State: "all", Head: c.Owner + ":" + branch, Base: "main", Sort: "updated", Direction: "desc", ListOptions: github.ListOptions{PerPage: 20}}
	list, _, err := c.gh.PullRequests.List(ctx, c.Owner, c.Repo, opts)
	if err != nil {
		return nil, err
	}
	var closed *PR
	for _, p := range list {
		pr := toPR(p)
		if pr.State == "open" {
			return &pr, nil
		}
		if closed == nil {
			closed = &pr
		}
	}
	return closed, nil
}

func (c *Client) ListOpen(ctx context.Context) ([]PR, error) {
	if err := c.require(); err != nil {
		return nil, err
	}
	var out []PR
	opts := &github.PullRequestListOptions{State: "open", Sort: "updated", Direction: "desc", ListOptions: github.ListOptions{PerPage: 100}}
	for {
		list, resp, err := c.gh.PullRequests.List(ctx, c.Owner, c.Repo, opts)
		if err != nil {
			return nil, err
		}
		for _, p := range list {
			out = append(out, toPR(p))
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}
	return out, nil
}

// ListOpenForArea finds PRs whose managed block names the area or whose files
// are under the area.
func (c *Client) ListOpenForArea(ctx context.Context, area string) ([]PR, error) {
	all, err := c.ListOpen(ctx)
	if err != nil {
		return nil, err
	}
	var out []PR
	for _, p := range all {
		if AreaFromBody(p.Body) == area {
			out = append(out, p)
			continue
		}
		files, _, err := c.gh.PullRequests.ListFiles(ctx, c.Owner, c.Repo, p.Number, &github.ListOptions{PerPage: 100})
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.HasPrefix(f.GetFilename(), strings.Trim(area, "/")+"/") {
				out = append(out, p)
				break
			}
		}
	}
	return out, nil
}

func (c *Client) CurrentLogin(ctx context.Context) (string, error) {
	if err := c.require(); err != nil {
		return "", err
	}
	u, _, err := c.gh.Users.Get(ctx, "")
	if err != nil {
		return "", err
	}
	return u.GetLogin(), nil
}

func (c *Client) CreateDraft(ctx context.Context, title, head, base, body string) (*PR, error) {
	if err := c.require(); err != nil {
		return nil, err
	}
	draft := true
	p, _, err := c.gh.PullRequests.Create(ctx, c.Owner, c.Repo, &github.NewPullRequest{
		Title: github.String(title),
		Head:  github.String(head),
		Base:  github.String(base),
		Body:  github.String(body),
		Draft: &draft,
	})
	if err != nil {
		return nil, err
	}
	out := toPR(p)
	return &out, nil
}

func (c *Client) Update(ctx context.Context, number int, title, body string) (*PR, error) {
	if err := c.require(); err != nil {
		return nil, err
	}
	p, _, err := c.gh.PullRequests.Edit(ctx, c.Owner, c.Repo, number, &github.PullRequest{
		Title: github.String(title),
		Body:  github.String(body),
	})
	if err != nil {
		return nil, err
	}
	out := toPR(p)
	return &out, nil
}

func (c *Client) Close(ctx context.Context, number int) error {
	if err := c.require(); err != nil {
		return err
	}
	state := "closed"
	_, _, err := c.gh.PullRequests.Edit(ctx, c.Owner, c.Repo, number, &github.PullRequest{State: &state})
	return err
}

// SetDraft moves a PR between draft and ready-for-review. GitHub only exposes
// this through GraphQL.
func (c *Client) SetDraft(ctx context.Context, number int, draft bool) error {
	if err := c.require(); err != nil {
		return err
	}
	pr, err := c.Get(ctx, number)
	if err != nil {
		return err
	}
	if pr.Draft == draft {
		return nil
	}
	if pr.NodeID == "" {
		return fmt.Errorf("PR #%d has no node ID", number)
	}
	mutation := "markPullRequestReadyForReview"
	if draft {
		mutation = "convertPullRequestToDraft"
	}
	query := fmt.Sprintf(`mutation($id: ID!) { %s(input: {pullRequestId: $id}) { pullRequest { isDraft } } }`, mutation)
	payload := map[string]any{"query": query, "variables": map[string]any{"id": pr.NodeID}}
	b, _ := json.Marshal(payload)
	apiURL, err := c.graphqlURL()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Data   map[string]any `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if len(result.Errors) > 0 {
		return fmt.Errorf("GitHub GraphQL: %s", result.Errors[0].Message)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("GitHub GraphQL returned HTTP %d", resp.StatusCode)
	}
	return nil
}

type ReviewComment struct {
	Author string `json:"author"`
	Body   string `json:"body"`
	Path   string `json:"path,omitempty"`
	Line   int    `json:"line,omitempty"`
	URL    string `json:"url,omitempty"`
}

func (c *Client) ReviewComments(ctx context.Context, number int) ([]ReviewComment, error) {
	if err := c.require(); err != nil {
		return nil, err
	}
	list, _, err := c.gh.PullRequests.ListComments(ctx, c.Owner, c.Repo, number, &github.PullRequestListCommentsOptions{ListOptions: github.ListOptions{PerPage: 100}})
	if err != nil {
		return nil, err
	}
	var out []ReviewComment
	for _, x := range list {
		out = append(out, ReviewComment{
			Author: x.GetUser().GetLogin(),
			Body:   x.GetBody(),
			Path:   x.GetPath(),
			Line:   x.GetLine(),
			URL:    x.GetHTMLURL(),
		})
	}
	return out, nil
}

func (c *Client) graphqlURL() (string, error) {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(u.Path, "/api/v3/") {
		u.Path = strings.TrimSuffix(u.Path, "/api/v3/") + "/api/graphql"
	} else {
		u.Path = strings.TrimRight(u.Path, "/") + "/graphql"
	}
	u.RawQuery = ""
	return u.String(), nil
}

const bodyBegin = "<!-- docsync:begin -->"
const bodyEnd = "<!-- docsync:end -->"

// ManagedBody preserves human-written text outside the managed marker block.
func ManagedBody(existing, changesetID, area string, files []string) string {
	var b strings.Builder
	b.WriteString(bodyBegin)
	b.WriteString("\n")
	fmt.Fprintf(&b, "Docsync-Changeset: %s\n", changesetID)
	fmt.Fprintf(&b, "Docsync-Area: %s\n", area)
	b.WriteString("Docsync-Files:\n")
	for _, f := range files {
		fmt.Fprintf(&b, "- `%s`\n", f)
	}
	b.WriteString(bodyEnd)
	managed := b.String()
	if i := strings.Index(existing, bodyBegin); i >= 0 {
		if j := strings.Index(existing[i:], bodyEnd); j >= 0 {
			return existing[:i] + managed + existing[i+j+len(bodyEnd):]
		}
	}
	existing = strings.TrimSpace(existing)
	if existing == "" {
		return managed
	}
	return existing + "\n\n" + managed
}

func AreaFromBody(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "Docsync-Area:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Docsync-Area:"))
		}
	}
	return ""
}

func ChangesetFromBody(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "Docsync-Changeset:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Docsync-Changeset:"))
		}
	}
	return ""
}
