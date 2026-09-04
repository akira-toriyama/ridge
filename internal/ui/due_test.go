package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// fixedZone pins THIS package's localZone (never time.Local — see its
// declaration) for the duration of a test. board.localZone stays at the
// runner's zone: a test that pins here and then parses a due through
// board.ParseDue would compute the instant in one zone and render it in
// another. A due is stored as a
// UTC instant, so "which day is this?" is only a real question off UTC.
func fixedZone(t *testing.T, name string, offsetHours int) {
	t.Helper()
	prev := localZone
	zone := time.FixedZone(name, offsetHours*3600)
	localZone = func() *time.Location { return zone }
	t.Cleanup(func() { localZone = prev })
}

// eveningDue builds the instant furrow stores for "2026-09-02 08:00 local" on a
// UTC+9 box: 2026-09-01T23:00:00Z. Formatting that in UTC reads 2026-09-01 —
// one day early, and it does NOT self-heal on reload, because the wrong day
// comes straight off furrow's own JSON.
func eveningDue() time.Time { return time.Date(2026, 9, 2, 8, 0, 0, 0, localZone()).UTC() }

func TestPeekRendersDueOnItsLocalDay(t *testing.T) {
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
		t.Errorf("the peek must date a due by its LOCAL day:\n%s", out)
	}
	if strings.Contains(out, "due 2026-09-01") {
		t.Errorf("the peek dated the due a day early (UTC instant, local promise):\n%s", out)
	}
}

func TestEditMenuRendersDueOnItsLocalDay(t *testing.T) {
	fixedZone(t, "TEST", 9)
	m := editModel(t, "t-9sa6")
	m.b.Task("t-9sa6").Due = eveningDue()
	out := frame(m)

	if !strings.Contains(out, "2026-09-02") || strings.Contains(out, "2026-09-01") {
		t.Errorf("the edit menu must show the due's LOCAL day:\n%s", out)
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
// date the LOCAL day exactly like due does. One instant, 2026-09-01T23:00Z, is
// 09-02 at UTC+9; a panel that said "due 09-02" beside "created 09-01" for it
// was measured before this test existed.
func TestPeekDatesCreatedAndOldUpdatedOnTheLocalDay(t *testing.T) {
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
			t.Errorf("the peek must date %q by its LOCAL day:\n%s", want, out)
		}
	}
	if strings.Contains(out, "2026-09-01") {
		t.Errorf("a stamp was dated in UTC, one day early:\n%s", out)
	}
}
