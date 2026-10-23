package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/bolacha/docsync/internal/codeindex"
	"github.com/bolacha/docsync/internal/gitx"
	"github.com/spf13/cobra"
)

func (a *App) codeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "code",
		Short: "Inspect the current repository's code map",
	}
	cmd.AddCommand(a.codeMapCmd(), a.codeFindCmd())
	return cmd
}

func (a *App) codeIndex() (*codeindex.Registry, *codeindex.Catalog, error) {
	s, err := a.session()
	if err != nil {
		return nil, nil, err
	}
	if s.Context.RepoRoot == "" {
		return nil, nil, fmt.Errorf("not inside a code repository")
	}
	commit, err := gitx.Run(context.Background(), s.Context.RepoRoot, "rev-parse", "HEAD")
	if err != nil {
		return nil, nil, err
	}
	reg := codeindex.NewDefaultRegistry()
	cat, err := reg.Index(context.Background(), codeindex.IndexRequest{
		Repo: s.Context.Project, Root: s.Context.RepoRoot, Commit: commit,
	})
	if err != nil {
		return nil, nil, err
	}
	return reg, cat, nil
}

func (a *App) codeMapCmd() *cobra.Command {
	var codePath, kind string
	var limit int
	cmd := &cobra.Command{
		Use:   "map",
		Short: "Print the current code symbol map",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cat, err := a.codeIndex()
			if err != nil {
				return err
			}
			var symbols []codeindex.Symbol
			var providers []string
			for _, snap := range cat.Snapshots {
				providers = append(providers, snap.Provider+" "+snap.Version)
				for _, s := range snap.Symbols {
					if kind != "" && !strings.EqualFold(s.Kind, kind) {
						continue
					}
					if codePath != "" && !cliCodePathMatches(s.Path, codePath) {
						continue
					}
					symbols = append(symbols, s)
				}
			}
			total := len(symbols)
			if limit <= 0 {
				limit = 200
			}
			if limit > 2000 {
				limit = 2000
			}
			if len(symbols) > limit {
				symbols = symbols[:limit]
			}
			if a.JSON {
				return a.printJSON(map[string]any{
					"repo": cat.Repo, "commit": cat.Commit, "providers": providers,
					"warnings": cat.Warnings, "symbols": symbols,
					"count": len(symbols), "total": total,
				})
			}
			for _, s := range symbols {
				fmt.Printf("%s:%d  %-9s %-9s %-40s %s\n", s.Path, s.Line, s.Provider, s.Kind, s.Qualified, s.Signature)
			}
			fmt.Printf("\n%d symbols (showing %d)\n", total, len(symbols))
			return nil
		},
	}
	cmd.Flags().StringVar(&codePath, "path", "", "file or directory filter")
	cmd.Flags().StringVar(&kind, "kind", "", "function|method|type|route")
	cmd.Flags().IntVar(&limit, "limit", 200, "maximum symbols")
	return cmd
}

func (a *App) codeFindCmd() *cobra.Command {
	var codePath string
	cmd := &cobra.Command{
		Use:   "find <symbol>",
		Short: "Resolve a code symbol in the current repository",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, cat, err := a.codeIndex()
			if err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			sym, err := reg.Resolve(context.Background(), codeindex.ResolveRequest{
				Repo: cat.Repo, Root: s.Context.RepoRoot, Commit: cat.Commit,
				Path: codePath, Symbol: args[0],
			})
			if err != nil {
				return err
			}
			if a.JSON {
				return a.printJSON(map[string]any{"provider": sym.Provider, "symbol": sym})
			}
			fmt.Printf("%s:%d [%s]\n%s (%s)\n%s\n", sym.Path, sym.Line, sym.Provider, sym.Qualified, sym.Kind, sym.Signature)
			return nil
		},
	}
	cmd.Flags().StringVar(&codePath, "path", "", "file or directory filter")
	return cmd
}

func cliCodePathMatches(symbolPath, filter string) bool {
	filter = strings.Trim(strings.ReplaceAll(filter, "\\\\", "/"), "/")
	if filter == "" || filter == "." {
		return true
	}
	return symbolPath == filter || strings.HasPrefix(symbolPath, strings.TrimSuffix(filter, "/")+"/")
}
