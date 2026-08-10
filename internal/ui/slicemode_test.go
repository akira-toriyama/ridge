package ui

import (
	"strings"
	"testing"
)

func TestSliceIssuesAQTermAndComposesWithTheFilter(t *testing.T) {
	m := boardModel(t, 240, 50)
	all := m.countVisible()

	press(m, "s")
	if m.mode != modeSlice || !m.sliceOpen {
		t.Fatal("s must open and focus the panel")
	}
	if c := m.selectSlice(sliceLabel, "ui"); c != nil {
		t.Fatal("a mock slice must apply synchronously")
	}
	if m.effectiveQuery() != "label:ui" {
		t.Fatalf("effective query = %q", m.effectiveQuery())
	}
	sliced := m.countVisible()
	if sliced == 0 || sliced >= all {
		t.Fatalf("slice narrowed %d -> %d", all, sliced)
	}

	// The typed filter ANDs with the slice — and stays UNEDITED.
	m.applyFilter("lane:backlog")
	if m.qRaw != "lane:backlog" {
		t.Errorf("the slice must not edit the typed query: %q", m.qRaw)
	}
	if m.effectiveQuery() != "lane:backlog label:ui" {
		t.Errorf("effective = %q", m.effectiveQuery())
	}
	both := m.countVisible()
	if both == 0 || both > sliced {
		t.Errorf("AND composition: %d sliced, %d with the filter", sliced, both)
	}

	// Selecting the active value again un-slices (radio semantics).
	m.selectSlice(sliceLabel, "ui")
	if m.sliceVal != "" || m.effectiveQuery() != "lane:backlog" {
		t.Errorf("re-select must clear: val=%q eff=%q", m.sliceVal, m.effectiveQuery())
	}
}

func TestSliceAxisSwitchClearsTheSelection(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "s")
	m.selectSlice(sliceLabel, "ui")
	m.cycleSliceField(+1)
	if m.sliceVal != "" {
		t.Error("a repo slice makes no claim about labels — switching the axis must clear")
	}
	if m.countVisible() != len(m.b.Tasks()) {
		t.Error("clearing the slice must restore the whole board")
	}
}

func TestSliceInsetsTheBoardAndItsHitTest(t *testing.T) {
	m := boardModel(t, 240, 50)
	col0 := m.lay.Cols[0].X

	press(m, "s")
	if m.lay.Cols[0].X != col0+sliceInsetW {
		t.Fatalf("first lane X = %d, want %d — layout and hit-test share the inset",
			m.lay.Cols[0].X, col0+sliceInsetW)
	}
	if lane, _, ok := m.lay.cardAt(2, sliceRowTop); ok || lane != "" {
		t.Error("a point inside the panel must not hit a card")
	}

	// Closing the panel restores the full width.
	press(m, "s")
	if m.sliceOpen || m.lay.Cols[0].X != col0 {
		t.Errorf("closing must restore the layout: open=%v X=%d", m.sliceOpen, m.lay.Cols[0].X)
	}
}

func TestSliceEpicRowsCarryProgressAndClickSelects(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "s")
	m.sliceField = sliceEpic
	rows := m.sliceRows()
	if len(rows) != 1 || rows[0].value != "e-fw2m" {
		t.Fatalf("epic rows = %+v", rows)
	}
	if !strings.Contains(rows[0].display, "6/18") {
		t.Errorf("the epic row must carry the store's progress: %q", rows[0].display)
	}

	// A click on the first value row selects it.
	if c := m.sliceClick(3, sliceRowTop); c != nil {
		t.Fatal("mock slice click must apply synchronously")
	}
	if m.sliceVal != "e-fw2m" || m.effectiveQuery() != "epic:e-fw2m" {
		t.Errorf("click selected %q, effective %q", m.sliceVal, m.effectiveQuery())
	}
	if got, want := m.countVisible(), 18; got != want {
		t.Errorf("epic slice shows %d, want %d members", got, want)
	}
}

func TestSliceSelectionSurvivesClosingThePanel(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "s")
	m.selectSlice(sliceLabel, "ui")
	sliced := m.countVisible()

	press(m, "esc") // leave the panel focused state; panel stays
	if m.mode != modeNormal || !m.sliceOpen {
		t.Fatal("esc must return the keyboard to the board and keep the panel")
	}
	press(m, "s", "s") // refocus, then close
	if m.sliceOpen {
		t.Fatal("s from the panel must close it")
	}
	if m.countVisible() != sliced || m.sliceVal != "ui" {
		t.Error("closing the panel must not clear the slice — GH's No-slicing is explicit")
	}
	out := frame(m)
	if !strings.Contains(out, "label:ui") {
		t.Error("a slice filtering a panel-less board must be visible in the filter bar")
	}
}

func TestSlicePanelRendersInTheFrame(t *testing.T) {
	m := boardModel(t, 240, 50)
	if err := m.demoState("slice"); err != nil {
		t.Fatal(err)
	}
	out := frame(m)
	for _, want := range []string{"Slice by", "repo", "label", "epic", "● ui"} {
		if !strings.Contains(out, want) {
			t.Errorf("the panel frame is missing %q", want)
		}
	}
}
