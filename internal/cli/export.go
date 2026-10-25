package cli

import (
	"context"
	"fmt"

	"github.com/repocompanion/companion/internal/export"
	"github.com/repocompanion/companion/internal/runtime"
	"github.com/spf13/cobra"
)

type exportFlags struct {
	output string
	force  bool
	stdout bool
}

func addExportFlags(cmd *cobra.Command, f *exportFlags) {
	cmd.Flags().StringVarP(&f.output, "output", "o", "", "write to a file instead of stdout")
	cmd.Flags().BoolVar(&f.force, "force", false, "overwrite a non-generated file")
	cmd.Flags().BoolVar(&f.stdout, "stdout", false, "print to stdout even when --output is set")
}

func (a *App) exportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export project context for non-MCP agents",
	}
	cmd.AddCommand(a.exportAgentsMDCmd(), a.exportLLMsTxtCmd(), a.exportContextCmd())
	return cmd
}

func (a *App) exportBundle() (*runtime.Session, *export.Bundle, error) {
	if err := a.requireStore(); err != nil {
		return nil, nil, err
	}
	s, err := a.session()
	if err != nil {
		return nil, nil, err
	}
	b, err := export.Build(context.Background(), s)
	if err != nil {
		return nil, nil, err
	}
	return s, b, nil
}

func emitExport(f exportFlags, content string, managed bool) error {
	if f.stdout || f.output == "" || f.output == "-" {
		fmt.Print(content)
		return nil
	}
	if managed {
		return export.WriteManaged(f.output, content, f.force)
	}
	return export.WriteFile(f.output, content, f.force)
}

func (a *App) exportAgentsMDCmd() *cobra.Command {
	var f exportFlags
	cmd := &cobra.Command{
		Use:   "agents-md",
		Short: "Render a managed AGENTS.md context block",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, b, err := a.exportBundle()
			if err != nil {
				return err
			}
			return emitExport(f, export.RenderAgentsMD(b), true)
		},
	}
	addExportFlags(cmd, &f)
	return cmd
}

func (a *App) exportLLMsTxtCmd() *cobra.Command {
	var f exportFlags
	cmd := &cobra.Command{
		Use:   "llms-txt",
		Short: "Render a compact llms.txt discovery file",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, b, err := a.exportBundle()
			if err != nil {
				return err
			}
			return emitExport(f, export.RenderLLMsTxt(b), false)
		},
	}
	addExportFlags(cmd, &f)
	return cmd
}

func (a *App) exportContextCmd() *cobra.Command {
	var f exportFlags
	var format string
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Export the bounded project context bundle",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, b, err := a.exportBundle()
			if err != nil {
				return err
			}
			var content string
			switch format {
			case "", "json":
				content, err = export.RenderContextJSON(b)
			case "markdown", "md":
				content = export.RenderContextMarkdown(b)
			default:
				return fmt.Errorf("unknown format %q; use json or markdown", format)
			}
			if err != nil {
				return err
			}
			return emitExport(f, content, false)
		},
	}
	addExportFlags(cmd, &f)
	cmd.Flags().StringVar(&format, "format", "json", "json or markdown")
	return cmd
}
