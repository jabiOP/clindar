// Package tui is the Bubble Tea interface: a 4-week grid and an agenda list.
// Update never blocks: anything touching the service runs in a tea.Cmd and
// reports back with a message.
package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/service"
	"github.com/jabiOP/clindar/internal/timeparse"
)

// Deps are the TUI's connections to the rest of the app.
type Deps struct {
	Service         *service.Service
	Sync            func(context.Context) error // nil disables syncing (tests)
	Now             func() time.Time
	WeekStart       time.Weekday
	DefaultCalendar string           // config default_calendar: pre-fills the add form
	Dates           timeparse.Parser // config date_order: how typed dates are read
	Mouse           bool             // config mouse: clicks, wheel, drag-and-drop (Phase 12)
	SyncInterval    time.Duration    // 0 disables periodic sync (tests)
}

type view int

const (
	viewGrid view = iota
	viewList
)

type mode int

const (
	modeNormal mode = iota
	modeDetail
	modeForm
	modeConfirm // y/n delete of a one-off event
	modeScope   // this/following/all for a recurring event
	modeRSVP    // y/n/m
	modeGoto
	modeHelp
)

type Model struct {
	deps          Deps
	ctx           context.Context
	width, height int
	view          view
	mode          mode

	today, selected, gridStart time.Time // local midnights
	cursor                     int       // list view: index into listEntries

	loadedFrom, loadedTo time.Time
	events               []model.Event
	calendars            map[string]model.Calendar
	status               service.Status
	started              time.Time
	syncing              bool
	message              string // last error or info, shown in the status bar

	form      form
	gotoInput textinput.Model
	detail    model.Event // the event shown in modeDetail
	target    model.Event // the event an RSVP (modeRSVP) applies to
	drag      dragState   // mouse drag in the grid (Phase 12)
	hover     edgeRef     // the handle under the pointer (Phase 12.7)
	lastWheel time.Time   // last wheel step taken (throttle, Task 14.4)

	dayMode   bool // event mode: j/k pick an event on the selected grid day (Task 14.8)
	dayCursor int  // event mode: index into dayEvents()

	listFrom   time.Time // first day of the list
	listSpan   int       // days loaded in the list
	listAnchor *entryRef // after prepending: the entry the cursor must land just before
	// afterScope runs the pending action once the user picks a scope.
	afterScope    func(service.Scope) tea.Cmd
	allowedScopes string // e.g. "tfa" or "ta"
}

// --- messages ---

type loadedMsg struct {
	from, to  time.Time
	events    []model.Event
	calendars []model.Calendar
	status    service.Status
	err       error
}
type syncDoneMsg struct{ err error }
type tickMsg struct{}
type actionDoneMsg struct {
	info string
	err  error
}

func New(ctx context.Context, d Deps) Model {
	now := d.Now()
	today := model.StartOfDay(now)
	gi := textinput.New()
	gi.Placeholder = "fri, +2w, 2026-12-24"
	gi.Cursor.SetMode(cursor.CursorStatic)
	return Model{
		deps: d, ctx: ctx, width: 80, height: 24,
		today: today, selected: today, gridStart: weekStart(today, d.WeekStart),
		started: now, calendars: map[string]model.Calendar{}, gotoInput: gi,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.load(), m.syncCmd(), m.tick())
}

// --- commands (run off the UI goroutine) ---

// window is the range the views need: the grid plus the list's two weeks.
func (m Model) window() (time.Time, time.Time) {
	from := m.gridStart
	if m.selected.Before(from) {
		from = m.selected
	}
	to := m.gridStart.AddDate(0, 0, gridDays)
	if m.view == viewList { // only while it shows: the list may span up to 2 years
		if m.listFrom.Before(from) {
			from = m.listFrom
		}
		if end := m.listFrom.AddDate(0, 0, m.listSpan); end.After(to) {
			to = end
		}
	}
	return from, to
}

func (m Model) load() tea.Cmd {
	from, to := m.window()
	svc, ctx, started := m.deps.Service, m.ctx, m.started
	return func() tea.Msg {
		msg := loadedMsg{from: from, to: to}
		if msg.events, msg.err = svc.Agenda(ctx, from, to, service.Filter{}); msg.err != nil {
			return msg
		}
		if msg.calendars, msg.err = svc.Calendars(ctx); msg.err != nil {
			return msg
		}
		msg.status, msg.err = svc.Status(ctx, started)
		return msg
	}
}

func (m Model) syncCmd() tea.Cmd {
	if m.deps.Sync == nil {
		return nil
	}
	sync, ctx := m.deps.Sync, m.ctx
	return func() tea.Msg { return syncDoneMsg{err: sync(ctx)} }
}

func (m Model) tick() tea.Cmd {
	if m.deps.SyncInterval <= 0 {
		return nil
	}
	return tea.Tick(m.deps.SyncInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// act runs a service call, then reloads and pushes.
func (m Model) act(info string, fn func(context.Context, *service.Service) error) tea.Cmd {
	svc, ctx := m.deps.Service, m.ctx
	return func() tea.Msg { return actionDoneMsg{info: info, err: fn(ctx, svc)} }
}

// --- update ---

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case loadedMsg:
		if msg.err != nil {
			m.message = "load failed: " + msg.err.Error()
			return m, nil
		}
		m.loadedFrom, m.loadedTo, m.events, m.status = msg.from, msg.to, msg.events, msg.status
		m.calendars = map[string]model.Calendar{}
		for _, c := range msg.calendars {
			m.calendars[c.ID] = c
		}
		if m.view == viewList {
			if m.listAnchor != nil { // just prepended: land before the old first entry
				if i := slices.Index(m.entryRefs(), *m.listAnchor); i > 0 {
					m.cursor = i - 1
				}
				m.listAnchor = nil
			}
			if m.listRows() < 2*(m.height-1) && m.listSpan < listMax { // a screenful, plus one ahead for J
				m.listSpan += listStep
				return m, m.load()
			}
		}
		m.cursor = min(m.cursor, max(len(m.entries())-1, 0))
		return m, nil
	case syncDoneMsg:
		m.syncing = false
		if msg.err != nil {
			m.message = "offline? " + msg.err.Error()
		}
		return m, m.load()
	case tickMsg:
		m.today = model.StartOfDay(m.deps.Now()) // the TUI may stay open past midnight
		m.syncing = m.deps.Sync != nil
		return m, tea.Batch(m.syncCmd(), m.tick())
	case actionDoneMsg:
		m.mode = modeNormal
		if msg.err != nil {
			m.message = msg.err.Error()
			return m, m.load()
		}
		m.message = msg.info
		return m, tea.Batch(m.load(), m.syncCmd())
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

func (m Model) loc() *time.Location { return m.deps.Now().Location() }

// entries is the list view's selectable events.
func (m Model) entries() []model.Event { return listEntries(m.listDays()) }

// dayEvents are the selected day's events, in display order.
func (m Model) dayEvents() []model.Event { return model.GroupByDay(m.events, m.selected, 1)[0].Events }

const (
	listInitial = 28      // days the list starts with
	listStep    = 14      // days added when scrolling past either end
	listMax     = 2 * 366 // never load more than this
)

// entryRef identifies a list entry across reloads (multi-day events appear once per day).
type entryRef struct {
	calendarID, eventID string
	day                 time.Time
}

func (m Model) listDays() []model.Day { return model.GroupByDay(m.events, m.listFrom, m.listSpan) }

func (m Model) entryRefs() []entryRef {
	var out []entryRef
	for _, d := range m.listDays() {
		for _, e := range d.Events {
			out = append(out, entryRef{e.CalendarID, e.ID, d.Date})
		}
	}
	return out
}

// listRows is how many lines the list draws (day headers + entries).
func (m Model) listRows() int {
	rows := 0
	for _, d := range m.listDays() {
		if len(d.Events) > 0 {
			rows += 1 + len(d.Events)
		}
	}
	return rows
}

// openList shows the list starting at day.
func (m Model) openList(day time.Time) Model {
	m.view, m.listFrom, m.listSpan, m.cursor = viewList, day, listInitial, 0
	return m
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

// Run starts the TUI full-screen and blocks until the user quits.
func Run(ctx context.Context, d Deps) error {
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithContext(ctx)}
	if d.Mouse {
		// All motion: clicks, wheel, dragging, and hover (reveals resize handles).
		opts = append(opts, tea.WithMouseAllMotion())
	}
	_, err := tea.NewProgram(New(ctx, d), opts...).Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil // Ctrl-C via the context
	}
	return err
}
