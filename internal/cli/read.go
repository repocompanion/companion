package cli

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/bolacha/docsync/internal/changeset"
	"github.com/bolacha/docsync/internal/config"
	"github.com/bolacha/docsync/internal/guidance"
	"github.com/bolacha/docsync/internal/runtime"
	"github.com/bolacha/docsync/internal/search"
	"github.com/bolacha/docsync/internal/store"
	"github.com/spf13/cobra"
)

func (a *App) contextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "context",
		Short: "Show resolved context and where each value came from",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(s.Context)
			}
			c := s.Context
			fmt.Printf("%-10s %-42s %s\n", "user", formatUser(c), source(c, "user"))
			fmt.Printf("%-10s %-42s %s\n", "machine", valueOr(c.Machine, "-"), source(c, "machine"))
			fmt.Printf("%-10s %-42s %s\n", "project", valueOr(c.Project, "-"), source(c, "project"))
			fmt.Printf("%-10s %-42s %s\n", "team", valueOr(c.Team, "-"), source(c, "team"))
			fmt.Printf("%-10s %-42s %s\n", "domain", valueOr(c.Domain, "-"), source(c, "domain"))
			fmt.Printf("%-10s %-42s %s\n", "languages", valueOr(strings.Join(c.Languages, ", "), "-"), source(c, "languages"))
			fmt.Printf("%-10s %-42s\n", "area", valueOr(c.ProjectArea(), "-"))
			fmt.Printf("%-10s %-42s %s\n", "reviewers", valueOr(c.Reviewers, "-"), source(c, "reviewers"))
			for _, u := range c.Unresolved {
				fmt.Printf("unresolved: %s\n", u)
			}
			for _, w := range c.Warnings {
				fmt.Printf("warning: %s\n", w)
			}
			return nil
		},
	}
}

func (a *App) projectCmd() *cobra.Command {
	var showPath bool
	var projectPath string
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Show the resolved project, team and area",
		RunE: func(cmd *cobra.Command, args []string) error {
			var s *runtime.Session
			var err error
			if projectPath != "" {
				rt, rerr := runtime.New(projectPath)
				if rerr != nil {
					return rerr
				}
				s, err = rt.Session(context.Background(), a.Project, a.Team, a.Domain, a.Languages)
			} else {
				s, err = a.session()
			}
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(map[string]any{
					"project": s.Context.Project, "team": s.Context.Team,
					"area": s.Context.ProjectArea(), "repo_root": s.Context.RepoRoot,
				})
			}
			if showPath {
				fmt.Println(s.Context.ProjectArea())
				return nil
			}
			fmt.Printf("project: %s\n", valueOr(s.Context.Project, "(unresolved)"))
			fmt.Printf("team:    %s\n", valueOr(s.Context.Team, "(unresolved)"))
			fmt.Printf("area:    %s\n", valueOr(s.Context.ProjectArea(), "(unresolved)"))
			return nil
		},
	}
	cmd.Flags().StringVar(&projectPath, "path", "", "resolve context from this directory instead of the current one")
	cmd.Flags().BoolVar(&showPath, "area", false, "print only the store-relative area")
	return cmd
}

func (a *App) lsCmd() *cobra.Command {
	var pending bool
	cmd := &cobra.Command{
		Use:   "ls [DIR]",
		Short: "List documents in an area",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireStore(); err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			dir := s.Context.ProjectArea()
			if len(args) == 1 {
				dir = resolveDirArg(s.Context.ProjectArea(), args[0])
			}
			if dir == "" {
				return fmt.Errorf("no area resolved; pass a directory")
			}
			o, err := s.Manager.OverlayDir(context.Background(), dir)
			if err != nil {
				return err
			}
			type item struct {
				Path   string `json:"path"`
				Title  string `json:"title,omitempty"`
				Kind   string `json:"kind,omitempty"`
				Status string `json:"status,omitempty"`
				Source string `json:"source,omitempty"`
				State  string `json:"state,omitempty"`
			}
			items := make([]item, 0, len(o.Docs))
			for _, d := range o.Docs {
				items = append(items, item{
					Path: d.Path, Title: d.Frontmatter.Title, Kind: d.Frontmatter.Kind,
					Status: d.Frontmatter.Status, Source: o.Sources[d.Path], State: o.States[d.Path],
				})
			}
			if a.JSON {
				return a.printJSON(map[string]any{"dir": dir, "docs": items})
			}
			for _, d := range items {
				extra := strings.TrimSpace(strings.Join(nonEmpty(d.Kind, d.Status), " "))
				if d.State != "" && d.State != changeset.DocStateMain {
					extra = strings.TrimSpace(extra + " " + d.State)
				}
				if extra != "" {
					fmt.Printf("%-70s %s\n", d.Path, extra)
				} else {
					fmt.Println(d.Path)
				}
				if d.Title != "" {
					fmt.Printf("  %s\n", d.Title)
				}
			}
			if pending {
				if s.Runtime.Forge == nil || !s.Runtime.Forge.Available() {
					fmt.Println("pending: GitHub token unavailable")
					return nil
				}
				area := dir
				if ar, err := changeset.AreaForPath(dir); err == nil {
					area = ar
				}
				prs, err := s.Runtime.Forge.ListOpenForArea(context.Background(), area)
				if err != nil {
					return err
				}
				if len(prs) == 0 {
					fmt.Println("pending: none")
					return nil
				}
				for _, pr := range prs {
					fmt.Printf("pending: #%d %s (%s, %s)\n", pr.Number, pr.Title, pr.User, pr.StateName())
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&pending, "pending", false, "also show open PRs touching this area")
	return cmd
}

func (a *App) catCmd() *cobra.Command {
	var rev string
	var pr int
	cmd := &cobra.Command{
		Use:   "cat <doc>",
		Short: "Print a document from main, a revision or a PR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireStore(); err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			rel, err := resolveDoc(s, args[0])
			if err != nil {
				return err
			}
			ctx := context.Background()
			var content, source, state string
			switch {
			case pr > 0:
				content, err = s.Runtime.Store.ReadPR(ctx, rel, pr)
				source = fmt.Sprintf("PR #%d", pr)
				state = "pr"
			case rev != "":
				content, err = s.Runtime.Store.ReadAtRev(ctx, rel, rev)
				source = rev
				state = "revision"
			default:
				if area, aerr := changeset.AreaForPath(rel); aerr == nil {
					content, source, state, err = s.Manager.ReadFileState(ctx, area, rel)
				} else {
					content, err = s.Runtime.Store.Read(rel)
					source, state = "main", changeset.DocStateMain
				}
			}
			if err != nil {
				return err
			}
			if state == changeset.DocStateDeleted {
				if a.JSON {
					return a.printJSON(map[string]any{"path": rel, "content": "", "source": source, "state": state, "deleted": true})
				}
				return fmt.Errorf("%s is deleted in %s", rel, source)
			}
			hash, _ := s.Runtime.Store.Hash(ctx, content)
			if a.JSON {
				return a.printJSON(map[string]any{"path": rel, "content": content, "hash": hash, "source": source, "state": state})
			}
			fmt.Print(content)
			if !strings.HasSuffix(content, "\n") {
				fmt.Println()
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&rev, "rev", "", "git revision (branch, tag or SHA)")
	cmd.Flags().IntVar(&pr, "pr", 0, "read from a pull request head")
	return cmd
}

func (a *App) searchCmd() *cobra.Command {
	var all bool
	var limit int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search documents",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireStore(); err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			dir := s.Context.ProjectArea()
			if all {
				dir = ""
			}
			o, err := s.Manager.OverlayDir(context.Background(), dir)
			if err != nil {
				return err
			}
			hits, err := search.Search(o.Docs, strings.Join(args, " "), limit)
			if err != nil {
				return err
			}
			for i := range hits {
				hits[i].Source = o.Sources[hits[i].Path]
				hits[i].State = o.States[hits[i].Path]
			}
			if a.JSON {
				return a.printJSON(hits)
			}
			for _, h := range hits {
				where := h.Path
				if h.Line > 0 {
					where = fmt.Sprintf("%s:%d", h.Path, h.Line)
				}
				extra := ""
				if h.State != "" && h.State != changeset.DocStateMain {
					extra = " [" + h.State + "]"
				}
				fmt.Printf("%s%s score=%.2f %s\n", where, extra, h.Score, h.Snippet)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "search the whole store")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum hits")
	return cmd
}

func (a *App) guidanceCmd() *cobra.Command {
	var maxResults int
	var full bool
	cmd := &cobra.Command{
		Use:   "guidance <task>",
		Short: "Find patterns, ADRs and standards for a task",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireStore(); err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			lang := ""
			if len(a.Languages) > 0 {
				lang = a.Languages[0]
			}
			results, err := guidance.Lookup(s.Runtime.Store, s.Context, strings.Join(args, " "), lang, maxResults)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(results)
			}
			fmt.Print(guidance.FormatCompact(results))
			if full {
				for _, r := range results {
					area, aerr := changeset.AreaForPath(r.Path)
					if aerr != nil {
						fmt.Printf("\n----- %s -----\n(unable to resolve area: %v)\n", r.Path, aerr)
						continue
					}
					content, source, docState, rerr := s.Manager.ReadFileState(context.Background(), area, r.Path)
					if rerr != nil {
						fmt.Printf("\n----- %s -----\n(unable to read: %v)\n", r.Path, rerr)
						continue
					}
					if docState == changeset.DocStateDeleted {
						fmt.Printf("\n----- %s -----\n(deleted in %s)\n", r.Path, source)
						continue
					}
					fmt.Printf("\n----- %s -----\n%s\n", r.Path, content)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&maxResults, "max", 5, "maximum results")
	cmd.Flags().BoolVar(&full, "full", false, "print full document bodies")
	return cmd
}

func resolveDoc(s *runtime.Session, arg string) (string, error) {
	arg = strings.ReplaceAll(strings.TrimSpace(arg), "\\", "/")
	area := s.Context.ProjectArea()
	if arg == "" {
		return "", fmt.Errorf("document path is required")
	}
	if strings.HasPrefix(arg, "org/") || strings.HasPrefix(arg, "teams/") || strings.HasPrefix(arg, "projects/") || strings.HasPrefix(arg, ".docsync/") {
		return store.CleanRel(arg)
	}
	if area == "" {
		return store.CleanRel(arg)
	}
	return store.CleanRel(path.Join(area, arg))
}

func resolveDirArg(area, arg string) string {
	arg = strings.ReplaceAll(strings.TrimSpace(arg), "\\", "/")
	if arg == "org" || arg == "teams" || arg == "projects" {
		return arg
	}
	if strings.HasPrefix(arg, "org/") || strings.HasPrefix(arg, "teams/") || strings.HasPrefix(arg, "projects/") {
		return strings.Trim(arg, "/")
	}
	if area == "" {
		return strings.Trim(arg, "/")
	}
	return path.Join(area, arg)
}

func formatUser(c *config.Context) string {
	name := strings.TrimSpace(c.UserName)
	email := strings.TrimSpace(c.UserEmail)
	switch {
	case name != "" && email != "":
		return name + " <" + email + ">"
	case name != "":
		return name
	case email != "":
		return email
	default:
		return "-"
	}
}

func valueOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func source(c *config.Context, key string) string {
	if c == nil || c.Sources == nil || c.Sources[key] == "" {
		return ""
	}
	return "← " + c.Sources[key]
}

func nonEmpty(vals ...string) []string {
	var out []string
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}
