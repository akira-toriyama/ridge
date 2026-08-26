package ui

import (
	"strings"
	"testing"
)

// commitAdd presses Enter in the modal and completes the queued add the way
// the program loop would: run the persist Cmd, deliver its result. (press
// deliberately drops returned cmds — running them would also execute cursor
// blink Ticks and sleep the suite.)
func commitAdd(t *testing.T, m *Model) {
	t.Helper()
	_, cmd := m.Update(keyMsg("enter"))
	if cmd == nil {
		return // refused in-modal (empty title), or queued behind an in-flight write
	}
	msg := cmd()
	res, ok := msg.(persistDoneMsg)
	if !ok {
		t.Fatalf("enter in the modal returned %T, want the queued persist", msg)
	}
	m.Update(res)
}

func TestInheritContextLiftsOnlySingleValuedTokens(t *testing.T) {
	tests := []struct {
		q                 string
		label, epic, repo string
	}{
		{q: "", label: "", epic: "", repo: ""},
		{q: "label:ui", label: "ui"},
		{q: "label:ui epic:e-fw2m repo:vista", label: "ui", epic: "e-fw2m", repo: "vista"},
		// OR'd and negated tokens are "one of these", not a value to stamp.
		{q: "label:ui,dx"},
		{q: "-label:ui"},
		{q: "is:blocked 自由語 label:ui", label: "ui"},
	}
	for _, tc := range tests {
		l, e, r := inheritContext(tc.q)
		if l != tc.label || e != tc.epic || r != tc.repo {
			t.Errorf("inheritContext(%q) = (%q,%q,%q), want (%q,%q,%q)",
				tc.q, l, e, r, tc.label, tc.epic, tc.repo)
		}
	}
}

func TestQuickAddCreatesInTheFocusedLaneAndSelects(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.curLane = m.b.LaneIndex("ready")
	before := len(m.b.Tasks())

	press(m, "a")
	if m.mode != modeAdd || m.add == nil {
		t.Fatal("a must open the quick-add modal")
	}
	if m.add.opts.Lane != "ready" {
		t.Errorf("lane = %q, want the focused lane", m.add.opts.Lane)
	}
	m.add.input.SetValue("新しいタスク")
	commitAdd(t, m)

	if len(m.b.Tasks()) != before+1 {
		t.Fatalf("board has %d tasks, want %d", len(m.b.Tasks()), before+1)
	}
	cur := m.curTask()
	if cur == nil || cur.Title != "新しいタスク" {
		t.Fatalf("selection = %+v, want the new task", cur)
	}
	if cur.Status != "ready" {
		t.Errorf("new task landed in %q, want ready", cur.Status)
	}
	// Appended at the lane's end, the way GH's + row does.
	lane := m.b.LaneTasks("ready")
	if lane[len(lane)-1].ID != cur.ID {
		t.Error("the new task must append at the lane's end")
	}
}

func TestQuickAddInheritsTheFilterContext(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.applyFilter("label:bbq")
	press(m, "a")
	if m.add.opts.Label != "bbq" {
		t.Fatalf("label = %q, want bbq inherited from the filter", m.add.opts.Label)
	}
	m.add.input.SetValue("フィルタ文脈つき")
	commitAdd(t, m)

	cur := m.curTask()
	if cur == nil || cur.Title != "フィルタ文脈つき" {
		t.Fatalf("selection = %+v", cur)
	}
	if !containsStrUI(cur.Labels, "bbq") {
		t.Errorf("labels = %v, want the inherited bbq", cur.Labels)
	}
	// The new card must be under the cursor even though... it MATCHES here;
	// the pin guarantee is what selectID(id, true) carries for the miss case.
	if !m.taskVisible(cur) {
		t.Error("the new task must be visible under the active filter")
	}
}

func TestQuickAddRefusesAnEmptyTitle(t *testing.T) {
	m := boardModel(t, 240, 50)
	before := len(m.b.Tasks())
	press(m, "a")
	m.add.input.SetValue("   ")
	commitAdd(t, m)
	if m.mode != modeAdd {
		t.Error("an empty title must keep the modal open")
	}
	if len(m.b.Tasks()) != before {
		t.Error("an empty title must create nothing")
	}
	press(m, "esc")
	if m.mode != modeNormal || m.add != nil {
		t.Error("esc must close the modal")
	}
}

func TestQuickAddModalRendersChips(t *testing.T) {
	m := boardModel(t, 240, 50)
	if err := m.demoState("add"); err != nil {
		t.Fatal(err)
	}
	out := frame(m)
	if !strings.Contains(out, "add item") {
		t.Error("the modal header is missing")
	}
	if !strings.Contains(out, "label bbq") {
		t.Error("the inherited label chip is missing — inheritance must be visible, never silent")
	}
	// The demo line is longer than the input's window, which scrolls to the
	// cursor — so the frame proves the TAIL of the typed line, not its head.
	if !strings.Contains(out, "effort:高") {
		t.Error("the typed line is missing from the modal")
	}
}

func TestQuickAddFromTheTableUsesTheStoreDefaultLane(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.view = viewTable
	press(m, "a")
	if m.mode != modeAdd {
		t.Fatal("a must open the modal on the table view too")
	}
	if m.add.opts.Lane != "" {
		t.Errorf("table add lane = %q, want the store default (empty)", m.add.opts.Lane)
	}
	m.add.input.SetValue("表から起票")
	commitAdd(t, m)
	cur := m.curTask()
	if cur == nil || cur.Title != "表から起票" {
		t.Fatalf("selection = %+v", cur)
	}
}
