package ui

import (
	"testing"
	"time"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// at builds a local instant inside the zone the test pinned. Every date here
// goes through it so a test cannot accidentally mix zones.
func at(y int, m time.Month, d, hh int) time.Time {
	return time.Date(y, m, d, hh, 0, 0, 0, localZone())
}

// The population rule and the order in one sweep: dateless absent, done
// absent, the rest due-ascending with the id as the tiebreak.
func TestRoadPopulationDropsDatelessAndDoneAndOrdersByDue(t *testing.T) {
	fixedZone(t, "TEST", 9)
	fixedNow(t, at(2026, 9, 1, 12))
	b := board.NewBoard([]*board.Task{
		{ID: "t-late", Status: "ready", Title: "late", Due: at(2026, 9, 20, 8)},
		{ID: "t-none", Status: "ready", Title: "dateless"},
		{ID: "t-done", Status: "done", Title: "kept", Due: at(2026, 9, 5, 8), Closed: at(2026, 8, 30, 8)},
		{ID: "t-b", Status: "backlog", Title: "tied b", Due: at(2026, 9, 10, 8)},
		{ID: "t-a", Status: "backlog", Title: "tied a", Due: at(2026, 9, 10, 8)},
	})
	m := New(memstore.NewWith(b), Options{})
	l := packRoad(m.roadPopulation(), zoomDay, nowFn())

	var got []string
	for _, r := range l.Rows {
		got = append(got, r.ID)
	}
	want := []string{"t-a", "t-b", "t-late"}
	if len(got) != len(want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rows = %q, want %q", got, want)
		}
	}
}

// A due is a UTC instant recording a LOCAL day (board.ParseDue puts a bare
// day at its last local second). 23:00Z on the 1st IS the 2nd on a UTC+9
// box, and an axis derived from the instant's UTC day would place the ◆ one
// cell early — the peek's "due a day early" bug, now on an axis where it
// also mis-sorts nothing but mis-places everything.
func TestRoadCellsSplitOnLocalDaysNotUTCDays(t *testing.T) {
	fixedZone(t, "TEST", 9)
	fixedNow(t, at(2026, 9, 1, 12))
	evening := eveningDue() // 2026-09-01T23:00:00Z = 2026-09-02 08:00 local
	l := packRoad([]*board.Task{
		{ID: "t-noon", Due: at(2026, 9, 1, 12)},
		{ID: "t-eve", Due: evening},
	}, zoomDay, nowFn())

	noon, eve := l.Row("t-noon"), l.Row("t-eve")
	if noon == nil || eve == nil {
		t.Fatal("both rows must be on the axis")
	}
	if eve.X != noon.X+1 {
		t.Errorf("the evening due landed on cell %d against noon's %d, want one day later — "+
			"the axis split on the UTC day", eve.X, noon.X)
	}
}

// Weeks are Monday-aligned local weeks, months calendar months — not 7-day or
// 30-day buckets counted from an arbitrary origin.
func TestRoadWeekAndMonthCellsSplitOnCalendarBoundaries(t *testing.T) {
	fixedZone(t, "TEST", 9)
	fixedNow(t, at(2026, 9, 1, 12))

	// 2026-09-06 is a Sunday, 09-07 the Monday after it.
	l := packRoad([]*board.Task{
		{ID: "t-sun", Due: at(2026, 9, 6, 12)},
		{ID: "t-mon", Due: at(2026, 9, 7, 12)},
		{ID: "t-nextsun", Due: at(2026, 9, 13, 12)},
	}, zoomWeek, nowFn())
	if l.Row("t-mon").X != l.Row("t-sun").X+1 {
		t.Errorf("Sunday and the Monday after it share a week cell (%d vs %d)",
			l.Row("t-sun").X, l.Row("t-mon").X)
	}
	if l.Row("t-mon").X != l.Row("t-nextsun").X {
		t.Errorf("Monday and its own Sunday split (%d vs %d) — the week is not Monday-aligned",
			l.Row("t-mon").X, l.Row("t-nextsun").X)
	}

	l = packRoad([]*board.Task{
		{ID: "t-aug", Due: at(2026, 8, 31, 12)},
		{ID: "t-sep1", Due: at(2026, 9, 1, 12)},
		{ID: "t-sep30", Due: at(2026, 9, 30, 12)},
	}, zoomMonth, nowFn())
	if l.Row("t-sep1").X != l.Row("t-aug").X+1 {
		t.Errorf("Aug 31 and Sep 1 share a month cell (%d vs %d)",
			l.Row("t-aug").X, l.Row("t-sep1").X)
	}
	if l.Row("t-sep1").X != l.Row("t-sep30").X {
		t.Errorf("Sep 1 and Sep 30 split (%d vs %d)", l.Row("t-sep1").X, l.Row("t-sep30").X)
	}
}

// The axis pads one unit past both extremes and always carries today — even
// when every promise is on one side of it.
func TestRoadAxisPadsItsEndsAndAlwaysCarriesToday(t *testing.T) {
	fixedZone(t, "TEST", 9)
	fixedNow(t, at(2026, 9, 7, 12))
	l := packRoad([]*board.Task{
		{ID: "t-a", Due: at(2026, 9, 5, 12)},
		{ID: "t-b", Due: at(2026, 9, 10, 12)},
	}, zoomDay, nowFn())

	if got := l.Row("t-a").X; got != 1 {
		t.Errorf("the earliest due sits at cell %d, want 1 (one pad cell before it)", got)
	}
	if got := l.TodayX; got != 3 {
		t.Errorf("today sits at cell %d, want 3", got)
	}
	if got := l.Cells; got != 8 {
		t.Errorf("axis is %d cells, want 8 (Sep 4 .. Sep 11)", got)
	}

	// Every due behind today: the axis must still reach forward to hold it.
	l = packRoad([]*board.Task{{ID: "t-old", Due: at(2026, 8, 1, 12)}}, zoomDay, nowFn())
	if l.TodayX != l.Cells-2 {
		t.Errorf("today sits at %d of %d cells, want the last content cell before the pad", l.TodayX, l.Cells)
	}
	if l.Empty() {
		t.Error("one dated task is not an empty axis")
	}
}

// The label rows: real period starts win, the window's left edge names the
// period it is inside only when no real label is close, and a label never
// overlaps its neighbour or hangs past the window.
func TestRoadTicksLabelPeriodsWithoutCollisions(t *testing.T) {
	fixedZone(t, "TEST", 9)
	start := dayNum(at(2026, 8, 10, 0)) // cell 0 = Monday 2026-08-10

	coarse, fine := roadTicks(zoomDay, start, 0, 40)
	if len(coarse) < 2 || coarse[0].Text != "2026-08" || coarse[0].X != 0 {
		t.Fatalf("coarse = %+v, want the edge context 2026-08 at 0", coarse)
	}
	if coarse[1].Text != "2026-09" || coarse[1].X != 22 {
		t.Errorf("coarse = %+v, want 2026-09 at cell 22 (Sep 1)", coarse)
	}
	if len(fine) == 0 || fine[0].Text != "10" || fine[0].X != 0 {
		t.Errorf("fine = %+v, want Monday '10' at cell 0", fine)
	}

	// Cell 0 = Aug 30: the real "2026-09" lands at cell 2, too close for the
	// 7-wide context to fit before it — the context yields, because the first
	// cut of roadTicks kept it and its collision guard then dropped September
	// itself, leaving a two-cell July labelled over an August-dominated frame.
	coarse, _ = roadTicks(zoomDay, dayNum(at(2026, 8, 30, 0)), 0, 40)
	if len(coarse) == 0 || coarse[0].Text != "2026-09" || coarse[0].X != 2 {
		t.Errorf("coarse = %+v, want 2026-09 at cell 2 with no context label jammed before it", coarse)
	}

	// A label that would hang past the window's right edge is dropped whole:
	// a truncated "2026-0…" names a period that does not exist.
	coarse, _ = roadTicks(zoomDay, start, 0, 5)
	for _, tk := range coarse {
		if tk.X+len(tk.Text) > 5 {
			t.Errorf("label %q at %d overflows a 5-cell window", tk.Text, tk.X)
		}
	}
}

func TestRoadStepWalksTheListAndClampsAtTheEnds(t *testing.T) {
	fixedZone(t, "TEST", 9)
	fixedNow(t, at(2026, 9, 1, 12))
	l := packRoad([]*board.Task{
		{ID: "t-a", Due: at(2026, 9, 2, 12)},
		{ID: "t-b", Due: at(2026, 9, 3, 12)},
	}, zoomDay, nowFn())

	if got := l.step("t-a", +1); got != "t-b" {
		t.Errorf("step down from t-a = %q, want t-b", got)
	}
	if got := l.step("t-a", -1); got != "t-a" {
		t.Errorf("step up at the top = %q, want to stay on t-a", got)
	}
	if got := l.step("t-b", +1); got != "t-b" {
		t.Errorf("step down at the bottom = %q, want to stay on t-b", got)
	}
	if got := l.step("t-gone", +1); got != "t-a" {
		t.Errorf("step from a vanished row = %q, want the first row", got)
	}
}
