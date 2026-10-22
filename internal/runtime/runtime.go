// Package runtime wires config, the local store, context resolution and the
// changeset manager together for the CLI and the MCP server.
package runtime

import (
	"context"
	"os"

	"github.com/bolacha/docsync/internal/changeset"
	"github.com/bolacha/docsync/internal/config"
	"github.com/bolacha/docsync/internal/forge"
	"github.com/bolacha/docsync/internal/gitx"
	"github.com/bolacha/docsync/internal/state"
	"github.com/bolacha/docsync/internal/store"
)

type Runtime struct {
	Home      string
	UserPath  string
	Cfg       config.UserConfig
	Store     *store.Store
	Forge     *forge.Client
	Cwd       string
	StatePath string
}

func New(cwd string) (*Runtime, error) {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	home, err := config.Home()
	if err != nil {
		return nil, err
	}
	userPath, err := config.UserConfigPath()
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadUser()
	if err != nil {
		return nil, err
	}
	remote := cfg.Remote
	if remote == "" {
		remote = config.DefaultRemote
	}
	s := store.New(home, remote)
	f, err := forge.NewFromRemote(remote, cfg.Forge)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		Home:      home,
		UserPath:  userPath,
		Cfg:       cfg,
		Store:     s,
		Forge:     f,
		Cwd:       cwd,
		StatePath: s.StatePath,
	}, nil
}

type Session struct {
	Runtime *Runtime
	Context *config.Context
	Repo    config.RepoConfig
	Manager *changeset.Manager
	State   *state.Store
}

// Session resolves context for a command or MCP tool. project is only needed
// when the caller wants to override the detected project.
func (r *Runtime) Session(ctx context.Context, project, team, domain string, languages []string) (*Session, error) {
	detected := gitx.Detect(ctx, r.Cwd)
	repo, _ := config.LoadRepo(detected.Root)
	registry, _ := config.LoadRegistry(r.Store.Mirror)
	resolved := config.Resolve(ctx, config.ResolveInput{
		Cwd:      r.Cwd,
		Flags:    config.Flags{Project: project, Team: team, Domain: domain, Languages: languages},
		User:     r.Cfg,
		Repo:     repo,
		Registry: registry,
		Store:    r.Store.Mirror,
	})
	st := state.NewStore(r.Store.StatePath)
	m := changeset.New(r.Store, st, r.Cfg, resolved, r.Forge)
	return &Session{Runtime: r, Context: resolved, Repo: repo, Manager: m, State: st}, nil
}
