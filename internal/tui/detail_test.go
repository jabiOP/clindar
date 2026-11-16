package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jabiOP/clindar/internal/model"
	"github.com/jabiOP/clindar/internal/testutil"
)

// openDetail shows e in the detail pop-up, as a click would.
func (f *fixture) openDetail(e model.Event) {
	m := f.model()
	m.detail, m.mode = e, modeDetail
	f.m = m
}

func TestDetailOfInviteHidesEdit(t *testing.T) {
	f := newMouseFixture(t)
	invite := testutil.Timed("inv", "Their party", time.Date(2026, 11, 20, 18, 0, 0, 0, f.ny), time.Hour)
	invite.Attendees = []model.Attendee{
		{Email: testutil.Account, Self: true}, {Email: "host@example.com", Organizer: true}}
	f.openDetail(invite)

	screen := f.m.View()
	if strings.Contains(screen, "[Edit]") || !strings.Contains(screen, "[Delete]") || !strings.Contains(screen, "[RSVP]") {
		t.Errorf("invite: want [Delete] [RSVP] but no [Edit]:\n%s", screen)
	}
	f.keys("e")
	if m := f.model(); m.mode != modeDetail || !strings.Contains(m.message, "guest") {
		t.Errorf("e on an invite: mode = %v, message = %q; want the pop-up kept open and the reason shown", m.mode, m.message)
	}
}

func TestDetailOfOwnEventHidesRSVP(t *testing.T) {
	f := newMouseFixture(t)
	lunch := testutil.Timed("lunch", "Lunch with Sam", time.Date(2026, 11, 12, 12, 0, 0, 0, f.ny), time.Hour)
	f.openDetail(lunch)

	screen := f.m.View()
	if strings.Contains(screen, "[RSVP]") || !strings.Contains(screen, "[Edit]") {
		t.Errorf("own event: want [Edit] but no [RSVP]:\n%s", screen)
	}
}

func TestDetailOnReadOnlyCalendarOffersOnlyClose(t *testing.T) {
	f := newMouseFixture(t)
	hol := testutil.AllDay("thx", "Thanksgiving", 2026, time.November, 26, 1)
	hol.CalendarID = "holidays" // not in the fixture's calendars: treated as read-only
	f.openDetail(hol)

	screen := f.m.View()
	for _, b := range []string{"[Edit]", "[Delete]", "[RSVP]"} {
		if strings.Contains(screen, b) {
			t.Errorf("read-only event shows %s", b)
		}
	}
}

// Invited, but seen through a read-only calendar (someone else's): no [RSVP],
// and r explains why instead of opening "Going?".
func TestDetailOfInviteOnReadOnlyCalendarHidesRSVP(t *testing.T) {
	f := newMouseFixture(t)
	invite := testutil.Timed("inv", "Their meeting", time.Date(2026, 11, 20, 10, 0, 0, 0, f.ny), time.Hour)
	invite.CalendarID = "holidays" // not in the fixture's calendars: treated as read-only
	invite.Attendees = []model.Attendee{
		{Email: testutil.Account, Self: true}, {Email: "host@example.com", Organizer: true}}
	f.openDetail(invite)

	if screen := f.m.View(); strings.Contains(screen, "[RSVP]") {
		t.Errorf("read-only invite shows [RSVP]:\n%s", screen)
	}
	f.keys("r")
	if m := f.model(); m.mode != modeDetail || !strings.Contains(m.message, "read-only") {
		t.Errorf("r: mode = %v, message = %q; want the pop-up kept open and the reason shown", m.mode, m.message)
	}
}

// A long link, guest list or description must wrap inside the box, not push its
// right border off the screen.
func TestDetailFitsTheTerminal(t *testing.T) {
	f := newFixture(t) // 84 columns
	e := testutil.Timed("long", "Planning", time.Date(2026, 11, 13, 10, 0, 0, 0, f.ny), time.Hour)
	e.Link = "https://www.google.com/calendar/event?eid=" + strings.Repeat("x", 120)
	e.Description = strings.Repeat("word ", 40)
	f.openDetail(e)

	screen := f.m.View()

	for i, line := range strings.Split(screen, "\n") {
		if w := lipgloss.Width(line); w > 84 {
			t.Errorf("line %d is %d columns wide (terminal: 84): %q", i, w, line)
		}
	}
	if strings.Count(screen, "╮") != 1 || strings.Count(screen, "╯") != 1 {
		t.Errorf("box borders missing:\n%s", screen)
	}
}

func TestOpenInGoogle(t *testing.T) {
	var opened string
	f := newFixtureWith(t, Deps{OpenURL: func(u string) error { opened = u; return nil }})
	e := testutil.Timed("ev", "Review", time.Date(2026, 11, 12, 15, 0, 0, 0, f.ny), time.Hour)
	e.Link = "https://www.google.com/calendar/event?eid=abc"
	f.put(e)
	f.openDetail(e)

	if s := f.m.View(); strings.Contains(s, "ID:") || strings.Contains(s, "Link:") {
		t.Errorf("internals in the details box:\n%s", s)
	}
	f.keys("o")

	if opened != e.Link || !strings.Contains(f.model().message, "browser") {
		t.Errorf("opened %q, message %q", opened, f.model().message)
	}
}

func TestMeetJoinAndCopy(t *testing.T) {
	var opened, copied string
	f := newFixtureWith(t, Deps{
		OpenURL: func(u string) error { opened = u; return nil },
		Copy:    func(s string) string { copied = s; return "<copy " + s + ">" },
	})
	e := testutil.Timed("m", "Sync call", time.Date(2026, 11, 12, 16, 0, 0, 0, f.ny), 30*time.Minute)
	e.MeetLink = "https://meet.google.com/abc-defg-hij"
	f.put(e)
	f.openDetail(e)

	f.keys("y")
	f.keys("m")

	if copied != e.MeetLink || opened != e.MeetLink {
		t.Errorf("copied %q, opened %q", copied, opened)
	}
}

// The copy sequence goes out inside a frame, so it can't interleave with one.
func TestCopyGoesOutWithTheFrame(t *testing.T) {
	f := newFixtureWith(t, Deps{Copy: func(s string) string { return "<copy " + s + ">" }})
	e := testutil.Timed("m", "Sync call", time.Date(2026, 11, 12, 16, 0, 0, 0, f.ny), 30*time.Minute)
	e.MeetLink = "https://meet.google.com/abc-defg-hij"
	f.put(e)
	f.openDetail(e)

	m, cmd := f.m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if got := m.View(); strings.Count(got, "<copy "+e.MeetLink+">") != 1 {
		t.Fatalf("the frame should carry the copy once:\n%s", got)
	}
	m, _ = m.Update(cmd())
	if strings.Contains(m.View(), "<copy") || m.(Model).message != "Meet link copied" {
		t.Errorf("after the frame: message %q, view still copying: %v", m.(Model).message, strings.Contains(m.View(), "<copy"))
	}
}

func TestNoMeetButtonsWithoutAMeetLink(t *testing.T) {
	f := newMouseFixture(t)
	f.openDetail(testutil.Timed("lunch", "Lunch with Sam", time.Date(2026, 11, 12, 12, 0, 0, 0, f.ny), time.Hour))
	if s := f.m.View(); strings.Contains(s, "[Join]") || strings.Contains(s, "[Copy]") {
		t.Errorf("Meet buttons without a Meet link:\n%s", s)
	}
}
