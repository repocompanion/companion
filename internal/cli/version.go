package cli

import (
	"fmt"

	"github.com/repocompanion/companion/internal/version"
	"github.com/spf13/cobra"
)

func (a *App) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the companion version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.JSON {
				return a.printJSON(map[string]string{"version": version.String()})
			}
			fmt.Println(version.String())
			return nil
		},
	}
}
