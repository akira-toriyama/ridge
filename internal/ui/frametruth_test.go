package ui

import (
	"strings"
	"testing"

	lg "charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

// States where the model and the frame disagreed: a cursor silently moved, a
// layer left open that nothing draws, an exemption granted nobody asked for, a
// refusal with no render site, and a key list that goes stale mid-gesture.

// `v` is a view switch, not a re-selection. Asking curTask() after mutating
// m.view returned table row 0 — the row just assigned — and wrote it back over
// the board cursor, lane and all.
func TestViewToggleCarriesTheSelectionBothWays(t *testing.T) {
	m := boardModel(t, 240, 60)

	// A selection that is neither row 0 nor in the first non-empty lane.
	var want string
	for _, lane := range []string{"ready", "backlog"} {
		if col := m.cols[lane]; len(col) > 1 {
			m.selectID(col[1].ID, false)
			want = col[1].ID
			break
		}
	}
	if want == "" {
		t.Skip("the fixture has no lane with two visible cards")
	}

	press(m, "v")
	if got := m.curTask(); got == nil || got.ID != want {
		id := "<nil>"
		if got != nil {
			id = got.ID
		}
		t.Errorf("board→table opened on %s, want the selected task %s", id, want)
	}

	press(m, "v")
	if got := m.curTask(); got == nil || got.ID != want {
		id := "<nil>"
		if got != nil {
			id = got.ID
		}
		t.Errorf("table→board landed on %s, want %s — `v` `v` moved the board cursor", id, want)
	}
}

// Moving in the table and coming back must carry the row, not discard it.
func TestViewToggleCarriesAMovedTableRowBackToTheBoard(t *testing.T) {
	m := boardModel(t, 240, 60)
	press(m, "v")
	press(m, "j")
	want := m.curTask()
	if want == nil {
		t.Skip("the table is empty")
	}
	press(m, "v")
	if got := m.curTask(); got == nil || got.ID != want.ID {
		id := "<nil>"
		if got != nil {
			id = got.ID
		}
		t.Errorf("the board cursor landed on %s after `v j v`, want %s", id, want.ID)
	}
}

// treeOpen renders only inside the peek, so peek=false/tree=true is a state no
// frame can show — and it cost the next esc AND the next `t` a dead press.
func TestClosingThePeekAlsoClosesTheTree(t *testing.T) {
	m := boardModel(t, 240, 60)
	press(m, "t") // opens the tree, which implies the peek
	if !m.treeOpen || !m.peekOpen {
		t.Fatalf("`t` left treeOpen=%v peekOpen=%v", m.treeOpen, m.peekOpen)
	}
	press(m, " ")
	if m.peekOpen {
		t.Fatal("space did not close the peek")
	}
	if m.treeOpen {
		t.Error("space closed the peek but left treeOpen set — an invisible state that eats the next esc and the next `t`")
	}
}

// A pin is a permanent filter exemption. On an unfiltered board nothing is
// hidden, so jumping and coming back must grant none.
func TestJumpBackPinsOnlyWhatTheFilterHides(t *testing.T) {
	m := boardModel(t, 240, 60)
	var start *board.Task
	for _, task := range m.b.Tasks() {
		if len(m.g.BlockedBy(task.ID)) > 0 {
			start = task
			break
		}
	}
	if start == nil {
		t.Skip("no blocked task in the fixture")
	}
	m.selectID(start.ID, false)
	m.jumpToBlocker()
	m.jumpBack()

	if len(m.pinned) != 0 {
		t.Errorf("`>` `<` on an unfiltered board left %d permanent filter exemptions: %v",
			len(m.pinned), pinIDs(m.pinned))
	}
	if strings.Contains(ansiStrip(m.View().Content), "pinned by jump") {
		t.Error(`the frame shows a "+N pinned by jump" chip for pins nothing needed`)
	}
}

// Closing the graph follows the same rule.
func TestClosingTheGraphPinsOnlyWhatTheFilterHides(t *testing.T) {
	m := graphModel(t, 240, 60)
	m.closeGraph()
	if len(m.pinned) != 0 {
		t.Errorf("closing the graph on an unfiltered board pinned %v", pinIDs(m.pinned))
	}
}

// The status row is m.status's only render site. A refusal landing while a card
// is lifted used to render nowhere at all, and esc then destroyed it.
func TestARefusalDuringAGestureReachesTheFrame(t *testing.T) {
	m := boardModel(t, 240, 60)
	if len(m.cols["backlog"]) == 0 {
		t.Skip("backlog is empty")
	}
	m.selectID(m.cols["backlog"][0].ID, false)
	press(m, "enter") // lift
	if m.mode != modeMove {
		t.Fatal("enter did not enter move mode")
	}

	m.fail("move t-x: store refused — rolling back")

	frame := ansiStrip(m.View().Content)
	if !strings.Contains(frame, "store refused") {
		t.Error("a persist refusal raised during move mode never reached the frame")
	}
	if !strings.Contains(frame, "MOVE") {
		t.Error("the refusal replaced the MOVE readout instead of riding alongside it")
	}

	// esc must not erase a warning the user has not had a chance to read.
	m.cancelMove()
	if !strings.Contains(ansiStrip(m.View().Content), "store refused") {
		t.Error("esc clobbered the unread refusal — the rollback lands silently again")
	}

	// The DRAG half of the same fix, which the move-only body left unpinned:
	// deleting the warning from both drag branches was green.
	t.Run("during a drag", func(t *testing.T) {
		for _, where := range []string{"over a column", "off the board"} {
			m := boardModel(t, 240, 60)
			c := m.lay.Col("backlog")
			if c == nil || len(c.Cards) == 0 {
				t.Skip("backlog is empty")
			}
			x, y := c.X+3, c.Top+1
			if where == "off the board" {
				y = rowColHdr
			}
			dragFrom(t, m, "backlog", x, y)
			m.fail("move t-x: store refused — rolling back")

			frame := ansiStrip(m.View().Content)
			if !strings.Contains(frame, "store refused") {
				t.Errorf("%s: a refusal raised mid-drag never reached the frame", where)
			}
			if !strings.Contains(frame, "DRAG") {
				t.Errorf("%s: the refusal replaced the DRAG readout instead of riding alongside it", where)
			}
		}
	})
}

// The bottom row makes imperative key claims; they must be true at read time.
func TestTheStatusRowFollowsTheEditStage(t *testing.T) {
	m := boardModel(t, 240, 60)
	if len(m.cols["backlog"]) == 0 {
		t.Skip("backlog is empty")
	}
	m.selectID(m.cols["backlog"][0].ID, false)
	m.enterEdit()
	if m.edit == nil {
		t.Fatal("the edit overlay did not open")
	}
	if !strings.Contains(m.status, "pick a field") {
		t.Fatalf("the menu stage should advertise picking a field; got %q", m.status)
	}

	t.Run("list stage", func(t *testing.T) {
		m.openField(fieldChecklist, m.editTask())
		if m.edit.stage != stageList {
			t.Skip("checklist did not open a list stage")
		}
		if strings.Contains(m.status, "pick a field") {
			t.Errorf("the checklist sub-editor still advertises the menu's keys: %q", m.status)
		}
		if !strings.Contains(m.status, "esc back") {
			t.Errorf("esc goes BACK to the menu here, but the row says %q", m.status)
		}
	})

	t.Run("esc returns to the menu wording", func(t *testing.T) {
		press(m, "esc")
		if m.edit == nil || m.edit.stage != stageMenu {
			t.Fatal("esc did not return to the menu")
		}
		if !strings.Contains(m.status, "pick a field") {
			t.Errorf("back at the menu the row should say so; got %q", m.status)
		}
	})

	t.Run("pick stage", func(t *testing.T) {
		m.openField(fieldValue, m.editTask())
		if m.edit.stage != stageGate {
			t.Skip("value did not open a pick stage")
		}
		if strings.Contains(m.status, "pick a field") {
			t.Errorf("stageGate makes ⏎ a literal no-op, but the row still advertises it: %q", m.status)
		}
	})
}

// The slice panel's epic rows are composed then truncated again by the
// renderer. Hard-coding a 7-cell suffix budget ate the count's digits and the
// stuck marker as soon as either grew.
func TestSliceEpicRowsKeepTheirCountAndStuckMarker(t *testing.T) {
	cases := []board.EpicInfo{
		{ID: "e-1", Title: "短い", Done: 6, Total: 18},
		{ID: "e-2", Title: "ridge: TUI v1 — 実 furrow に接続し CI が立つ", Done: 15, Total: 16, Stuck: true},
		{ID: "e-3", Title: "a very long ASCII epic title that will not fit", Done: 100, Total: 250, Stuck: true},
		{ID: "e-4", Title: "日本語のとても長いエピックのタイトルです", Done: 999, Total: 999},
	}
	lanes := []board.Lane{{Name: "backlog"}}
	b := board.NewStoreBoard(lanes, nil, cases, true, "")

	m := boardModel(t, 240, 60)
	m.b = b
	m.recompute()
	m.sliceField = sliceEpic

	rows := m.sliceRows()
	if len(rows) != len(cases) {
		t.Fatalf("got %d epic rows, want %d", len(rows), len(cases))
	}
	for i, r := range rows {
		e := cases[i]
		// The renderer truncates to slicePanelW-4; anything wider loses its tail.
		if w := lg.Width(r.display); w > slicePanelW-4 {
			t.Errorf("%s renders %d cells, over the %d the panel gives it: %q",
				e.ID, w, slicePanelW-4, r.display)
		}
		count := itoa(e.Done) + "/" + itoa(e.Total)
		if !strings.Contains(r.display, count) {
			t.Errorf("%s lost its progress count %q: %q", e.ID, count, r.display)
		}
		if e.Stuck && !strings.HasSuffix(r.display, "!") {
			t.Errorf("%s is stuck but the row does not say so: %q", e.ID, r.display)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// The stage note must not erase a refusal the user has not read — the same rule
// this branch gave cancelMove. A rejected edit reports through m.fail, and the
// stage change that follows would wipe it.
func TestTheEditStageNoteDoesNotClobberARefusal(t *testing.T) {
	m := boardModel(t, 240, 60)
	if len(m.cols["backlog"]) == 0 {
		t.Skip("backlog is empty")
	}
	m.selectID(m.cols["backlog"][0].ID, false)
	m.enterEdit()
	if m.edit == nil {
		t.Fatal("the edit overlay did not open")
	}

	m.fail("value: store refused the write — rolling back")
	m.openField(fieldChecklist, m.editTask())

	if !strings.Contains(m.status, "store refused") {
		t.Errorf("opening a field erased the unread refusal; the row now reads %q", m.status)
	}
	if !m.statusErr {
		t.Error("the refusal lost its error styling")
	}
}

// Esc out of a text input lands on a different stage, so the row has to follow.
func TestEscapingATextInputRenotesTheStage(t *testing.T) {
	m := boardModel(t, 240, 60)
	if len(m.cols["backlog"]) == 0 {
		t.Skip("backlog is empty")
	}
	m.selectID(m.cols["backlog"][0].ID, false)
	m.enterEdit()

	m.openField(fieldTitle, m.editTask()) // stageInput
	if m.edit.stage != stageInput {
		t.Fatalf("title did not open a text input, stage=%v", m.edit.stage)
	}
	if !strings.Contains(m.status, "apply") {
		t.Errorf("the input stage should advertise applying; got %q", m.status)
	}

	press(m, "esc") // back to the menu
	if m.edit == nil || m.edit.stage != stageMenu {
		t.Fatal("esc did not return to the menu")
	}
	if !strings.Contains(m.status, "pick a field") {
		t.Errorf("escaping the input left the row on the input's keys: %q", m.status)
	}

	// The OTHER exit: submitting an empty value also backs out to the list,
	// where ⏎ toggles rather than applies. Only the esc exit re-noted, so the
	// row kept "⏎ apply" while ⏎ had become a board write.
	m.openField(fieldLabels, m.editTask())
	if m.edit.stage != stageList {
		t.Fatalf("labels did not open a list, stage=%v", m.edit.stage)
	}
	press(m, "a") // new-label input
	if m.edit.stage != stageInput {
		t.Fatalf("`a` did not open the input, stage=%v", m.edit.stage)
	}
	press(m, "enter") // empty submission
	if m.edit.stage != stageList {
		t.Fatalf("an empty submission left the stage at %v", m.edit.stage)
	}
	if strings.Contains(m.status, "apply") {
		t.Errorf("after an empty submission the row still claims ⏎ applies, but ⏎ now toggles: %q", m.status)
	}
	if !strings.Contains(m.status, "toggle") {
		t.Errorf("the row does not name the list stage's keys: %q", m.status)
	}
}
