package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// The filter row's chips must survive the input taking the keyboard. The input
// pads to its whole width, and it used to be sized once per WindowSizeMsg to a
// fixed w-30 remainder — a slice term plus the table's sort readout overran
// that, so the sort fell off the row exactly while the user was typing
// (t-a54p). The WindowSizeMsg here is load-bearing: the old sizing only ran on
// resize, so a bare Dump rendered the constructor's 48-cell input and the bug
// was invisible headless — which is also why the demo alone is not enough.
func TestFilterRowKeepsItsChipsWhileTheInputHasTheKeyboard(t *testing.T) {
	for _, w := range []int{240, 320, 400} {
		m := New(memstore.New(), Options{Table: true})
		m.Update(tea.WindowSizeMsg{Width: w, Height: 50})
		frame, err := m.Dump(w, 50, "filterchips", true)
		if err != nil {
			t.Fatal(err)
		}
		row := strings.Split(frame, "\n")[rowFilter]
		for _, want := range []string{"lane:backlog is:blocked", "slice epic:e-fw2m", "sort updated"} {
			if !strings.Contains(row, want) {
				t.Errorf("width %d: %q is missing from the filter row: %q",
					w, want, strings.TrimRight(row, " "))
			}
		}
	}
}

// A pathological chip pile must squeeze the input no further than its floor —
// the row degrades by truncating chips, never by erasing what is being typed.
// The width must make the floor actually engage: at 60 cells the chips left
// 25 > 20 and the first cut of this test passed with the sizing deleted
// (independent review of this PR, R2), so the floor is asserted directly.
func TestFilterInputKeepsItsFloorUnderWideChips(t *testing.T) {
	m := New(memstore.New(), Options{Table: true})
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 50})
	m.view = viewTable
	m.setSort(sortUpdated, false)
	if c := m.selectSlice(sliceEpic, "e-fw2m"); c != nil {
		_ = c
	}
	m.mode = modeFilter
	m.ti.SetValue("lane:backlog")
	m.ti.Focus()
	frame, err := m.Dump(50, 50, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.ti.Width(); got != minFilterInputW {
		t.Errorf("input width = %d, want the %d-cell floor to hold", got, minFilterInputW)
	}
	row := strings.Split(frame, "\n")[rowFilter]
	if !strings.Contains(row, "lane:backlog") {
		t.Errorf("the typed text fell below the input floor: %q", strings.TrimRight(row, " "))
	}
}

// Entering the mode with an existing query must show the query from its HEAD.
// The value is seeded while the input still has some other width, and bubbles
// v2 recomputes the horizontal scroll window only on cursor moves — without
// the render-time SetCursor no-op, a 68-cell query showed only its tail in a
// stale 48-cell window while the row had 237 cells of room (independent
// review of this PR, R1: a main-relative regression of exactly the class this
// PR fixes).
func TestEnteringFilterWithAQueryShowsItsHead(t *testing.T) {
	q := "lane:backlog is:blocked label:ui label:board label:table label:graph"
	m := New(memstore.New(), Options{Filter: q})
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 50})
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	frame, err := m.Dump(240, 50, "", true)
	if err != nil {
		t.Fatal(err)
	}
	row := strings.Split(frame, "\n")[rowFilter]
	if !strings.Contains(row, q) {
		t.Errorf("the query is not shown whole while the row has room: %q", strings.TrimRight(row, " "))
	}
}

// Shrinking the terminal mid-edit must keep the chips. The give-back branch's
// SECOND SetCursor is what re-runs the overflow window at the reduced width —
// review round 2 (N1) measured that deleting it alone evicted both chips on
// every 400→smaller resize while all other tests stayed green, so this is the
// one test that owns it.
func TestShrinkResizeMidEditKeepsTheChips(t *testing.T) {
	m := New(memstore.New(), Options{Table: true})
	m.Update(tea.WindowSizeMsg{Width: 400, Height: 50})
	m.view = viewTable
	m.setSort(sortUpdated, false)
	if c := m.selectSlice(sliceEpic, "e-fw2m"); c != nil {
		_ = c
	}
	m.mode = modeFilter
	m.ti.SetValue(strings.Repeat("label:board label:ui ", 20)) // ~420 cells, wider than any window
	m.ti.Focus()
	if _, err := m.Dump(400, 50, "", true); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 50})
	frame, err := m.Dump(240, 50, "", true)
	if err != nil {
		t.Fatal(err)
	}
	row := strings.Split(frame, "\n")[rowFilter]
	for _, want := range []string{"slice epic:e-fw2m", "sort updated"} {
		if !strings.Contains(row, want) {
			t.Errorf("%q fell off the row after the shrink: %q", want, strings.TrimRight(row, " "))
		}
	}
}
