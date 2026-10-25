package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/repocompanion/companion/internal/changeset"
	"github.com/repocompanion/companion/internal/codeindex"
	"github.com/repocompanion/companion/internal/config"
	"github.com/repocompanion/companion/internal/docrules"
	"github.com/repocompanion/companion/internal/gitx"
	"github.com/repocompanion/companion/internal/lint"
	"github.com/repocompanion/companion/internal/mcpserver"
	"github.com/repocompanion/companion/internal/state"
	"github.com/repocompanion/companion/internal/store"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func (a *App) lintCmd() *cobra.Command {
	var all bool
	var storePath string
	cmd := &cobra.Command{
		Use:   "lint",
		Short: "Validate frontmatter, links, reference impls and secrets",
		RunE: func(cmd *cobra.Command, args []string) error {
			var target *store.Store
			if storePath != "" {
				abs, err := filepath.Abs(storePath)
				if err != nil {
					return err
				}
				target = &store.Store{Mirror: abs}
			} else {
				if err := a.requireStore(); err != nil {
					return err
				}
				rt, err := a.runtime()
				if err != nil {
					return err
				}
				target = rt.Store
			}
			sess, serr := a.session()
			if serr != nil {
				return serr
			}
			dir := ""
			if !all {
				dir = sess.Context.ProjectArea()
				if dir == "" {
					return fmt.Errorf("project is unresolved; use --all or pass --project")
				}
			}
			res, err := lint.RunDirWithCodeRefs(context.Background(), target, dir, sess.Context.RepoRoot, sess.Context.Project)
			if err != nil {
				return err
			}
			if a.JSON {
				if err := a.printJSON(res); err != nil {
					return err
				}
			} else {
				fmt.Print(lint.Format(res))
			}
			for _, i := range res.Issues {
				if i.Level == "error" {
					return fmt.Errorf("lint failed with errors")
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", true, "lint the whole store")
	cmd.Flags().StringVar(&storePath, "store", "", "lint a checked-out store directory instead of the local mirror")
	return cmd
}

func (a *App) configCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Read and write config"}
	cmd.AddCommand(a.configGetCmd(), a.configSetCmd(), a.configInitRepoCmd())
	return cmd
}

func (a *App) configGetCmd() *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := a.runtime()
			if err != nil {
				return err
			}
			var value string
			switch scope {
			case "user":
				value, err = config.GetUserKey(rt.Cfg, args[0])
			case "repo":
				root := gitx.Detect(context.Background(), a.Cwd).Root
				if root == "" {
					return fmt.Errorf("not inside a git repository")
				}
				var repo config.RepoConfig
				repo, err = config.LoadRepo(root)
				if err == nil {
					value, err = config.GetRepoKey(repo, args[0])
				}
			default:
				return fmt.Errorf("scope must be user or repo")
			}
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(map[string]string{"key": args[0], "value": value, "scope": scope})
			}
			fmt.Println(value)
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "user", "user|repo")
	return cmd
}

func (a *App) configSetCmd() *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := a.runtime()
			if err != nil {
				return err
			}
			switch scope {
			case "user":
				if err := config.SetUserKey(&rt.Cfg, args[0], args[1]); err != nil {
					return err
				}
				return config.SaveUser(rt.Cfg)
			case "repo":
				root := gitx.Detect(context.Background(), a.Cwd).Root
				if root == "" {
					return fmt.Errorf("not inside a git repository")
				}
				repo, err := config.LoadRepo(root)
				if err != nil {
					return err
				}
				if err := config.SetRepoKey(&repo, args[0], args[1]); err != nil {
					return err
				}
				return config.SaveRepo(root, repo)
			default:
				return fmt.Errorf("scope must be user or repo")
			}
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "user", "user|repo")
	return cmd
}

func (a *App) configInitRepoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init-repo",
		Short: "Write .companion.yaml with detected values",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := a.runtime()
			if err != nil {
				return err
			}
			s, err := rt.Session(context.Background(), a.Project, a.Team, a.Domain, a.Languages)
			if err != nil {
				return err
			}
			root := s.Context.RepoRoot
			if root == "" {
				return fmt.Errorf("not inside a git repository")
			}
			repo := config.RepoConfig{
				Project:   s.Context.Project,
				Team:      s.Context.Team,
				Domain:    s.Context.Domain,
				Languages: s.Context.Languages,
			}
			if err := config.SaveRepo(root, repo); err != nil {
				return err
			}
			fmt.Printf("wrote %s\n", filepath.Join(root, ".companion.yaml"))
			return nil
		},
	}
}

func (a *App) registryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "registry", Short: "Manage the org registry"}
	cmd.AddCommand(a.registryAddRepoCmd())
	return cmd
}

func (a *App) registryAddRepoCmd() *cobra.Command {
	var team, domain string
	var submit bool
	cmd := &cobra.Command{
		Use:   "add-repo <owner/repo>",
		Short: "Add or update a repo mapping and open a docs PR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireStore(); err != nil {
				return err
			}
			if team == "" {
				return fmt.Errorf("--team is required")
			}
			if err := docrules.ValidateProject(args[0]); err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			area := ".companion"
			c, err := s.Manager.Ensure(context.Background(), area)
			if err != nil {
				return err
			}
			rel := ".companion/registry.yaml"
			reg, err := s.Manager.LoadRegistry(c)
			if err != nil {
				return err
			}
			domainChanged := cmd.Flags().Changed("domain")
			reg.SetRepoMapping(args[0], func(m *config.RepoMapping) {
				m.Team = team
				if domainChanged {
					m.Domain = domain
				}
			})
			b, err := yaml.Marshal(&reg)
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
			if _, _, err := s.Manager.WriteFile(context.Background(), area, rel, string(b), base, "cli"); err != nil {
				return err
			}
			var res changeset.Result
			if submit {
				res, err = s.Manager.Submit(context.Background(), c.ID, "registry: add "+args[0], "cli")
			} else {
				res, err = s.Manager.Sync(context.Background(), c.ID, "cli")
			}
			if err != nil {
				if pendingNote(err) {
					printResult(res)
					return ErrPending
				}
				return err
			}
			printResult(res)
			return nil
		},
	}
	cmd.Flags().StringVar(&team, "team", "", "team key")
	cmd.Flags().StringVar(&domain, "domain", "", "optional domain")
	cmd.Flags().BoolVar(&submit, "submit", false, "mark the PR ready instead of leaving it draft")
	return cmd
}

func (a *App) doctorCmd() *cobra.Command {
	var online, repair, strict bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check git, store, auth, mirror and state",
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := a.runtime()
			if err != nil {
				return err
			}
			type check struct {
				Name    string `json:"name"`
				OK      bool   `json:"ok"`
				Message string `json:"message"`
			}
			var checks []check
			add := func(name string, ok bool, msg string) { checks = append(checks, check{name, ok, msg}) }

			gitVersion, err := gitx.Run(context.Background(), "", "version")
			okGit := false
			major, minor := 0, 0
			if re := regexp.MustCompile(`git version (\d+)\.(\d+)`); re.MatchString(gitVersion) {
				m := re.FindStringSubmatch(gitVersion)
				major, _ = strconv.Atoi(m[1])
				minor, _ = strconv.Atoi(m[2])
				okGit = major > 2 || (major == 2 && minor >= 30)
			}
			add("git", okGit, gitVersion+" (need >= 2.30)")
			add("store", rt.Store.Exists(), rt.Store.Bare)
			add("remote", rt.Store.Remote != "" && !strings.Contains(rt.Store.Remote, "your-org"), gitx.RedactRemote(rt.Store.Remote))
			add("github token", rt.Forge != nil && rt.Forge.Available(), "COMPANION_FORGE_TOKEN, forge.token, or gh auth token")
			add("code provider go", true, codeindex.NewGoProvider().Version())
			ctags := codeindex.NewCtagsProvider()
			if ctags.Available {
				add("code provider ctags", true, ctags.Version())
			} else {
				add("code provider ctags", false, "install universal-ctags for non-Go symbols")
			}

			if rt.Store.Exists() {
				rev, t, err := rt.Store.MirrorRev(context.Background())
				add("main mirror", err == nil, fmt.Sprintf("%s %s", shortSHA(rev), humanAge(time.Since(t))))
				dirty, _ := gitx.Run(context.Background(), rt.Store.Mirror, "status", "--porcelain")
				add("mirror clean", strings.TrimSpace(dirty) == "", "if dirty: companion change adopt")
				worktrees, _ := gitx.Run(context.Background(), "", "--git-dir", rt.Store.Bare, "worktree", "list", "--porcelain")
				add("worktrees", worktrees != "", fmt.Sprintf("%d entries", strings.Count(worktrees, "worktree ")))
				st, err := state.NewStore(rt.Store.StatePath).Load()
				n := 0
				if st != nil {
					n = len(st.Changes)
				}
				add("state", err == nil, fmt.Sprintf("%d changesets", n))
				if repair {
					s, serr := a.session()
					if serr != nil {
						add("state repair", false, serr.Error())
					} else {
						rec, rerr := s.Manager.ReconcileState(context.Background())
						msg := fmt.Sprintf("added %d, updated %d, removed %d", rec.Added, rec.Updated, rec.Removed)
						if len(rec.Warnings) > 0 {
							msg += fmt.Sprintf(", %d warnings", len(rec.Warnings))
						}
						if rerr != nil {
							msg += ": " + rerr.Error()
						}
						add("state repair", rerr == nil, msg)
					}
				}
			}
			if online {
				out, err := gitx.Run(context.Background(), "", "ls-remote", "--heads", rt.Store.Remote, "main")
				add("SSH reachability", err == nil, strings.TrimSpace(out))
			}
			if a.JSON {
				if err := a.printJSON(checks); err != nil {
					return err
				}
			} else {
				for _, c := range checks {
					mark := "ok"
					if !c.OK {
						mark = "warn"
					}
					fmt.Printf("%-18s %-5s %s\n", c.Name, mark, c.Message)
				}
			}
			if strict {
				failed := 0
				for _, c := range checks {
					if !c.OK {
						failed++
					}
				}
				if failed > 0 {
					return fmt.Errorf("doctor: %d check(s) failed", failed)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&online, "online", false, "also test SSH reachability")
	cmd.Flags().BoolVar(&repair, "repair", false, "rebuild state.json from local worktrees")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit non-zero when any check fails")
	return cmd
}

func (a *App) mcpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP server on stdio",
		RunE: func(cmd *cobra.Command, args []string) error {
			return mcpserver.Run(context.Background(), mcpserver.Options{
				Cwd: a.Cwd, Project: a.Project, Team: a.Team, Domain: a.Domain, Languages: a.Languages,
			})
		},
	}
}
