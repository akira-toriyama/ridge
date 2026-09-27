package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// roadModel opens the roadmap over the fixture through the real key path,
// with the zone and the clock pinned inside the fixture's dated window
// (dues 2026-07-31 .. 2026-09-30) so "overdue" has one answer.
func roadModel(t *testing.T, w, h int) *Model {
	t.Helper()
	fixedZone(t, "TEST", 9)
	fixedNow(t, time.Date(2026, 8, 31, 12, 0, 0, 0, board.Zone()))
	m := boardModel(t, w, h)
	// The real program's order: the terminal reports its size, then keys
	// arrive, and a frame follows every Update. The window is only placed on
	// a sized frame, so a test that skips the WindowSizeMsg is testing a
	// program that does not exist (found by review).
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	press(m, "C")
	if m.view != viewRoadmap {
		t.Fatal("C did not open the roadmap")
	}
	_ = frame(m)
	return m
}

// The fixture's five dated tasks not done, in due order, each row carrying
// its board-calendar date — and no row at all for the dateless majority. The
// fifth is the parked t-w7ne: the timeline draws every due furrow would still
// show (a parked lane drops the alarm, not the date), which is also the due
// its box waits until.
func TestRoadmapRowsAreTheDatedTasksNotDoneInDueOrder(t *testing.T) {
	m := roadModel(t, 240, 40)
	l := m.road.lay
	want := []string{"t-jv3j", "t-ehk7", "t-p7xw", "t-9sa6", "t-w7ne"}
	if len(l.Rows) != len(want) {
		ids := make([]string, 0, len(l.Rows))
		for _, r := range l.Rows {
			ids = append(ids, r.ID)
		}
		t.Fatalf("rows = %q, want %q", ids, want)
	}
	for i, r := range l.Rows {
		if r.ID != want[i] {
			t.Errorf("row %d = %s, want %s", i, r.ID, want[i])
		}
	}
	out := frame(m)
	for _, date := range []string{"07-31", "08-20", "08-25", "09-30"} {
		if !strings.Contains(out, date) {
			t.Errorf("the frame does not date a row %q:\n%s", date, out)
		}
	}
}

// The ◆'s colour is the promise's state: past-and-open is danger, today is
// warn, the future is plain — and the styles must reach the frame, because
// this is the one view whose whole point is that colour.
func TestRoadmapDiamondColoursByOverdue(t *testing.T) {
	m := roadModel(t, 240, 40)
	l := m.road.lay
	tlW := m.roadTLW()

	row := m.roadRowLine(l, l.Row("t-jv3j"), tlW) // due 07-31, open → overdue
	if !strings.Contains(row, m.th.danger.Render(glyphDue)) {
		t.Errorf("t-jv3j's ◆ is not danger-styled: %q", row)
	}
	row = m.roadRowLine(l, l.Row("t-9sa6"), tlW) // due 09-30 → a plain future promise
	if strings.Contains(row, m.th.danger.Render(glyphDue)) || !strings.Contains(row, m.th.base.Render(glyphDue)) {
		t.Errorf("t-9sa6's ◆ must be plain, not danger: %q", row)
	}

	// Due exactly today: the one kind the reader must not scan past.
	b := board.NewBoard([]*board.Task{
		{ID: "t-now", Status: "ready", Title: "today", Due: board.Now().Add(2 * time.Hour)},
	})
	m2 := New(memstore.NewWith(b), Options{})
	m2.Update(tea.WindowSizeMsg{Width: 240, Height: 40})
	m2.openRoadmap()
	_ = frame(m2)
	l2 := m2.road.lay
	if row := m2.roadRowLine(l2, l2.Row("t-now"), m2.roadTLW()); !strings.Contains(row, m2.th.warn.Render(glyphDue)) {
		t.Errorf("a due-today ◆ is not warn-styled: %q", row)
	}
}

// The timeline half's geometry, on a row with no epic chip to muddy it: the
// ◆ sits at the due's cell, today's ┊ at today's — both windowed by road.xOff.
func TestRoadmapPlacesTheDiamondAndTodayOnTheirCells(t *testing.T) {
	m := roadModel(t, 240, 40)
	l := m.road.lay
	r := l.Row("t-p7xw") // due 08-25, no epic membership in the fixture
	tlW := m.roadTLW()

	cells := ansiStrip(m.roadCells(l, m.b.Task("t-p7xw"), r, tlW))
	if got := pos(cells, glyphDue); got != r.X-m.road.xOff {
		t.Errorf("◆ at column %d, want %d in %q", got, r.X-m.road.xOff, cells)
	}
	if got := pos(cells, glyphToday); got != l.TodayX-m.road.xOff {
		t.Errorf("┊ at column %d, want %d in %q", got, l.TodayX-m.road.xOff, cells)
	}
}

// pos is the display column of needle's first occurrence — the cells before
// it are single-width here, but counting bytes would still lie about ◆/┊.
func pos(s, needle string) int {
	i := strings.Index(s, needle)
	if i < 0 {
		return -1
	}
	return lg.Width(s[:i])
}

// The epic chip rides the cells right of the ◆, resolved to the box's title —
// the raw e- id in a frame is a leak two other views already assert against.
func TestRoadmapEpicChipRidesTheTimelineResolved(t *testing.T) {
	m := roadModel(t, 240, 40)
	out := frame(m)
	if !strings.Contains(out, glyphEpic+" 九州キャンプ旅") {
		t.Errorf("t-jv3j's epic chip is missing or unresolved:\n%s", out)
	}
	if strings.Contains(out, "e-fw2m") {
		t.Errorf("a raw epic id leaked into the frame:\n%s", out)
	}
}

// A ◆ panned out of the window leaves an edge arrow, not a blank — a dated
// row whose date is off screen must not read as dateless. Overdue keeps its
// danger through the substitution.
func TestRoadmapOffWindowDiamondBecomesAnEdgeArrow(t *testing.T) {
	m := roadModel(t, 240, 40)
	l := m.road.lay

	m.road.xOff = l.Row("t-jv3j").X + 5 // the overdue ◆ is now left of the window
	cells := m.roadCells(l, m.b.Task("t-jv3j"), l.Row("t-jv3j"), m.roadTLW())
	if !strings.Contains(cells, m.th.danger.Render(glyphDropR)) {
		t.Errorf("an off-left overdue ◆ must leave a danger ◂ at the edge: %q", cells)
	}

	m.road.xOff = 0
	cells = ansiStrip(m.roadCells(l, m.b.Task("t-9sa6"), l.Row("t-9sa6"), 10))
	if !strings.HasSuffix(cells, glyphDropL) {
		t.Errorf("an off-right ◆ must leave a ▸ at the window's last cell: %q", cells)
	}
}

// The filter mutes rows and counts them in the header; it never drops one —
// a deadline that vanishes because of a query is a lie about the board.
func TestRoadmapFilterMutesAndCountsRatherThanDrops(t *testing.T) {
	m := roadModel(t, 240, 40)
	rows := len(m.road.lay.Rows)
	m.ti.SetValue("label:gear")
	_ = m.applyFilter("label:gear")
	out := frame(m)

	if got := len(m.road.lay.Rows); got != rows {
		t.Fatalf("the filter dropped roadmap rows: %d, want %d", got, rows)
	}
	hidden := 0
	for _, r := range m.road.lay.Rows {
		if m.taskHidden(r.ID) {
			hidden++
		}
	}
	if hidden == 0 {
		t.Fatal("setup: label:gear was expected to hide at least one dated row")
	}
	if want := fmt.Sprintf("%d hidden by the filter", hidden); !strings.Contains(out, want) {
		t.Errorf("the header does not say %q:\n%s", want, out)
	}
}

// C seeds the roadmap with the board cursor when it is on the axis, esc
// carries a WALKED cursor back — and only a walked one: the fallback row the
// clamp picked on entry must not relocate the board cursor on a read-only
// round trip (the map's contract, verbatim).
func TestRoadmapCarriesTheCursorBothWaysButOnlyWhenWalked(t *testing.T) {
	m := roadModel(t, 240, 40)
	m.closeRoadmap()
	if !m.selectID("t-9sa6", false) {
		t.Fatal("setup: t-9sa6 is not selectable")
	}
	press(m, "C")
	if m.road.sel != "t-9sa6" {
		t.Fatalf("the roadmap opened on %q, want the board cursor's t-9sa6", m.road.sel)
	}
	press(m, "k")
	if m.road.sel != "t-p7xw" {
		t.Fatalf("k walked to %q, want t-p7xw (one due earlier)", m.road.sel)
	}
	press(m, "esc")
	if m.view != viewBoard {
		t.Fatal("esc did not return to the board")
	}
	if got := m.curTask(); got == nil || got.ID != "t-p7xw" {
		t.Errorf("the board cursor did not follow the walk to t-p7xw")
	}

	// A dueless seed falls to the first row, says why — and an esc with no
	// walk leaves the board cursor exactly where it was.
	if !m.selectID("t-7wdg", false) {
		t.Fatal("setup: t-7wdg is not on the fixture board")
	}
	press(m, "C")
	if m.road.sel != "t-jv3j" || m.road.moved {
		t.Fatalf("a dueless seed must land unwalked on the first row, got %q moved=%v", m.road.sel, m.road.moved)
	}
	if !strings.Contains(m.status, "carries no due") {
		t.Errorf("the fallback did not explain itself: %q", m.status)
	}
	press(m, "esc")
	if got := m.curTask(); got == nil || got.ID != "t-7wdg" {
		t.Error("an unwalked round trip relocated the board cursor")
	}
}

// A done task still carries its due, and opening the roadmap from it must
// say THAT — "no due" would be a flat falsehood (the map's fallback records
// what the wrong sentence costs).
func TestRoadmapSeedOnADoneTaskNamesTheRightReason(t *testing.T) {
	fixedZone(t, "TEST", 9)
	fixedNow(t, time.Date(2026, 8, 31, 12, 0, 0, 0, board.Zone()))
	b := board.NewBoard([]*board.Task{
		{ID: "t-kept", Status: "done", Title: "kept", Due: at(2026, 8, 20, 8), Closed: at(2026, 8, 19, 8)},
		{ID: "t-open", Status: "ready", Title: "open", Due: at(2026, 9, 20, 8)},
	})
	m := New(memstore.NewWith(b), Options{})
	m.w, m.h = 240, 40
	m.recompute()
	if !m.selectID("t-kept", false) {
		t.Fatal("setup: the done task is not selectable")
	}
	m.openRoadmap()
	if m.road.sel != "t-open" {
		t.Fatalf("the cursor fell to %q, want the one open promise", m.road.sel)
	}
	if !strings.Contains(m.status, "is done") {
		t.Errorf("the fallback blamed the wrong thing: %q", m.status)
	}
}

// z cycles the three axes and the header names each — the only place the
// zoom is stated, so it must never lag the keystroke.
func TestRoadmapZoomCyclesAndTheHeaderNamesIt(t *testing.T) {
	m := roadModel(t, 240, 40)
	for _, want := range []string{"week", "month", "day"} {
		press(m, "z")
		if m.road.zoom.String() != want {
			t.Fatalf("zoom = %s, want %s", m.road.zoom, want)
		}
		if !strings.Contains(frame(m), "zoom "+want+" (1 cell = 1 "+want+")") {
			t.Errorf("the header does not name zoom %s", want)
		}
	}
}

// h/l pan the window and clamp at the axis ends; the selection deliberately
// stays put (a window panned away from it is a legitimate state).
func TestRoadmapPanClampsAtTheAxisEnds(t *testing.T) {
	// Narrow enough that the fixture's ~9-week axis overflows the window.
	m := roadModel(t, 80, 40)
	l := m.road.lay
	tlW := m.roadTLW()
	if l.Cells <= tlW {
		t.Fatalf("setup: axis %d cells must overflow the %d-cell window", l.Cells, tlW)
	}
	sel := m.road.sel
	for range 100 {
		press(m, "l")
	}
	if got, want := m.road.xOff, l.Cells-tlW; got != want {
		t.Errorf("panning right stopped at %d, want the clamp %d", got, want)
	}
	for range 100 {
		press(m, "h")
	}
	if m.road.xOff != 0 {
		t.Errorf("panning left stopped at %d, want 0", m.road.xOff)
	}
	if m.road.sel != sel || m.road.moved {
		t.Error("panning moved the selection")
	}
}

// ^u/^d page by rows and say so at the ends — the advertised key must never
// be a silent dead one.
func TestRoadmapPageSaysSoAtTheEnds(t *testing.T) {
	m := roadModel(t, 240, 40)
	m.Update(ctrlD())
	if m.road.sel != "t-w7ne" {
		t.Fatalf("^d landed on %q, want the last row t-w7ne", m.road.sel)
	}
	m.Update(ctrlD())
	if !strings.Contains(m.status, "already at the bottom") {
		t.Errorf("a ^d that cannot move said nothing: %q", m.status)
	}
}

// The `?` overlay knows where you are, and the roadmap section is generated
// from the same bindings onRoadKey matches on.
func TestRoadmapHelpSectionSaysYouAreHere(t *testing.T) {
	m := roadModel(t, 240, 50)
	press(m, "?")
	out := frame(m)
	if !strings.Contains(out, "roadmap — you are here") {
		t.Errorf("the help overlay does not mark the roadmap section:\n%s", out)
	}
	if !strings.Contains(out, "zoom day/week/month") {
		t.Errorf("the roadmap section does not advertise z:\n%s", out)
	}
	press(m, "esc")
	if m.view != viewRoadmap || m.fullHelp {
		t.Error("esc must take the overlay off and stay on the roadmap")
	}
}

// An empty axis says so in words — and still draws the frame around them.
func TestRoadmapEmptyBoardSaysSo(t *testing.T) {
	fixedZone(t, "TEST", 9)
	fixedNow(t, time.Date(2026, 8, 31, 12, 0, 0, 0, board.Zone()))
	b := board.NewBoard([]*board.Task{
		{ID: "t-none", Status: "ready", Title: "dateless"},
	})
	m := New(memstore.NewWith(b), Options{})
	m.w, m.h = 240, 40
	m.openRoadmap()
	if !strings.Contains(frame(m), "no open task carries a due") {
		t.Error("an empty roadmap did not say why it is empty")
	}
}

// Today's gridline survives the epic chip: on an overdue boxed row the
// chip's natural span crosses today, and the chip yields — found by review:
// every headless frame the first cut shipped had a hole in the ┊ on exactly
// the rows the view exists to surface.
func TestRoadmapTodayGridlineSurvivesTheEpicChip(t *testing.T) {
	m := roadModel(t, 240, 40)
	l := m.road.lay
	r := l.Row("t-jv3j") // overdue AND boxed
	cells := ansiStrip(m.roadCells(l, m.b.Task("t-jv3j"), r, m.roadTLW()))
	if got, want := pos(cells, glyphToday), l.TodayX-m.road.xOff; got != want {
		t.Errorf("┊ at column %d, want %d — the chip covered the gridline: %q", got, want, cells)
	}
	if !strings.Contains(cells, glyphEpic) {
		t.Errorf("the chip vanished instead of yielding: %q", cells)
	}
}

// The opening window is placed only on a frame whose size is REAL: the
// interactive program draws one frame before the WindowSizeMsg arrives, and
// anchoring on it derives the window from the constructor's default width —
// today then sat off screen on any other terminal. When the anchor does
// land, today is a third of the way in, and a seeded selection a year of
// cells away keeps the selection but degrades to its edge arrow rather than
// dragging the window off the one anchor a timeline opens on. Found by
// review, in two rounds: the first cut let the seed win, the second anchored
// on the pre-size frame.
func TestRoadmapOpensWithTodayPlacedAgainstTheRealWidth(t *testing.T) {
	fixedZone(t, "TEST", 9)
	fixedNow(t, time.Date(2026, 8, 31, 12, 0, 0, 0, board.Zone()))
	b := board.NewBoard([]*board.Task{
		{ID: "t-old", Status: "ready", Title: "a year late", Due: at(2025, 9, 1, 12)},
		{ID: "t-soon", Status: "ready", Title: "soon", Due: at(2026, 9, 2, 12)},
	})
	m := New(memstore.NewWith(b), Options{Roadmap: true})

	// The pre-size frame the real program draws: it must not burn the anchor.
	_ = frame(m)
	if m.road.anchored {
		t.Fatal("the window anchored against the constructor's default size")
	}

	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	out := frame(m)
	l, tlW := m.road.lay, m.roadTLW()
	if l.Cells <= tlW {
		t.Fatalf("setup: axis %d cells must overflow the %d-cell window", l.Cells, tlW)
	}
	if m.road.sel != "t-old" {
		t.Fatalf("seed = %q, want the board cursor's t-old", m.road.sel)
	}
	if got, want := m.road.xOff, clamp(l.TodayX-tlW/3, 0, l.Cells-tlW); got != want {
		t.Errorf("road.xOff = %d, want today a third into the REAL %d-cell window (%d)", got, tlW, want)
	}
	if tx := l.TodayX - m.road.xOff; tx < 0 || tx >= tlW {
		t.Errorf("today sits at window column %d of %d — the window opened away from it", tx, tlW)
	}
	if !strings.Contains(out, glyphDropR) {
		t.Error("the off-window seed ◆ left no ◂ edge arrow")
	}
}

// The tab strip is spelled once and lists every view: the graph's and the
// map's hand-written strips never learned the box overview existed, which is
// the drift this helper exists to end.
func TestFullTabsListEveryViewEverywhere(t *testing.T) {
	m := roadModel(t, 240, 40)
	for _, open := range []struct {
		name string
		to   func()
	}{
		{"roadmap", func() {}},
		{"map", func() { m.openMap("") }},
		{"boxes", func() { m.openBoxes() }},
		{"graph", func() { m.openGraph() }},
	} {
		open.to()
		out := frame(m)
		for _, tab := range []string{"Board", "Table", "Graph", "Map", "Boxes", "Roadmap"} {
			if !strings.Contains(out, tab) {
				t.Errorf("%s view's tab strip is missing %s", open.name, tab)
			}
		}
	}
}

// ⏎ on a row opens the edit overlay straight onto the due input, seeded with
// the row's date; the apply lands in place and closes the overlay, with the
// row on its new date under a cursor that followed it, and the view still
// the roadmap (t-m5d5).
func TestRoadmapEnterEditsTheDueInPlace(t *testing.T) {
	m := roadModel(t, 240, 40)
	m.road.sel = "t-9sa6"
	press(m, "enter")
	if m.mode != modeEdit || m.edit == nil || m.edit.stage != stageInput || m.edit.inputFor != inputDue || !m.edit.direct {
		t.Fatalf("⏎ on a row must open the due input directly: mode=%d edit=%+v", m.mode, m.edit)
	}
	if got := m.edit.input.Value(); got != "2026-09-30" {
		t.Errorf("the input must open on the row's date, got %q", got)
	}
	out := frame(m)
	// The frame's own key line and the status row are asserted apart: an
	// OR over the two let either go silent (found by review).
	for _, want := range []string{"due date", "dated task", "⏎ apply · esc closes"} {
		if !strings.Contains(out, want) {
			t.Errorf("the overlay over the roadmap lost %q", want)
		}
	}
	if !strings.Contains(m.status, "esc closes") {
		t.Errorf("status = %q, want the direct input's key claim", m.status)
	}
	m.edit.input.SetValue("2026-08-15")
	press(m, "enter")
	if m.edit != nil || m.mode != modeNormal || m.view != viewRoadmap {
		t.Errorf("the apply must close the overlay and stay on the roadmap: edit=%v mode=%d view=%d", m.edit != nil, m.mode, m.view)
	}
	if got := m.b.Task("t-9sa6").Due.In(board.Zone()).Format("2006-01-02"); got != "2026-08-15" {
		t.Errorf("due = %s, want 2026-08-15", got)
	}
	// Re-laid eagerly: the walk after the apply must step the NEW pack.
	ids := make([]string, 0, len(m.road.lay.Rows))
	for _, r := range m.road.lay.Rows {
		ids = append(ids, r.ID)
	}
	if len(ids) < 2 || ids[1] != "t-9sa6" {
		t.Errorf("the row must move to its new date (second, after 07-31): %q", ids)
	}
	if m.road.sel != "t-9sa6" {
		t.Errorf("the cursor must follow the task to its new date, sits on %q", m.road.sel)
	}
	drainPersists(m, t)

	// A date past the window's edge: the window pans to the ◆ it just
	// placed, as it does when the cursor walks there.
	press(m, "enter")
	m.edit.input.SetValue("2026-12-24")
	press(m, "enter")
	r := m.road.lay.Row("t-9sa6")
	if r == nil {
		t.Fatal("t-9sa6 must still be on the timeline")
	}
	if tlW := m.roadTLW(); r.X < m.road.xOff || r.X >= m.road.xOff+tlW {
		t.Errorf("the window must pan to the moved ◆: x=%d window=[%d,%d)", r.X, m.road.xOff, m.road.xOff+tlW)
	}
	drainPersists(m, t)
}

// A cleared due takes the row off the timeline: the cursor lands on the row
// that followed it, the window does not jump to the top, and the note says
// the row left.
func TestRoadmapClearingADueLandsTheCursorOnTheNeighbour(t *testing.T) {
	m := roadModel(t, 240, 40)
	m.road.sel = "t-p7xw" // no rule, so the clear lands; t-9sa6 follows it
	var after string
	for i, r := range m.road.lay.Rows {
		if r.ID == "t-p7xw" && i+1 < len(m.road.lay.Rows) {
			after = m.road.lay.Rows[i+1].ID
		}
	}
	if after == "" {
		t.Fatal("setup: t-p7xw must have a row after it")
	}
	scroll := m.road.scroll
	press(m, "enter")
	m.edit.input.SetValue("")
	press(m, "enter")
	if m.edit != nil || !m.b.Task("t-p7xw").Due.IsZero() {
		t.Fatalf("an empty ⏎ must clear the due and close the overlay: edit=%v due=%v", m.edit != nil, m.b.Task("t-p7xw").Due)
	}
	if m.road.lay.Row("t-p7xw") != nil {
		t.Error("a task without a due must leave the timeline")
	}
	if m.road.sel != after {
		t.Errorf("the cursor must land on the row that followed (%s), sits on %q", after, m.road.sel)
	}
	if m.road.scroll != scroll {
		t.Errorf("the window must not scroll on its own: %d → %d", scroll, m.road.scroll)
	}
	if !strings.Contains(m.status, "left the timeline") || !strings.Contains(m.status, after) {
		t.Errorf("the note must say the row left and where the cursor is: %q", m.status)
	}
	drainPersists(m, t)
}

// `m` is the other spelling; esc closes the overlay with the due kept; a
// refused form stays in the input beside its refusal (the note input's
// rule) — a garbage form, and the empty form on a task that repeats, which
// furrow answers with "must keep a due" — and esc then closes.
func TestRoadmapDueInputEscClosesAndARefusalKeepsTheInput(t *testing.T) {
	m := roadModel(t, 240, 40)
	m.road.sel = "t-9sa6"
	before := m.b.Task("t-9sa6").Due
	press(m, "m")
	if m.edit == nil || m.edit.inputFor != inputDue {
		t.Fatal("m must open the due input like ⏎")
	}
	press(m, "esc")
	if m.edit != nil || m.view != viewRoadmap || !strings.Contains(m.status, "edit t-9sa6 · due date unchanged") {
		t.Errorf("esc must close the overlay on the roadmap with the due kept: edit=%v view=%d status=%q", m.edit != nil, m.view, m.status)
	}
	for _, form := range []string{"someday", ""} {
		press(m, "enter")
		m.edit.input.SetValue(form)
		press(m, "enter")
		if m.edit == nil || m.edit.stage != stageInput || m.edit.input.Value() != form || !m.statusErr {
			t.Fatalf("form %q: a refused form must stay in the input beside its refusal: edit=%+v err=%v status=%q", form, m.edit, m.statusErr, m.status)
		}
		if !m.b.Task("t-9sa6").Due.Equal(before) {
			t.Errorf("form %q: the due must be kept", form)
		}
		press(m, "esc")
		if m.edit != nil || m.mode != modeNormal || m.view != viewRoadmap {
			t.Errorf("form %q: esc must close the overlay: edit=%v mode=%d view=%d", form, m.edit != nil, m.mode, m.view)
		}
	}
}

// The note input has the same rule, and both of its key lines say so —
// the overlay's foot and the status row (one said "esc back" while
// closing; found by review).
func TestNoteInputSaysEscClosesOnBothLines(t *testing.T) {
	lines := dumpFrame(t, 240, 40, "note")
	body, status := strings.Join(lines[:len(lines)-1], "\n"), lines[len(lines)-1]
	if !strings.Contains(body, "⏎ apply · esc closes") {
		t.Error("the note input's foot must say esc closes")
	}
	if !strings.Contains(status, "esc closes") || strings.Contains(status, "esc back") {
		t.Errorf("the status row must agree: %q", status)
	}
}

// With no row under the cursor the key says so rather than opening an input
// on nothing — the board with no dues is the shape.
func TestRoadmapEnterOnNoRowSaysSo(t *testing.T) {
	m := advSmallModel(t, 240, 50)
	press(m, "C")
	if m.view != viewRoadmap {
		t.Fatal("C did not open the roadmap")
	}
	press(m, "enter")
	if m.edit != nil || !strings.Contains(m.status, "no dated task") {
		t.Errorf("⏎ with no row must refuse in words: edit=%v status=%q", m.edit != nil, m.status)
	}
}

// The frame carries what the demo exists to show: the due input over the
// timeline's own header.
func TestRoadDueDemoCarriesTheInputOverTheTimeline(t *testing.T) {
	out := strings.Join(dumpFrame(t, 240, 40, "roaddue"), "\n")
	for _, want := range []string{"due date", "dated task", "⏎ apply · esc closes"} {
		if !strings.Contains(out, want) {
			t.Errorf("-demo roaddue lost %q", want)
		}
	}
}
