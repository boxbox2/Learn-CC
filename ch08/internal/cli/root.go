package cli

import (
	"context"

	"mewcode/internal/app"

	"github.com/spf13/cobra"
)

type App interface {
	RunChat(ctx context.Context, opts app.ChatOptions) error
}

func NewRootCommand(application App) *cobra.Command {
	var opts app.ChatOptions
	cmd := &cobra.Command{
		Use:           "mewcode",
		Short:         "MewCode terminal AI coding assistant",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return application.RunChat(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.Resume, "resume", "", "resume a saved session by id, or use latest")
	cmd.Flags().BoolVar(&opts.ListSessions, "list-sessions", false, "list saved sessions and exit")
	return cmd
}
