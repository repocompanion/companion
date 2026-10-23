// Package mcpserver exposes docsync through the Model Context Protocol.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/bolacha/docsync/internal/changeset"
	"github.com/bolacha/docsync/internal/codeindex"
	"github.com/bolacha/docsync/internal/guidance"
	"github.com/bolacha/docsync/internal/runtime"
	"github.com/bolacha/docsync/internal/search"
	"github.com/bolacha/docsync/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const serverInstructions = `docsync is the local entry point for project documentation and organization guidance.

Before implementing infrastructure, integration or cross-cutting code (queues, databases, migrations, observability, APIs, auth), call docs_get_guidance.
Before writing or refining documentation, call docs_get_context. If the mirror is stale, call docs_pull first.
For a project with no docs, call docs_scaffold_project before writing; do not invent a flat layout.

Never write docs into the code repo. All doc changes go through docsync tools and become PRs.
docs_write enforces YAML frontmatter and canonical paths (README.md, architecture.md, adr/, flows/, runbooks/, patterns/, standards/). When it rejects a document, read docs_get_context.structure and reorganize rather than retrying the same flat file.
Use docs_change_sync to create/update a draft PR, docs_change_submit when the work is ready for review,
and docs_review_comments to address feedback. Agents can never merge, abandon or clean up changesets.
If a shared PR already exists for the area, join it with docs_change_join instead of creating a duplicate.`

type Options struct {
	Cwd       string
	Project   string
	Team      string
	Domain    string
	Languages []string
}

type provider struct {
	opts Options
	mu   sync.Mutex
	rt   *runtime.Runtime

	rootsMu    sync.Mutex
	roots      map[*mcp.ServerSession]string
	rootsKnown map[*mcp.ServerSession]bool
}

func (p *provider) runtime() (*runtime.Runtime, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rt != nil {
		return p.rt, nil
	}
	rt, err := runtime.New(p.opts.Cwd)
	if err != nil {
		return nil, err
	}
	p.rt = rt
	return rt, nil
}

func (p *provider) session(ctx context.Context, req *mcp.CallToolRequest, project string) (*runtime.Session, error) {
	var session *mcp.ServerSession
	if req != nil {
		session = req.Session
	}
	return p.sessionFor(ctx, session, project)
}

func (p *provider) sessionFor(ctx context.Context, session *mcp.ServerSession, project string) (*runtime.Session, error) {
	rt, err := p.runtime()
	if err != nil {
		return nil, err
	}
	if project == "" {
		project = p.opts.Project
	}
	cwd := p.opts.Cwd
	if root := p.rootFor(ctx, session); root != "" {
		cwd = root
	}
	return rt.SessionAt(ctx, cwd, project, p.opts.Team, p.opts.Domain, p.opts.Languages)
}

func requireStore(s *runtime.Session) error {
	if !s.Runtime.Store.Exists() {
		return errors.New("docsync store is not initialized; run `docsync init --remote <git@github.com:org/docs-store.git>`")
	}
	return nil
}

func textResult(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
}

func errorResult(err error) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}, nil, nil
}

// jsonErrorResult returns structured JSON while keeping the MCP error flag set.
func jsonErrorResult(v any) (*mcp.CallToolResult, any, error) {
	res, _, err := textResult(v)
	if err != nil {
		return nil, nil, err
	}
	res.IsError = true
	return res, nil, nil
}

func pendingResult(res changeset.Result, err error) (*mcp.CallToolResult, any, error) {
	if changeset.IsPending(err) {
		out := map[string]any{"ok": true, "pending": true, "warning": err.Error(), "result": res}
		return textResult(out)
	}
	return errorResult(err)
}

func NewServer(opts Options) (*mcp.Server, error) {
	if opts.Cwd == "" {
		return nil, errors.New("mcp server requires a working directory")
	}
	p := &provider{opts: opts, roots: map[*mcp.ServerSession]string{}, rootsKnown: map[*mcp.ServerSession]bool{}}
	server := mcp.NewServer(&mcp.Implementation{
		Name: "docsync", Title: "docsync", Version: "0.1.0",
		Description: "Git-backed project docs and guidance for coding agents",
	}, &mcp.ServerOptions{
		Instructions: serverInstructions,
		// MCP roots are deprecated from protocol 2026-07-28 onward. Keep the
		// 2025-11-25 protocol so roots remain available through the documented
		// deprecation window; newer clients fall back to cwd when we migrate to
		// multi-round-trip input requests.
		SupportedProtocolVersions: []string{"2025-11-25"},
		RootsListChangedHandler: func(ctx context.Context, req *mcp.RootsListChangedRequest) {
			p.invalidateRoot(req.Session)
		},
	})

	registerContextTools(server, p)
	registerChangeTools(server, p)
	registerKnowledgeTools(server, p)
	registerResources(server, p)
	registerPrompts(server, p)
	return server, nil
}

func Run(ctx context.Context, opts Options) error {
	server, err := NewServer(opts)
	if err != nil {
		return err
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}

// ---- read tools ----

type projectIn struct {
	Project string `json:"project,omitempty" jsonschema:"project as owner/repo; defaults to the detected current repository"`
	MaxDocs int    `json:"max_docs,omitempty" jsonschema:"page size for the doc index; default 200"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"opaque pagination cursor from a previous response"`
	Full    bool   `json:"full,omitempty" jsonschema:"return the full document index instead of the bounded view"`
}

type contextOut struct {
	Context        any           `json:"context"`
	Area           string        `json:"area"`
	Docs           []docIndex    `json:"docs"`
	TotalDocs      int           `json:"total_docs"`
	ReturnedDocs   int           `json:"returned_docs"`
	Truncated      bool          `json:"truncated,omitempty"`
	Missing        []string      `json:"missing_required,omitempty"`
	OpenPRs        []any         `json:"open_prs,omitempty"`
	OpenPRsChecked bool          `json:"open_prs_checked"`
	Changesets     []any         `json:"active_changesets,omitempty"`
	MirrorRev      string        `json:"mirror_rev"`
	MirrorAge      string        `json:"mirror_age"`
	Warnings       []string      `json:"warnings,omitempty"`
	NextCursor     string        `json:"next_cursor,omitempty"`
	Structure      structureInfo `json:"structure"`
}

type docIndex struct {
	Path   string `json:"path"`
	Title  string `json:"title,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Status string `json:"status,omitempty"`
	Source string `json:"source,omitempty"`
	State  string `json:"state,omitempty"`
}

func registerContextTools(s *mcp.Server, p *provider) {
	addTool(s, &mcp.Tool{
		Name:        "docs_get_context",
		Description: "Entry point for doc work. Resolves the project and returns the doc index, open PRs, missing required docs, active changesets and mirror freshness. Does not create anything.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in projectIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		if err := requireStore(sess); err != nil {
			return errorResult(err)
		}
		area := sess.Context.ProjectArea()
		var docs []docIndex
		for _, dir := range []string{area, sess.Context.TeamArea(), "org"} {
			if dir == "" {
				continue
			}
			o, err := sess.Manager.OverlayDir(ctx, dir)
			if err != nil {
				list, _ := sess.Runtime.Store.WalkDocs(dir)
				for _, d := range list {
					docs = append(docs, docIndex{Path: d.Path, Title: d.Frontmatter.Title, Kind: d.Frontmatter.Kind, Status: d.Frontmatter.Status, Source: "main", State: changeset.DocStateMain})
				}
				continue
			}
			for _, d := range o.Docs {
				docs = append(docs, docIndex{
					Path: d.Path, Title: d.Frontmatter.Title, Kind: d.Frontmatter.Kind,
					Status: d.Frontmatter.Status, Source: o.Sources[d.Path], State: o.States[d.Path],
				})
			}
		}
		missing := missingRequired(ctx, sess, area)
		totalDocs := len(docs)
		maxDocs := in.MaxDocs
		if maxDocs <= 0 {
			maxDocs = 200
		}
		if maxDocs > 2000 {
			maxDocs = 2000
		}
		truncated := false
		nextCursor := ""
		if !in.Full {
			page, next, perr := pageItems(docs, in.Cursor, maxDocs, 200, 2000)
			if perr != nil {
				return errorResult(perr)
			}
			docs = page
			nextCursor = next
			truncated = next != ""
		}
		var prs []any
		prsChecked := false
		warnings := append([]string{}, sess.Context.Warnings...)
		if area != "" {
			if sess.Runtime.Forge == nil || !sess.Runtime.Forge.Available() {
				warnings = append(warnings, "GitHub token unavailable; open PRs were not checked")
			} else if list, err := sess.Runtime.Forge.ListOpenForArea(ctx, area); err != nil {
				warnings = append(warnings, "could not check open PRs: "+err.Error())
			} else {
				prsChecked = true
				for _, pr := range list {
					prs = append(prs, map[string]any{
						"number": pr.Number, "title": pr.Title, "author": pr.User,
						"state": pr.StateName(), "url": pr.URL, "updated_at": pr.UpdatedAt,
					})
				}
			}
		}
		var cs []any
		if list, err := sess.Manager.List(); err == nil {
			for _, c := range list {
				if c.IsOpen() && (area == "" || c.Area == area || strings.HasPrefix(c.Area, area+"/")) {
					cs = append(cs, map[string]any{"id": c.ID, "area": c.Area, "title": c.Title, "pr": c.PR, "state": c.PRState})
				}
			}
		}
		rev, ct, _ := sess.Runtime.Store.MirrorRev(ctx)
		out := contextOut{
			Context: sess.Context, Area: area, Docs: docs, TotalDocs: totalDocs,
			ReturnedDocs: len(docs), Truncated: truncated, Missing: missing,
			OpenPRs: prs, OpenPRsChecked: prsChecked, Changesets: cs, MirrorRev: rev,
			MirrorAge: humanAge(time.Since(ct)), Warnings: warnings,
			NextCursor: nextCursor, Structure: canonicalStructure(),
		}
		if truncated {
			out.Warnings = append(out.Warnings, fmt.Sprintf("context index page shows %d of %d docs; use next_cursor, docs_search/docs_list, or full=true", len(docs), totalDocs))
		}
		if time.Since(ct) > 24*time.Hour && !ct.IsZero() {
			out.Warnings = append(out.Warnings, "the main mirror is more than 24h old; call docs_pull")
		}
		return textResult(out)
	})

	type guidIn struct {
		Task       string `json:"task" jsonschema:"the coding task, e.g. implement an SQS consumer in Go"`
		Language   string `json:"language,omitempty" jsonschema:"optional language filter"`
		MaxResults int    `json:"max_results,omitempty" jsonschema:"maximum results, default 5"`
		Project    string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_get_guidance",
		Description: "Entry point for coding tasks. Returns ranked patterns, ADRs and standards with reference implementation locations.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in guidIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		if err := requireStore(sess); err != nil {
			return errorResult(err)
		}
		results, err := guidance.Lookup(sess.Runtime.Store, sess.Context, in.Task, in.Language, in.MaxResults)
		if err != nil {
			return errorResult(err)
		}
		return textResult(map[string]any{"task": in.Task, "results": results})
	})

	type listIn struct {
		Dir     string `json:"dir,omitempty"`
		Scope   string `json:"scope,omitempty" jsonschema:"project|team|org|all"`
		Limit   int    `json:"limit,omitempty" jsonschema:"maximum documents in this page; default 200"`
		Cursor  string `json:"cursor,omitempty" jsonschema:"opaque pagination cursor from a previous response"`
		Project string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_list",
		Description: "List documents in the current project, team, org or whole store.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in listIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		if err := requireStore(sess); err != nil {
			return errorResult(err)
		}
		dir, err := scopeDir(sess, in.Scope, in.Dir)
		if err != nil {
			return errorResult(err)
		}
		o, err := sess.Manager.OverlayDir(ctx, dir)
		if err != nil {
			return errorResult(err)
		}
		out := make([]docIndex, 0, len(o.Docs))
		for _, d := range o.Docs {
			out = append(out, docIndex{
				Path: d.Path, Title: d.Frontmatter.Title, Kind: d.Frontmatter.Kind,
				Status: d.Frontmatter.Status, Source: o.Sources[d.Path], State: o.States[d.Path],
			})
		}
		total := len(out)
		page, next, err := pageItems(out, in.Cursor, in.Limit, 200, 1000)
		if err != nil {
			return errorResult(err)
		}
		return textResult(map[string]any{"dir": dir, "docs": page, "total": total, "next_cursor": next})
	})

	type searchIn struct {
		Query   string `json:"query"`
		Scope   string `json:"scope,omitempty" jsonschema:"project|team|org|all"`
		Limit   int    `json:"limit,omitempty" jsonschema:"maximum hits in this page; default 50"`
		Cursor  string `json:"cursor,omitempty" jsonschema:"opaque pagination cursor from a previous response"`
		Project string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_search",
		Description: "Search with BM25 ranking, field weighting and snippets. Includes the active changeset overlay.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in searchIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		if err := requireStore(sess); err != nil {
			return errorResult(err)
		}
		dir, err := scopeDir(sess, in.Scope, "")
		if err != nil {
			return errorResult(err)
		}
		o, err := sess.Manager.OverlayDir(ctx, dir)
		if err != nil {
			return errorResult(err)
		}
		hits, err := search.Search(o.Docs, in.Query, len(o.Docs)+1)
		if err != nil {
			return errorResult(err)
		}
		total := len(hits)
		hits, next, err := pageItems(hits, in.Cursor, in.Limit, 50, 200)
		if err != nil {
			return errorResult(err)
		}
		for i := range hits {
			hits[i].Source = o.Sources[hits[i].Path]
			hits[i].State = o.States[hits[i].Path]
		}
		return textResult(map[string]any{"query": in.Query, "hits": hits, "count": len(hits), "total": total, "next_cursor": next})
	})

	type validateRefsIn struct {
		Path    string `json:"path,omitempty" jsonschema:"optional document path or subtree filter"`
		Scope   string `json:"scope,omitempty" jsonschema:"project|team|org|all; default project"`
		Project string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_validate_code_refs",
		Description: "Validate reference_impls paths at their resolved ref and provide a git-grep symbol fallback. Symbol/semantic checks depend on available providers.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in validateRefsIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		if err := requireStore(sess); err != nil {
			return errorResult(err)
		}
		dir, err := scopeDir(sess, in.Scope, "")
		if err != nil {
			return errorResult(err)
		}
		o, err := sess.Manager.OverlayDir(ctx, dir)
		if err != nil {
			return errorResult(err)
		}
		docs := o.Docs
		if strings.TrimSpace(in.Path) != "" {
			rel, err := resolvePath(sess, in.Path)
			if err != nil {
				return errorResult(err)
			}
			filtered := make([]*store.Doc, 0, 1)
			for _, d := range docs {
				if d.Path == rel {
					filtered = append(filtered, d)
				}
			}
			docs = filtered
		}
		validated, err := codeindex.ValidateDocs(ctx, sess.Context.RepoRoot, sess.Context.Project, docs)
		if err != nil {
			return errorResult(err)
		}
		return textResult(map[string]any{
			"repo_root": sess.Context.RepoRoot,
			"project":   sess.Context.Project,
			"docs":      validated,
			"summary":   codeindex.Summarize(validated),
		})
	})

	type readIn struct {
		Path    string `json:"path" jsonschema:"document path, absolute to the store or relative to the project area"`
		Rev     string `json:"rev,omitempty"`
		PR      int    `json:"pr,omitempty"`
		Project string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_read",
		Description: "Read a document from main, a revision or a PR. Returns base_rev/hash for optimistic writes and pending PRs touching the area.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in readIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		if err := requireStore(sess); err != nil {
			return errorResult(err)
		}
		rel, err := resolvePath(sess, in.Path)
		if err != nil {
			return errorResult(err)
		}
		var content, source, docState string
		switch {
		case in.PR > 0:
			content, err = sess.Runtime.Store.ReadPR(ctx, rel, in.PR)
			source = fmt.Sprintf("PR #%d", in.PR)
			docState = "pr"
		case in.Rev != "":
			content, err = sess.Runtime.Store.ReadAtRev(ctx, rel, in.Rev)
			source = in.Rev
			docState = "revision"
		default:
			// Prefer the active changeset when the agent is editing, so the
			// returned base_rev matches the file it will patch.
			read := false
			if area, aerr := changeset.AreaForPath(rel); aerr == nil {
				if activeContent, activeSource, activeState, rerr := sess.Manager.ReadFileState(ctx, area, rel); rerr == nil {
					content, source, docState, read = activeContent, activeSource, activeState, true
				}
			}
			if !read {
				content, err = sess.Runtime.Store.Read(rel)
				source = "main"
				docState = changeset.DocStateMain
			}
		}
		if err != nil {
			return errorResult(err)
		}
		if docState == changeset.DocStateDeleted {
			return textResult(map[string]any{
				"path": rel, "deleted": true, "content": "", "hash": "", "source": source, "state": docState,
			})
		}
		hash, _ := sess.Runtime.Store.Hash(ctx, content)
		var pending []any
		if area, err := changeset.AreaForPath(rel); err == nil && sess.Runtime.Forge != nil && sess.Runtime.Forge.Available() {
			if prs, err := sess.Runtime.Forge.ListOpenForArea(ctx, area); err == nil {
				for _, pr := range prs {
					pending = append(pending, map[string]any{"number": pr.Number, "title": pr.Title, "author": pr.User, "updated_at": pr.UpdatedAt})
				}
			}
		}
		return textResult(map[string]any{"path": rel, "content": content, "hash": hash, "source": source, "state": docState, "pending_prs": pending})
	})

	type historyIn struct {
		Path    string `json:"path"`
		WIP     bool   `json:"wip,omitempty"`
		Project string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_history",
		Description: "Show merged versions, authors, PRs and agent involvement; optionally local changeset commits.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in historyIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		if err := requireStore(sess); err != nil {
			return errorResult(err)
		}
		rel, err := resolvePath(sess, in.Path)
		if err != nil {
			return errorResult(err)
		}
		var revs []store.Revision
		if in.WIP {
			area, _ := changeset.AreaForPath(rel)
			c, err := sess.Manager.Active(area)
			if err != nil || c == nil {
				return errorResult(errors.New("no active changeset for this document"))
			}
			revs, err = sess.Runtime.Store.LogWIP(ctx, c.Path, rel)
			if err != nil {
				return errorResult(err)
			}
		} else {
			revs, err = sess.Runtime.Store.Log(ctx, rel, false)
			if err != nil {
				return errorResult(err)
			}
		}
		return textResult(map[string]any{"path": rel, "versions": revs})
	})

	type diffIn struct {
		Path    string `json:"path"`
		A       string `json:"a,omitempty"`
		B       string `json:"b,omitempty"`
		PR      int    `json:"pr,omitempty"`
		Project string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_diff",
		Description: "Diff document versions or a PR against main.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in diffIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		if err := requireStore(sess); err != nil {
			return errorResult(err)
		}
		rel, err := resolvePath(sess, in.Path)
		if err != nil {
			return errorResult(err)
		}
		var out string
		if in.PR > 0 {
			out, err = sess.Runtime.Store.DiffPR(ctx, rel, in.PR)
		} else {
			out, err = sess.Runtime.Store.Diff(ctx, rel, in.A, in.B)
		}
		if err != nil {
			return errorResult(err)
		}
		return textResult(map[string]any{"path": rel, "diff": out})
	})

	addTool(s, &mcp.Tool{
		Name:        "docs_pull",
		Description: "Fetch origin/main and fast-forward the local mirror. Use when docs_get_context reports a stale mirror.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, "")
		if err != nil {
			return errorResult(err)
		}
		if err := requireStore(sess); err != nil {
			return errorResult(err)
		}
		rev, t, err := sess.Runtime.Store.Pull(ctx)
		if err != nil {
			return errorResult(err)
		}
		return textResult(map[string]any{"rev": rev, "commit_time": t})
	})
}

func scopeDir(s *runtime.Session, scope, dir string) (string, error) {
	if dir != "" {
		dir = strings.Trim(dir, "/")
		if strings.HasPrefix(dir, "org/") || strings.HasPrefix(dir, "teams/") || strings.HasPrefix(dir, "projects/") || dir == "org" {
			return store.CleanRel(dir)
		}
		area := s.Context.ProjectArea()
		if area == "" {
			return "", errors.New("no project resolved")
		}
		return store.CleanRel(path.Join(area, dir))
	}
	switch strings.ToLower(scope) {
	case "", "project":
		if s.Context.ProjectArea() == "" {
			return "", errors.New("project is unresolved; pass project or scope=all")
		}
		return s.Context.ProjectArea(), nil
	case "team":
		if s.Context.TeamArea() == "" {
			return "", errors.New("team is unresolved")
		}
		return s.Context.TeamArea(), nil
	case "org":
		return "org", nil
	case "all":
		return "", nil
	default:
		return "", fmt.Errorf("unknown scope %q (project|team|org|all)", scope)
	}
}

func resolvePath(s *runtime.Session, p string) (string, error) {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if p == "" {
		return "", errors.New("path is required")
	}
	if strings.HasPrefix(p, "org/") || strings.HasPrefix(p, "teams/") || strings.HasPrefix(p, "projects/") || strings.HasPrefix(p, ".docsync/") {
		return store.CleanRel(p)
	}
	area := s.Context.ProjectArea()
	if area == "" {
		return "", errors.New("project is unresolved; pass a store-relative path or set project")
	}
	return store.CleanRel(path.Join(area, p))
}

func missingRequired(ctx context.Context, s *runtime.Session, area string) []string {
	o, err := s.Manager.Overlay(ctx, area)
	if err != nil {
		o = nil
	}
	present := map[string]bool{}
	if o != nil {
		for _, d := range o.Docs {
			present[d.Path] = true
		}
	}
	var missing []string
	for _, req := range s.Repo.Docs.Required {
		rel := store.JoinArea(area, req)
		if o != nil {
			if o.States[rel] == changeset.DocStateDeleted || !present[rel] {
				missing = append(missing, rel)
			}
			continue
		}
		if !s.Runtime.Store.ExistsPath(rel) {
			missing = append(missing, rel)
		}
	}
	return missing
}

func humanAge(d time.Duration) string {
	if d < 0 || d > 50*365*24*time.Hour {
		return "unknown"
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
