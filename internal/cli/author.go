package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bolacha/docsync/internal/changeset"
	"github.com/bolacha/docsync/internal/docrules"
	"github.com/bolacha/docsync/internal/gitx"
	"github.com/bolacha/docsync/internal/runtime"
	"github.com/bolacha/docsync/internal/state"
	"github.com/bolacha/docsync/internal/store"
	"github.com/bolacha/docsync/internal/templates"
	"github.com/spf13/cobra"
)

func (a *App) editCmd() *cobra.Command {
	var changesetID string
	cmd := &cobra.Command{
		Use:   "edit <doc>",
		Short: "Open a document in the active changeset worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			var c state.Change
			var rel string
			var area string
			if changesetID != "" {
				c, err = s.Manager.Switch(changesetID)
				if err != nil {
					return err
				}
				area = c.Area
				rel, err = store.CleanRel(store.JoinArea(area, args[0]))
				if err != nil {
					return err
				}
			} else {
				rel, err = resolveDoc(s, args[0])
				if err != nil {
					return err
				}
				area, err = changeset.AreaForPath(rel)
				if err != nil {
					area = s.Context.ProjectArea()
				}
				if area == "" {
					return err
				}
				c, err = s.Manager.Ensure(context.Background(), area)
			}
			if err != nil {
				return err
			}
			full := filepath.Join(c.Path, filepath.FromSlash(rel))
			if _, err := os.Stat(full); err != nil {
				return fmt.Errorf("%s is not in changeset %s; use `docsync new` first", rel, c.ID)
			}
			editor := os.Getenv("VISUAL")
			if editor == "" {
				editor = os.Getenv("EDITOR")
			}
			if editor == "" {
				editor = "vi"
				if os.PathSeparator == '\\' {
					editor = "notepad"
				}
			}
			parts := strings.Fields(editor)
			ecmd := exec.Command(parts[0], append(parts[1:], full)...)
			ecmd.Dir = c.Path
			ecmd.Stdin, ecmd.Stdout, ecmd.Stderr = os.Stdin, os.Stdout, os.Stderr
			if err := ecmd.Run(); err != nil {
				return err
			}
			fmt.Printf("\nedited in changeset %s; run `docsync change sync` to commit, push and open the draft PR.\n", c.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&changesetID, "changeset", "", "target changeset ID")
	return cmd
}

func (a *App) moveCmd() *cobra.Command {
	var changesetID string
	cmd := &cobra.Command{
		Use:   "move <doc> <new-path>",
		Short: "Move or rename a document with git history preserved",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireStore(); err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			ctx := context.Background()
			var c state.Change
			var area string
			if changesetID != "" {
				c, err = s.Manager.Switch(changesetID)
				if err != nil {
					return err
				}
				area = c.Area
			} else {
				area = s.Context.ProjectArea()
				arg := strings.ReplaceAll(strings.TrimSpace(args[0]), "\\", "/")
				if strings.HasPrefix(arg, "org/") || strings.HasPrefix(arg, "teams/") || strings.HasPrefix(arg, "projects/") {
					area, err = changeset.AreaForPath(arg)
					if err != nil {
						return err
					}
				}
				if area == "" {
					return fmt.Errorf("project is unresolved; pass --project or a store-relative document path")
				}
				c, err = s.Manager.Ensure(ctx, area)
				if err != nil {
					return err
				}
			}
			oldRel := store.RelativeToArea(area, args[0])
			newRel := store.RelativeToArea(area, args[1])
			base, err := s.Manager.HashFile(ctx, c, oldRel)
			if err != nil {
				return fmt.Errorf("read %s in changeset %s: %w", oldRel, c.ID, err)
			}
			c, err = s.Manager.MoveFile(ctx, area, oldRel, newRel, base, "cli")
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(map[string]any{"changeset": c, "moved_from": oldRel, "moved_to": newRel})
			}
			fmt.Printf("moved %s -> %s in changeset %s\n", oldRel, newRel, c.ID)
			fmt.Println("run `docsync change sync` to publish the rename.")
			return nil
		},
	}
	cmd.Flags().StringVar(&changesetID, "changeset", "", "target changeset ID")
	return cmd
}

func (a *App) newCmd() *cobra.Command {
	var area, templateName, title, changesetID string
	var org bool
	cmd := &cobra.Command{
		Use:   "new <doc>",
		Short: "Create a document from a template",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			if changesetID != "" {
				c, err := s.Manager.Switch(changesetID)
				if err != nil {
					return err
				}
				area = c.Area
			} else {
				area, err = a.changeArea(s, area, org)
				if err != nil {
					return err
				}
			}
			rel := store.JoinArea(area, args[0])
			rel, err = store.CleanRel(rel)
			if err != nil {
				return err
			}
			if templateName == "" {
				templateName = inferTemplate(rel)
			}
			if title == "" {
				title = titleFromPath(rel)
			}
			base := path.Base(rel)
			id := strings.TrimSuffix(base, path.Ext(base))
			content, err := templates.Render(templateName, templates.Data{
				Title: title, ID: id, Date: time.Now().Format("2006-01-02"),
				Status: "approved", Owner: ownerFor(s),
			})
			if err != nil {
				return err
			}
			if problems, err := docrules.ValidateMarkdown(rel, content); err != nil {
				fmt.Fprintf(os.Stderr, "warning: generated document is invalid: %v\n", err)
			} else if len(problems) > 0 {
				fmt.Fprintf(os.Stderr, "warning: generated document is invalid: %s\n", problems[0].Message)
			}
			if s.Runtime.Store.ExistsPath(rel) {
				return fmt.Errorf("%s already exists in main; use `docsync edit`", rel)
			}
			if changesetID == "" {
				if _, err := s.Manager.EnsureNamed(context.Background(), area, title); err != nil {
					return err
				}
			}
			c, _, err := s.Manager.WriteFile(context.Background(), area, rel, content, "", "cli")
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(map[string]any{"changeset": c, "path": rel})
			}
			fmt.Printf("created %s in changeset %s\n", rel, c.ID)
			fmt.Printf("worktree: %s\n", c.Path)
			fmt.Println("run `docsync change sync` to push and open the draft PR.")
			return nil
		},
	}
	cmd.Flags().StringVar(&area, "area", "", "area to create the doc in")
	cmd.Flags().BoolVar(&org, "org", false, "use the org area")
	cmd.Flags().StringVarP(&templateName, "template", "t", "", "adr|pattern|flow|runbook|standard|readme|architecture")
	cmd.Flags().StringVar(&title, "title", "", "document title")
	cmd.Flags().StringVar(&changesetID, "changeset", "", "target changeset ID")
	return cmd
}

func (a *App) adrCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "adr", Short: "Architecture decision records"}
	cmd.AddCommand(a.adrNewCmd())
	return cmd
}

func (a *App) adrNewCmd() *cobra.Command {
	var area, status, changesetID string
	var org bool
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Create a date-stamped ADR",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			if changesetID != "" {
				c, err := s.Manager.Switch(changesetID)
				if err != nil {
					return err
				}
				area = c.Area
			} else {
				area, err = a.changeArea(s, area, org)
				if err != nil {
					return err
				}
			}
			title := strings.Join(args, " ")
			slug := gitx.Slug(title)
			date := time.Now().Format("2006-01-02")
			var rel string
			if area == "org" {
				rel = path.Join("org", "adr", date+"-"+slug+".md")
			} else {
				rel = path.Join(area, "adr", date+"-"+slug+".md")
			}
			if status == "" {
				status = "proposed"
			}
			content, err := templates.Render("adr", templates.Data{
				Title: title, ID: slug, Date: date, Status: status, Owner: ownerFor(s),
			})
			if err != nil {
				return err
			}
			if problems, err := docrules.ValidateMarkdown(rel, content); err != nil {
				fmt.Fprintf(os.Stderr, "warning: generated ADR is invalid: %v\n", err)
			} else if len(problems) > 0 {
				fmt.Fprintf(os.Stderr, "warning: generated ADR is invalid: %s\n", problems[0].Message)
			}
			if changesetID == "" {
				if _, err := s.Manager.EnsureNamed(context.Background(), area, "ADR: "+title); err != nil {
					return err
				}
			}
			c, _, err := s.Manager.WriteFile(context.Background(), area, rel, content, "", "cli")
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(map[string]any{"changeset": c, "path": rel})
			}
			fmt.Printf("created %s in changeset %s\n", rel, c.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&area, "area", "", "area to create the ADR in")
	cmd.Flags().BoolVar(&org, "org", false, "create an org-wide ADR")
	cmd.Flags().StringVar(&status, "status", "proposed", "proposed|approved|deprecated|superseded")
	cmd.Flags().StringVar(&changesetID, "changeset", "", "target changeset ID")
	return cmd
}

func (a *App) restoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore <doc> <version>",
		Short: "Open a changeset that restores an old document version",
		Args:  cobra.ExactArgs(2),
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
			rev := args[1]
			if v, err := strconv.Atoi(strings.TrimPrefix(strings.ToLower(rev), "v")); err == nil {
				revs, err := s.Runtime.Store.Log(context.Background(), rel, false)
				if err != nil {
					return err
				}
				found := false
				for _, r := range revs {
					if r.Version == v {
						rev = r.Commit
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("version %d of %s not found", v, rel)
				}
			}
			content, err := s.Runtime.Store.ReadAtRev(context.Background(), rel, rev)
			if err != nil {
				return err
			}
			area, err := changeset.AreaForPath(rel)
			if err != nil {
				area = s.Context.ProjectArea()
			}
			if area == "" {
				return err
			}
			c, err := s.Manager.Ensure(context.Background(), area)
			if err != nil {
				return err
			}
			base := ""
			if _, err := os.Stat(filepath.Join(c.Path, filepath.FromSlash(rel))); err == nil {
				base, err = s.Manager.HashFile(context.Background(), c, rel)
				if err != nil {
					return err
				}
			}
			c, _, err = s.Manager.WriteFile(context.Background(), area, rel, content, base, "cli")
			if err != nil {
				return err
			}
			fmt.Printf("restored %s from %s in changeset %s\n", rel, shortSHA(rev), c.ID)
			return nil
		},
	}
}

func inferTemplate(rel string) string {
	switch {
	case strings.Contains(rel, "/adr/"):
		return "adr"
	case strings.Contains(rel, "/runbooks/"):
		return "runbook"
	case strings.Contains(rel, "/flows/"):
		return "flow"
	case strings.HasSuffix(rel, "architecture.md"):
		return "architecture"
	case strings.HasSuffix(rel, "README.md"):
		return "readme"
	default:
		return "pattern"
	}
}

func titleFromPath(rel string) string {
	base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	if strings.EqualFold(base, "readme") {
		return "README"
	}
	if base == "" {
		return "Document"
	}
	return strings.ToUpper(base[:1]) + base[1:]
}

func ownerFor(s *runtime.Session) string {
	if s.Context.Team != "" {
		return s.Context.Team
	}
	if s.Context.UserName != "" {
		return gitx.Slug(s.Context.UserName)
	}
	return "platform-team"
}
