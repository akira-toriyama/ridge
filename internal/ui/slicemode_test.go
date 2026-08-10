package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

// --- 2026-08-10 independent-review regressions (t-74y3) ---

// A slice value with a space used to be issued bare: furrow tokenises -q on
// whitespace, answered a DIFFERENT query with exit 0, and the board went
// blank with no warning. The quoted form is what the real binary matches.
func TestSliceTermQuotesValuesWithSpaces(t *testing.T) {
	m, p := scriptedModel(t)
	if c := m.selectSlice(sliceLabel, "needs review"); c != nil {
		m.Update(c())
	}
	if got := m.sliceTerm(); got != `label:"needs review"` {
		t.Fatalf("sliceTerm = %q, want the quoted form", got)
	}
	if n := len(p.queries); n == 0 || p.queries[n-1] != `label:"needs review"` {
		t.Errorf("store was asked %v, want the quoted term", p.queries)
	}
}

// furrow's -q quoting has no escape, so a value containing a double quote has
// no spelling at all: refuse loudly instead of issuing a query that means
// something else.
func TestSliceRefusesAValueWithADoubleQuote(t *testing.T) {
	m, _ := scriptedModel(t)
	if c := m.selectSlice(sliceLabel, `say "no"`); c != nil {
		t.Fatal("an unexpressible value must not refire the query")
	}
	if m.sliceVal != "" || !m.statusErr {
		t.Errorf("sliceVal=%q statusErr=%v — want a loud refusal and no slice", m.sliceVal, m.statusErr)
	}
}

// The click band used to validate against the FULL row list while the frame
// rendered a truncated one: clicking "+N more" or even the status line
// sliced the board to an invisible value. Renderer and click now share
// sliceViewport, and the cursor scrolls the window instead of walking off it.
func TestSlicePanelScrollsAndClicksAgree(t *testing.T) {
	h := sliceRowTop + footerH + 4 // value-region capacity 3 → window 1 when overflowing
	m := boardModel(t, 240, h)
	press(m, "s")
	m.Update(keyMsg("tab")) // repo → label (labels: ci, cli, testing, ui)
	rows := m.sliceRows()
	if len(rows) < 4 {
		t.Fatalf("fixture drifted: want ≥4 label rows, got %d", len(rows))
	}
	_, window, overflow := m.sliceViewport(len(rows))
	if !overflow || window != 1 {
		t.Fatalf("viewport = window %d overflow %v, want the 1-row overflowing shape", window, overflow)
	}

	// Cursor past the window: the window follows, the frame shows the cursor.
	press(m, "down", "down", "down") // sliceIdx 3
	out := frame(m)
	if !strings.Contains(out, "▌ ") {
		t.Error("the cursor row must be rendered — it used to walk off the panel")
	}
	if !strings.Contains(out, "↑ 3 more") {
		t.Errorf("the up indicator must count the rows above the window")
	}

	// The indicator lines are inert; the one value row selects what it shows.
	if c := m.sliceClick(3, sliceRowTop); c != nil {
		t.Error("a click on the ↑ indicator must be inert")
	}
	if c := m.sliceClick(3, sliceRowTop+2); c != nil {
		t.Error("a click on the +N more indicator must be inert")
	}
	if c := m.sliceClick(3, m.h-footerH); c != nil {
		t.Error("a click on the status line must be inert")
	}
	if c := m.sliceClick(3, sliceRowTop+1); c != nil {
		m.Update(c())
	}
	if m.sliceVal != rows[3].value {
		t.Errorf("clicked the visible row, got slice %q want %q", m.sliceVal, rows[3].value)
	}
}

// Esc used to have no case for a slice-only filter: the board stayed
// narrowed with no gesture to widen it short of reopening the panel.
func TestEscClearsASliceOnlyFilter(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "s")
	m.Update(keyMsg("tab"))
	press(m, "enter", "esc") // slice to the first label row, leave the panel
	if m.sliceVal == "" {
		t.Fatal("setup: expected an active slice")
	}
	press(m, "esc")
	if m.sliceVal != "" {
		t.Error("esc on the board must clear a slice-only filter")
	}
}

// Pins are artifacts of the view they were made in; a slice switch is a new
// view. They used to survive every slice change and — with an empty typed
// query — were unclearable by any gesture.
func TestSliceChangeClearsPins(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.pinned["t-ghost"] = true
	if c := m.selectSlice(sliceLabel, "ui"); c != nil {
		m.Update(c())
	}
	if len(m.pinned) != 0 {
		t.Error("selecting a slice must clear stale pins")
	}
}

// Quick add inherits the APPLIED filter — and the slice term is part of it
// (it is rendered in the filter bar for exactly that reason). It used to
// inherit only the typed query, so the chips claimed "board auto" while the
// created task fell outside the very slice on screen.
func TestQuickAddInheritsTheSliceTerm(t *testing.T) {
	m := boardModel(t, 240, 50)
	if c := m.selectSlice(sliceLabel, "ui"); c != nil {
		m.Update(c())
	}
	press(m, "a")
	if m.add == nil || m.add.opts.Label != "ui" {
		t.Fatalf("add opts = %+v, want the slice's label inherited", m.add)
	}
}

// The graph's visibility predicate mirrors taskVisible; it used to consult
// the typed query only, so a slice that narrowed the board dimmed nothing.
func TestGraphDimsNodesOutsideTheSlice(t *testing.T) {
	m := boardModel(t, 240, 50)
	if err := m.demoState("graph"); err != nil {
		t.Fatal(err)
	}
	m.sliceField, m.sliceVal = sliceLabel, "ui"
	m.qMatched = map[string]bool{m.graphFocus: true}
	lay := m.buildGraph()
	dimmed := 0
	for _, n := range lay.Nodes {
		if n.ID != "" && n.ID != m.graphFocus && n.Hidden {
			dimmed++
		}
	}
	if dimmed == 0 {
		t.Error("a slice that hides board cards must dim the same tasks in the graph")
	}
}

// `s` reaches the table view too; the panel used to capture the keyboard
// there while rendering nothing.
func TestSlicePanelRendersInTheTableView(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "v", "s")
	if m.mode != modeSlice {
		t.Fatal("s must focus the panel in the table view")
	}
	if out := frame(m); !strings.Contains(out, "Slice by") {
		t.Error("the panel must be VISIBLE in the table view — an invisible panel eats the keyboard")
	}
}

// Opening the panel re-insets every column; a drag surviving the shift used
// to drop 27 cells away from the pointer and persist the move.
func TestTogglingThePanelCancelsADragInFlight(t *testing.T) {
	m := boardModel(t, 240, 50)
	col := m.lay.Col("backlog")
	if col == nil || len(col.Cards) == 0 {
		t.Fatal("no backlog cards laid out")
	}
	card := col.Cards[0]
	before := ids(m.b.LaneTasks("backlog"))
	m.Update(tea.MouseClickMsg{X: card.X + 2, Y: card.Y + 1, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: card.X + 12, Y: card.Y + 6, Button: tea.MouseLeft})
	press(m, "s")
	m.Update(tea.MouseReleaseMsg{X: card.X + 12, Y: card.Y + 6, Button: tea.MouseLeft})
	if got := ids(m.b.LaneTasks("backlog")); !slices.Equal(got, before) {
		t.Errorf("backlog = %v, want %v — the cut gesture must not move anything", got, before)
	}
	if m.inflight || len(m.pending) > 0 {
		t.Error("the cut gesture must not persist anything")
	}
}
