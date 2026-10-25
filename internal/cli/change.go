package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/repocompanion/companion/internal/changeset"
	"github.com/repocompanion/companion/internal/runtime"
	"github.com/repocompanion/companion/internal/state"
	"github.com/spf13/cobra"
)

func (a *App) changeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change",
		Short: "Manage changesets (branches, worktrees and PRs)",
	}
	cmd.AddCommand(
		a.changeStartCmd(),
		a.changeListCmd(),
		a.changeSwitchCmd(),
		a.changeJoinCmd(),
		a.changeSyncCmd(),
		a.changeSubmitCmd(),
		a.changeReopenCmd(),
		a.changeRebaseCmd(),
		a.changeCleanupCmd(),
		a.changeAbandonCmd(),
		a.changeAdoptCmd(),
	)
	return cmd
}

func (a *App) changeArea(s *runtime.Session, area string, org bool) (string, error) {
	if org {
		return "org", nil
	}
	if strings.TrimSpace(area) != "" {
		if err := changeset.ValidateArea(area); err != nil {
			return "", err
		}
		return strings.Trim(area, "/"), nil
	}
	if s != nil && s.Context.ProjectArea() != "" {
		return s.Context.ProjectArea(), nil
	}
	return "", fmt.Errorf("no area resolved; pass --area org, teams/<team> or projects/<owner>/<repo>")
}

func (a *App) changeStartCmd() *cobra.Command {
	var area string
	var org bool
	cmd := &cobra.Command{
		Use:   "start <topic>",
		Short: "Start a new changeset",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireStore(); err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			area, err = a.changeArea(s, area, org)
			if err != nil {
				return err
			}
			c, err := s.Manager.Start(context.Background(), strings.Join(args, " "), area)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(c)
			}
			printChange(c)
			return nil
		},
	}
	cmd.Flags().StringVar(&area, "area", "", "area, e.g. projects/acme/api, org or teams/payments")
	cmd.Flags().BoolVar(&org, "org", false, "use the org area")
	return cmd
}

func (a *App) changeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List changesets",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireStore(); err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			list, err := s.Manager.List()
			if err != nil {
				return err
			}
			stFile, _ := s.State.Load()
			for i := range list {
				if c, err := s.Manager.Reconcile(context.Background(), list[i]); err == nil {
					list[i] = c
				}
			}
			if a.JSON {
				return a.printJSON(list)
			}
			for _, c := range list {
				pr := "-"
				if c.PR > 0 {
					pr = fmt.Sprintf("#%d", c.PR)
				}
				mark := " "
				if stFile != nil && stFile.Active[c.Area] == c.ID {
					mark = "*"
				}
				fmt.Printf("%s %-14s %-7s %-9s %s\n", mark, c.ID, pr, c.PRState, c.Area)
				fmt.Printf("  %s\n  %s\n", c.Title, c.Path)
			}
			return nil
		},
	}
}

func (a *App) changeSwitchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "switch <id|area>",
		Short: "Make a changeset active for its area",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			c, err := s.Manager.Switch(args[0])
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(c)
			}
			printChange(c)
			return nil
		},
	}
}

func (a *App) changeJoinCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "join <PR#>",
		Short: "Join an open shared changeset/PR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(strings.TrimPrefix(args[0], "#"))
			if err != nil || n <= 0 {
				return fmt.Errorf("invalid PR number %q", args[0])
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			c, err := s.Manager.Join(context.Background(), n)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(c)
			}
			printChange(c)
			return nil
		},
	}
}

func (a *App) changeSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync [id]",
		Short: "Commit locally, integrate the remote branch and push",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			id := firstArg(args)
			res, err := s.Manager.Sync(context.Background(), id, "cli")
			if err != nil {
				if pendingNote(err) {
					if werr := a.writeResult(res); werr != nil {
						return werr
					}
					return ErrPending
				}
				return err
			}
			if a.JSON {
				return a.printJSON(res)
			}
			printResult(res)
			return nil
		},
	}
}

func (a *App) changeSubmitCmd() *cobra.Command {
	var summary string
	var skipLint, skipCodeRefs bool
	cmd := &cobra.Command{
		Use:   "submit [id]",
		Short: "Sync, rebase on main, mark the PR ready and freeze the changeset",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			res, err := s.Manager.SubmitWithOptions(context.Background(), firstArg(args), summary, "cli", changeset.SubmitOptions{SkipLint: skipLint, SkipCodeRefs: skipCodeRefs})
			if err != nil {
				if pendingNote(err) {
					if werr := a.writeResult(res); werr != nil {
						return werr
					}
					return ErrPending
				}
				return err
			}
			if a.JSON {
				return a.printJSON(res)
			}
			printResult(res)
			fmt.Println("PR is ready for review; the changeset is frozen until `companion change reopen`.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&summary, "message", "m", "", "PR title summary")
	cmd.Flags().BoolVar(&skipLint, "skip-lint", false, "mark ready even if changed documents have lint errors")
	cmd.Flags().BoolVar(&skipCodeRefs, "skip-code-refs", false, "mark ready even if code-reference preflight fails")
	return cmd
}

func (a *App) changeReopenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reopen [id]",
		Short: "Return a ready PR to draft so more commits can be pushed",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			c, err := s.Manager.Reopen(context.Background(), firstArg(args))
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(c)
			}
			printChange(c)
			return nil
		},
	}
}

func (a *App) changeRebaseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rebase [id]",
		Short: "Integrate the remote branch and rebase on main",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			c, err := s.Manager.Rebase(context.Background(), firstArg(args))
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(c)
			}
			printChange(c)
			fmt.Println("resolved locally; run `change sync` after reopening if the PR is ready")
			return nil
		},
	}
}

func (a *App) changeCleanupCmd() *cobra.Command {
	var archive, force bool
	cmd := &cobra.Command{
		Use:   "cleanup [id]",
		Short: "Remove a merged/closed changeset",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			c, err := s.Manager.Cleanup(context.Background(), firstArg(args), archive, force)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(c)
			}
			fmt.Printf("removed changeset %s (%s)\n", c.ID, c.Area)
			return nil
		},
	}
	cmd.Flags().BoolVar(&archive, "archive", false, "save a patch before removing")
	cmd.Flags().BoolVar(&force, "force", false, "discard unpushed local work")
	return cmd
}

func (a *App) changeAbandonCmd() *cobra.Command {
	var archive, force bool
	cmd := &cobra.Command{
		Use:   "abandon [id]",
		Short: "Close the PR and remove the worktree",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.session()
			if err != nil {
				return err
			}
			c, err := s.Manager.Abandon(context.Background(), firstArg(args), archive, force)
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(c)
			}
			fmt.Printf("abandoned changeset %s (%s)\n", c.ID, c.Area)
			return nil
		},
	}
	cmd.Flags().BoolVar(&archive, "archive", false, "save a patch before removing")
	cmd.Flags().BoolVar(&force, "force", false, "discard unpushed local work")
	return cmd
}

func (a *App) changeAdoptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "adopt",
		Short: "Move accidental main-mirror edits into a new changeset",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireStore(); err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			c, err := s.Manager.Adopt(context.Background(), "cli")
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(c)
			}
			printChange(c)
			fmt.Println("edits adopted and committed locally; run `companion change sync` to push.")
			return nil
		},
	}
}

func printChange(c state.Change) {
	pr := "-"
	if c.PR > 0 {
		pr = fmt.Sprintf("#%d", c.PR)
	}
	fmt.Printf("changeset %s\n", c.ID)
	fmt.Printf("  area     %s\n", c.Area)
	fmt.Printf("  branch   %s\n", c.Branch)
	fmt.Printf("  worktree %s\n", c.Path)
	fmt.Printf("  PR       %s (%s)\n", pr, c.PRState)
}

func printResult(r changeset.Result) {
	printChange(r.Change)
	if len(r.Files) > 0 {
		fmt.Println("  files")
		for _, f := range r.Files {
			fmt.Printf("    %s\n", f)
		}
	}
	if r.PR != nil {
		fmt.Printf("  url      %s\n", r.PR.URL)
	}
	if r.Pending {
		fmt.Println("  pending  local commits kept; rerun sync later")
	}
	if r.Warning != "" {
		fmt.Printf("  warning  %s\n", r.Warning)
	}
	for _, w := range r.Warnings {
		fmt.Printf("  warning  %s\n", w)
	}
}

func firstArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}
