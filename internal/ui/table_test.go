package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func tableModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := boardModel(t, w, h)
	m.view = viewTable
	return m
}

// fixedNow pins the ui clock, so "is this due in the past?" has one answer.
func fixedNow(t *testing.T, at time.Time) {
	t.Helper()
	prev := nowFn
	nowFn = func() time.Time { return at }
	t.Cleanup(func() { nowFn = prev })
}

func day(s string) time.Time {
	d, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return d
}

// sortBoard is four tasks across two lanes whose field order disagrees with
// their canonical (lane, priority) order on every sort key.
func sortBoard() *board.Board {
	return board.NewBoard([]*board.Task{
		{ID: "t-a", Status: "ready", Priority: 10, Title: "a", Value: 1, Effort: 4,
			Created: day("2026-01-04T00:00:00Z"), Updated: day("2026-01-01T00:00:00Z"),
			Due: day("2026-03-01T00:00:00Z")},
		{ID: "t-b", Status: "ready", Priority: 20, Title: "b", Value: 3, Effort: 2,
			Created: day("2026-01-03T00:00:00Z"), Updated: day("2026-01-02T00:00:00Z")},
		{ID: "t-c", Status: "backlog", Priority: 10, Title: "c", Value: 2,
			Created: day("2026-01-02T00:00:00Z"), Updated: day("2026-01-03T00:00:00Z"),
			Due: day("2026-02-01T00:00:00Z")},
		{ID: "t-d", Status: "backlog", Priority: 20, Title: "d", Effort: 1,
			Created: day("2026-01-01T00:00:00Z"), Updated: day("2026-01-04T00:00:00Z")},
	})
}

func rowIDs(m *Model) []string {
	var ids []string
	for _, t := range m.tableRows() {
		ids = append(ids, t.ID)
	}
	return ids
}

func TestTableSortOrdersRowsAndKeepsUnsetLast(t *testing.T) {
	cases := []struct {
		key  sortKey
		asc  bool
		want []string
	}{
		// Canonical: lane order (backlog before ready in boardLanes), priority within.
		{sortCanonical, false, []string{"t-c", "t-d", "t-a", "t-b"}},
		{sortUpdated, false, []string{"t-d", "t-c", "t-b", "t-a"}},
		{sortUpdated, true, []string{"t-a", "t-b", "t-c", "t-d"}},
		{sortCreated, false, []string{"t-a", "t-b", "t-c", "t-d"}},
		// Value: t-d has none and stays LAST even descending.
		{sortValue, false, []string{"t-b", "t-c", "t-a", "t-d"}},
		{sortValue, true, []string{"t-a", "t-c", "t-b", "t-d"}},
		// Effort: t-c has none and stays last in both directions.
		{sortEffort, true, []string{"t-d", "t-b", "t-a", "t-c"}},
		{sortEffort, false, []string{"t-a", "t-b", "t-d", "t-c"}},
		// Due: the undated pair keeps ITS canonical order (t-d before t-b),
		// after every dated row, ascending AND descending.
		{sortDue, true, []string{"t-c", "t-a", "t-d", "t-b"}},
		{sortDue, false, []string{"t-a", "t-c", "t-d", "t-b"}},
	}
	for _, c := range cases {
		t.Run(c.key.String()+"/"+sortArrow(c.asc), func(t *testing.T) {
			m := New(memstore.NewWith(sortBoard()), Options{Table: true})
			m.w, m.h = 240, 40
			m.tableSort, m.tableSortAsc = c.key, c.asc
			got := rowIDs(m)
			if strings.Join(got, " ") != strings.Join(c.want, " ") {
				t.Errorf("sort %s %s: got %v, want %v", c.key, sortArrow(c.asc), got, c.want)
			}
		})
	}
}

func TestSortCycleVisitsEveryKeyInBothDirections(t *testing.T) {
	m := tableModel(t, 240, 40)
	type state struct {
		key sortKey
		asc bool
	}
	want := []state{
		{sortUpdated, false}, {sortUpdated, true},
		{sortCreated, false}, {sortCreated, true},
		{sortValue, false}, {sortValue, true},
		{sortEffort, true}, {sortEffort, false},
		{sortDue, true}, {sortDue, false},
		{sortCanonical, false},
	}
	for i, w := range want {
		press(m, "o")
		if m.tableSort != w.key || m.tableSortAsc != w.asc {
			t.Fatalf("press %d: got %s asc=%v, want %s asc=%v",
				i+1, m.tableSort, m.tableSortAsc, w.key, w.asc)
		}
	}
	// The full cycle is a loop: one more press starts over.
	press(m, "o")
	if m.tableSort != sortUpdated || m.tableSortAsc {
		t.Fatalf("the cycle does not loop: got %s asc=%v", m.tableSort, m.tableSortAsc)
	}
}

func TestSortKeepsSelectionOnTheSameTask(t *testing.T) {
	m := tableModel(t, 240, 40)
	m.setPos(4)
	id := m.curTask().ID
	press(m, "o") // updated ▼ — reorders almost every fixture row
	if got := m.curTask(); got == nil || got.ID != id {
		t.Fatalf("sorting moved the selection off %s", id)
	}
	if m.tableRows()[m.tableIdx].ID != id {
		t.Fatal("tableIdx does not point at the selected task")
	}
}

func TestSortInBoardViewOnlyExplains(t *testing.T) {
	m := boardModel(t, 240, 40)
	press(m, "o")
	if m.tableSort != sortCanonical {
		t.Fatal("o must not change sort state from the board view")
	}
	if !strings.Contains(m.status, "table") {
		t.Fatalf("o on the board should point at the table view, said %q", m.status)
	}
}

func TestHeaderClickSortsAndReclickFlips(t *testing.T) {
	m := tableModel(t, 240, 40)
	var upd, lane tableCol
	for _, c := range m.tableGeom() {
		switch c.sort {
		case sortUpdated:
			upd = c
		case sortCanonical:
			lane = c
		}
	}
	click := func(c tableCol) {
		m.Update(tea.MouseClickMsg{X: c.x + 1, Y: rowColHdr, Button: tea.MouseLeft})
	}

	click(upd)
	if m.tableSort != sortUpdated || m.tableSortAsc {
		t.Fatalf("clicking the updated header must sort updated ▼, got %s asc=%v",
			m.tableSort, m.tableSortAsc)
	}
	click(upd)
	if m.tableSort != sortUpdated || !m.tableSortAsc {
		t.Fatal("a second click on the same header must flip the direction")
	}
	click(lane)
	if m.tableSort != sortCanonical {
		t.Fatal("clicking the lane header must return to canonical order")
	}
	// A row click is NOT a sort gesture and must change nothing.
	before := m.tableIdx
	m.Update(tea.MouseClickMsg{X: upd.x + 1, Y: rowColHdr + 3, Button: tea.MouseLeft})
	if m.tableSort != sortCanonical || m.tableIdx != before {
		t.Fatal("a click below the header row changed sort or selection")
	}
}

func TestHeaderClickHonoursTheSliceInset(t *testing.T) {
	m := tableModel(t, 240, 40)
	m.sliceOpen = true
	m.mode = modeNormal // panel visible but NOT focused: the strip stays live
	var due tableCol
	for _, c := range m.tableGeom() {
		if c.sort == sortDue {
			due = c
		}
	}
	// The same cell x, shifted by the panel inset — clicking the un-shifted
	// coordinate would miss (that is the bug the shared geometry prevents).
	m.Update(tea.MouseClickMsg{X: m.sliceInset() + due.x + 1, Y: rowColHdr, Button: tea.MouseLeft})
	if m.tableSort != sortDue {
		t.Fatalf("inset header click missed: got %s", m.tableSort)
	}
}

func TestSortedTableRefusesQuickReorder(t *testing.T) {
	m := tableModel(t, 240, 40)
	press(m, "o") // updated ▼
	before := strings.Join(rowIDs(m), " ")
	press(m, "K")
	if got := strings.Join(rowIDs(m), " "); got != before {
		t.Fatal("K reordered a sorted table")
	}
	if m.inflight || len(m.pending) > 0 {
		t.Fatal("the refused reorder still queued a write")
	}
	if !strings.Contains(m.status, "canonical") {
		t.Fatalf("the refusal must say how to get back, said %q", m.status)
	}
}

func TestTableRendersDueEpicUpdatedColumns(t *testing.T) {
	// Pinned: the fixture's Updated stamps are fixed instants, so on a wall
	// clock ago() would cross its 90-day line (2026-10-14) and stop saying
	// "d ago" — a test that goes red by calendar.
	fixedNow(t, day("2026-08-10T00:00:00Z"))
	m := tableModel(t, 240, 40)
	out := frame(m)
	head := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "title") {
			head = l
			break
		}
	}
	for _, col := range []string{"due", "epic", "updated"} {
		if !strings.Contains(head, col) {
			t.Errorf("header lacks the %s column: %q", col, head)
		}
	}
	// The fixture's dated tasks render their LOCAL day — computed through the
	// same conversion, so the test holds in every timezone (a hardcoded
	// "2026-08-20" fails east of UTC+9, where that instant is the 21st).
	if want := m.b.Task("t-ehk7").Due.Local().Format("2006-01-02"); !strings.Contains(out, want) {
		t.Errorf("t-ehk7's due day %s is not in the frame", want)
	}
	if !strings.Contains(out, "九州キャンプ") || strings.Contains(out, "e-fw2m") {
		t.Error("the epic column must resolve the id to its title")
	}
	if !strings.Contains(out, "d ago") {
		t.Error("the updated column must render ago() ages")
	}
}

// Every sort state must stay READABLE somewhere persistent. Keys with a
// column carry ▲▼ in the header; created and effort have no column, so the
// filter bar names the sort — without it those four cycle stops rendered
// identically to canonical.
func TestSortStateIsAlwaysVisible(t *testing.T) {
	m := tableModel(t, 240, 40)
	for i := 0; i < 10; i++ {
		press(m, "o")
		out := frame(m)
		want := "sort " + m.tableSort.String() + " " + sortArrow(m.tableSortAsc)
		if !strings.Contains(out, want) {
			t.Errorf("press %d: %q is nowhere in the frame — invisible sort state", i+1, want)
		}
	}
	press(m, "o") // back to canonical: no sort is active, so no indicator
	if out := frame(m); strings.Contains(out, "sort canonical") {
		t.Error("canonical is the absence of a sort and must not render an indicator")
	}
	// The indicator is table state; the board must not wear it.
	m.tableSort, m.tableSortAsc = sortCreated, false
	press(m, "v")
	if out := frame(m); strings.Contains(out, "sort created") {
		t.Error("the board view must not show the table's sort")
	}
}

func TestOverdueDueRendersDanger(t *testing.T) {
	fixedNow(t, day("2026-08-10T00:00:00Z"))
	b := board.NewBoard([]*board.Task{
		{ID: "t-late", Status: "ready", Title: "late", Due: day("2026-08-01T00:00:00Z")},
		{ID: "t-ok", Status: "ready", Title: "ok", Due: day("2026-12-01T00:00:00Z")},
	})
	m := New(memstore.NewWith(b), Options{Table: true})
	m.w, m.h = 240, 20
	m.setPos(1) // keep the cursor OFF t-late: the inverse band drops cell styles
	out := m.View().Content

	late := m.th.danger.Render(pad(day("2026-08-01T00:00:00Z").Local().Format("2006-01-02"), 10))
	if !strings.Contains(out, late) {
		t.Error("an overdue due must render in the danger style")
	}
	fine := m.th.danger.Render(pad(day("2026-12-01T00:00:00Z").Local().Format("2006-01-02"), 10))
	if strings.Contains(out, fine) {
		t.Error("a future due must NOT render in the danger style")
	}
}

// The CJK rule: every column boundary must sit where tableGeom says it does,
// at every supported width — drift accumulates one double-width glyph at a
// time and only shows at the far edge. Cells are CUT OUT of the frame by
// display column (ansi.Cut is grapheme-aware), so the header row AND every
// body row are checked against the same measurement the hit-test uses; a
// weaker version only followed the due dates and skipped most of the rows.
func TestTableColumnsAlignUnderCJKTitles(t *testing.T) {
	fixedNow(t, day("2026-08-10T00:00:00Z")) // ago() must never emit a date here
	for _, w := range []int{240, 241, 259, 320, 399, 400} {
		m := tableModel(t, w, 40)
		m.setSort(sortDue, true) // the header arrow must not shift a boundary
		out, err := m.Dump(w, 40, "", true)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(out, "\n")
		g := m.tableGeom()
		cell := func(line string, c tableCol) string {
			return strings.TrimRight(ansi.Cut(line, c.x, c.x+c.w), " ")
		}

		for _, c := range g {
			if c.name == "" {
				continue
			}
			want := c.name
			if c.sort == sortDue {
				want += " " + glyphSortAsc
			}
			if got := cell(lines[rowColHdr], c); got != want {
				t.Errorf("w=%d header cell %q reads %q at [%d,%d)", w, c.name, got, c.x, c.x+c.w)
			}
		}

		var idCol, dueCol, updCol tableCol
		for _, c := range g {
			switch c.name {
			case "id":
				idCol = c
			case "due":
				dueCol = c
			case "updated":
				updCol = c
			}
		}
		rows := m.tableRows()
		for i, task := range rows {
			y := rowRule + i // 34 fixture rows fit h=40's 35-row budget (1 spare), so top is 0
			if y >= len(lines) || y >= 40-footerH {
				t.Fatalf("w=%d: row %d fell off the frame — the test premise broke", w, i)
			}
			if got := cell(lines[y], idCol); got != task.ID {
				t.Errorf("w=%d row %d: id cell reads %q, want %q", w, i, got, task.ID)
			}
			want := ""
			if !task.Due.IsZero() {
				want = task.Due.Local().Format("2006-01-02")
			}
			if got := cell(lines[y], dueCol); got != want {
				t.Errorf("w=%d row %d (%s): due cell reads %q, want %q", w, i, task.ID, got, want)
			}
			// A cell AFTER due too: a body-side width divergence at due slides
			// updated/deps right without moving due's own start — this cut is
			// what catches that mutant.
			if got := cell(lines[y], updCol); got != ago(task.Updated) {
				t.Errorf("w=%d row %d (%s): updated cell reads %q, want %q",
					w, i, task.ID, got, ago(task.Updated))
			}
			if lw := lg.Width(lines[y]); lw > w {
				t.Errorf("w=%d row %d is %d cells wide", w, i, lw)
			}
		}
	}
}

// The header click, end to end: a REAL tea.Program fed the SGR bytes a
// terminal sends, so the 1-based wire → 0-based Update decoding is on the
// path (the model-level tests above construct MouseClickMsg directly and
// bypass it).
func TestHeaderClickSortsEndToEnd(t *testing.T) {
	probe := tableModel(t, 240, 40)
	var due tableCol
	for _, c := range probe.tableGeom() {
		if c.sort == sortDue {
			due = c
		}
	}

	m := run(t, 240, 40,
		"v", // board → table
		mousePress(due.x+1, rowColHdr), mouseRelease(due.x+1, rowColHdr),
	)
	if m.view != viewTable || m.tableSort != sortDue || !m.tableSortAsc {
		t.Fatalf("e2e header click: view=%v sort=%s asc=%v, want table/due/asc",
			m.view, m.tableSort, m.tableSortAsc)
	}

	// Re-click flips; a press that wanders before its release still sorted on
	// the press and must not arm a drag in this view.
	m = run(t, 240, 40,
		"v",
		mousePress(due.x+1, rowColHdr), mouseRelease(due.x+1, rowColHdr),
		mousePress(due.x+1, rowColHdr), mouseMotion(due.x+40, rowColHdr+8), mouseRelease(due.x+40, rowColHdr+8),
	)
	if m.tableSort != sortDue || m.tableSortAsc {
		t.Fatalf("second e2e click must flip to due ▼, got %s asc=%v", m.tableSort, m.tableSortAsc)
	}
	if m.drag.armed || m.drag.moved {
		t.Fatal("a table-view press must never arm a drag")
	}
}

func TestDemoSortShowsTheSortedTable(t *testing.T) {
	m := boardModel(t, 240, 40)
	out, err := m.Dump(240, 40, "sort", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "due "+glyphSortAsc) {
		t.Error("the sorted header must carry the direction marker")
	}
	rows := rowIDs(m)
	if len(rows) == 0 || rows[0] != "t-jv3j" {
		t.Errorf("due ▲ must put the earliest-dated fixture task first, got %v", rows[:minInt(3, len(rows))])
	}
}
