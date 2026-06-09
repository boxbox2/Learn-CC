package cli

import (
	"context"

	"github.com/spf13/cobra"
)

type App interface {
	RunChat(ctx context.Context) error
}

func NewRootCommand(app App) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "mewcode",
		Short:         "MewCode terminal AI coding assistant",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return app.RunChat(cmd.Context())
		},
	}
	return cmd
}
