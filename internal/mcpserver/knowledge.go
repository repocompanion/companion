package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/bolacha/docsync/internal/changeset"
	"github.com/bolacha/docsync/internal/config"
	"github.com/bolacha/docsync/internal/gitx"
	"github.com/bolacha/docsync/internal/runtime"
	"github.com/bolacha/docsync/internal/state"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gopkg.in/yaml.v3"
)

type refIn struct {
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Ref    string `json:"ref,omitempty" jsonschema:"branch or tag; prefer main"`
	Commit string `json:"commit,omitempty"`
}

func registerKnowledgeTools(s *mcp.Server, p *provider) {
	type decisionIn struct {
		Title        string `json:"title"`
		Context      string `json:"context"`
		Decision     string `json:"decision"`
		Consequences string `json:"consequences"`
		Scope        string `json:"scope,omitempty" jsonschema:"project|team|org; default project"`
		Project      string `json:"project,omitempty"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "docs_record_decision",
		Description: "Create a date-stamped ADR. Project scope can use the active project changeset; org/team scope gets its own changeset and draft PR.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in decisionIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, in.Project)
		if err != nil {
			return errorResult(err)
		}
		area, own, err := knowledgeArea(sess, in.Scope)
		if err != nil {
			return errorResult(err)
		}
		if in.Scope == "team" && sess.Context.TeamArea() == "" {
			return errorResult(errors.New("team is unresolved; set team first"))
		}
		if own && sess.Runtime.Cfg.MCP.OrgWrite == "disabled" {
			return errorResult(errors.New("org/team writes are disabled by mcp.org_write"))
		}
		title := strings.TrimSpace(in.Title)
		if title == "" {
			return errorResult(errors.New("title is required"))
		}
		date := time.Now().Format("2006-01-02")
		slug := gitx.Slug(title)
		rel := path.Join(area, "adr", date+"-"+slug+".md")
		content := docWithFrontmatter(map[string]any{
			"id": slug, "kind": "adr", "title": title, "status": "proposed",
			"superseded_by": nil, "overrides": nil, "topics": []string{},
			"languages": sess.Context.Languages, "related": []string{},
			"owners": ownerList(sess), "last_reviewed": date,
		}, fmt.Sprintf("# %s\n\n## Context\n\n%s\n\n## Decision\n\n%s\n\n## Consequences\n\n%s\n", title, in.Context, in.Decision, in.Consequences))
		c, err := writeNew(ctx, sess, area, rel, content, own, "ADR: "+title)
		if err != nil {
			var existing *ExistingDocError
			if errors.As(err, &existing) {
				return existingDocResult(existing)
			}
			return errorResult(err)
		}
		return decisionResult(ctx, sess, c, rel, own)
	})

	type patternIn struct {
		Title          string   `json:"title"`
		WhenToUse      string   `json:"when_to_use"`
		Rules          []string `json:"rules"`
		Skeleton       string   `json:"skeleton,omitempty"`
		Category       string   `json:"category,omitempty" jsonschema:"optional subdirectory under patterns"`
		Scope          string   `json:"scope,omitempty" jsonschema:"org|team; default org"`
		Languages      []string `json:"languages,omitempty"`
		ReferenceImpls []refIn  `json:"reference_impls,omitempty"`
		Project        string   `json:"project,omitempty"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "docs_propose_pattern",
		Description: "Draft a new org/team pattern (status proposed) from code the agent just built, in its own changeset and draft PR.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in patternIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, in.Project)
		if err != nil {
			return errorResult(err)
		}
		if sess.Runtime.Cfg.MCP.OrgWrite == "disabled" {
			return errorResult(errors.New("org writes are disabled by mcp.org_write"))
		}
		title := strings.TrimSpace(in.Title)
		if title == "" {
			return errorResult(errors.New("title is required"))
		}
		if in.WhenToUse == "" {
			return errorResult(errors.New("when_to_use is required"))
		}
		slug := gitx.Slug(title)
		area := "org"
		if in.Scope == "team" {
			area = sess.Context.TeamArea()
			if area == "" {
				return errorResult(errors.New("team is unresolved; set team first"))
			}
		} else if in.Scope != "" && in.Scope != "org" {
			return errorResult(errors.New("scope must be org or team"))
		}
		dir := path.Join(area, "patterns")
		if in.Category != "" {
			dir = path.Join(dir, gitx.Slug(in.Category))
		}
		rel := path.Join(dir, slug+".md")
		refs := make([]map[string]any, 0, len(in.ReferenceImpls))
		for _, r := range in.ReferenceImpls {
			ref := r.Ref
			if ref == "" && r.Commit == "" {
				ref = "main"
			}
			refs = append(refs, map[string]any{"repo": r.Repo, "path": r.Path, "ref": ref, "commit": r.Commit})
		}
		languages := in.Languages
		if len(languages) == 0 {
			languages = sess.Context.Languages
		}
		var rules strings.Builder
		for _, r := range in.Rules {
			fmt.Fprintf(&rules, "- %s\n", r)
		}
		content := docWithFrontmatter(map[string]any{
			"id": slug, "kind": "pattern", "title": title, "status": "proposed",
			"superseded_by": nil, "overrides": nil, "topics": []string{},
			"languages": languages, "related": []string{}, "reference_impls": refs,
			"owners": ownerList(sess), "last_reviewed": time.Now().Format("2006-01-02"),
		}, fmt.Sprintf("# %s\n\n## When to use\n\n%s\n\n## Rules\n\n%s\n\n## Skeleton\n\n~~~text\n%s\n~~~\n\n## Reference implementations\n\n%s\n\n## Pitfalls\n\n- Review this proposed pattern before adoption.\n", title, in.WhenToUse, rules.String(), in.Skeleton, formatRefs(in.ReferenceImpls)))
		c, err := writeNew(ctx, sess, area, rel, content, true, "pattern: "+title)
		if err != nil {
			var existing *ExistingDocError
			if errors.As(err, &existing) {
				return existingDocResult(existing)
			}
			return errorResult(err)
		}
		return decisionResult(ctx, sess, c, rel, true)
	})

	type scaffoldIn struct {
		Project string `json:"project,omitempty"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "docs_scaffold_project",
		Description: "Explicitly create a README/architecture skeleton for a project with no docs, in a new changeset and draft PR.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in scaffoldIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, in.Project)
		if err != nil {
			return errorResult(err)
		}
		project := sess.Context.Project
		if in.Project != "" {
			project = in.Project
		}
		if project == "" {
			return errorResult(errors.New("project is unresolved"))
		}
		area := "projects/" + strings.Trim(project, "/")
		o, err := sess.Manager.Overlay(ctx, area)
		if err != nil {
			return errorResult(err)
		}
		present := map[string]bool{}
		for _, d := range o.Docs {
			present[d.Path] = true
		}
		readmeRel := path.Join(area, "README.md")
		architectureRel := path.Join(area, "architecture.md")
		if present[readmeRel] && present[architectureRel] {
			out := map[string]any{
				"ok": true, "existing": true, "files": []string{readmeRel, architectureRel},
			}
			if c, _ := sess.Manager.Active(area); c != nil {
				out["changeset"] = *c
				out["pending_sync"] = true
			} else {
				out["published"] = true
			}
			return textResult(out)
		}
		c, err := sess.Manager.EnsureNamed(ctx, area, "project docs: "+project)
		if err != nil {
			return errorResult(err)
		}
		files := map[string]string{}
		for _, name := range []string{"README.md", "architecture.md"} {
			title := project + " " + strings.TrimSuffix(name, ".md")
			if name == "README.md" {
				title = project
			}
			files[name] = docWithFrontmatter(map[string]any{
				"id": gitx.Slug(strings.TrimSuffix(name, ".md")), "kind": "project-doc", "title": title,
				"status": "approved", "superseded_by": nil, "overrides": nil,
				"topics": []string{}, "languages": sess.Context.Languages, "related": []string{},
				"owners": ownerList(sess), "last_reviewed": time.Now().Format("2006-01-02"),
			}, fmt.Sprintf("# %s\n\n## What it is\n\nTODO: describe the service and its responsibility.\n\n## Architecture\n\nTODO: components, data flow and trade-offs.\n\n## Operations\n\nTODO: dashboards, alerts, runbooks and ownership.\n\n## Documentation map\n\n- `architecture.md` — components, data flow and trade-offs\n- `adr/` — one dated file per decision\n- `flows/` — one file per runtime or business flow\n- `runbooks/` — one file per operational procedure\n\nDo not collapse multiple decisions, flows or runbooks into one aggregate file.\n", title))
		}
		for name, content := range files {
			rel := path.Join(area, name)
			if _, err := os.Stat(filepath.Join(c.Path, filepath.FromSlash(rel))); err == nil {
				continue
			}
			if err := validateAgentDoc(rel, content); err != nil {
				return errorResult(err)
			}
			if _, _, err := sess.Manager.WriteFile(ctx, area, rel, content, "", "agent:mcp"); err != nil {
				return errorResult(err)
			}
		}
		res, err := sess.Manager.Sync(ctx, c.ID, "agent:mcp")
		if err != nil {
			return pendingResult(res, err)
		}
		return textResult(map[string]any{"ok": true, "changeset": res.Change, "files": []string{area + "/README.md", area + "/architecture.md"}, "pr": res.PR})
	})

	type setContextIn struct {
		Field   string `json:"field" jsonschema:"project|team|domain|languages|machine|user.default_team"`
		Value   string `json:"value"`
		Scope   string `json:"scope" jsonschema:"user|repo|registry"`
		Project string `json:"project,omitempty"`
	}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "docs_set_context",
		Description: "Set user or repo context. registry scope opens a docsync PR against the org registry.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in setContextIn) (*mcp.CallToolResult, any, error) {
		sess, err := p.session(ctx, in.Project)
		if err != nil {
			return errorResult(err)
		}
		switch in.Scope {
		case "user":
			rt, err := p.runtime()
			if err != nil {
				return errorResult(err)
			}
			if err := config.SetUserKey(&rt.Cfg, in.Field, in.Value); err != nil {
				return errorResult(err)
			}
			if err := config.SaveUser(rt.Cfg); err != nil {
				return errorResult(err)
			}
			return textResult(map[string]any{"ok": true, "scope": "user", "field": in.Field, "value": in.Value})
		case "repo":
			root := sess.Context.RepoRoot
			if root == "" {
				return errorResult(errors.New("not inside a git repository"))
			}
			repo, err := config.LoadRepo(root)
			if err != nil {
				return errorResult(err)
			}
			if err := config.SetRepoKey(&repo, in.Field, in.Value); err != nil {
				return errorResult(err)
			}
			if err := config.SaveRepo(root, repo); err != nil {
				return errorResult(err)
			}
			return textResult(map[string]any{"ok": true, "scope": "repo", "path": path.Join(root, ".docsync.yaml"), "note": "commit this through the code repo's normal PR flow"})
		case "registry":
			return setRegistry(ctx, sess, in.Project, in.Field, in.Value)
		default:
			return errorResult(errors.New("scope must be user, repo or registry"))
		}
	})
}

func knowledgeArea(sess *runtime.Session, scope string) (area string, own bool, err error) {
	switch strings.ToLower(scope) {
	case "", "project":
		area = sess.Context.ProjectArea()
		if area == "" {
			return "", false, errors.New("project is unresolved")
		}
		return area, false, nil
	case "team":
		area = sess.Context.TeamArea()
		if area == "" {
			return "", false, errors.New("team is unresolved")
		}
		return area, true, nil
	case "org":
		return "org", true, nil
	default:
		return "", false, errors.New("scope must be project, team or org")
	}
}

// ExistingDocError means the idempotent target already exists, so the caller
// should return it instead of creating another changeset or file.
type ExistingDocError struct {
	Path   string
	Source string
	State  string
	Change *state.Change
}

func (e *ExistingDocError) Error() string {
	return fmt.Sprintf("%s already exists (%s)", e.Path, e.State)
}

func findExistingDoc(ctx context.Context, sess *runtime.Session, area, rel string) (*ExistingDocError, error) {
	o, err := sess.Manager.Overlay(ctx, area)
	if err != nil {
		return nil, err
	}
	state := o.States[rel]
	if state == changeset.DocStateDeleted {
		return nil, nil
	}
	if state != "" {
		existing := &ExistingDocError{Path: rel, Source: o.Sources[rel], State: state}
		if c, _ := sess.Manager.Active(area); c != nil && state != changeset.DocStateMain {
			existing.Change = c
		}
		return existing, nil
	}
	// Org/team knowledge writes are usually separate changesets, so the
	// active overlay may not include an earlier request. Check every open local
	// changeset in this area before creating another file/PR.
	list, err := sess.Manager.List()
	if err != nil {
		return nil, err
	}
	for i := range list {
		c := list[i]
		if c.Area != area || !c.IsOpen() {
			continue
		}
		full := filepath.Join(c.Path, filepath.FromSlash(rel))
		if _, err := os.Stat(full); err == nil {
			return &ExistingDocError{Path: rel, Source: "changeset " + c.ID, State: "changeset", Change: &c}, nil
		}
	}
	return nil, nil
}

func existingDocResult(e *ExistingDocError) (*mcp.CallToolResult, any, error) {
	out := map[string]any{
		"ok": true, "existing": true, "path": e.Path, "source": e.Source, "state": e.State,
	}
	if e.Change != nil {
		out["changeset"] = *e.Change
		out["pending_sync"] = true
	} else {
		out["published"] = true
	}
	return textResult(out)
}

func writeNew(ctx context.Context, sess *runtime.Session, area, rel, content string, own bool, title string) (stateChange, error) {
	if err := validateAgentDoc(rel, content); err != nil {
		return state.Change{}, err
	}
	if existing, err := findExistingDoc(ctx, sess, area, rel); err != nil {
		return state.Change{}, err
	} else if existing != nil {
		return state.Change{}, existing
	}
	if own {
		c, err := sess.Manager.Start(ctx, title, area)
		if err != nil {
			return state.Change{}, err
		}
		_, _, err = sess.Manager.WriteFile(ctx, area, rel, content, "", "agent:mcp")
		return c, err
	}
	if _, err := sess.Manager.EnsureNamed(ctx, area, title); err != nil {
		return state.Change{}, err
	}
	c, _, err := sess.Manager.WriteFile(ctx, area, rel, content, "", "agent:mcp")
	return c, err
}

type stateChange = state.Change

func decisionResult(ctx context.Context, sess *runtime.Session, c state.Change, rel string, own bool) (*mcp.CallToolResult, any, error) {
	if own {
		res, err := sess.Manager.Sync(ctx, c.ID, "agent:mcp")
		if err != nil {
			return pendingResult(res, err)
		}
		return textResult(map[string]any{"ok": true, "changeset": res.Change, "path": rel, "pr": res.PR, "draft_pr": true})
	}
	return textResult(map[string]any{"ok": true, "changeset": c, "path": rel, "pending_sync": true})
}

func docWithFrontmatter(fm map[string]any, body string) string {
	b, _ := yaml.Marshal(fm)
	return "---\n" + string(b) + "---\n\n" + body
}

func ownerList(sess *runtime.Session) []string {
	if sess.Context.Team != "" {
		return []string{sess.Context.Team}
	}
	if sess.Context.UserName != "" {
		return []string{gitx.Slug(sess.Context.UserName)}
	}
	return []string{"platform-team"}
}

func formatRefs(refs []refIn) string {
	if len(refs) == 0 {
		return "_Add reference implementations before adoption._"
	}
	var b strings.Builder
	b.WriteString("| Repository | Path | Ref |\n|---|---|---|\n")
	for _, r := range refs {
		ref := r.Ref
		if ref == "" {
			ref = r.Commit
		}
		if ref == "" {
			ref = "main"
		}
		fmt.Fprintf(&b, "| %s | `%s` | `%s` |\n", r.Repo, r.Path, ref)
	}
	return b.String()
}

func setRegistry(ctx context.Context, sess *runtime.Session, project, field, value string) (*mcp.CallToolResult, any, error) {
	if project == "" {
		project = sess.Context.Project
	}
	if project == "" {
		return errorResult(errors.New("project is unresolved"))
	}
	if field != "team" && field != "domain" {
		return errorResult(errors.New("registry scope supports field team or domain"))
	}
	if sess.Runtime.Cfg.MCP.OrgWrite == "disabled" {
		return errorResult(errors.New("registry writes are disabled by mcp.org_write"))
	}
	area := ".docsync"
	c, err := sess.Manager.Ensure(ctx, area)
	if err != nil {
		return errorResult(err)
	}
	rel := ".docsync/registry.yaml"
	var reg config.Registry
	if raw, err := sess.Runtime.Store.Read(rel); err == nil {
		_ = yaml.Unmarshal([]byte(raw), &reg)
	}
	if reg.Repos == nil {
		reg.Repos = map[string]config.RepoMapping{}
	}
	m := reg.Repos[project]
	if field == "team" {
		m.Team = value
	} else {
		m.Domain = value
	}
	reg.Repos[project] = m
	b, err := yaml.Marshal(&reg)
	if err != nil {
		return errorResult(err)
	}
	base := ""
	if _, err := os.Stat(filepath.Join(c.Path, filepath.FromSlash(rel))); err == nil {
		base, err = sess.Manager.HashFile(ctx, c, rel)
		if err != nil {
			return errorResult(err)
		}
	}
	if _, _, err := sess.Manager.WriteFile(ctx, area, rel, string(b), base, "agent:mcp"); err != nil {
		return errorResult(err)
	}
	res, err := sess.Manager.Sync(ctx, c.ID, "agent:mcp")
	if err != nil {
		return pendingResult(res, err)
	}
	return textResult(map[string]any{"ok": true, "changeset": res.Change, "pr": res.PR, "registry": reg.Repos[project]})
}

func registerPrompts(s *mcp.Server, p *provider) {
	addPrompt(s, &mcp.Prompt{
		Name: "start-tech-docs", Description: "Bootstrap a project's README and architecture docs",
		Arguments: []*mcp.PromptArgument{{Name: "project", Description: "owner/repo", Required: false}},
	}, func(args map[string]string) string {
		return fmt.Sprintf("Call docs_get_context%s and read its structure. If it lists an open shared PR, join it with docs_change_join. If the project has no docs, call docs_scaffold_project first. Analyze the codebase, then create/refine README.md and architecture.md in the canonical layout with docs_write/docs_patch. Submit with docs_change_submit when ready.", projectArg(args))
	})
	addPrompt(s, &mcp.Prompt{
		Name: "refine-flow", Description: "Read, patch and submit a flow document",
		Arguments: []*mcp.PromptArgument{{Name: "name", Description: "flow name or path", Required: true}},
	}, func(args map[string]string) string {
		return fmt.Sprintf("Call docs_get_context, then docs_read the flow %q and docs_history. Patch it with docs_patch using base_rev from docs_read, then docs_change_submit.", args["name"])
	})
	addPrompt(s, &mcp.Prompt{
		Name: "generate-docs", Description: "Find undocumented modules and draft docs",
	}, func(args map[string]string) string {
		return "Call docs_get_context and read its structure. Find undocumented modules in the current codebase. For each, draft a doc in the canonical layout with docs_write, using docs_get_guidance for patterns. Do not create flat aggregate files. Submit when the drafts are coherent."
	})
	addPrompt(s, &mcp.Prompt{
		Name: "implement-with-guidance", Description: "Implement a task using organization guidance",
		Arguments: []*mcp.PromptArgument{{Name: "task", Description: "coding task", Required: true}},
	}, func(args map[string]string) string {
		return fmt.Sprintf("Call docs_get_guidance for %q. Use the returned patterns, ADRs and reference implementations. After implementing, offer to record an ADR or propose a pattern if something new was decided.", args["task"])
	})
	addPrompt(s, &mcp.Prompt{
		Name: "address-doc-review", Description: "Address PR review comments",
		Arguments: []*mcp.PromptArgument{{Name: "pr", Description: "PR number", Required: true}},
	}, func(args map[string]string) string {
		return fmt.Sprintf("Call docs_review_comments for PR #%s. Reopen the changeset with docs_change_reopen if it is ready, make the fixes with docs_patch, then docs_change_submit again.", args["pr"])
	})
}

func addPrompt(s *mcp.Server, prompt *mcp.Prompt, text func(map[string]string) string) {
	s.AddPrompt(prompt, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: prompt.Description,
			Messages: []*mcp.PromptMessage{{
				Role:    mcp.Role("user"),
				Content: &mcp.TextContent{Text: text(req.Params.Arguments)},
			}},
		}, nil
	})
}

func projectArg(args map[string]string) string {
	if v := strings.TrimSpace(args["project"]); v != "" {
		return " for project " + v
	}
	return ""
}
