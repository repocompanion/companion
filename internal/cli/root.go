package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bolacha/docsync/internal/changeset"
	"github.com/bolacha/docsync/internal/config"
	"github.com/bolacha/docsync/internal/gitx"
	"github.com/bolacha/docsync/internal/runtime"
	"github.com/bolacha/docsync/internal/state"
	"github.com/spf13/cobra"
)

var Version = "dev"

type App struct {
	Cwd       string
	Home      string
	Config    string
	Project   string
	Team      string
	Domain    string
	Languages []string
	JSON      bool

	rt *runtime.Runtime
}

func NewRootCommand() *cobra.Command {
	a := &App{}
	root := &cobra.Command{
		Use:           "docsync",
		Version:       Version,
		Short:         "Git-backed project docs for humans and coding agents",
		Long:          "docsync keeps project documentation in one central Git repository. Every change goes through a changeset and a GitHub PR.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if a.Home != "" {
				_ = os.Setenv("DOCSYNC_HOME", a.Home)
			}
			if a.Config != "" {
				_ = os.Setenv("DOCSYNC_CONFIG", a.Config)
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			a.Cwd = cwd
			return nil
		},
	}
	root.PersistentFlags().StringVar(&a.Home, "home", "", "docsync state directory (default ~/.docsync)")
	root.PersistentFlags().StringVar(&a.Config, "config", "", "user config file")
	root.PersistentFlags().StringVar(&a.Project, "project", "", "override project (owner/repo)")
	root.PersistentFlags().StringVar(&a.Team, "team", "", "override team")
	root.PersistentFlags().StringVar(&a.Domain, "domain", "", "override domain")
	root.PersistentFlags().StringSliceVar(&a.Languages, "lang", nil, "override languages (repeatable or comma-separated)")
	root.PersistentFlags().BoolVar(&a.JSON, "json", false, "emit JSON where supported")

	root.AddCommand(
		a.initCmd(),
		a.pullCmd(),
		a.statusCmd(),
		a.contextCmd(),
		a.projectCmd(),
		a.lsCmd(),
		a.catCmd(),
		a.searchCmd(),
		a.guidanceCmd(),
		a.exportCmd(),
		a.codeCmd(),
		a.changeCmd(),
		a.editCmd(),
		a.moveCmd(),
		a.newCmd(),
		a.adrCmd(),
		a.logCmd(),
		a.diffCmd(),
		a.blameCmd(),
		a.restoreCmd(),
		a.lintCmd(),
		a.validateCodeRefsCmd(),
		a.staleCodeRefsCmd(),
		a.importCmd(),
		a.configCmd(),
		a.registryCmd(),
		a.doctorCmd(),
		a.mcpCmd(),
	)
	return root
}

func (a *App) runtime() (*runtime.Runtime, error) {
	if a.rt != nil {
		return a.rt, nil
	}
	rt, err := runtime.New(a.Cwd)
	if err != nil {
		return nil, err
	}
	a.rt = rt
	return rt, nil
}

func (a *App) session() (*runtime.Session, error) {
	rt, err := a.runtime()
	if err != nil {
		return nil, err
	}
	return rt.Session(context.Background(), a.Project, a.Team, a.Domain, a.Languages)
}

func (a *App) requireStore() error {
	rt, err := a.runtime()
	if err != nil {
		return err
	}
	if !rt.Store.Exists() {
		return fmt.Errorf("docsync store is not initialized; run `docsync init --remote <git@github.com:org/docs-store.git>`")
	}
	return nil
}

func (a *App) printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(b))
	return nil
}

func (a *App) areaOrOrg(org bool) (string, error) {
	s, err := a.session()
	if err != nil {
		return "", err
	}
	if org {
		return "org", nil
	}
	area := s.Context.ProjectArea()
	if area == "" {
		return "", fmt.Errorf("project is unresolved; pass --project owner/repo or run inside a git checkout with a GitHub origin")
	}
	return area, nil
}

func (a *App) initCmd() *cobra.Command {
	var remote string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Clone the store and write config",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := a.runtime()
			if err != nil {
				return err
			}
			if remote == "" {
				remote = rt.Cfg.Remote
			}
			if remote == "" {
				remote = config.DefaultRemote
			}
			if strings.Contains(remote, "your-org/docs-store") {
				return fmt.Errorf("set the real store remote: docsync init --remote git@github.com:<org>/docs-store.git")
			}
			rt.Store.Remote = remote
			if err := rt.Store.Init(context.Background()); err != nil {
				return err
			}
			rt.Cfg.Remote = remote
			if err := saveUser(rt); err != nil {
				return err
			}
			fmt.Printf("store:  %s\n", rt.Store.Bare)
			fmt.Printf("mirror: %s\n", rt.Store.Mirror)
			fmt.Printf("config: %s\n", rt.UserPath)
			fmt.Println()
			fmt.Println("Add this to each code repo's CLAUDE.md / AGENTS.md / .cursor/rules:")
			fmt.Println()
			fmt.Println(snippet)
			return nil
		},
	}
	cmd.Flags().StringVar(&remote, "remote", "", "store Git remote (SSH), e.g. git@github.com:your-org/docs-store.git")
	return cmd
}

const snippet = `## Documentation

Before implementing infrastructure, integration or cross-cutting code (queues,
databases, migrations, observability, APIs, auth), call the docsync
` + "`docs_get_guidance`" + ` MCP tool. Before writing or refining documentation, call
` + "`docs_get_context`" + `. If the mirror is stale, call ` + "`docs_pull`" + `.

Never write docs into the code repo. All doc changes go through docsync tools
and become PRs.`

func (a *App) pullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Fetch main and fast-forward the local mirror",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireStore(); err != nil {
				return err
			}
			rt, _ := a.runtime()
			rev, t, err := rt.Store.Pull(context.Background())
			if err != nil {
				return err
			}
			var behind []string
			if st, err := state.NewStore(rt.Store.StatePath).Load(); err == nil {
				for _, c := range st.Changes {
					if !c.IsOpen() {
						continue
					}
					if _, err := gitx.Run(context.Background(), c.Path, "merge-base", "--is-ancestor", "origin/main", "HEAD"); err != nil {
						behind = append(behind, c.ID)
					}
				}
			}
			if a.JSON {
				return a.printJSON(map[string]any{"rev": rev, "commit_time": t, "mirror": rt.Store.Mirror, "behind_main": behind})
			}
			fmt.Printf("main is at %s (%s)\n", shortSHA(rev), humanAge(time.Since(t)))
			for _, id := range behind {
				fmt.Printf("behind main: %s (run `docsync change rebase %s` or submit)\n", id, id)
			}
			return nil
		},
	}
}

func (a *App) statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the mirror and local changesets",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireStore(); err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			reconciliation, reconcileErr := s.Manager.ReconcileState(context.Background())
			if reconcileErr != nil {
				fmt.Fprintf(os.Stderr, "warning: reconcile state: %v\n", reconcileErr)
			}
			for _, warning := range reconciliation.Warnings {
				fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
			}
			rt, _ := a.runtime()
			rev, ct, _ := rt.Store.MirrorRev(context.Background())
			type row struct {
				ID      string   `json:"id"`
				Title   string   `json:"title"`
				Area    string   `json:"area"`
				PR      int      `json:"pr,omitempty"`
				State   string   `json:"state"`
				Frozen  bool     `json:"frozen"`
				Dirty   []string `json:"dirty,omitempty"`
				Error   string   `json:"error,omitempty"`
				Dir     string   `json:"dir"`
				Pending bool     `json:"needs_sync,omitempty"`
				Active  bool     `json:"active,omitempty"`
				Behind  bool     `json:"behind_main,omitempty"`
			}
			changes, err := s.Manager.List()
			if err != nil {
				return err
			}
			stFile, _ := s.State.Load()
			var rows []row
			for _, c := range changes {
				if c2, err := s.Manager.Reconcile(context.Background(), c); err == nil {
					c = c2
				}
				dirty, _ := s.Manager.Dirty(context.Background(), c)
				active := stFile != nil && stFile.Active[c.Area] == c.ID
				behind := false
				if c.IsOpen() {
					if _, err := gitx.Run(context.Background(), c.Path, "merge-base", "--is-ancestor", "origin/main", "HEAD"); err != nil {
						behind = true
					}
				}
				rows = append(rows, row{c.ID, c.Title, c.Area, c.PR, c.PRState, c.Frozen, dirty, c.Error, c.Path, c.NeedsSync, active, behind})
			}
			if a.JSON {
				return a.printJSON(map[string]any{
					"mirror": rt.Store.Mirror, "rev": rev, "commit_time": ct,
					"changesets": rows, "state_reconciliation": reconciliation,
				})
			}
			fmt.Printf("mirror %s (%s)\n", shortSHA(rev), humanAge(time.Since(ct)))
			if len(rows) == 0 {
				fmt.Println("no changesets")
				return nil
			}
			fmt.Println()
			fmt.Printf("  %-14s %-7s %-10s %-8s %s\n", "ID", "PR", "STATE", "DIRTY", "AREA")
			for _, r := range rows {
				pr := "-"
				if r.PR > 0 {
					pr = fmt.Sprintf("#%d", r.PR)
				}
				st := r.State
				if r.Frozen && st == "ready" {
					st = "ready/frozen"
				}
				if r.Pending {
					st += "/pending"
				}
				mark := " "
				if r.Active {
					mark = "*"
				}
				suffix := ""
				if r.Behind {
					suffix = "  (behind main)"
				}
				fmt.Printf("%s %-14s %-7s %-10s %-8d %s%s\n", mark, r.ID, pr, st, len(r.Dirty), r.Area, suffix)
				fmt.Printf("  %s\n", r.Title)
				if r.Error != "" {
					fmt.Printf("  ! %s\n", r.Error)
				}
			}
			return nil
		},
	}
}

func saveUser(rt *runtime.Runtime) error {
	return config.SaveUser(rt.Cfg)
}

func shortSHA(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
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

func pendingNote(err error) bool {
	if changeset.IsPending(err) {
		fmt.Fprintln(os.Stderr, "warning:", err)
		return true
	}
	return false
}
