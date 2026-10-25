// Package runtime wires config, the local store, context resolution and the
// changeset manager together for the CLI and the MCP server.
package runtime

import (
	"context"
	"fmt"
	"os"

	"github.com/repocompanion/companion/internal/changeset"
	"github.com/repocompanion/companion/internal/config"
	"github.com/repocompanion/companion/internal/forge"
	"github.com/repocompanion/companion/internal/gitx"
	"github.com/repocompanion/companion/internal/state"
	"github.com/repocompanion/companion/internal/store"
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
	return r.SessionAt(ctx, r.Cwd, project, team, domain, languages)
}

// SessionAt resolves context from an explicit working directory. MCP roots use
// this so a client-provided root can determine the project without relying on
// the server process cwd.
func (r *Runtime) SessionAt(ctx context.Context, cwd, project, team, domain string, languages []string) (*Session, error) {
	if cwd == "" {
		cwd = r.Cwd
	}
	detected := gitx.Detect(ctx, cwd)
	repo, err := config.LoadRepo(detected.Root)
	if err != nil {
		return nil, fmt.Errorf("load repository config: %w", err)
	}
	registry, err := config.LoadRegistry(r.Store.Mirror)
	if err != nil {
		return nil, fmt.Errorf("load store registry: %w", err)
	}
	resolved := config.Resolve(ctx, config.ResolveInput{
		Cwd:      cwd,
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
