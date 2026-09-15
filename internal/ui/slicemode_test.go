package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/charmbracelet/x/ansi"
)

func TestSliceIssuesAQTermAndComposesWithTheFilter(t *testing.T) {
	m := boardModel(t, 240, 50)
	all := m.countVisible()

	press(m, "s")
	if m.mode != modeSlice || !m.sliceOpen {
		t.Fatal("s must open and focus the panel")
	}
	if c := m.selectSlice(sliceLabel, "bbq"); c != nil {
		t.Fatal("a mock slice must apply synchronously")
	}
	if m.effectiveQuery() != "label:bbq" {
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
	if m.effectiveQuery() != "lane:backlog label:bbq" {
		t.Errorf("effective = %q", m.effectiveQuery())
	}
	both := m.countVisible()
	if both == 0 || both > sliced {
		t.Errorf("AND composition: %d sliced, %d with the filter", sliced, both)
	}

	// Selecting the active value again un-slices (radio semantics).
	m.selectSlice(sliceLabel, "bbq")
	if m.sliceVal != "" || m.effectiveQuery() != "lane:backlog" {
		t.Errorf("re-select must clear: val=%q eff=%q", m.sliceVal, m.effectiveQuery())
	}
}

func TestSliceAxisSwitchClearsTheSelection(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "s")
	m.selectSlice(sliceLabel, "bbq")
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
	if len(rows) != 5 || rows[0].value != "e-fw2m" {
		t.Fatalf("epic rows = %+v", rows)
	}
	if !strings.Contains(rows[0].text(), "6/18") {
		t.Errorf("the epic row must carry the store's progress: %q", rows[0].text())
	}
	// The dep readout: →N is furrow's derived open_deps, verbatim.
	// e-fw2m waits on the open e-p3dx; e-c4mt declares THREE deps but furrow
	// resolved two away — e-2b7h is closed and e-x0k9 resolves to nothing —
	// so both rows read →1, and the dep-less e-p3dx row carries no arrow.
	for i, want := range map[int]string{0: "6/18 →1", 3: "0/1 →1"} {
		if !strings.Contains(rows[i].text(), want) {
			t.Errorf("epic row %d = %q, want it to contain %q", i, rows[i].text(), want)
		}
	}
	// On the SUFFIX, not the whole row: this box's title is 常備菜ライン 2026
	// 夏→秋, and now that the row wraps, its own arrow is on screen.
	if strings.Contains(rows[1].suffix, "→") {
		t.Errorf("e-p3dx has no deps; its row must carry no arrow: %q", rows[1].suffix)
	}
	// The stuck epic keeps its marker alongside the new suffix grammar.
	if !strings.Contains(rows[2].text(), "0/2 !") {
		t.Errorf("the stuck epic row must keep its marker: %q", rows[2].text())
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
	m.selectSlice(sliceLabel, "bbq")
	sliced := m.countVisible()

	press(m, "esc") // leave the panel focused state; panel stays
	if m.mode != modeNormal || !m.sliceOpen {
		t.Fatal("esc must return the keyboard to the board and keep the panel")
	}
	press(m, "s", "s") // refocus, then close
	if m.sliceOpen {
		t.Fatal("s from the panel must close it")
	}
	if m.countVisible() != sliced || m.sliceVal != "bbq" {
		t.Error("closing the panel must not clear the slice — GH's No-slicing is explicit")
	}
	out := frame(m)
	if !strings.Contains(out, "label:bbq") {
		t.Error("a slice filtering a panel-less board must be visible in the filter bar")
	}
}

func TestSlicePanelRendersInTheFrame(t *testing.T) {
	m := boardModel(t, 240, 50)
	if err := m.demoState("slice"); err != nil {
		t.Fatal(err)
	}
	out := frame(m)
	for _, want := range []string{"Slice by", "repo", "label", "epic", "● bbq"} {
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
	h := sliceRowTop + footerH + 3 // value-region capacity 3 → window 1 + indicators
	m := boardModel(t, 240, h)
	press(m, "s")
	m.Update(keyMsg("tab")) // repo → label (labels: ci, cli, testing, ui)
	rows := m.sliceRows()
	if len(rows) < 4 {
		t.Fatalf("fixture drifted: want ≥4 label rows, got %d", len(rows))
	}
	g := m.sliceViewport(rows)
	if !g.indicators || g.window != 1 {
		t.Fatalf("viewport = window %d indicators %v, want the 1-row indicator shape", g.window, g.indicators)
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

	// The indicator/chrome lines are inert — asserted on STATE, not on the
	// returned Cmd: on the fixture a real slice also returns nil, which made
	// the Cmd-shaped assertion vacuous.
	for _, y := range []int{sliceRowTop, sliceRowTop + 2, m.h - footerH} {
		before := m.sliceVal
		if c := m.sliceClick(3, y); c != nil {
			m.Update(c())
		}
		if m.sliceVal != before {
			t.Errorf("click at y=%d changed the slice to %q — that line is not a value row", y, m.sliceVal)
		}
	}
	// The one value row selects exactly what it shows (rows[3], scrolled to).
	if c := m.sliceClick(3, sliceRowTop+1); c != nil {
		m.Update(c())
	}
	if m.sliceVal != rows[3].value {
		t.Errorf("clicked the visible row, got slice %q want %q", m.sliceVal, rows[3].value)
	}
}

// Every height from cramped to comfortable: a click maps to a value row ONLY
// inside the rendered value region. At h≤11 the status and help lines used
// to map to rows — the region math was one off and had no floor for the
// indicator shape.
//
// Swept on EVERY axis: pressing tab once lands on the label axis, so the epic
// axis — the only one with its own row arithmetic — went uncovered.
func TestSliceClickNeverMapsOutsideTheRenderedRegion(t *testing.T) {
	for _, axis := range []sliceField{sliceRepo, sliceLabel, sliceEpic} {
		for h := 8; h <= 30; h++ {
			m := boardModel(t, 240, h)
			press(m, "s")
			m.sliceField = axis
			rows := m.sliceRows()
			for y := 0; y < h; y++ {
				if i := m.sliceRowAt(y, rows); i >= 0 {
					if y < sliceRowTop || y >= m.h-footerH {
						t.Errorf("%s axis, h=%d: y=%d maps to row %d but is outside the panel's value region", axis, h, y, i)
					}
				}
			}
		}
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
	if c := m.selectSlice(sliceLabel, "bbq"); c != nil {
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
	if c := m.selectSlice(sliceLabel, "bbq"); c != nil {
		m.Update(c())
	}
	press(m, "a")
	if m.add == nil || m.add.opts.Label != "bbq" {
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
	m.sliceField, m.sliceVal = sliceLabel, "bbq"
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
	// The CANCEL must happen at toggle time, not lean on the release being
	// swallowed elsewhere — an assertion on the release alone cannot tell
	// the two apart.
	if !m.drag.cancelled {
		t.Error("toggling the panel must cancel the drag itself")
	}
	m.Update(tea.MouseReleaseMsg{X: card.X + 12, Y: card.Y + 6, Button: tea.MouseLeft})
	if got := ids(m.b.LaneTasks("backlog")); !slices.Equal(got, before) {
		t.Errorf("backlog = %v, want %v — the cut gesture must not move anything", got, before)
	}
	if m.inflight || len(m.pending) > 0 {
		t.Error("the cut gesture must not persist anything")
	}
}

// A panel click while a modal owns the keyboard must not switch the mode out
// from under it (observed: a click during modeAdd stranded a half-typed title
// behind a broken "add non-nil exactly while modeAdd" invariant).
func TestSliceClickIsRefusedUnderModals(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "s", "esc") // panel open, board focused
	press(m, "a")
	m.Update(tea.MouseClickMsg{X: 3, Y: sliceRowTop, Button: tea.MouseLeft})
	if m.mode != modeAdd || m.add == nil {
		t.Fatalf("a panel click must not steal the keyboard from the add modal (mode=%d)", m.mode)
	}
	if m.sliceVal != "" {
		t.Error("nor may it slice the board")
	}
}

// The panel's clicks work in the table view too — the render and the
// keyboard once went live there while every click stayed dead.
func TestSliceClickWorksInTheTableView(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "v", "s")
	m.Update(keyMsg("tab"))
	rows := m.sliceRows()
	m.Update(tea.MouseClickMsg{X: 3, Y: sliceRowTop, Button: tea.MouseLeft})
	if m.sliceVal != rows[0].value {
		t.Errorf("slice = %q, want %q — table-view panel clicks were dead", m.sliceVal, rows[0].value)
	}
}

// The wheel scrolls the panel window (cursor stays put, like a board column).
func TestPanelWheelScrollsTheWindow(t *testing.T) {
	h := sliceRowTop + footerH + 3
	m := boardModel(t, 240, h)
	press(m, "s")
	m.Update(keyMsg("tab"))
	m.Update(tea.MouseWheelMsg{X: 3, Y: sliceRowTop + 1, Button: tea.MouseWheelDown})
	if m.sliceOff != 1 {
		t.Errorf("sliceOff = %d, want 1 after wheel-down over the panel", m.sliceOff)
	}
	if m.sliceIdx != 0 {
		t.Errorf("the wheel must not drag the cursor (sliceIdx = %d)", m.sliceIdx)
	}
}

// The board columns keep scrolling while the panel holds the keyboard — the
// panel sits beside them, not over them.
func TestBoardWheelWorksWhileThePanelHoldsTheKeyboard(t *testing.T) {
	m := advTallModel(t, 140, 12)
	press(m, "s")
	const lane = "backlog"
	c := m.lay.Col(lane)
	if c == nil || c.Hidden == 0 {
		t.Fatalf("setup: backlog no longer folds at 140x12 with the panel open (%+v)", c)
	}
	before := c.Scroll
	m.Update(tea.MouseWheelMsg{X: c.X + 2, Y: boardTop + 2, Button: tea.MouseWheelDown})
	if m.scroll[lane] != before+1 {
		t.Errorf("scroll = %d, want %d — modeSlice must not freeze the board wheel", m.scroll[lane], before+1)
	}
}

// The inset must come out of the row budget, not push the rightmost columns
// off the frame edge (observed: repo/labels/deps were clipped by 27 cells).
func TestTableRowsSurviveThePanelInset(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "v", "s")
	if out := frame(m); !strings.Contains(out, "deps") {
		t.Error("the table header's rightmost column must survive the panel inset")
	}
}

// Commas OR-split in furrow's -q exactly as spaces term-split; both spellings
// must be quoted away (verified against the real binary — `label:a,b` answers
// a BROADER query with exit 0).
func TestSliceTermQuotesCommas(t *testing.T) {
	m, _ := scriptedModel(t)
	if c := m.selectSlice(sliceLabel, "tui,cli"); c != nil {
		m.Update(c())
	}
	if got := m.sliceTerm(); got != `label:"tui,cli"` {
		t.Errorf("sliceTerm = %q, want the quoted form", got)
	}
}

// The epic row's grammar, after the lifecycle moved out of the suffix: the
// mark LEADS at a fixed column and is drawn before the title is ever cut,
// `◆` stays additive so a closed-and-pinned box can still say both, and the
// title is the only segment that yields.
func TestSliceEpicRowsLeadWithTheLifecycleMark(t *testing.T) {
	shut := time.Date(2026, 7, 15, 9, 12, 7, 0, time.UTC)
	boxes := []board.EpicInfo{
		{ID: "e-act", Title: "動いている箱", Active: true, Done: 1, Total: 3},
		{ID: "e-pin", Title: "留めてある箱", Pinned: true, Done: 0, Total: 2},
		{ID: "e-both", Title: "閉じたが留めてある箱", Pinned: true, Closed: shut},
		{ID: "e-long", Title: "ridge: TUI v2 — furrow parity・俯瞰・時間軸・保存ビュー", Done: 15, Total: 18, Stuck: true},
	}
	m := boardModel(t, 240, 50)
	m.b = board.NewStoreBoard([]board.Lane{{Name: "backlog"}}, nil, boxes, true, "")
	m.recompute()
	m.sliceField, m.sliceEpicAll = sliceEpic, true

	rows := m.sliceRows()
	if len(rows) != len(boxes) {
		t.Fatalf("got %d epic rows, want %d", len(rows), len(boxes))
	}
	for i, want := range []string{glyphEpicActive + " ", "  ", glyphDone + " ", "  "} {
		if rows[i].mark != want {
			t.Errorf("%s leads with %q, want %q", boxes[i].ID, rows[i].mark, want)
		}
		if !strings.HasPrefix(rows[i].lines[0], want) {
			t.Errorf("%s renders %q, want it to lead with %q", boxes[i].ID, rows[i].lines[0], want)
		}
	}
	// furrow keeps `pinned` when it closes a box, so the row must say both.
	if !rows[2].closed || !strings.Contains(rows[2].suffix, glyphEpicPinned) {
		t.Errorf("a closed pinned box lost one of its two marks: %+v", rows[2])
	}
	// The suffix is measured first and never truncated; the title yields.
	if !strings.HasSuffix(rows[3].text(), "15/18 "+glyphWIPOver) {
		t.Errorf("the long row lost its numbers: %q", rows[3].text())
	}
	// The title is the segment that yields — onto a second line first, and
	// only its tail carries the ellipsis.
	if rows[3].tail == "" || !strings.HasSuffix(rows[3].tail, "…") {
		t.Errorf("the long title must wrap and then yield: head %q tail %q", rows[3].title, rows[3].tail)
	}
	if len(rows[3].lines) != 2 {
		t.Errorf("a title that does not fit takes two lines, got %d: %q", len(rows[3].lines), rows[3].lines)
	}
	// …and a title that fits still costs one line. The wrap is per row, not a
	// uniform two-line grid: on the real board only 26 of 133 open boxes wrap.
	if len(rows[0].lines) != 1 || rows[0].tail != "" {
		t.Errorf("a title that fits must not wrap: %q", rows[0].lines)
	}
	// The mark column is paid for by the panel's width, not out of the title:
	// this row's suffix is 8 cells, so a title that does not fit still gets
	// more than the 14 cells the 26-cell panel left it.
	if w := lg.Width(rows[3].title); w < 18 {
		t.Errorf("title budget is %d cells, want the marker column not to eat it", w)
	}
}

// The scope line: the population, and on the epic axis the key that reaches
// the boxes the default population hides. The note cannot carry this — the
// next `sliced to …` overwrites it.
func TestSliceScopeLineNamesTheHiddenClosedBoxes(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "s")
	if got := m.sliceScope(len(m.sliceRows())); !strings.HasSuffix(got, "repos") {
		t.Errorf("repo axis scope = %q, want it to count the repos", got)
	}
	m.sliceField = sliceEpic
	narrow := m.sliceScope(len(m.sliceRows()))
	if !strings.Contains(narrow, "+2 closed") || !strings.Contains(narrow, "z") {
		t.Errorf("narrow scope = %q, want the hidden count and the key that shows them", narrow)
	}
	press(m, "z")
	wide := m.sliceScope(len(m.sliceRows()))
	if !strings.Contains(wide, "2 closed") || strings.Contains(wide, "+") {
		t.Errorf("widened scope = %q, want it to stop advertising a widening", wide)
	}
	if !strings.Contains(frame(m), wide) {
		t.Error("the scope line must be in the frame, not only in the model")
	}
}

// text is what the row SAYS, independent of how many lines it takes.
func (r sliceRow) text() string { return strings.Join(r.lines, " ") }

// styleAt returns the escape sequence that opens the style in force at byte
// index i of a rendered line — the last one before it. The house pattern from
// boxboard_test: compare OPENING sequences, not whole rendered strings.
func styleAt(line string, i int) string {
	j := strings.LastIndex(line[:i], "\x1b[")
	if j < 0 {
		return ""
	}
	if k := strings.Index(line[j:], "m"); k >= 0 {
		return line[j : j+k+1]
	}
	return line[j:]
}

// rawPanelLine returns the rendered (still styled) frame line carrying sub.
func rawPanelLine(t *testing.T, m *Model, sub string) string {
	t.Helper()
	for _, l := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(ansiStrip(l), sub) {
			return l
		}
	}
	t.Fatalf("no rendered line carries %q", sub)
	return ""
}

// The row's STYLES, asserted through the rendered frame. Deleting the closed
// dim or the active/stuck colours left every other test in this package green.
func TestSlicePanelStylesTheLifecycleAndTheClosedRow(t *testing.T) {
	shut := time.Date(2026, 7, 15, 9, 12, 7, 0, time.UTC)
	boxes := []board.EpicInfo{
		{ID: "e-act", Title: "動いている箱", Active: true, Done: 1, Total: 3},
		{ID: "e-stuck", Title: "詰まった箱", Done: 0, Total: 4, Stuck: true},
		{ID: "e-shut", Title: "閉じた箱", Closed: shut, Done: 2, Total: 2},
		{ID: "e-both", Title: "閉じたのに詰まった箱", Closed: shut, Done: 2, Total: 5, Stuck: true},
	}
	m := boardModel(t, 240, 50)
	m.b = board.NewStoreBoard([]board.Lane{{Name: "backlog"}}, nil, boxes, true, "")
	m.recompute()
	m.relayout()
	m.toggleSlice()
	m.sliceField, m.sliceEpicAll = sliceEpic, true
	m.sliceIdx = 0 // the cursor is on the ACTIVE row, clear of the closed ones

	th := m.th
	open := func(s lg.Style) string { return styleAt(s.Render("x"), strings.Index(s.Render("x"), "x")) }

	// The active mark is the one green thing on the row.
	l := rawPanelLine(t, m, "動いている箱")
	if got, want := styleAt(l, strings.Index(l, glyphEpicActive)), open(th.ok); got != want {
		t.Errorf("the active mark is styled %q, want th.ok %q", got, want)
	}
	// An OPEN stuck box keeps its warn-coloured marker.
	l = rawPanelLine(t, m, "詰まった箱 ")
	if got, want := styleAt(l, strings.LastIndex(l, glyphWIPOver)), open(th.warn); got != want {
		t.Errorf("an open stuck row's %q is styled %q, want th.warn %q", glyphWIPOver, got, want)
	}
	// A closed box recedes: the title dims even though nothing else on the row
	// changed, and the mark leads it.
	l = rawPanelLine(t, m, "閉じた箱 ")
	if got, want := styleAt(l, strings.Index(l, "閉じた箱")), open(th.dim); got != want {
		t.Errorf("a closed row's title is styled %q, want th.dim %q", got, want)
	}
	// …and a closed box furrow still calls stuck recedes WHOLE: the warn cell
	// would otherwise be the loudest thing in a dim row.
	l = rawPanelLine(t, m, "閉じたのに")
	if got := styleAt(l, strings.LastIndex(l, glyphWIPOver)); got == open(th.warn) {
		t.Errorf("a CLOSED stuck row still shouts: %q is styled th.warn", glyphWIPOver)
	}
	// The repo chip is the CARD's chip, not a colour of its own, and on a
	// closed row it recedes with everything else. Both were mutable to a bare
	// string with the whole package still green.
	m2 := boardModel(t, 240, 50)
	m2.toggleSlice()
	m2.sliceField, m2.sliceEpicAll = sliceEpic, true
	m2.sliceIdx = 0 // clear of both repo rows
	l = rawPanelLine(t, m2, "parking-lot joubisai")
	if got, want := styleAt(l, strings.Index(l, "joubisai")), open(th.chipAlt); got != want {
		t.Errorf("the repo is styled %q, want the card's repo chip %q", got, want)
	}
	l = rawPanelLine(t, m2, "parking-lot kyushu-tr…")
	if got, want := styleAt(l, strings.Index(l, "kyushu-tr…")), open(th.dim); got != want {
		t.Errorf("a closed row's repo is styled %q, want th.dim %q", got, want)
	}

	// The cursor outranks the dim — the row you are standing on is never the
	// recessed one. (The demo frame sliceepicclosed is its headless form.)
	m.sliceIdx = len(m.sliceRows()) - 1
	l = rawPanelLine(t, m, "閉じたのに")
	if got := styleAt(l, strings.Index(l, "閉じたのに")); got == open(th.dim) {
		t.Errorf("the cursor row is dimmed like any other closed box: %q", got)
	}
}

// The panel's rendered lines are exactly slicePanelW cells wide, with the rule
// in the next cell — measured on the FRAME, on every axis, at the widths and
// heights the app negotiates. The row-level invariant was asserted on
// sliceRow.display, a single string the epic renderer no longer draws.
func TestSlicePanelFrameLinesAreExactlyTheirWidth(t *testing.T) {
	boxes := []board.EpicInfo{
		{ID: "e-1", Title: "短い", Done: 1, Total: 2},
		{ID: "e-2", Title: "日本語のとても長いエピックのタイトルです", Done: 6, Total: 18, Stuck: true},
		// A title too wide to sit beside its suffix but not wider than the
		// line: the band where the row used to compose past its own width.
		{ID: "e-band", Title: "chord: action-keys 完成", Done: 0, Total: 1},
		// The suffix at its worst: every optional piece, and counts past any
		// real board. This is the row that reaches sliceRows' title floor.
		{ID: "e-3", Title: "ridge: TUI v2 — furrow parity", Pinned: true,
			Done: 999999, Total: 999999, Stuck: true, Deps: []string{"e-1", "e-2"}, OpenDeps: []string{"e-1", "e-2"}},
	}
	for _, w := range []int{240, 400} {
		for _, h := range []int{12, 24, 50} {
			for _, axis := range []sliceField{sliceRepo, sliceLabel, sliceEpic} {
				m := boardModel(t, w, h)
				m.b = board.NewStoreBoard([]board.Lane{{Name: "backlog"}}, nil, boxes, true, "")
				m.recompute()
				m.relayout()
				m.toggleSlice()
				m.sliceField, m.sliceEpicAll = axis, true
				lines := strings.Split(frame(m), "\n")
				for y := boardTop; y < m.h-footerH && y < len(lines); y++ {
					head := ansi.Truncate(lines[y], slicePanelW, "")
					if lg.Width(head) != slicePanelW {
						t.Fatalf("%s axis %dx%d y=%d: the panel's first %d cells do not land on a boundary: %q",
							axis, w, h, y, slicePanelW, lines[y])
					}
					if rest := lines[y][len(head):]; !strings.HasPrefix(rest, "│") {
						t.Errorf("%s axis %dx%d y=%d: cell %d is %q, want the panel's rule",
							axis, w, h, y, slicePanelW, ansi.Truncate(rest, 1, ""))
					}
				}
				// What the row says it is, is what the frame draws.
				rows := m.sliceRows()
				g := m.sliceViewport(rows)
				body := frame(m)
				for i := g.off; i < g.off+g.window && i < len(rows); i++ {
					if !sliceRowDrawn(body, rows[i]) {
						t.Errorf("%s axis %dx%d: row %d renders as something other than %q",
							axis, w, h, i, rows[i].lines)
					}
				}
			}
		}
	}
}

// sliceRowDrawn reports whether every line the row says it has is in the
// frame. The tie between what sliceRows composes and what the panel paints.
func sliceRowDrawn(frame string, r sliceRow) bool {
	for _, l := range r.lines {
		if !strings.Contains(frame, l) {
			return false
		}
	}
	return true
}

// The wrap's own arithmetic: line 1 is the title's alone — the suffix moved to
// the last line, so furrow's numbers can no longer squeeze a title to nothing —
// and the split loses no cells.
func TestSliceEpicRowWrapsWithoutLosingCells(t *testing.T) {
	title := "ridge: TUI v2 — furrow parity・俯瞰・時間軸・保存ビュー"
	boxes := []board.EpicInfo{
		{ID: "e-plain", Title: title, Done: 1, Total: 2},
		// Every optional piece at once: the suffix at its widest.
		{ID: "e-loud", Title: title, Pinned: true, Done: 999, Total: 999, Stuck: true,
			Deps: []string{"e-plain"}, OpenDeps: []string{"e-plain"}},
	}
	m := boardModel(t, 240, 50)
	m.b = board.NewStoreBoard([]board.Lane{{Name: "backlog"}}, nil, boxes, true, "")
	m.recompute()
	m.sliceField = sliceEpic

	rows := m.sliceRows()
	head := rows[0].title
	if rows[1].title != head {
		t.Errorf("the suffix shrank line 1: %q with a small suffix, %q with a large one",
			head, rows[1].title)
	}
	if w := lg.Width(head); w != slicePanelW-4-sliceMarkW {
		t.Errorf("line 1's title is %d cells, want the whole %d the line has",
			w, slicePanelW-4-sliceMarkW)
	}
	for _, r := range rows {
		// The split is a prefix and its remainder: no cell is spent on a word
		// boundary, and no cell is dropped between the lines.
		rest := strings.TrimSuffix(r.tail, "…")
		if !strings.HasPrefix(title, r.title+rest) {
			t.Errorf("%s: %q + %q is not how %q starts", r.value, r.title, rest, title)
		}
		if !strings.HasSuffix(r.lines[len(r.lines)-1], r.suffix) {
			t.Errorf("%s: the numbers must ride the LAST line: %q", r.value, r.lines)
		}
	}
}

// The hit test over mixed row heights: every line a wrapped row draws belongs
// to that row, including its continuation, and no line past the region does.
func TestSliceClickHitsTheRowUnderEachOfItsLines(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "s")
	m.sliceField = sliceEpic
	rows := m.sliceRows()
	if len(rows) < 2 || len(rows[0].lines) != 2 {
		t.Fatalf("fixture drifted: want a wrapped first epic row, got %d rows %q",
			len(rows), rows[0].lines)
	}
	// y walked down the region, against the heights the frame drew.
	y := sliceRowTop
	for i, r := range rows {
		for li := range r.lines {
			if got := m.sliceRowAt(y, rows); got != i {
				t.Errorf("y=%d is line %d of row %d, but the click path says row %d", y, li, i, got)
			}
			y++
		}
	}
	if got := m.sliceRowAt(y, rows); got != -1 {
		t.Errorf("y=%d is past the last row, but maps to %d", y, got)
	}
	// And a real click on a CONTINUATION line selects that row, not its
	// neighbour — the failure a line-blind y→row map would produce.
	if c := m.sliceClick(3, sliceRowTop+1); c != nil {
		m.Update(c())
	}
	if m.sliceVal != rows[0].value {
		t.Errorf("a click on row 0's second line sliced to %q, want %q", m.sliceVal, rows[0].value)
	}
}

// A wrapped row is scrolled in WHOLE, and on a region too short to hold two
// lines the rows do not wrap at all. Asserted on the FRAME after every step of
// the cursor, not on the geometry: what the window claims and what the panel
// painted are the two things that must not drift apart.
func TestSliceCursorScrollsAWrappedRowFullyIntoView(t *testing.T) {
	if rows := boardModel(t, 240, 50).sliceRows(); len(rows) == 0 {
		t.Fatal("no rows to scroll")
	}
	for h := 12; h <= 30; h++ {
		m := boardModel(t, 240, h)
		press(m, "s")
		m.sliceField = sliceEpic
		rows := m.sliceRows()
		roomy := h-footerH-sliceRowTop >= sliceWrapCap
		if got := len(rows[0].lines) == 2; got != roomy {
			t.Fatalf("h=%d: row wrapped=%v, want %v for a region of %d lines: %q",
				h, got, roomy, h-footerH-sliceRowTop, rows[0].lines)
		}
		// Down the whole list and back up: every stop must show the cursor
		// row's LAST line, or the row cannot be read where it is selected.
		for _, key := range []string{"down", "down", "down", "G", "up", "g"} {
			press(m, key)
			out := frame(m)
			for li, l := range rows[m.sliceIdx].lines {
				if !strings.Contains(out, l) {
					t.Fatalf("h=%d after %q: cursor row %d line %d (%q) is not in the frame",
						h, key, m.sliceIdx, li, l)
				}
			}
		}
	}
}

// The same invariant over MIXED heights, which the fixture cannot produce (all
// five of its box titles wrap). With heights mixed, "how far down must the
// window start" stops being a subtraction: a window measured from the old
// offset over-counts the rows that fit from the new one, and the cursor row
// lands one row past the end of the window.
func TestSliceCursorScrollsOverMixedRowHeights(t *testing.T) {
	long := "ridge: TUI v2 — furrow parity・俯瞰・時間軸・保存ビュー"
	var boxes []board.EpicInfo
	for i, title := range []string{"短い", "短い箱", "みじかい", long, long, "短", long} {
		boxes = append(boxes, board.EpicInfo{
			ID: "e-" + string(rune('a'+i)), Title: title, Done: i, Total: 9,
		})
	}
	for h := 13; h <= 24; h++ {
		m := boardModel(t, 240, h)
		press(m, "s")
		m.sliceField = sliceEpic
		m.b = board.NewStoreBoard([]board.Lane{{Name: "backlog"}}, nil, boxes, true, "")
		m.recompute()
		got := m.sliceRows()
		if len(got[0].lines) != 1 || len(got[3].lines) != 2 {
			t.Fatalf("h=%d: want mixed heights, got %d and %d lines",
				h, len(got[0].lines), len(got[3].lines))
		}
		for i := range got {
			m.sliceIdx = i
			m.ensureSliceVisible()
			out := frame(m)
			for li, l := range got[i].lines {
				if !strings.Contains(out, l) {
					t.Fatalf("h=%d: cursor on row %d, line %d (%q) is not in the frame",
						h, i, li, l)
				}
			}
		}
	}
}

// The bottom row names the cursor row in full while the panel holds the
// keyboard — the reading surface the truncated row is scanned against.
func TestSliceReadoutNamesTheCursorRowInFull(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "s")
	m.sliceField = sliceEpic
	m.sliceIdx = 0
	full := m.b.Epics()[0].Title
	row := ansiStrip(m.sliceRowBody(m.sliceRows()[0], 0, m.th.base, false))
	if strings.Contains(row, full) {
		t.Fatalf("fixture drifted: the row already shows the whole title %q", full)
	}
	line := ansiStrip(m.statusLine())
	if !strings.Contains(line, full) {
		t.Errorf("the readout does not carry the cursor row's whole title: %q", line)
	}
	if !strings.Contains(line, "6/18 done") || !strings.Contains(line, "repos tomo/kyushu-trip") {
		t.Errorf("the readout lost furrow's own words for the box: %q", line)
	}
	// The note keeps the right end, and a refusal outranks the readout for it.
	if !strings.Contains(line, "esc leaves") {
		t.Errorf("the panel's note must keep the row's right end: %q", line)
	}
	m.fail("nope")
	if line := ansiStrip(m.statusLine()); !strings.Contains(line, "⚠ nope") {
		t.Errorf("a refusal must survive the readout: %q", line)
	}
	// With the keyboard back on the board, the row is the note's again.
	press(m, "esc")
	if line := ansiStrip(m.statusLine()); strings.Contains(line, full) {
		t.Errorf("the readout outlived the panel's focus: %q", line)
	}
}

// The three rules a wrapped row is drawn by, asserted on the frame: the cursor
// bar spans BOTH lines (it is one row, and a bar on the head alone reads as a
// one-line row above a stray), the selection dot marks the value ONCE, and the
// continuation hangs clear of the lifecycle column. All three survived every
// other test in this package when mutated away.
func TestSliceWrappedRowIsDrawnAsOneRow(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "s")
	m.sliceField = sliceEpic
	rows := m.sliceRows()
	if len(rows[0].lines) != 2 {
		t.Fatalf("fixture drifted: want a wrapped first row, got %q", rows[0].lines)
	}
	if c := m.selectSlice(sliceEpic, rows[0].value); c != nil {
		m.Update(c())
	}
	lines := strings.Split(frame(m), "\n")
	head, cont := lines[sliceRowTop], lines[sliceRowTop+1]
	if !strings.HasPrefix(head, "▌ ● ") {
		t.Errorf("the head line is %q, want the cursor bar and the selection dot", head[:12])
	}
	if !strings.HasPrefix(cont, "▌ ") {
		t.Errorf("the continuation is %q, want the cursor bar to span the whole row", cont[:12])
	}
	if strings.Contains(cont[:8], "●") {
		t.Errorf("the continuation carries a second selection dot: %q", cont[:12])
	}
	// The hanging indent: the continuation starts where the TITLE starts, not
	// where the lifecycle mark does. In CELLS — the lines are CJK, so a byte
	// offset says nothing about a column.
	cell := func(line, sub string) int {
		i := strings.Index(line, sub)
		if i < 0 {
			t.Fatalf("%q is not in %q", sub, line)
		}
		return lg.Width(line[:i])
	}
	title := cell(head, strings.TrimSpace(rows[0].title))
	if got := cell(cont, strings.TrimSpace(rows[0].tail)); got != title {
		t.Errorf("the continuation starts at cell %d, the title at %d — it must hang under the title", got, title)
	}
}

// The repo on the row: the reserved boxes carry the same title in every repo,
// so without it three quarters of the real board's epic axis is rows that
// render identically. It takes only cells nothing else wanted.
func TestSliceEpicRowCarriesTheRepoWhereTheTitleDoesNot(t *testing.T) {
	long := "ridge: TUI v2 — furrow parity・俯瞰・時間軸・保存ビュー"
	boxes := []board.EpicInfo{
		{ID: "e-a", Title: "parking-lot", Repos: []string{"tomo/joubisai"}, Done: 1, Total: 4},
		{ID: "e-b", Title: "parking-lot", Repos: []string{"tomo/kyushu-trip"}, Done: 1, Total: 4},
		// The title already opens with the repo: saying it twice is noise.
		{ID: "e-c", Title: "ridge: TUI v2", Repos: []string{"akira-toriyama/ridge"}, Done: 2, Total: 3},
		// No room: the title fills the line, so the repo yields, not the title.
		{ID: "e-d", Title: long, Repos: []string{"tomo/joubisai"}, Done: 2, Total: 3},
		// Several repos: name one and count the rest, as a card does.
		{ID: "e-e", Title: "mandate", Repos: []string{"tomo/joubisai", "tomo/kyushu-trip"}, Done: 0, Total: 1},
		// Several repos AND a title that opens with the first: only the count
		// of the others is left worth showing.
		{ID: "e-f", Title: "joubisai", Repos: []string{"tomo/joubisai", "tomo/kyushu-trip"}, Done: 0, Total: 1},
		// Wraps, but its continuation has cells to spare — the row the
		// wrapped-row rule is actually about.
		{ID: "e-w", Title: "夏休み自由研究 — 火起こしと星の観察", Repos: []string{"tomo/kyushu-trip"}, Done: 0, Total: 2},
	}
	m := boardModel(t, 240, 50)
	m.b = board.NewStoreBoard([]board.Lane{{Name: "backlog"}}, nil, boxes, true, "")
	m.recompute()
	m.sliceField = sliceEpic
	rows := m.sliceRows()

	// The whole point: two boxes with the SAME title no longer render the same.
	if rows[0].text() == rows[1].text() {
		t.Errorf("two reserved boxes still render identically: %q", rows[0].text())
	}
	// e-b's row has 10 free cells and "kyushu-trip" is 11, so it is shortened
	// rather than dropped — the row still separates from e-a, and the readout
	// under the cursor spells it out.
	//
	// e-f's title opens with its first repo, so only the `+1` remainder is
	// left worth showing: matching the title against ShortRepo's own `name+N`
	// form let every multi-repo box through the same-word-twice rule.
	for i, want := range []string{"joubisai", "kyushu-tr…", "", "", "joubisai+1", "+1", ""} {
		if rows[i].repo != want {
			t.Errorf("%s carries repo %q, want %q", boxes[i].ID, rows[i].repo, want)
		}
	}
	// A row whose title WRAPPED carries none — including e-w, whose
	// continuation has room to spare. The long title already identifies the
	// box, and the continuation is where an elided title lands.
	for _, i := range []int{3, 6} {
		if len(rows[i].lines) != 2 || rows[i].repo != "" {
			t.Errorf("%s wrapped and still carries a repo: %q in %q",
				boxes[i].ID, rows[i].repo, rows[i].lines)
		}
	}
	// And the floor holds FROM BELOW: a row with 7 cells to spare shows
	// nothing rather than a shortened stub. The title length here is a fixed
	// 14 cells — deriving it from sliceRepoMin would move the case with the
	// constant and pin nothing.
	tight := []board.EpicInfo{{ID: "e-g", Title: strings.Repeat("x", 14),
		Repos: []string{"tomo/joubisai"}, Done: 1, Total: 4}}
	m2 := boardModel(t, 240, 50)
	m2.b = board.NewStoreBoard([]board.Lane{{Name: "backlog"}}, nil, tight, true, "")
	m2.recompute()
	m2.sliceField = sliceEpic
	if got := m2.sliceRows()[0]; got.repo != "" {
		t.Errorf("a row with 7 cells to spare still shows %q — that is under sliceRepoMin (%d)",
			got.repo, sliceRepoMin)
	}

	// The repo never costs the title a cell: e-d's title is what it would be
	// with no repo at all.
	noRepo := boxes[3]
	noRepo.Repos = nil
	m.b = board.NewStoreBoard([]board.Lane{{Name: "backlog"}}, nil, []board.EpicInfo{noRepo}, true, "")
	m.recompute()
	if got := m.sliceRows()[0]; got.title != rows[3].title || got.tail != rows[3].tail {
		t.Errorf("the title yielded for the repo: %q/%q with a repo, %q/%q without",
			rows[3].title, rows[3].tail, got.title, got.tail)
	}
}
