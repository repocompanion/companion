package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/repocompanion/companion/internal/changeset"
	"github.com/repocompanion/companion/internal/docrules"
	"github.com/repocompanion/companion/internal/importdocs"
	"github.com/spf13/cobra"
)

func (a *App) importCmd() *cobra.Command {
	var from, area, mapPath, reportPath, status, lastReviewed string
	var owners []string
	var apply bool
	cmd := &cobra.Command{
		Use:   "import --from <dir>",
		Short: "Migrate a Markdown tree into the canonical layout",
		Long: `Scan a local Markdown tree and propose canonical destinations. Dry-run is
the default. Use --report to review the mapping, --map to override ambiguous
destinations, and --apply to create a changeset and open/update a draft PR.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(from) == "" {
				return fmt.Errorf("--from is required")
			}
			if err := a.requireStore(); err != nil {
				return err
			}
			s, err := a.session()
			if err != nil {
				return err
			}
			ctx := context.Background()
			targetArea := strings.Trim(area, "/")
			if targetArea == "" {
				targetArea = s.Context.ProjectArea()
			}
			if targetArea == "" {
				return fmt.Errorf("no target area resolved; pass --project owner/repo or --area org|teams/<team>|projects/<owner>/<repo>")
			}
			if err := changeset.ValidateArea(targetArea); err != nil {
				return err
			}
			if status == "" {
				status = "approved"
			}
			if !docrules.ValidStatuses[status] {
				return fmt.Errorf("unknown status %q", status)
			}
			if len(owners) == 0 {
				owners = []string{ownerFor(s)}
			}
			mapping := map[string]string{}
			if mapPath != "" {
				b, err := os.ReadFile(mapPath)
				if err != nil {
					return err
				}
				if err := json.Unmarshal(b, &mapping); err != nil {
					return fmt.Errorf("parse --map %s: %w", mapPath, err)
				}
				normalized := map[string]string{}
				for src, dst := range mapping {
					src = strings.TrimPrefix(strings.ReplaceAll(src, "\\", "/"), "./")
					normalized[src] = strings.Trim(strings.ReplaceAll(dst, "\\", "/"), "/")
				}
				mapping = normalized
			}
			o, err := s.Manager.Overlay(ctx, targetArea)
			if err != nil {
				return err
			}
			existing := map[string]bool{}
			existingIDs := map[string]string{}
			for _, d := range o.Docs {
				existing[d.Path] = true
				if d.Frontmatter.ID != "" {
					existingIDs[d.Frontmatter.ID] = d.Path
				}
			}
			report, err := importdocs.Scan(from, targetArea, importdocs.Options{
				Owners: owners, Status: status, LastReviewed: lastReviewed,
				Mapping: mapping, Existing: existing, ExistingIDs: existingIDs,
				Now: time.Now(),
			})
			if err != nil {
				return err
			}
			if reportPath != "" {
				if err := importdocs.WriteReport(report, reportPath); err != nil {
					return err
				}
			}
			if a.JSON {
				if err := a.printJSON(report); err != nil {
					return err
				}
			} else {
				fmt.Print(importdocs.Format(report))
			}
			if !apply {
				if report.Unresolved > 0 || report.Collisions > 0 {
					fmt.Println("dry run only; resolve unresolved/collision entries with --map before --apply")
				}
				return nil
			}
			if report.Ready == 0 {
				return fmt.Errorf("nothing to import")
			}
			if report.Unresolved > 0 || report.Collisions > 0 {
				return fmt.Errorf("cannot apply: %d unresolved, %d collisions; use --report/--map and rerun", report.Unresolved, report.Collisions)
			}
			c, err := s.Manager.EnsureNamed(ctx, targetArea, "import docs into "+targetArea)
			if err != nil {
				return err
			}
			for _, e := range report.Entries {
				if e.Status != importdocs.StatusReady {
					continue
				}
				if _, _, err := s.Manager.WriteFile(ctx, targetArea, e.Destination, e.Content, "", "cli"); err != nil {
					return fmt.Errorf("write %s: %w", e.Destination, err)
				}
			}
			res, err := s.Manager.Sync(ctx, c.ID, "cli")
			if err != nil {
				if pendingNote(err) {
					if a.JSON {
						if werr := a.printJSON(map[string]any{"report": report, "result": res, "imported": report.Ready, "pending": true}); werr != nil {
							return werr
						}
					} else {
						printResult(res)
					}
					return ErrPending
				}
				return err
			}
			if a.JSON {
				return a.printJSON(map[string]any{"report": report, "result": res, "imported": report.Ready})
			}
			printResult(res)
			fmt.Printf("imported %d documents into changeset %s\n", report.Ready, c.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "source directory containing Markdown files")
	cmd.Flags().StringVar(&area, "area", "", "target area: org, teams/<team> or projects/<owner>/<repo>")
	cmd.Flags().StringVar(&mapPath, "map", "", "JSON object mapping source paths to destination paths")
	cmd.Flags().StringVar(&reportPath, "report", "", "write the migration report as JSON")
	cmd.Flags().BoolVar(&apply, "apply", false, "apply ready entries to a changeset (default: dry run)")
	cmd.Flags().StringSliceVar(&owners, "owner", nil, "owner(s) for generated frontmatter")
	cmd.Flags().StringVar(&status, "status", "approved", "status for generated frontmatter")
	cmd.Flags().StringVar(&lastReviewed, "last-reviewed", "", "last_reviewed for generated frontmatter (YYYY-MM-DD)")
	return cmd
}
