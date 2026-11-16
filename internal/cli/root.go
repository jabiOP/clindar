package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/jabiOP/clindar/internal/auth"
	"github.com/jabiOP/clindar/internal/browser"
	"github.com/jabiOP/clindar/internal/clipboard"
	"github.com/jabiOP/clindar/internal/config"
	"github.com/jabiOP/clindar/internal/gcal"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/syncer"
	"github.com/jabiOP/clindar/internal/tui"
)

// Version is set by main.
var Version = "dev"

// Main is the real entry point: wires the App and runs the command line.
func Main() int {
	app, err := NewApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, "clindar:", err)
		return exitError
	}
	defer app.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Run(ctx, app, os.Args[1:])
}

// Run executes one command line against app and returns the exit code.
// Main and the tests both go through here.
func Run(ctx context.Context, app *App, args []string) int {
	root := NewRootCmd(app)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err != nil && err.Error() != "" {
		fmt.Fprintln(app.Stderr, "clindar:", err)
	}
	return exitCode(err)
}

// NewApp builds the App used in production.
func NewApp() (*App, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(paths.ConfigFile())
	if err != nil {
		return nil, err
	}
	return &App{
		Paths: paths, Config: cfg, Now: time.Now,
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		IsTTY: term.IsTerminal(int(os.Stdout.Fd())),
		Remote: func(ctx context.Context, account string) (syncer.Remote, error) {
			oauthCfg, err := auth.LoadClient(paths.ClientFile())
			if err != nil {
				return nil, err
			}
			hc, err := auth.HTTPClient(ctx, oauthCfg, auth.DefaultTokenStore(paths.TokenFile()), account)
			if err != nil {
				return nil, err
			}
			return gcal.NewClient(ctx, account, hc)
		},
	}, nil
}

// NewRootCmd builds the command tree. Tests call it with their own App.
func NewRootCmd(app *App) *cobra.Command {
	root := &cobra.Command{
		Use:           "clindar",
		Short:         "A fast terminal client for Google Calendar",
		Version:       Version,
		SilenceUsage:  true, // don't dump usage on runtime errors
		SilenceErrors: true, // Main prints errors once
		RunE: func(cmd *cobra.Command, args []string) error {
			if !app.IsTTY {
				return cmd.Help()
			}
			svc, err := app.Service(cmd.Context())
			if err != nil {
				return err
			}
			return tui.Run(cmd.Context(), tuiDeps(app, svc))
		},
	}
	root.SetIn(app.Stdin)
	root.SetOut(app.Stdout)
	root.SetErr(app.Stderr)
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return usageError{err} })
	root.AddCommand(
		newAgendaCmd(app), newNextCmd(app), newShowCmd(app), newCalendarsCmd(app),
		newAddCmd(app), newEditCmd(app), newRmCmd(app), newRSVPCmd(app),
		newSyncCmd(app), newLogCmd(app), newAuthCmd(app), newDaemonCmd(app),
	)
	return root
}

func addOutputFlags(cmd *cobra.Command, f *outputFlags) {
	cmd.Flags().BoolVar(&f.json, "json", false, "print JSON Lines (one event per line)")
	cmd.Flags().BoolVar(&f.tsv, "tsv", false, "print tab-separated: id start end all_day calendar title")
	cmd.Flags().StringVar(&f.format, "format", "", `print with a Go template, e.g. '{{time "15:04" .Start}} {{.Title}}'`)
}

func addOfflineFlag(cmd *cobra.Command, offline *bool) {
	cmd.Flags().BoolVar(offline, "offline", false, "don't sync with Google; use local data only")
}

// exactArgs is cobra.ExactArgs, but reports a usage error (exit 2).
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(n)(cmd, args); err != nil {
			return usageError{err}
		}
		return nil
	}
}

// tuiDeps maps the app's config onto the TUI. It's a separate function so a
// test can check every setting actually reaches the TUI.
func tuiDeps(app *App, svc *service.Service) tui.Deps {
	return tui.Deps{
		Service: svc,
		Sync: func(ctx context.Context) error {
			_, err := app.SyncNow(ctx) // skipped quietly if the daemon is syncing
			return err
		},
		Now:             app.Now,
		WeekStart:       app.Config.FirstWeekday(),
		SyncInterval:    app.Config.SyncInterval.Duration,
		DefaultCalendar: app.Config.DefaultCalendar,
		Dates:           app.Dates(),
		OpenURL:         browser.Open,
		Copy:            clipboard.Copy,
		Mouse:           app.Config.Mouse,
	}
}
