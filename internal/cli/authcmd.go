package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/jabiOP/clindar/internal/auth"
	"github.com/jabiOP/clindar/internal/gcal"
)

func newAuthCmd(app *App) *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Manage your Google login"}
	cmd.AddCommand(newAuthSetupCmd(app), newAuthLoginCmd(app), newAuthStatusCmd(app), newAuthLogoutCmd(app))
	return cmd
}

func newAuthSetupCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "setup CLIENT_SECRET.json",
		Short: "Install your Google OAuth client (see docs/setup-google-cloud.md)",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := auth.LoadClient(args[0]); err != nil { // validate before copying
				return err
			}
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			if err := app.Paths.EnsureDirs(); err != nil {
				return err
			}
			if err := os.WriteFile(app.Paths.ClientFile(), data, 0o600); err != nil {
				return err
			}
			fmt.Fprintf(app.Stderr, "Saved to %s. Next: clindar auth login\n", app.Paths.ClientFile())
			return nil
		},
	}
}

func newAuthLoginCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in to Google in your browser, then do a first sync",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, err := auth.LoadClient(app.Paths.ClientFile())
			if err != nil {
				return err
			}
			st, err := app.Store()
			if err != nil {
				return err
			}
			existing, err := app.Account(ctx)
			if err != nil && !errors.Is(err, ErrNotLoggedIn) {
				return err
			}
			tok, err := auth.Login(ctx, cfg, func(url string) error {
				fmt.Fprintf(app.Stderr, "Opening your browser. If it doesn't open, visit:\n\n  %s\n\n", url)
				_ = auth.OpenBrowser(url) // the printed URL is the fallback
				return nil
			})
			if err != nil {
				return err
			}
			client, err := gcal.NewClient(ctx, "", cfg.Client(ctx, tok))
			if err != nil {
				return err
			}
			email, err := client.PrimaryCalendarID(ctx) // the primary calendar's ID is your email
			if err != nil {
				return err
			}
			if existing != "" && existing != email {
				return fmt.Errorf("already logged in as %s; run `clindar auth logout` first (one account in this version)", existing)
			}
			if err := auth.DefaultTokenStore(app.Paths.TokenFile()).Save(email, tok); err != nil {
				return err
			}
			if err := st.EnsureAccount(ctx, email); err != nil {
				return err
			}
			fmt.Fprintf(app.Stderr, "Logged in as %s. Syncing...\n", email)
			if _, err := app.SyncNow(ctx); err != nil {
				return err
			}
			fmt.Fprintln(app.Stderr, "Done. Try: clindar agenda")
			return nil
		},
	}
}

func newAuthStatusCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show login and sync status",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Service(cmd.Context())
			if err != nil {
				return err
			}
			st, err := svc.Status(cmd.Context(), time.Time{})
			if err != nil {
				return err
			}
			last := "never"
			if !st.LastSync.IsZero() {
				last = app.Now().Sub(st.LastSync).Round(time.Second).String() + " ago"
			}
			fmt.Fprintf(app.Stdout, "account:   %s\nlast sync: %s\npending:   %d change(s)\ndiscarded: %d edit(s) (see clindar log)\n",
				svc.AccountID(), last, st.Pending, st.Discarded)
			return nil
		},
	}
}

func newAuthLogoutCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget your login and delete the local cache (unsynced changes are lost)",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			account, err := app.Account(ctx)
			if err != nil {
				return err
			}
			if err := auth.DefaultTokenStore(app.Paths.TokenFile()).Delete(account); err != nil {
				return err
			}
			if err := app.store.DeleteAccount(ctx, account); err != nil {
				return err
			}
			fmt.Fprintf(app.Stderr, "Logged out %s.\n", account)
			return nil
		},
	}
}
