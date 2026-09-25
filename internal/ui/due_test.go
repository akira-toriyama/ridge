package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// fixedZone pins the one zone (board.Zone; never time.Local — see the
// clock's declaration) for the duration of a test, so a due parsed through
// board.ParseDue and rendered here agree on the day; the pin outranks the
// calendar a fixture store declares. A due is stored as a UTC instant, so
// "which day is this?" is only a real question off UTC.
func fixedZone(t *testing.T, name string, offsetHours int) {
	t.Helper()
	zone := time.FixedZone(name, offsetHours*3600)
	t.Cleanup(board.SetClock(nil, func() *time.Location { return zone }))
}

// eveningDue builds the instant furrow stores for "2026-09-02 08:00" in a
// UTC+9 calendar: 2026-09-01T23:00:00Z. Formatting that in UTC reads 2026-09-01 —
// one day early, and it does NOT self-heal on reload, because the wrong day
// comes straight off furrow's own JSON.
func eveningDue() time.Time { return time.Date(2026, 9, 2, 8, 0, 0, 0, board.Zone()).UTC() }

func TestPeekRendersDueOnItsCalendarDay(t *testing.T) {
	fixedZone(t, "TEST", 9)
	b := board.NewBoard([]*board.Task{
		{ID: "a", Status: "ready", Title: "promise", Due: eveningDue()},
	})
	m := New(memstore.NewWith(b), Options{})
	m.w, m.h = 120, 30
	m.peekOpen = true
	m.recompute()
	out := ansiStrip(m.peekContent(60))

	if !strings.Contains(out, "due 2026-09-02") {
		t.Errorf("the peek must date a due by its board-calendar day:\n%s", out)
	}
	if strings.Contains(out, "due 2026-09-01") {
		t.Errorf("the peek dated the due a day early (UTC instant, calendar promise):\n%s", out)
	}
}

func TestEditMenuRendersDueOnItsCalendarDay(t *testing.T) {
	fixedZone(t, "TEST", 9)
	m := editModel(t, "t-9sa6")
	m.b.Task("t-9sa6").Due = eveningDue()
	out := frame(m)

	if !strings.Contains(out, "2026-09-02") || strings.Contains(out, "2026-09-01") {
		t.Errorf("the edit menu must show the due's board-calendar day:\n%s", out)
	}
}

// The offset forms furrow accepts have to be reachable from the overlay — an
// hour-scale snooze is the whole reason `+2h` exists.
func TestEditDueAcceptsFurrowsOffsetForms(t *testing.T) {
	for _, form := range []string{"+1m", "+2h", "-1d", "+0d"} {
		t.Run(form, func(t *testing.T) {
			m := editModel(t, "t-9sa6")
			m.edit.menuIdx = int(fieldDue)
			press(m, "enter")
			m.edit.input.SetValue(form)
			press(m, "enter")

			if m.statusErr {
				t.Fatalf("furrow accepts %s; ridge refused it", form)
			}
			if m.b.Task("t-9sa6").Due.IsZero() {
				t.Fatalf("%s left the due unset", form)
			}
			drainPersists(m, t)
		})
	}
}

// The same instant, every stamp on the panel: created and the ago() fallback
// date the board-calendar day exactly like due does. One instant,
// 2026-09-01T23:00Z, is 09-02 at UTC+9; a panel that said "due 09-02" beside
// "created 09-01" for it was measured before this test existed.
func TestPeekDatesCreatedAndOldUpdatedOnTheCalendarDay(t *testing.T) {
	fixedZone(t, "TEST", 9)
	// 200 days on, so ago() takes its date fallback instead of "Nd ago".
	fixedNow(t, eveningDue().Add(200*24*time.Hour))
	b := board.NewBoard([]*board.Task{
		{ID: "a", Status: "ready", Title: "promise", Created: eveningDue(), Updated: eveningDue()},
	})
	m := New(memstore.NewWith(b), Options{})
	m.w, m.h = 120, 30
	m.peekOpen = true
	m.recompute()
	out := ansiStrip(m.peekContent(60))

	for _, want := range []string{"created 2026-09-02", "updated 2026-09-02"} {
		if !strings.Contains(out, want) {
			t.Errorf("the peek must date %q by its board-calendar day:\n%s", want, out)
		}
	}
	if strings.Contains(out, "2026-09-01") {
		t.Errorf("a stamp was dated in UTC, one day early:\n%s", out)
	}
}

// isOverdue is the one overdue predicate (six readers). Its "and not closed"
// clause is pinned here on a synthetic task: the fixture's one closed task
// with a due (t-2qyb) reaches memstore's twin through is:overdue, but no ui
// test frames it, so dropping this clause would fail nothing else.
func TestIsOverdueIgnoresAClosedTask(t *testing.T) {
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	fixedNow(t, at)

	past, future := at.Add(-24*time.Hour), at.Add(24*time.Hour)
	for _, tc := range []struct {
		name string
		task board.Task
		want bool
	}{
		{"past due, open", board.Task{Due: past}, true},
		{"past due, closed", board.Task{Due: past, Closed: past.Add(time.Hour)}, false},
		{"future due, open", board.Task{Due: future}, false},
		{"no due", board.Task{}, false},
	} {
		if got := isOverdue(&tc.task); got != tc.want {
			t.Errorf("%s: isOverdue = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A due typed as a bare day is parsed in the zone board.ParseDue reads and
// rendered in the zone the peek formats with. They are the same clock now;
// when ui and board each had their own, a test that pinned only ui's parsed
// the instant in the runner's zone (UTC on CI) and rendered it in the pinned
// one, so the day slid by one — and the same slide was reachable in a
// headless frame. Pinned off UTC on purpose: on UTC the two clocks agreed by
// accident.
func TestATypedDueRendersOnTheDayItWasTyped(t *testing.T) {
	fixedZone(t, "JST", 9)
	due, err := board.ParseDue("2026-09-02")
	if err != nil {
		t.Fatal(err)
	}
	m := boardModel(t, 240, 50)
	task := m.b.Task("t-jv3j")
	task.Due = due
	m.selectID(task.ID, false)
	m.peekOpen = true
	m.syncPeek()
	if out := ansiStrip(m.peekContent(80)); !strings.Contains(out, "due 2026-09-02") {
		t.Errorf("the peek renders the typed day through another zone:\n%s", out)
	}
}
