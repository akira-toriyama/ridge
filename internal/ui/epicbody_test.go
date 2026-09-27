package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

// boxOverlay opens the epic overlay on id from the slice panel's epic axis.
func boxOverlay(t *testing.T, id string) *Model {
	t.Helper()
	m := boardModel(t, 240, 50)
	m.toggleSlice()
	m.sliceField = sliceEpic
	m.enterEpic(id)
	if m.epic == nil {
		t.Fatalf("the overlay did not open on %s", id)
	}
	return m
}

// The body row opens a read stage over the record's lines — the activation
// log furrow appends, wrapped to the list — and `a` appends a paragraph
// optimistically, persisted through the same PersistNote a task uses.
func TestEpicBodyStageShowsTheRecordAndAppendsANote(t *testing.T) {
	m := boxOverlay(t, "e-fw2m")
	m.epic.menuIdx = int(epicFieldBody)
	press(m, "enter")
	if m.epic.stage != stageList || m.epic.field != epicFieldBody {
		t.Fatalf("the body row must open a list stage: %+v", m.epic)
	}
	out := frame(m)
	for _, want := range []string{"body — the box's own record", "2026-07-16 09:12 activated", "a append a paragraph · e $EDITOR · g/G ^u/^d page · esc back"} {
		if !strings.Contains(out, want) {
			t.Errorf("the body stage lost %q:\n%s", want, out)
		}
	}
	press(m, "enter") // a read: ⏎ on a line does nothing
	if m.epic.stage != stageList || len(m.pending) != 0 {
		t.Error("⏎ on a record line must neither write nor leave the stage")
	}
	press(m, "a")
	if m.epic.stage != stageInput || m.epic.inputFor != epicInputNote {
		t.Fatalf("a must open the note input: %+v", m.epic)
	}
	m.epic.input.SetValue("予約 3 件確定")
	press(m, "enter")
	if m.epic.stage != stageList {
		t.Error("the apply must land back in the body stage")
	}
	if e := m.b.Epic("e-fw2m"); !strings.HasSuffix(e.Body, "予約 3 件確定\n") {
		t.Errorf("the paragraph must be appended optimistically: %q", e.Body)
	}
	if !strings.Contains(frame(m), "予約 3 件確定") {
		t.Error("the stage must show the appended paragraph")
	}
	drainPersists(m, t)
	if !strings.Contains(m.status, "note e-fw2m") {
		t.Errorf("status = %q", m.status)
	}
	// An empty ⏎ backs out to the stage without a write.
	press(m, "a")
	press(m, "enter")
	if m.epic.stage != stageList || len(m.pending) != 0 {
		t.Error("an empty note is a back-out")
	}
}

// The reviewed row is a gate; ⏎ stamps furrow's review clock on the box —
// Reviewed alone, Updated untouched — and the row reads the stamp.
func TestEpicReviewedRowStampsTheReviewClock(t *testing.T) {
	m := boxOverlay(t, "e-c4mt")
	if e := m.b.Epic("e-c4mt"); !e.Reviewed.IsZero() {
		t.Fatal("setup: e-c4mt is never reviewed on the fixture")
	}
	if !strings.Contains(frame(m), "never") {
		t.Error("the reviewed row must read never before the stamp")
	}
	updated := m.b.Epic("e-c4mt").Updated
	m.epic.menuIdx = int(epicFieldReviewed)
	press(m, "enter")
	if m.epic.stage != stageGate || !strings.Contains(frame(m), "stamp reviewed") {
		t.Fatalf("the reviewed row must open its gate: %+v", m.epic)
	}
	press(m, "enter")
	e := m.b.Epic("e-c4mt")
	if e.Reviewed.IsZero() || !e.Updated.Equal(updated) {
		t.Errorf("⏎ must stamp Reviewed alone: %+v", e)
	}
	if m.epic.stage != stageMenu || strings.Contains(frame(m), "never") {
		t.Error("the row must read the stamp back in the menu")
	}
	drainPersists(m, t)
}

// `e` in the body stage hands the record to $EDITOR, and the result lands
// through the same path a task's does — on the box.
func TestEpicBodyEditorResultLandsOnTheBox(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir()) // editBodyCmd's temp file; only $EDITOR's exit removes it
	m := boxOverlay(t, "e-fw2m")
	m.epic.menuIdx = int(epicFieldBody)
	press(m, "enter")
	if c := m.onEpicBodyKey(tea.KeyPressMsg{Code: 'e', Text: "e"}, m.b.Epic("e-fw2m")); c == nil {
		t.Fatal("e must return the editor command")
	}
	m.Update(editorDoneMsg{id: "e-fw2m", body: "# 箱\n\n書き直した記録\n"})
	if e := m.b.Epic("e-fw2m"); e.Body != "# 箱\n\n書き直した記録\n" {
		t.Errorf("the editor result must replace the box's record: %q", e.Body)
	}
	if !strings.Contains(frame(m), "書き直した記録") {
		t.Error("the open stage must show the new record")
	}
	m.Update(editorDoneMsg{id: "e-fw2m", body: " \n"})
	if !m.statusErr || m.b.Epic("e-fw2m").Body != "# 箱\n\n書き直した記録\n" {
		t.Error("a wiped buffer must be refused and keep the record")
	}
	drainPersists(m, t)
}

// `e` is refused while a store-first write of this overlay has landed
// unread: `epic activate --reason` appends to the record furrow-side, and a
// $EDITOR round trip started on the pre-activation Body would hand
// PersistBody a replacement without that line (found by review).
func TestEpicBodyEditorIsRefusedInsideAStoreFirstWindow(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir()) // editBodyCmd's temp file; only $EDITOR's exit removes it
	m := boxOverlay(t, "e-fw2m")
	// A real store-first write of this overlay, IN FLIGHT: the standing
	// gate's ⏎, undrained.
	m.epic.menuIdx = int(epicFieldStanding)
	press(m, "enter", "enter")
	if !m.storeFirstInflight() {
		t.Fatal("setup: the standing write must be in flight")
	}
	m.epic.menuIdx = int(epicFieldBody)
	press(m, "enter")
	e := tea.KeyPressMsg{Code: 'e', Text: "e"}
	if c := m.onEpicBodyKey(e, m.b.Epic("e-fw2m")); c != nil || !m.statusErr || !strings.Contains(m.status, "in flight") {
		t.Errorf("e must be refused while a box write is in flight: cmd=%v status=%q", c != nil, m.status)
	}
	// …and LANDED UNREAD (the fixture re-reads on the drain, so the flag is
	// armed by hand for the second arm).
	drainPersists(m, t)
	m.storeFirstUnread = true
	if c := m.onEpicBodyKey(e, m.b.Epic("e-fw2m")); c != nil || !m.statusErr || !strings.Contains(m.status, "esc out, then r") {
		t.Errorf("e must be refused while the write is unread, naming the way out: cmd=%v status=%q", c != nil, m.status)
	}
	// `a` needs no gate: PersistNote appends furrow-side.
	m.onEpicBodyKey(tea.KeyPressMsg{Code: 'a', Text: "a"}, m.b.Epic("e-fw2m"))
	if m.epic.stage != stageInput {
		t.Errorf("a must still open the note input (stage=%d)", m.epic.stage)
	}
	press(m, "esc")
	// Once the re-read has applied, e hands the record to $EDITOR.
	m.clearUnread()
	if c := m.onEpicBodyKey(e, m.b.Epic("e-fw2m")); c == nil {
		t.Error("e must be accepted once the window has closed")
	}
}

// A long record pages: g/G and ^u/^d move the cursor over hundreds of rows,
// and a CJK line with no whitespace wraps to the list's width, never over
// it (CLAUDE.md's width rule; a sabotage of wrapLines had no shipped guard).
func TestEpicBodyStagePagesAndWrapsCJK(t *testing.T) {
	m := boxOverlay(t, "e-fw2m")
	var sb strings.Builder
	sb.WriteString("# 長い記録\n")
	for i := 0; i < 120; i++ {
		sb.WriteString("\n二〇二六年の秋に阿蘇から高千穂へ抜ける三泊四日の行程を家族会議で固め直した記録の一行目がここに続く\n")
	}
	if err := m.b.SetEpicBody("e-fw2m", sb.String()); err != nil {
		t.Fatal(err)
	}
	m.epic.menuIdx = int(epicFieldBody)
	press(m, "enter")
	rows := m.epicListRows(m.b.Epic("e-fw2m"))
	if len(rows) < 240 {
		t.Fatalf("setup: %d rows, want the record wrapped to hundreds", len(rows))
	}
	w := m.overlayInner() - 2
	for i, r := range rows {
		if lg.Width(r) > w {
			t.Fatalf("row %d is %d cells wide, over the list's %d: %q", i, lg.Width(r), w, r)
		}
	}
	press(m, "G")
	if m.epic.listIdx != len(rows)-1 {
		t.Errorf("G must land on the last row, got %d of %d", m.epic.listIdx, len(rows))
	}
	press(m, "g")
	if m.epic.listIdx != 0 {
		t.Errorf("g must land on the first row, got %d", m.epic.listIdx)
	}
	press(m, "ctrl+d")
	if m.epic.listIdx == 0 {
		t.Error("^d must page down")
	}
	at := m.epic.listIdx
	press(m, "ctrl+u")
	if m.epic.listIdx >= at {
		t.Error("^u must page back up")
	}
	if out := frame(m); !strings.Contains(out, "below") {
		t.Errorf("the windowed stage must say how many rows are below")
	}
	// An append on a long record lands at the tail, and the cursor follows
	// it there — or the gesture changes nothing on screen (found by review).
	press(m, "g", "a")
	m.epic.input.SetValue("末尾に追記した一段落")
	press(m, "enter")
	rows = m.epicListRows(m.b.Epic("e-fw2m"))
	if m.epic.listIdx != len(rows)-1 || !strings.Contains(frame(m), "末尾に追記した一段落") {
		t.Errorf("the cursor must follow the append to the tail (idx %d of %d)", m.epic.listIdx, len(rows))
	}
	drainPersists(m, t)
}

// The body cell tells a real record from the `# <title>` line every fresh box
// holds, and from no record at all.
func TestEpicBodyCellReadsTheRecordsShape(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"", "—"},
		{"# 箱\n", "— only the title line"},
		{"# 箱\n\n一段落\n", "2 line(s)"},
	} {
		got := epicBodyCell(&board.EpicInfo{Body: tc.body})
		if !strings.HasPrefix(got, tc.want) {
			t.Errorf("cell(%q) = %q, want %q…", tc.body, got, tc.want)
		}
	}
}

// The strip states the record and the review clock, the two facts the rows
// edit.
func TestBoxStripNamesTheRecordAndTheReviewClock(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.openBoxes()
	for _, r := range m.buildBoxes().Rows {
		if r.ID == "e-fw2m" {
			m.boxes.sel = r.Key
		}
	}
	out := frame(m)
	for _, want := range []string{"body 4 line(s)", "reviewed "} {
		if !strings.Contains(out, want) {
			t.Errorf("the strip lost %q", want)
		}
	}
}
