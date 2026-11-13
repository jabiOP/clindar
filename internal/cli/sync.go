package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newSyncCmd(app *App) *cobra.Command {
	var full bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync with Google now",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			account, err := app.Account(ctx)
			if err != nil {
				return err
			}
			if full {
				if err := app.store.ResetSyncTokens(ctx, account); err != nil {
					return err
				}
			}
			ran, err := app.SyncNow(ctx)
			if err != nil {
				return err
			}
			if !ran {
				fmt.Fprintln(app.Stderr, "clindar: another sync is running (the daemon?); it will pick up your changes")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&full, "full", false, "re-download everything (fixes a confused cache)")
	return cmd
}

func newLogCmd(app *App) *cobra.Command {
	var n int
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show discarded edits and sync errors, newest first",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Service(cmd.Context())
			if err != nil {
				return err
			}
			entries, err := svc.Logs(cmd.Context(), n)
			if err != nil {
				return err
			}
			for _, e := range entries {
				fmt.Fprintf(app.Stdout, "%s  %-14s  %s\n", e.Time.In(app.Now().Location()).Format("2006-01-02 15:04"), e.Kind, e.Message)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&n, "lines", "n", 20, "number of entries")
	return cmd
}
