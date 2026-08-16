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
func TestFilterInputKeepsItsFloorUnderWideChips(t *testing.T) {
	m := New(memstore.New(), Options{Table: true})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 50})
	m.view = viewTable
	m.setSort(sortUpdated, false)
	if c := m.selectSlice(sliceEpic, "e-fw2m"); c != nil {
		_ = c
	}
	m.mode = modeFilter
	m.ti.SetValue("lane:backlog")
	m.ti.Focus()
	frame, err := m.Dump(60, 50, "", true)
	if err != nil {
		t.Fatal(err)
	}
	row := strings.Split(frame, "\n")[rowFilter]
	if !strings.Contains(row, "lane:backlog") {
		t.Errorf("the typed text fell below the input floor: %q", strings.TrimRight(row, " "))
	}
}
