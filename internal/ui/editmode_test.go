package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

func press(m *Model, keys ...string) {
	for _, k := range keys {
		m.Update(keyMsg(k))
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

// drainPersists runs every queued write to completion, the way the program
// loop would, so a test can assert on the store-visible outcome. An EMPTY
// queue fails the test: every caller just performed an edit that must reach
// the store, and a silent no-op here once hid the entire edit-overlay persist
// path being deleted — the mutation survived the old helper.
func drainPersists(m *Model, t *testing.T) {
	t.Helper()
	if !m.inflight && len(m.pending) == 0 {
		t.Fatal("nothing was queued — the edit never reached the store-write path")
	}
	for i := 0; i < 32 && (m.inflight || len(m.pending) > 0); i++ {
		cmd := m.firePersist()
		if cmd == nil {
			break
		}
		m.Update(cmd())
	}
	if m.inflight || len(m.pending) > 0 {
		t.Fatal("the persist queue did not drain")
	}
}

func editModel(t *testing.T, id string) *Model {
	t.Helper()
	m := boardModel(t, 240, 50)
	if !m.selectID(id, false) {
		t.Fatalf("could not select %s", id)
	}
	m.enterEdit()
	if m.mode != modeEdit || m.edit == nil {
		t.Fatal("enterEdit did not open the overlay")
	}
	return m
}

func TestEnterEditsWithPeekAndMovesWithout(t *testing.T) {
	// Board, no peek: Enter is move mode — the muscle memory stays.
	m := boardModel(t, 240, 50)
	press(m, "enter")
	if m.mode != modeMove {
		t.Errorf("board Enter without a peek must lift the card, mode=%d", m.mode)
	}
	press(m, "esc")

	// Peek open: Enter edits.
	m.peekOpen = true
	press(m, "enter")
	if m.mode != modeEdit {
		t.Errorf("board Enter with the peek open must edit, mode=%d", m.mode)
	}
	press(m, "esc")

	// Table rows: Enter edits (GitHub's Enter = cell editing).
	m2 := boardModel(t, 240, 50)
	m2.view = viewTable
	press(m2, "enter")
	if m2.mode != modeEdit {
		t.Errorf("table Enter must edit, mode=%d", m2.mode)
	}
}

func TestEditValuePickAppliesAndPersists(t *testing.T) {
	m := editModel(t, "t-9sa6")
	m.edit.menuIdx = int(fieldValue)
	press(m, "enter") // open the picker
	if m.edit.stage != stagePick {
		t.Fatalf("stage = %d, want pick", m.edit.stage)
	}
	press(m, "3")
	if got := m.b.Task("t-9sa6").Value; got != 3 {
		t.Errorf("value = %d, want 3 (optimistic apply)", got)
	}
	if m.edit.stage != stageMenu {
		t.Error("a pick must return to the menu")
	}
	drainPersists(m, t)

	// 0 clears.
	m.edit.menuIdx = int(fieldValue)
	press(m, "enter", "0")
	if got := m.b.Task("t-9sa6").Value; got != 0 {
		t.Errorf("value = %d, want cleared", got)
	}
	drainPersists(m, t)
}

func TestEditLabelToggleRoundTrips(t *testing.T) {
	m := editModel(t, "t-9sa6") // labels: [ui]
	m.edit.menuIdx = int(fieldLabels)
	press(m, "enter")
	rows := m.editListRows(m.b.Task("t-9sa6"))
	idx := -1
	for i, r := range rows {
		if r == "bbq" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("label vocabulary %v lacks ui", rows)
	}
	m.edit.listIdx = idx
	press(m, "x")
	if containsStrUI(m.b.Task("t-9sa6").Labels, "bbq") {
		t.Error("toggling an owned label must remove it")
	}
	press(m, "x")
	if !containsStrUI(m.b.Task("t-9sa6").Labels, "bbq") {
		t.Error("toggling again must add it back")
	}
	drainPersists(m, t)
}

func TestEditEpicFileAndUnfile(t *testing.T) {
	m := editModel(t, "t-ehk7") // unfiled in the fixture
	m.edit.menuIdx = int(fieldEpic)
	press(m, "enter")
	m.edit.listIdx = 1 // the one fixture epic
	press(m, "enter")
	if got := m.b.Task("t-ehk7").Epic; got != "e-fw2m" {
		t.Errorf("epic = %q, want e-fw2m", got)
	}
	drainPersists(m, t)

	m.edit.menuIdx = int(fieldEpic)
	press(m, "enter") // reopen: the cursor lands on the CURRENT epic, not on row 0
	if m.edit.listIdx != 1 {
		t.Fatalf("listIdx = %d, want the current epic's row", m.edit.listIdx)
	}
	press(m, "up", "enter") // row 0 = (unfiled), one deliberate step away
	if got := m.b.Task("t-ehk7").Epic; got != "" {
		t.Errorf("epic = %q, want unfiled", got)
	}
	drainPersists(m, t)
}

func TestEditDueRefusesGarbageAndAcceptsForms(t *testing.T) {
	m := editModel(t, "t-9sa6")
	m.edit.menuIdx = int(fieldDue)
	press(m, "enter")
	if m.edit.stage != stageInput {
		t.Fatal("due must open the text input")
	}
	m.edit.input.SetValue("someday")
	press(m, "enter")
	// t-9sa6 carries a fixture due: the refusal must keep it, not clear it.
	if before := memstore.New().Board().Task("t-9sa6").Due; !m.b.Task("t-9sa6").Due.Equal(before) {
		t.Error("garbage due must refuse and keep the existing promise")
	}
	if !m.statusErr {
		t.Error("the refusal must be surfaced in the status line")
	}

	m.edit.menuIdx = int(fieldDue)
	press(m, "enter")
	m.edit.input.SetValue("2026-09-01")
	press(m, "enter")
	if got := m.b.Task("t-9sa6").Due.Local().Format("2006-01-02"); got != "2026-09-01" {
		t.Errorf("due = %s, want 2026-09-01", got)
	}
	drainPersists(m, t)

	// Empty input clears.
	m.edit.menuIdx = int(fieldDue)
	press(m, "enter")
	m.edit.input.SetValue("")
	press(m, "enter")
	if !m.b.Task("t-9sa6").Due.IsZero() {
		t.Error("an empty due must clear")
	}
	drainPersists(m, t)
}

func TestEditChecklistCursorTogglesTheSelectedItem(t *testing.T) {
	m := editModel(t, "t-9sa6") // 6 unchecked items
	m.edit.menuIdx = int(fieldChecklist)
	press(m, "enter", "down", "down") // cursor on item 2
	press(m, "x")
	cl := m.b.Task("t-9sa6").Checklist
	if !cl[2].Done || cl[0].Done {
		t.Errorf("x must toggle the SELECTED item, not the first unfinished: %+v", cl[:3])
	}
	drainPersists(m, t)

	// a appends.
	press(m, "a")
	m.edit.input.SetValue("新しい項目")
	press(m, "enter")
	cl = m.b.Task("t-9sa6").Checklist
	if cl[len(cl)-1].Text != "新しい項目" {
		t.Errorf("a must append, got %+v", cl[len(cl)-1])
	}
	drainPersists(m, t)

	// r rewords the selected item.
	m.edit.listIdx = 0
	press(m, "r")
	m.edit.input.SetValue("書き直した")
	press(m, "enter")
	if got := m.b.Task("t-9sa6").Checklist[0].Text; got != "書き直した" {
		t.Errorf("reword = %q", got)
	}
	drainPersists(m, t)

	// d deletes the selected item.
	n := len(m.b.Task("t-9sa6").Checklist)
	m.edit.listIdx = 0
	press(m, "d")
	cl = m.b.Task("t-9sa6").Checklist
	if len(cl) != n-1 || cl[0].Text == "書き直した" {
		t.Errorf("d must delete item 0: %d items, head %q", len(cl), cl[0].Text)
	}
	drainPersists(m, t)
}

func TestEditRetitleRefusesEmptyAndApplies(t *testing.T) {
	m := editModel(t, "t-9sa6")
	press(m, "enter") // menu row 0 = title
	m.edit.input.SetValue("   ")
	press(m, "enter")
	if strings.TrimSpace(m.b.Task("t-9sa6").Title) == "" {
		t.Error("an empty retitle must refuse")
	}
	press(m, "enter")
	m.edit.input.SetValue("新しいタイトル")
	press(m, "enter")
	if got := m.b.Task("t-9sa6").Title; got != "新しいタイトル" {
		t.Errorf("title = %q", got)
	}
	drainPersists(m, t)
}

func TestEditOverlayRendersInTheFrame(t *testing.T) {
	m := boardModel(t, 240, 50)
	if err := m.demoState("edit"); err != nil {
		t.Fatal(err)
	}
	out := frame(m)
	if !strings.Contains(out, "edit t-9sa6") {
		t.Error("the overlay header is missing")
	}
	if !strings.Contains(out, "⏎/x toggle · a add · d delete · r reword") {
		t.Error("the checklist stage hints are missing")
	}
	if !strings.Contains(out, "▌ [ ]") {
		t.Error("the checklist cursor is missing")
	}
}

func TestEditEscWalksBackOut(t *testing.T) {
	m := editModel(t, "t-9sa6")
	m.edit.menuIdx = int(fieldLabels)
	press(m, "enter") // list stage
	press(m, "esc")
	if m.edit.stage != stageMenu {
		t.Error("esc from a sub-editor must return to the menu")
	}
	press(m, "esc")
	if m.mode != modeNormal || m.edit != nil {
		t.Error("esc from the menu must close the overlay")
	}
}

// epicPickerModel is the shape that broke the picker: a task whose epic
// vocabulary is far taller than the terminal. The real board carries 101
// epics against a 50-row window, so every row past the fold — the footer
// included — used to be clipped away by editLayer's MaxHeight.
func epicPickerModel(t *testing.T, epics int, filed string) *Model {
	t.Helper()
	vocab := make([]board.EpicInfo, epics)
	for i := range vocab {
		vocab[i] = board.EpicInfo{ID: fmt.Sprintf("e-%04d", i), Title: fmt.Sprintf("エピック %04d", i)}
	}
	task := &board.Task{ID: "t-big", Status: "ready", Title: "the task under edit", Epic: filed}
	m := New(memstore.NewWith(board.NewBoard([]*board.Task{task}, vocab...)), Options{})
	m.w, m.h = 200, 50
	m.recompute()
	m.relayout()
	if !m.selectID("t-big", false) {
		t.Fatal("could not select t-big")
	}
	m.enterEdit()
	m.edit.menuIdx = int(fieldEpic)
	press(m, "enter")
	if m.edit.stage != stageList || m.edit.field != fieldEpic {
		t.Fatalf("stage = %d field = %d, want the epic list", m.edit.stage, m.edit.field)
	}
	return m
}

// cursorLine is the frame line carrying the list cursor. The board view draws
// no other ▌, so this is unambiguous while the overlay is up.
func cursorLine(t *testing.T, out string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "▌ ") {
			return l
		}
	}
	t.Fatalf("no list cursor in the frame:\n%s", out)
	return ""
}

func TestEditListKeepsTheFooterOnFrame(t *testing.T) {
	m := epicPickerModel(t, 101, "")
	out := frame(m)
	if !strings.Contains(out, "⏎ select · esc back") {
		t.Error("a vocabulary taller than the terminal must not push the key hints off-frame")
	}
	// The clipped tail has to SAY it is clipped, the way a clipped column does.
	if !strings.Contains(out, "below") {
		t.Errorf("the clipped tail must be announced:\n%s", out)
	}
}

func TestEditListWindowsAroundTheCursor(t *testing.T) {
	m := epicPickerModel(t, 101, "")
	press(m, strings.Split(strings.Repeat("down ", 60), " ")[:60]...)
	if m.edit.listIdx != 60 {
		t.Fatalf("listIdx = %d, want 60", m.edit.listIdx)
	}
	rows := m.editListRows(m.b.Task("t-big"))
	out := frame(m)
	if line := cursorLine(t, out); !strings.Contains(line, rows[60]) {
		t.Errorf("the cursor walked off-frame: cursor line %q, want %q", line, rows[60])
	}
	if !strings.Contains(out, "above") || !strings.Contains(out, "below") {
		t.Errorf("a window clipped at both ends must announce both:\n%s", out)
	}
	if !strings.Contains(out, "⏎ select · esc back") {
		t.Error("the key hints must survive a deep cursor")
	}

	// Selection semantics are untouched: up from row 0 still wraps to the last
	// row, and the render follows it to the tail.
	press(m, strings.Split(strings.Repeat("up ", 60), " ")[:60]...)
	press(m, "up")
	if m.edit.listIdx != len(rows)-1 {
		t.Fatalf("listIdx = %d, want a wrap to %d", m.edit.listIdx, len(rows)-1)
	}
	out = frame(m)
	if line := cursorLine(t, out); !strings.Contains(line, rows[len(rows)-1]) {
		t.Errorf("the wrapped cursor is off-frame: cursor line %q", line)
	}
	if !strings.Contains(out, "⏎ select · esc back") {
		t.Error("the key hints must survive the wrap to the tail")
	}
}

func TestEditEpicOpensOnTheCurrentEpic(t *testing.T) {
	// Row 0 is "(unfiled)", whose Enter PERSISTS `furrow set -e ""`. A filed
	// task must never open there.
	m := epicPickerModel(t, 101, "e-0042")
	if got := m.edit.listIdx; got != 43 {
		t.Errorf("listIdx = %d, want 43 (row 0 is (unfiled), so e-0042 is row 43)", got)
	}
	rows := m.editListRows(m.b.Task("t-big"))
	if line := cursorLine(t, frame(m)); !strings.Contains(line, rows[43]) {
		t.Errorf("the opening cursor is off-frame: cursor line %q", line)
	}

	// Unfiled still opens on "(unfiled)".
	m2 := epicPickerModel(t, 101, "")
	if got := m2.edit.listIdx; got != 0 {
		t.Errorf("unfiled listIdx = %d, want 0", got)
	}

	// An id the board cannot resolve falls back to row 0 rather than to a
	// wrong epic.
	m3 := epicPickerModel(t, 101, "e-gone")
	if got := m3.edit.listIdx; got != 0 {
		t.Errorf("stale membership listIdx = %d, want 0", got)
	}
}

func TestEditDepsRemovesAndAddsEdges(t *testing.T) {
	m := editModel(t, "t-jv3j") // waits on t-ehk7 (open) and t-t38k (done)
	m.edit.menuIdx = int(fieldDeps)
	press(m, "enter") // into the deps list
	if m.edit.stage != stageList || m.edit.field != fieldDeps {
		t.Fatalf("the deps sub-editor did not open: stage=%d field=%d", m.edit.stage, m.edit.field)
	}

	// ⏎ on a row removes the edge — deps have no re-add toggle.
	press(m, "enter")
	if got := strings.Join(m.b.Task("t-jv3j").Deps, ","); got != "t-t38k" {
		t.Errorf("deps = %q, want t-t38k (t-ehk7 removed)", got)
	}
	drainPersists(m, t)

	// a + a task id re-adds it, and the rebuilt graph blocks on it again.
	press(m, "a")
	m.edit.input.SetValue("t-ehk7")
	press(m, "enter")
	if got := strings.Join(m.b.Task("t-jv3j").Deps, ","); got != "t-t38k,t-ehk7" {
		t.Errorf("deps = %q, want t-t38k,t-ehk7", got)
	}
	if got := m.g.BlockedBy("t-jv3j"); len(got) != 1 || got[0] != "t-ehk7" {
		t.Errorf("the graph must be recomputed after the add: BlockedBy = %v", got)
	}
	// The apply lands back in the LIST, where ⏎ removes — the input's
	// "⏎ apply" note surviving past the add made the very next ⏎ silently
	// delete the dep under the cursor.
	if strings.Contains(m.status, "⏎ apply") {
		t.Errorf("the status still advertises the input's keys after the add: %q", m.status)
	}
	drainPersists(m, t)

	// Removing the LAST row must pull the cursor back inside the list — a
	// cursor parked one past the end makes the next ⏎ an invisible no-op.
	m.edit.listIdx = 1 // t-ehk7, the re-added tail
	press(m, "enter")
	if got := strings.Join(m.b.Task("t-jv3j").Deps, ","); got != "t-t38k" {
		t.Errorf("deps = %q, want t-t38k", got)
	}
	if m.edit.listIdx != 0 {
		t.Errorf("listIdx = %d after removing the last row, want 0", m.edit.listIdx)
	}
	drainPersists(m, t)
}

// A dep whose target is gone (archived, say) still renders — as the ? row —
// and its removal must go through: real furrow accepts `dep --rm` on a
// dangling edge, so a store adapter that validates the DEP id refuses the one
// removal that matters and rolls the board back.
func TestEditDepsRemovesADanglingDep(t *testing.T) {
	b := board.NewBoard([]*board.Task{
		{ID: "t-aaa", Title: "生きている方", Status: "backlog", Priority: 10, Deps: []string{"t-ghost"}},
	})
	m := New(memstore.NewWith(b), Options{})
	m.w, m.h = 240, 50
	m.recompute()
	m.relayout()
	if !m.selectID("t-aaa", false) {
		t.Fatal("could not select t-aaa")
	}
	m.enterEdit()
	m.edit.menuIdx = int(fieldDeps)
	press(m, "enter")

	if frame := ansiStrip(m.View().Content); !strings.Contains(frame, "? t-ghost") {
		t.Error("the dangling dep must render as the ? row")
	}

	press(m, "enter") // remove the edge
	if got := m.b.Task("t-aaa").Deps; len(got) != 0 {
		t.Fatalf("the dangling edge survived: %v", got)
	}
	drainPersists(m, t)
	if m.statusErr {
		t.Errorf("removing a dangling dep must persist cleanly, got %q", m.status)
	}
}

func TestEditDepsRefusesWhatFurrowWould(t *testing.T) {
	m := editModel(t, "t-jv3j")
	m.edit.menuIdx = int(fieldDeps)
	press(m, "enter")

	// An id not on the board: refused locally, nothing queued.
	press(m, "a")
	m.edit.input.SetValue("t-nope")
	press(m, "enter")
	if got := strings.Join(m.b.Task("t-jv3j").Deps, ","); got != "t-ehk7,t-t38k" {
		t.Errorf("a refused add must leave deps untouched: %q", got)
	}
	if !m.statusErr {
		t.Error("the refusal must land on the status row")
	}
	if m.inflight || len(m.pending) > 0 {
		t.Error("a locally refused add must not reach the persist queue")
	}

	// A cycle: t-pk4f already waits on t-jv3j, so t-jv3j → t-pk4f closes it.
	press(m, "a")
	m.edit.input.SetValue("t-pk4f")
	press(m, "enter")
	if got := strings.Join(m.b.Task("t-jv3j").Deps, ","); got != "t-ehk7,t-t38k" {
		t.Errorf("a cycle-closing add must refuse: %q", got)
	}
	if m.inflight || len(m.pending) > 0 {
		t.Error("a cycle-closing add must not reach the persist queue")
	}
}
