package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
	for _, want := range []string{"body — the box's own record", "2026-07-16 09:12 activated", "a append a paragraph · e $EDITOR · esc back"} {
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
	m.edit = nil
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
	for _, want := range []string{"body 7 line(s)", "reviewed "} {
		if !strings.Contains(out, want) {
			t.Errorf("the strip lost %q", want)
		}
	}
}
