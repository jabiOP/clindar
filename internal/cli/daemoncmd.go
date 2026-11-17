package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/jabiOP/clindar/internal/daemon"
	"github.com/jabiOP/clindar/internal/syncer"
)

func newDaemonCmd(app *App) *cobra.Command {
	var interval time.Duration
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Keep the local cache in sync in the background",
		Long: `Runs until stopped, syncing every --interval (config: sync_interval).
Usually started by your service manager: see 'clindar daemon install'.`,
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if interval == 0 {
				interval = app.Config.SyncInterval.Duration
			}
			log := slog.New(slog.NewTextHandler(app.Stderr, nil))
			log.Info("clindar daemon started", "data", app.Paths.DataDir, "interval", interval)
			var engine *syncer.Engine
			daemon.Loop(cmd.Context(), interval, 5*time.Minute, func(ctx context.Context) error {
				if engine == nil { // not logged in yet: wait and retry, rather than exit and be restarted at once
					e, err := app.Engine(ctx)
					if err != nil {
						return fmt.Errorf("%w (data folder: %s)", err, app.Paths.DataDir)
					}
					engine = e
					log.Info("syncing", "account", engine.AccountID)
				}
				ran, err := syncer.WithLock(ctx, app.Paths.LockFile(), engine.Sync)
				if !ran && err == nil {
					log.Debug("skipped: another sync is running")
				}
				return err
			}, time.After, log)
			return nil
		},
	}
	cmd.Flags().DurationVar(&interval, "interval", 0, "time between syncs (default: config sync_interval, 20s)")
	cmd.AddCommand(newDaemonInstallCmd(app), newDaemonUninstallCmd(app), newDaemonRestartCmd(app))
	return cmd
}

func newDaemonInstallCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install a systemd user service (Linux) or launchd agent (macOS)",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			unit, err := thisOSUnit(app)
			if err != nil {
				return err
			}
			if err := unit.Write(); err != nil {
				return err
			}
			fmt.Fprintf(app.Stderr, "Wrote %s (it uses %s and %s)\nStart it now with:\n\n  %s\n",
				unit.Path, app.Paths.ConfigDir, app.Paths.DataDir, unit.Enable)
			return nil
		},
	}
}

// runService runs a service-manager command (launchctl, systemctl); tests replace it.
var runService = func(command string, stdout, stderr io.Writer) error {
	c := exec.Command("sh", "-c", command)
	c.Stdout, c.Stderr = stdout, stderr
	return c.Run()
}

func newDaemonRestartCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the installed daemon on this clindar (after an update); does nothing if none is installed",
		Long: `Rewrites the service file written by 'daemon install' for this clindar binary and
its config and data folders, then restarts the daemon, so it runs the version you
just installed. 'make install' runs it after building.`,
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			unit, err := thisOSUnit(app)
			if err != nil {
				return err
			}
			if _, err := os.Stat(unit.Path); errors.Is(err, fs.ErrNotExist) {
				fmt.Fprintln(app.Stderr, "No daemon installed (see 'clindar daemon install'); nothing to restart.")
				return nil
			}
			if err := unit.Write(); err != nil {
				return err
			}
			if err := runService(unit.Restart, app.Stdout, app.Stderr); err != nil {
				return fmt.Errorf("restart the daemon: %w", err)
			}
			fmt.Fprintf(app.Stderr, "Daemon restarted (%s).\n", unit.Path)
			return nil
		},
	}
}

func newDaemonUninstallCmd(app *App) *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the service file written by 'daemon install'",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			unit, err := thisOSUnit(app)
			if err != nil {
				return err
			}
			fmt.Fprintf(app.Stderr, "First stop it with:\n\n  %s\n\n", unit.Disable)
			if err := os.Remove(unit.Path); err != nil && !os.IsNotExist(err) {
				return err
			}
			fmt.Fprintf(app.Stderr, "Removed %s\n", unit.Path)
			return nil
		},
	}
}

func thisOSUnit(app *App) (daemon.Unit, error) {
	exe, err := os.Executable()
	if err != nil {
		return daemon.Unit{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return daemon.Unit{}, err
	}
	config, err := filepath.Abs(app.Paths.ConfigDir)
	if err != nil {
		return daemon.Unit{}, err
	}
	data, err := filepath.Abs(app.Paths.DataDir)
	if err != nil {
		return daemon.Unit{}, err
	}
	return daemon.ForThisOS(exe, home, daemon.Dirs{Config: config, Data: data})
}
