package cli

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/quickadd"
	"github.com/jabiOP/clindar/internal/recur"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/timeparse"
)

// eventFlags are shared by add and edit.
type eventFlags struct {
	at, end, dur     string
	allDay           bool
	calendar         string
	where, desc      string
	invite           []string
	repeat, until    string
	count            int
	offline          bool
	scope, titleFlag string
}

func addEventFlags(cmd *cobra.Command, f *eventFlags) {
	cmd.Flags().StringVar(&f.at, "at", "", `start: "tomorrow 14:00", "fri 2pm", "2026-11-20 09:15"; a date alone means all-day`)
	cmd.Flags().StringVar(&f.end, "end", "", "end time (relative to the start day), or last day for all-day events")
	cmd.Flags().StringVar(&f.dur, "dur", "", "duration, e.g. 30m, 1h30m, 2d (default 1h, or 1 day)")
	cmd.Flags().BoolVar(&f.allDay, "all-day", false, "make it an all-day event")
	cmd.Flags().StringVar(&f.where, "where", "", "location")
	cmd.Flags().StringVar(&f.desc, "desc", "", "description")
	cmd.Flags().StringArrayVar(&f.invite, "invite", nil, "attendee email (repeatable)")
	addOfflineFlag(cmd, &f.offline)
}

func newAddCmd(app *App) *cobra.Command {
	var f eventFlags
	cmd := &cobra.Command{
		Use:   "add [\"TITLE [date] [time] [duration] [repeat]\" | TITLE... --at WHEN | -]",
		Short: "Create an event; prints its ID",
		Long: `Create an event. Without --at, the line is read like Google's quick add: the
title, then optional date, time, duration and repeat words at the end (no date
means today; no time means all day). With --at, the title is taken literally and
the flags decide the timing. With no TITLE or with -, the title is the first line
of stdin, taken literally; --at is then required.`,
		Example: `  clindar add "Dentist 20/11 15:30 45m"
  clindar add "Standup weekdays 9:00 15m"
  clindar add "Trip 20/11-23/11"
  clindar add Dentist --at "tue 15:00" --dur 45m --where "Main St"
  grep 'report' ~/gtd/next.txt | clindar add - --at "tomorrow 9am" --dur 2h`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			title, fromStdin, err := titleFrom(args, app)
			if err != nil {
				return usageError{err}
			}
			if fromStdin && f.at == "" { // a line from a file must never be parsed as dates or repeats
				return usageError{errors.New("a title read from stdin needs --at (only a title typed on the command line is read like quick add)")}
			}
			now := app.Now()
			var t timeparse.Span
			var rec []string
			if f.at == "" { // one line, Google-style
				if f.end != "" || f.dur != "" || f.allDay || f.repeat != "" || f.until != "" || f.count > 0 {
					return usageError{fmt.Errorf("--end/--dur/--all-day/--repeat need --at; or write it all on one line: clindar add \"Dentist 20/11 15:30 45m\"")}
				}
				today := model.StartOfDay(now)
				r, err := quickadd.Parse(title, quickadd.Defaults{From: today, To: today}, now, app.Dates())
				if err != nil {
					return usageError{err}
				}
				title, t, rec = r.Title, r.Span, r.Recurrence
			} else {
				if t, err = app.Dates().ParseSpan(now, f.at, f.end, f.dur, f.allDay); err != nil {
					return usageError{err}
				}
				var until time.Time
				if f.until != "" {
					if until, _, err = app.Dates().ParseTime(f.until, now); err != nil {
						return usageError{fmt.Errorf("--until: %w", err)}
					}
				}
				if rec, err = recur.ParseRepeat(f.repeat, t.Start, t.AllDay, until, f.count); err != nil {
					return usageError{err}
				}
			}
			svc, err := app.Service(ctx)
			if err != nil {
				return err
			}
			e, err := svc.Create(ctx, service.Draft{
				CalendarID: firstNonEmpty(f.calendar, app.Config.DefaultCalendar), Title: title,
				Description: f.desc, Location: f.where, Start: t.Start, End: t.End, AllDay: t.AllDay,
				Recurrence: rec, Invite: f.invite,
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(app.Stdout, e.ID)
			app.PushAfterWrite(ctx, f.offline)
			return nil
		},
	}
	addEventFlags(cmd, &f)
	cmd.Flags().StringVarP(&f.calendar, "calendar", "c", "", "calendar name or ID (default: config default_calendar, else primary)")
	cmd.Flags().StringVar(&f.repeat, "repeat", "", "daily | weekdays | weekly | monthly | yearly | FREQ=... (RFC 5545)")
	cmd.Flags().StringVar(&f.until, "until", "", "last date of a repeating event")
	cmd.Flags().IntVar(&f.count, "count", 0, "number of occurrences of a repeating event")
	return cmd
}

func titleFrom(args []string, app *App) (title string, fromStdin bool, err error) {
	if len(args) > 0 && !(len(args) == 1 && args[0] == "-") {
		return strings.Join(args, " "), false, nil
	}
	line, err := bufio.NewReader(app.Stdin).ReadString('\n')
	if line = strings.TrimSpace(line); line == "" {
		return "", true, fmt.Errorf("no title: give one as arguments or on stdin (%v)", err)
	}
	return line, true, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func addScopeFlag(cmd *cobra.Command, scope *string, allowed string) {
	cmd.Flags().StringVar(scope, "scope", "", "for recurring events: "+allowed)
}

func newEditCmd(app *App) *cobra.Command {
	var f eventFlags
	cmd := &cobra.Command{
		Use:   "edit ID",
		Short: "Change an event; only the flags you give are changed",
		Example: `  clindar edit abc123 --at "thu 16:00"            # move, keeping duration
  clindar edit abc123 -c Work                      # move to another calendar
  clindar edit abc123_20261116T140000Z --title Retro --scope this`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			scope, err := service.ParseScope(f.scope)
			if err != nil {
				return usageError{err}
			}
			svc, err := app.Service(ctx)
			if err != nil {
				return err
			}
			e, err := svc.Get(ctx, args[0])
			if err != nil {
				return err
			}
			ch, err := changesFrom(cmd, f, e, app.Now(), app.Dates())
			if err != nil {
				return usageError{err}
			}
			updated, err := svc.Update(ctx, args[0], ch, scope)
			if err != nil {
				return err
			}
			fmt.Fprintln(app.Stdout, updated.ID)
			app.PushAfterWrite(ctx, f.offline)
			return nil
		},
	}
	addEventFlags(cmd, &f)
	cmd.Flags().StringVar(&f.titleFlag, "title", "", "new title")
	cmd.Flags().StringVarP(&f.calendar, "calendar", "c", "",
		"move to this calendar (name or ID); a recurring event moves as a whole series (--scope all)")
	addScopeFlag(cmd, &f.scope, "this | following | all")
	return cmd
}

// changesFrom builds service.Changes from the flags the user actually set.
func changesFrom(cmd *cobra.Command, f eventFlags, e model.Event, now time.Time, dates timeparse.Parser) (service.Changes, error) {
	var ch service.Changes
	set := cmd.Flags().Changed
	if set("title") {
		ch.Title = &f.titleFlag
	}
	if set("where") {
		ch.Location = &f.where
	}
	if set("desc") {
		ch.Description = &f.desc
	}
	if set("calendar") {
		ch.CalendarID = &f.calendar
	}
	if set("invite") {
		ch.Invite = &f.invite
	}
	allDay := e.AllDay
	if set("all-day") {
		allDay = f.allDay
		ch.AllDay = &allDay
	}
	start := e.Start
	if set("at") {
		t, dateOnly, err := dates.ParseTime(f.at, now)
		if err != nil {
			return ch, fmt.Errorf("--at: %w", err)
		}
		if dateOnly && !allDay { // "--at fri" on a timed event: same time, new day
			clock := e.Start.In(t.Location())
			t = time.Date(t.Year(), t.Month(), t.Day(), clock.Hour(), clock.Minute(), 0, 0, t.Location())
		}
		start = t
		ch.Start = &start
	}
	if set("end") && set("dur") {
		return ch, fmt.Errorf("use --end or --dur, not both")
	}
	if set("end") {
		end, _, err := dates.ParseTime(f.end, start)
		if err != nil {
			return ch, fmt.Errorf("--end: %w", err)
		}
		if allDay {
			end = model.StartOfDay(end).AddDate(0, 0, 1)
		}
		ch.End = &end
	}
	if set("dur") {
		d, err := timeparse.ParseDuration(f.dur)
		if err != nil {
			return ch, fmt.Errorf("--dur: %w", err)
		}
		end := start.Add(d)
		ch.End = &end
	}
	return ch, nil
}

func newRmCmd(app *App) *cobra.Command {
	var scope string
	var offline bool
	cmd := &cobra.Command{
		Use:   "rm ID",
		Short: "Delete an event",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sc, err := service.ParseScope(scope)
			if err != nil {
				return usageError{err}
			}
			svc, err := app.Service(cmd.Context())
			if err != nil {
				return err
			}
			if err := svc.Delete(cmd.Context(), args[0], sc); err != nil {
				return err
			}
			app.PushAfterWrite(cmd.Context(), offline)
			return nil
		},
	}
	addScopeFlag(cmd, &scope, "this | following | all")
	addOfflineFlag(cmd, &offline)
	return cmd
}

var responses = map[string]model.ResponseStatus{
	"yes": model.ResponseAccepted, "no": model.ResponseDeclined, "maybe": model.ResponseTentative,
}

func newRSVPCmd(app *App) *cobra.Command {
	var scope string
	var offline bool
	cmd := &cobra.Command{
		Use:   "rsvp ID yes|no|maybe",
		Short: "Respond to an invitation",
		Args:  exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, ok := responses[strings.ToLower(args[1])]
			if !ok {
				return usageError{fmt.Errorf("answer must be yes, no, or maybe")}
			}
			sc, err := service.ParseScope(scope)
			if err != nil {
				return usageError{err}
			}
			svc, err := app.Service(cmd.Context())
			if err != nil {
				return err
			}
			if err := svc.RSVP(cmd.Context(), args[0], resp, sc); err != nil {
				return err
			}
			app.PushAfterWrite(cmd.Context(), offline)
			return nil
		},
	}
	addScopeFlag(cmd, &scope, "this | all")
	addOfflineFlag(cmd, &offline)
	return cmd
}
