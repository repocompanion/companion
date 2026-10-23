package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bolacha/docsync/internal/changeset"
	"github.com/bolacha/docsync/internal/docrules"
	"github.com/bolacha/docsync/internal/runtime"
	"github.com/bolacha/docsync/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerChangeTools(s *mcp.Server, p *provider) {
	type startIn struct {
		Title   string `json:"title"`
		Area    string `json:"area,omitempty" jsonschema:"org..., teams/<team>... or projects/<owner>/<repo>...; defaults to the current project"`
		Project string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_change_start",
		Description: "Start a named changeset and make it active for its area. Optional, since writes auto-create one.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in startIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		area, err := agentArea(sess, in.Area, "")
		if err != nil {
			return errorResult(err)
		}
		c, err := sess.Manager.Start(ctx, in.Title, area)
		if err != nil {
			return errorResult(err)
		}
		return textResult(c)
	})

	type joinIn struct {
		PR      int    `json:"pr"`
		Project string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_change_join",
		Description: "Join an open shared changeset/PR and make it active for its area.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in joinIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		c, err := sess.Manager.Join(ctx, in.PR)
		if err != nil {
			return errorResult(err)
		}
		return textResult(c)
	})

	type writeIn struct {
		Path    string `json:"path" jsonschema:"document path, absolute to the store or relative to the project area"`
		Content string `json:"content"`
		BaseRev string `json:"base_rev,omitempty" jsonschema:"required for an existing file; use the hash returned by docs_read"`
		Area    string `json:"area,omitempty"`
		Project string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_write",
		Description: "Create or replace a doc in the active changeset. Enforces YAML frontmatter and canonical paths; base_rev is required for existing files.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in writeIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		area, err := agentArea(sess, in.Area, in.Path)
		if err != nil {
			return errorResult(err)
		}
		if !agentWritable(area) {
			return errorResult(errors.New("org/ and teams/ writes must use docs_record_decision or docs_propose_pattern"))
		}
		if err := validAgentWritePath(in.Path); err != nil {
			return errorResult(err)
		}
		rel := store.RelativeToArea(area, in.Path)
		if err := validateAgentDoc(rel, in.Content); err != nil {
			return errorResult(err)
		}
		c, changed, err := sess.Manager.WriteFile(ctx, area, rel, in.Content, in.BaseRev, "agent:mcp")
		if err != nil {
			var bre *changeset.BaseRevError
			if errors.As(err, &bre) {
				hash, _ := sess.Runtime.Store.Hash(ctx, bre.Content)
				return textResult(map[string]any{
					"ok": false, "error": "base_rev_required_or_stale",
					"path": bre.Path, "base_rev": hash, "latest_content": bre.Content,
				})
			}
			return errorResult(err)
		}
		hash, _ := sess.Runtime.Store.Hash(ctx, in.Content)
		return textResult(map[string]any{
			"ok": true, "changed": changed, "changeset": c, "path": rel,
			"base_rev": hash, "pending_sync": true,
		})
	})

	type patchEdit struct {
		Old string `json:"old" jsonschema:"exact text to replace"`
		New string `json:"new"`
	}
	type patchIn struct {
		Path    string      `json:"path"`
		Edits   []patchEdit `json:"edits"`
		BaseRev string      `json:"base_rev"`
		Area    string      `json:"area,omitempty"`
		Project string      `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_patch",
		Description: "Apply targeted find/replace edits to an existing doc and commit locally. Enforces frontmatter and canonical paths; prefer this over whole-file replacement.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in patchIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		area, err := agentArea(sess, in.Area, in.Path)
		if err != nil {
			return errorResult(err)
		}
		if !agentWritable(area) {
			return errorResult(errors.New("org/ and teams/ writes must use docs_record_decision or docs_propose_pattern"))
		}
		edits := make([]changeset.Edit, 0, len(in.Edits))
		for _, e := range in.Edits {
			edits = append(edits, changeset.Edit{Old: e.Old, New: e.New})
		}
		rel := store.RelativeToArea(area, in.Path)
		current, _, rerr := sess.Manager.ReadFile(ctx, area, rel)
		if rerr != nil {
			return errorResult(rerr)
		}
		final := current
		for _, e := range edits {
			if e.Old == "" || !strings.Contains(final, e.Old) {
				return errorResult(fmt.Errorf("patch block not found in %s", rel))
			}
			final = strings.Replace(final, e.Old, e.New, 1)
		}
		if err := validateAgentDoc(rel, final); err != nil {
			return errorResult(err)
		}
		c, err := sess.Manager.PatchFile(ctx, area, rel, in.BaseRev, edits, "agent:mcp")
		if err != nil {
			var bre *changeset.BaseRevError
			if errors.As(err, &bre) {
				hash, _ := sess.Runtime.Store.Hash(ctx, bre.Content)
				return textResult(map[string]any{
					"ok": false, "error": "base_rev_required_or_stale",
					"path": bre.Path, "base_rev": hash, "latest_content": bre.Content,
				})
			}
			return errorResult(err)
		}
		return textResult(map[string]any{"ok": true, "changeset": c, "pending_sync": true})
	})

	type moveIn struct {
		Path    string `json:"path" jsonschema:"current document path"`
		NewPath string `json:"new_path" jsonschema:"destination path inside the same area"`
		BaseRev string `json:"base_rev" jsonschema:"blob hash from docs_read"`
		Area    string `json:"area,omitempty"`
		Project string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_move",
		Description: "Move or rename a document inside its area with git mv. Requires base_rev, validates the destination layout and rejects collisions.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in moveIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		area, err := agentArea(sess, in.Area, in.Path)
		if err != nil {
			return errorResult(err)
		}
		if !agentWritable(area) {
			return errorResult(errors.New("org/ and teams/ moves must use the human CLI"))
		}
		if err := validAgentWritePath(in.Path); err != nil {
			return errorResult(err)
		}
		if err := validAgentWritePath(in.NewPath); err != nil {
			return errorResult(err)
		}
		oldRel := store.RelativeToArea(area, in.Path)
		newRel := store.RelativeToArea(area, in.NewPath)
		c, err := sess.Manager.MoveFile(ctx, area, oldRel, newRel, in.BaseRev, "agent:mcp")
		if err != nil {
			var bre *changeset.BaseRevError
			if errors.As(err, &bre) {
				hash, _ := sess.Runtime.Store.Hash(ctx, bre.Content)
				return textResult(map[string]any{
					"ok": false, "error": "base_rev_required_or_stale",
					"path": bre.Path, "base_rev": hash, "latest_content": bre.Content,
				})
			}
			return errorResult(err)
		}
		return textResult(map[string]any{"ok": true, "moved_from": oldRel, "moved_to": newRel, "changeset": c, "pending_sync": true})
	})

	type deleteIn struct {
		Path    string `json:"path"`
		BaseRev string `json:"base_rev" jsonschema:"blob hash from docs_read"`
		Area    string `json:"area,omitempty"`
		Project string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_delete",
		Description: "Delete a document in the active changeset. Requires base_rev so a newer edit is never removed accidentally.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in deleteIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		area, err := agentArea(sess, in.Area, in.Path)
		if err != nil {
			return errorResult(err)
		}
		if !agentWritable(area) {
			return errorResult(errors.New("org/ and teams/ deletes are human CLI actions"))
		}
		if err := validAgentWritePath(in.Path); err != nil {
			return errorResult(err)
		}
		rel := store.RelativeToArea(area, in.Path)
		c, err := sess.Manager.DeleteFile(ctx, area, rel, in.BaseRev, "agent:mcp")
		if err != nil {
			var bre *changeset.BaseRevError
			if errors.As(err, &bre) {
				hash, _ := sess.Runtime.Store.Hash(ctx, bre.Content)
				return textResult(map[string]any{
					"ok": false, "error": "base_rev_required_or_stale",
					"path": bre.Path, "base_rev": hash, "latest_content": bre.Content,
				})
			}
			return errorResult(err)
		}
		return textResult(map[string]any{"ok": true, "deleted": rel, "changeset": c, "pending_sync": true})
	})

	type syncIn struct {
		Changeset string `json:"changeset,omitempty"`
		Project   string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_change_sync",
		Description: "Commit, integrate the remote branch, push and create/update the draft PR. Refuses to push when the PR is ready; call docs_change_reopen first.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in syncIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		res, err := sess.Manager.Sync(ctx, in.Changeset, "agent:mcp")
		if err != nil {
			return pendingResult(res, err)
		}
		return textResult(map[string]any{"ok": true, "result": res})
	})

	type submitIn struct {
		Changeset string `json:"changeset,omitempty"`
		Summary   string `json:"summary,omitempty"`
		Project   string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_change_submit",
		Description: "Sync, lint changed docs, preflight code references, rebase on main if needed, write the PR description, mark the PR ready and freeze the changeset. Lint/code-reference errors keep the PR draft. Refuses if already ready.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in submitIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		res, err := sess.Manager.Submit(ctx, in.Changeset, in.Summary, "agent:mcp")
		if err != nil {
			var lintErr *changeset.LintError
			if errors.As(err, &lintErr) {
				return jsonErrorResult(map[string]any{
					"ok": false, "error": "lint_failed", "issues": lintErr.Result.Issues,
					"docs":   lintErr.Result.Docs,
					"hint":   "fix the errors, or a human can run `docsync change submit --skip-lint`",
					"result": res,
				})
			}
			var codeErr *changeset.CodeRefsError
			if errors.As(err, &codeErr) {
				return jsonErrorResult(map[string]any{
					"ok": false, "error": "code_refs_failed", "issues": codeErr.Issues,
					"docs":   codeErr.Docs,
					"hint":   "fix missing paths/symbols; stale or unverified references are warnings",
					"result": res,
				})
			}
			return pendingResult(res, err)
		}
		return textResult(map[string]any{"ok": true, "result": res, "frozen": true})
	})

	type reopenIn struct {
		Changeset string `json:"changeset,omitempty"`
		Project   string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_change_reopen",
		Description: "Return a ready PR to draft so further syncs may push commits.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in reopenIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		c, err := sess.Manager.Reopen(ctx, in.Changeset)
		if err != nil {
			return errorResult(err)
		}
		return textResult(c)
	})

	type statusIn struct {
		Changeset string `json:"changeset,omitempty"`
		Project   string `json:"project,omitempty"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_change_status",
		Description: "Show local dirty/committed state, PR state, CI/review status where available, frozen state and sync errors.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in statusIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, in.Project)
		if err != nil {
			return errorResult(err)
		}
		c, err := sess.Manager.Resolve(in.Changeset)
		if err != nil {
			return errorResult(err)
		}
		if c2, err := sess.Manager.Reconcile(ctx, c); err == nil {
			c = c2
		}
		dirty, _ := sess.Manager.Dirty(ctx, c)
		out := map[string]any{"changeset": c, "dirty": dirty, "frozen": c.IsReady()}
		if c.PR > 0 && sess.Runtime.Forge != nil && sess.Runtime.Forge.Available() {
			if pr, err := sess.Runtime.Forge.Get(ctx, c.PR); err == nil {
				out["pr"] = pr
			}
		}
		return textResult(out)
	})

	type commentsIn struct {
		PR int `json:"pr"`
	}
	addTool(s, &mcp.Tool{
		Name:        "docs_review_comments",
		Description: "Fetch review comments so the agent can address feedback in the same changeset.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in commentsIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, req, "")
		if err != nil {
			return errorResult(err)
		}
		if sess.Runtime.Forge == nil || !sess.Runtime.Forge.Available() {
			return errorResult(errors.New("GitHub token unavailable"))
		}
		comments, err := sess.Runtime.Forge.ReviewComments(ctx, in.PR)
		if err != nil {
			return errorResult(err)
		}
		return textResult(map[string]any{"pr": in.PR, "comments": comments})
	})
}

func agentArea(s *runtime.Session, area, p string) (string, error) {
	if area != "" {
		area = strings.Trim(area, "/")
		if err := changeset.ValidateArea(area); err != nil {
			return "", err
		}
		return area, nil
	}
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if strings.HasPrefix(p, "org/") || strings.HasPrefix(p, "teams/") || strings.HasPrefix(p, "projects/") || strings.HasPrefix(p, ".docsync/") {
		return changeset.AreaForPath(p)
	}
	if s.Context.ProjectArea() == "" {
		return "", errors.New("project is unresolved; pass project or area")
	}
	return s.Context.ProjectArea(), nil
}

func agentWritable(area string) bool {
	return !(area == "org" || strings.HasPrefix(area, "org/") ||
		area == "teams" || strings.HasPrefix(area, "teams/") ||
		area == ".docsync" || strings.HasPrefix(area, ".docsync/"))
}

func validAgentWritePath(p string) error {
	if docrules.IsAgentDocumentPath(p) {
		return nil
	}
	return fmt.Errorf("agents may write or delete .md and .mmd files; adding binary images is a human action")
}
