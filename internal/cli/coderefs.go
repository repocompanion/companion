package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/bolacha/docsync/internal/changeset"
	"github.com/bolacha/docsync/internal/codeindex"
	"github.com/bolacha/docsync/internal/runtime"
	"github.com/bolacha/docsync/internal/store"
	"github.com/spf13/cobra"
)

func (a *App) validateCodeRefsCmd() *cobra.Command {
	var all bool
	var scope, docPath string
	cmd := &cobra.Command{
		Use:   "validate-code-refs",
		Short: "Validate reference_impls paths and symbol fallbacks",
		Long: `Validate reference_impls from the active overlay. Paths are always checked
at their resolved ref. Symbol checks use a semantic provider when available and
fall back to git grep (reported as unverified).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireStore(); err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			ctx := context.Background()
			docs, err := a.codeRefDocs(ctx, s, all, scope, docPath)
			if err != nil {
				return err
			}
			validated, err := codeindex.ValidateDocs(ctx, s.Context.RepoRoot, s.Context.Project, docs)
			if err != nil {
				return err
			}
			summary := codeindex.Summarize(validated)
			if a.JSON {
				if err := a.printJSON(map[string]any{"repo_root": s.Context.RepoRoot, "project": s.Context.Project, "docs": validated, "summary": summary}); err != nil {
					return err
				}
			} else {
				fmt.Print(codeindex.Format(validated))
			}
			if summary.MissingPaths > 0 {
				return fmt.Errorf("%d reference path(s) are missing", summary.MissingPaths)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "validate the whole store")
	cmd.Flags().StringVar(&scope, "scope", "", "project|team|org (default project)")
	cmd.Flags().StringVar(&docPath, "path", "", "validate only one document")
	return cmd
}

func (a *App) staleCodeRefsCmd() *cobra.Command {
	var all bool
	var scope, docPath string
	cmd := &cobra.Command{
		Use:   "stale",
		Short: "Show references that may be stale",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := a.requireStore(); err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			ctx := context.Background()
			docs, err := a.codeRefDocs(ctx, s, all, scope, docPath)
			if err != nil {
				return err
			}
			validated, err := codeindex.ValidateDocs(ctx, s.Context.RepoRoot, s.Context.Project, docs)
			if err != nil {
				return err
			}
			var stale []codeindex.DocValidation
			for _, d := range validated {
				var refs []codeindex.ReferenceValidation
				for _, r := range d.References {
					if r.DriftStatus == codeindex.DriftStale {
						refs = append(refs, r)
					}
				}
				if len(refs) > 0 {
					d.References = refs
					stale = append(stale, d)
				}
			}
			if a.JSON {
				return a.printJSON(map[string]any{"docs": stale, "summary": codeindex.Summarize(stale)})
			}
			if len(stale) == 0 {
				fmt.Println("no stale code references")
				return nil
			}
			fmt.Print(codeindex.Format(stale))
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "check the whole store")
	cmd.Flags().StringVar(&scope, "scope", "", "project|team|org (default project)")
	cmd.Flags().StringVar(&docPath, "path", "", "check only one document")
	return cmd
}

func (a *App) codeRefDocs(ctx context.Context, s *runtime.Session, all bool, scope, docPath string) ([]*store.Doc, error) {
	dir := ""
	if !all {
		dir = s.Context.ProjectArea()
		if scope != "" {
			var err error
			dir, err = codeRefsScopeDir(s, scope)
			if err != nil {
				return nil, err
			}
		}
		if dir == "" {
			return nil, fmt.Errorf("project is unresolved; use --all or --scope org|project|team")
		}
	}
	o, err := s.Manager.OverlayDir(ctx, dir)
	if err != nil {
		return nil, err
	}
	docs := o.Docs
	if strings.TrimSpace(docPath) != "" {
		rel, err := resolveDoc(s, docPath)
		if err != nil {
			return nil, err
		}
		area, err := changeset.AreaForPath(rel)
		if err != nil {
			return nil, err
		}
		o, err := s.Manager.OverlayDir(ctx, area)
		if err != nil {
			return nil, err
		}
		docs = nil
		for _, d := range o.Docs {
			if d.Path == rel {
				docs = append(docs, d)
			}
		}
		if len(docs) == 0 {
			return nil, fmt.Errorf("%s not found", rel)
		}
	}
	return docs, nil
}

func codeRefsScopeDir(s *runtime.Session, scope string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "project":
		if s.Context.ProjectArea() == "" {
			return "", fmt.Errorf("project is unresolved")
		}
		return s.Context.ProjectArea(), nil
	case "team":
		if s.Context.TeamArea() == "" {
			return "", fmt.Errorf("team is unresolved")
		}
		return s.Context.TeamArea(), nil
	case "org":
		return "org", nil
	case "all":
		return "", nil
	default:
		return "", fmt.Errorf("unknown scope %q (project|team|org|all)", scope)
	}
}
