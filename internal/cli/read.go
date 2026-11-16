package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
)

func newAgendaCmd(app *App) *cobra.Command {
	var wf windowFlags
	var of outputFlags
	var calendars []string
	var offline bool
	cmd := &cobra.Command{
		Use:   "agenda",
		Short: "List events (default: today and the next 6 days)",
		Example: `  clindar agenda --today
  clindar agenda --from mon --days 5 --json
  clindar agenda --within 15m --format '{{.Title}}'   # for notifications`,
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w, err := agendaWindow(app.Now(), wf, app.Dates())
			if err != nil {
				return usageError{err}
			}
			app.RefreshIfStale(ctx, offline)
			svc, err := app.Service(ctx)
			if err != nil {
				return err
			}
			filter, err := calendarFilter(cmd, svc, calendars)
			if err != nil {
				return err
			}
			events, err := svc.Agenda(ctx, w.from, w.to, filter)
			if err != nil {
				return err
			}
			if w.startingOnly {
				events = startingIn(events, w)
			}
			p, err := app.Printer(ctx, svc, of)
			if err != nil {
				return err
			}
			return p.Events(events, w.from, w.days())
		},
	}
	cmd.Flags().StringVar(&wf.from, "from", "", "start of range (default: today)")
	cmd.Flags().StringVar(&wf.to, "to", "", "end of range; a date includes that whole day")
	cmd.Flags().IntVar(&wf.days, "days", 0, "number of days from --from (default 7)")
	cmd.Flags().BoolVar(&wf.today, "today", false, "only today")
	cmd.Flags().StringVar(&wf.within, "within", "", "only events starting between now and now+DURATION")
	cmd.Flags().StringArrayVarP(&calendars, "calendar", "c", nil, "calendar name or ID (repeatable; default: calendars shown in Google)")
	addOutputFlags(cmd, &of)
	addOfflineFlag(cmd, &offline)
	return cmd
}

func calendarFilter(cmd *cobra.Command, svc *service.Service, names []string) (service.Filter, error) {
	var f service.Filter
	for _, n := range names {
		c, err := svc.ResolveCalendar(cmd.Context(), n)
		if err != nil {
			return f, err
		}
		f.CalendarIDs = append(f.CalendarIDs, c.ID)
	}
	return f, nil
}

func startingIn(events []model.Event, w window) []model.Event {
	var out []model.Event
	for _, e := range events {
		if !e.AllDay && !e.Start.Before(w.from) && e.Start.Before(w.to) {
			out = append(out, e)
		}
	}
	return out
}

func newNextCmd(app *App) *cobra.Command {
	var of outputFlags
	var offline bool
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Show the current or next timed event (exit status 1 if none in 7 days)",
		Example: `  clindar next                       # in 12m  09:00–09:15  Standup
  clindar next --format '{{.Title}}'  # for a status bar`,
		Args: exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			app.RefreshIfStale(ctx, offline)
			svc, err := app.Service(ctx)
			if err != nil {
				return err
			}
			e, ok, err := svc.Next(ctx)
			if err != nil {
				return err
			}
			if !ok {
				return silentError{code: exitError}
			}
			p, err := app.Printer(ctx, svc, of)
			if err != nil {
				return err
			}
			return p.Next(e, app.Now())
		},
	}
	addOutputFlags(cmd, &of)
	addOfflineFlag(cmd, &offline)
	return cmd
}

func newShowCmd(app *App) *cobra.Command {
	var of outputFlags
	var offline bool
	cmd := &cobra.Command{
		Use:   "show ID",
		Short: "Show one event in detail",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			app.RefreshIfStale(ctx, offline)
			svc, err := app.Service(ctx)
			if err != nil {
				return err
			}
			e, err := svc.Get(ctx, args[0])
			if err != nil {
				return err
			}
			p, err := app.Printer(ctx, svc, of)
			if err != nil {
				return err
			}
			return p.Event(e)
		},
	}
	addOutputFlags(cmd, &of)
	addOfflineFlag(cmd, &offline)
	return cmd
}

func newCalendarsCmd(app *App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "calendars",
		Short: "List calendars (* = shown; change in the TUI with c)",
		Args:  exactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Service(cmd.Context())
			if err != nil {
				return err
			}
			cals, err := svc.Calendars(cmd.Context())
			if err != nil {
				return err
			}
			for _, c := range cals {
				if asJSON {
					b, _ := json.Marshal(c)
					fmt.Fprintf(app.Stdout, "%s\n", b)
					continue
				}
				mark := " "
				if c.Visible {
					mark = "*"
				}
				fmt.Fprintf(app.Stdout, "%s %-24s %-8s %s\n", mark, c.Name, c.AccessRole, c.ID)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON Lines")
	return cmd
}
