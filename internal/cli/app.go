// Package cli implements the clindar command line with cobra. Commands are thin:
// parse flags, call the service, print with the output package.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jabiOP/clindar/internal/config"
	"github.com/jabiOP/clindar/internal/output"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/store"
	"github.com/jabiOP/clindar/internal/syncer"
)

var ErrNotLoggedIn = errors.New("not logged in: run `clindar auth login` (first time? see `clindar auth setup --help`)")

// App holds everything commands need. Main builds the real one; tests build
// one with temp dirs, a fixed clock, buffers, and a fake Remote.
type App struct {
	Paths  config.Paths
	Config config.Config
	Now    func() time.Time
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	IsTTY  bool // stdout is a terminal
	// Remote builds the Google client for an account.
	Remote func(ctx context.Context, accountID string) (syncer.Remote, error)

	store *store.Store
}

// Store opens the database on first use.
func (a *App) Store() (*store.Store, error) {
	if a.store != nil {
		return a.store, nil
	}
	if err := a.Paths.EnsureDirs(); err != nil {
		return nil, err
	}
	st, err := store.Open(a.Paths.DBFile())
	if err != nil {
		return nil, err
	}
	a.store = st
	return st, nil
}

func (a *App) Close() error {
	if a.store == nil {
		return nil
	}
	return a.store.Close()
}

// Account returns the logged-in account (v1 supports exactly one).
func (a *App) Account(ctx context.Context) (string, error) {
	st, err := a.Store()
	if err != nil {
		return "", err
	}
	ids, err := st.Accounts(ctx)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		return "", ErrNotLoggedIn
	}
	return ids[0], nil
}

func (a *App) Service(ctx context.Context) (*service.Service, error) {
	account, err := a.Account(ctx)
	if err != nil {
		return nil, err
	}
	return service.New(a.store, account, a.Now, a.Config.SendUpdates), nil
}

func (a *App) Engine(ctx context.Context) (*syncer.Engine, error) {
	account, err := a.Account(ctx)
	if err != nil {
		return nil, err
	}
	remote, err := a.Remote(ctx, account)
	if err != nil {
		return nil, err
	}
	return &syncer.Engine{Store: a.store, Remote: remote, AccountID: account, Now: a.Now}, nil
}

// SyncNow runs one sync unless another process (the daemon) is already
// syncing, in which case ran is false and that's fine.
func (a *App) SyncNow(ctx context.Context) (ran bool, err error) {
	engine, err := a.Engine(ctx)
	if err != nil {
		return false, err
	}
	return syncer.WithLock(ctx, a.Paths.LockFile(), engine.Sync)
}

// syncQuietly syncs within the configured timeout, printing a warning
// instead of failing: reads and writes always work from the local store.
func (a *App) syncQuietly(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, a.Config.SyncTimeout.Duration)
	defer cancel()
	if _, err := a.SyncNow(ctx); err != nil {
		fmt.Fprintf(a.Stderr, "clindar: warning: sync failed, using local data: %v\n", err)
	}
}

// RefreshIfStale is called by read commands: sync first if the last sync is
// older than stale_after, unless --offline.
func (a *App) RefreshIfStale(ctx context.Context, offline bool) {
	if offline {
		return
	}
	account, err := a.Account(ctx)
	if err != nil {
		return // the command itself will report this
	}
	last, err := a.store.LastSync(ctx, account)
	if err == nil && a.Now().Sub(last) < a.Config.StaleAfter.Duration {
		return
	}
	a.syncQuietly(ctx)
}

// PushAfterWrite is called by write commands so edits reach Google promptly.
func (a *App) PushAfterWrite(ctx context.Context, offline bool) {
	if !offline {
		a.syncQuietly(ctx)
	}
}

// outputFlags are shared by every command that prints events.
type outputFlags struct {
	json, tsv bool
	format    string
}

func (a *App) Printer(ctx context.Context, svc *service.Service, f outputFlags) (*output.Printer, error) {
	mode, err := output.ChooseMode(f.json, f.tsv, f.format, a.IsTTY)
	if err != nil {
		return nil, usageError{err}
	}
	cals, err := svc.Calendars(ctx)
	if err != nil {
		return nil, err
	}
	return output.NewPrinter(a.Stdout, mode, f.format, a.Now().Location(), cals)
}
