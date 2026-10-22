package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/bolacha/docsync/internal/changeset"
	"github.com/bolacha/docsync/internal/store"
	"github.com/spf13/cobra"
)

func (a *App) logCmd() *cobra.Command {
	var wip bool
	cmd := &cobra.Command{
		Use:   "log <doc>",
		Short: "Show document versions from main history",
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
			var revs []store.Revision
			if wip {
				area, err := changeset.AreaForPath(rel)
				if err != nil {
					area = s.Context.ProjectArea()
				}
				c, err := s.Manager.Active(area)
				if err != nil {
					return err
				}
				if c == nil {
					return fmt.Errorf("no active changeset for %s", area)
				}
				revs, err = s.Runtime.Store.LogWIP(ctx, c.Path, rel)
				if err != nil {
					return err
				}
			} else {
				revs, err = s.Runtime.Store.Log(ctx, rel, false)
				if err != nil {
					return err
				}
			}
			if a.JSON {
				return a.printJSON(revs)
			}
			for _, r := range revs {
				pr := ""
				if r.PR > 0 {
					pr = fmt.Sprintf("#%d", r.PR)
				}
				source := ""
				if r.Source != "" {
					source = " (" + r.Source + ")"
				}
				reviewer := ""
				if r.Reviewer != "" {
					reviewer = " approved by " + r.Reviewer
				}
				fmt.Printf("v%-3d %-6s %s  %s%s%s  %q\n", r.Version, pr, r.Date.Format("2006-01-02"), r.Author, source, reviewer, r.Subject)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&wip, "wip", false, "include local changeset commits")
	return cmd
}

func (a *App) diffCmd() *cobra.Command {
	var pr int
	cmd := &cobra.Command{
		Use:   "diff <doc> [A] [B]",
		Short: "Diff document versions or a PR",
		Args:  cobra.RangeArgs(1, 3),
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
			var out string
			if pr > 0 {
				out, err = s.Runtime.Store.DiffPR(ctx, rel, pr)
			} else {
				var av, bv string
				if len(args) >= 2 {
					av = args[1]
				}
				if len(args) >= 3 {
					bv = args[2]
				}
				if len(args) == 2 {
					bv = "HEAD"
				}
				out, err = s.Runtime.Store.Diff(ctx, rel, av, bv)
			}
			if err != nil {
				return err
			}
			fmt.Print(out)
			if out != "" && !strings.HasSuffix(out, "\n") {
				fmt.Println()
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&pr, "pr", 0, "diff a pull request against main")
	return cmd
}

func (a *App) blameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "blame <doc>",
		Short: "Show line authorship",
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
			out, err := s.Runtime.Store.Blame(context.Background(), rel)
			if err != nil {
				return err
			}
			fmt.Println(out)
			return nil
		},
	}
}
