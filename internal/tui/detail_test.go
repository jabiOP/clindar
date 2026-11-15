package tui

import (
	"strings"
	"testing"
	"time"

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
