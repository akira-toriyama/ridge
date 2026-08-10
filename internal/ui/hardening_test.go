package ui

import (
	"errors"
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// This file is the regression net for the 2026-08-10 independent review
// (t-74y3): every test here reproduces a confirmed finding and fails on the
// pre-fix code.

func ctrlC() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl} }

// The checklist persist closure used to re-read the live board at drain time
// — toggling an item and deleting it before the write landed was an index
// panic that killed the whole program (and a -race data race even when the
// index survived). The persist must carry the value captured at gesture time.
func TestChecklistPersistCarriesTheGestureTimeValue(t *testing.T) {
	m, p := scriptedModel(t)
	if !m.selectID("a", false) {
		t.Fatal("could not select a")
	}
	m.enterEdit()
	m.openField(fieldChecklist, m.b.Task("a"))
	m.edit.listIdx = 2 // "three", Done=true → the toggle records done=false

	// Toggle fires the write (inflight), but do NOT run its Cmd yet: the
	// store call has not happened when the next gesture arrives.
	if c := m.editListSelect(m.b.Task("a")); c == nil {
		t.Fatal("the toggle must fire the persist")
	}
	// Delete the same item while the toggle is still in flight. The local
	// checklist shrinks to 2 entries; index 2 no longer exists.
	m.edit.listIdx = 2
	if c := m.onEditListKey(keyMsg("d"), m.b.Task("a")); c != nil {
		t.Fatal("the delete must queue behind the in-flight toggle")
	}

	drainPersists(m, t) // pre-fix: panics with index out of range [2]

	if got := strings.Join(p.calls, ";"); got != "check a[2]=false;checkrm a[2]" {
		t.Errorf("store saw %q, want the gesture-time values", got)
	}
}

// Quick add used to return a bare Cmd, racing the strictly-serial queue with
// a second furrow process — and quitOrFlush could not see it, so quit
// abandoned it mid-write.
func TestQuickAddRidesThePersistQueue(t *testing.T) {
	m, p := scriptedModel(t)

	_, first, err := m.commitMove("a", "ready", "ready", 0, 3)
	if err != nil || first == nil {
		t.Fatal(err)
	}

	m.curLane = 0
	press(m, "a")
	if m.mode != modeAdd {
		t.Fatal("a must open the quick-add modal")
	}
	m.add.input.SetValue("追加タスク")
	if _, c := m.Update(keyMsg("enter")); c != nil {
		t.Fatal("the add must QUEUE behind the in-flight move, not fire its own Cmd")
	}
	if len(m.pending) != 2 {
		t.Fatalf("pending = %d, want the move and the add", len(m.pending))
	}
	if strings.Join(p.calls, ";") != "" {
		t.Fatalf("store saw %v before any Cmd ran", p.calls)
	}

	next := m.onPersistDone(first().(persistDoneMsg))
	if next == nil {
		t.Fatal("draining the move must fire the queued add")
	}
	rec := m.onPersistDone(next().(persistDoneMsg))
	if got := strings.Join(p.calls, ";"); got != "move a;add 追加タスク" {
		t.Errorf("store call order = %q", got)
	}
	if m.selectAfterReload != "t-new" {
		t.Errorf("selectAfterReload = %q, want the store's id for the reconcile to land on", m.selectAfterReload)
	}
	if rec == nil {
		t.Error("the drained queue must reconcile")
	}
}

// Quit while an add is outstanding used to leave immediately: a failed add
// after quit was never reported, a successful one never shown.
func TestQuitWaitsForAQueuedAdd(t *testing.T) {
	m, _ := scriptedModel(t)
	press(m, "a")
	m.add.input.SetValue("さよなら前の一枚")
	_, addCmd := m.Update(keyMsg("enter"))
	if addCmd == nil {
		t.Fatal("an idle queue must fire the add")
	}

	if c := m.quitOrFlush(); c != nil {
		t.Fatal("quit must wait for the outstanding add")
	}
	if !m.quitting {
		t.Fatal("the deferred quit must be recorded")
	}
	if !strings.Contains(m.status, "quitting once") {
		t.Errorf("status = %q — the deferred quit must SAY it is waiting; a silent wait reads as a hang", m.status)
	}

	done := m.onPersistDone(addCmd().(persistDoneMsg))
	if done == nil {
		t.Fatal("the drain must resolve the deferred quit")
	}
	if _, ok := done().(tea.QuitMsg); !ok {
		t.Errorf("after the drain the deferred quit must fire, got %T", done())
	}
}

// Options.Filter was assigned to the model but its verdict Cmd was dropped:
// against a live store the bar showed the text over an unfiltered board,
// forever. (The fixture answers synchronously, which is why -dump never saw
// it.)
func TestStartupFilterReachesTheLiveStore(t *testing.T) {
	p := newScriptedProvider(scriptedBoard)
	p.qIDs = []string{"a"}
	m := New(p, Options{Filter: "lane:ready"})
	if m.startupFilter == nil {
		t.Fatal("a startup filter on a live store must leave its verdict Cmd for Init")
	}

	// Run the debounce round the way the program loop would.
	c := m.onFilterTick(filterTickMsg{seq: m.qSeq})
	if c == nil {
		t.Fatal("the startup tick must query the store")
	}
	m.Update(c())
	if len(p.queries) != 1 || p.queries[0] != "lane:ready" {
		t.Fatalf("store queries = %v, want the startup filter", p.queries)
	}
	if m.countVisible() != 1 || !m.qMatched["a"] {
		t.Errorf("visible = %d, qMatched[a]=%v — the startup verdict must filter the board",
			m.countVisible(), m.qMatched["a"])
	}
}

// A verdict computed before an optimistic write must not land while that
// write is queued: it would blink the user's own edit off the board.
func TestFilterVerdictIsDroppedWhileWritesAreInFlight(t *testing.T) {
	m, _ := scriptedModel(t)
	m.applyFilter("lane:ready")
	m.onFilterResult(filterResultMsg{seq: m.qSeq, ids: []string{"a", "b", "c"}})
	if m.countVisible() != 3 {
		t.Fatalf("visible = %d, want 3", m.countVisible())
	}

	if _, c, err := m.commitMove("a", "ready", "ready", 0, 3); err != nil || c == nil {
		t.Fatal(err)
	}
	// This verdict predates the move; same seq, older truth.
	m.onFilterResult(filterResultMsg{seq: m.qSeq, ids: []string{"b", "c"}})
	if !m.qMatched["a"] {
		t.Error("a stale verdict landed over the queued optimistic write")
	}
}

// The board's hit-test (m.lay) describes board geometry only. Clicks and
// releases under a modal overlay, the full help, or another view used to fall
// through to invisible cards — a drag completed under the quick-add modal
// even PERSISTED the move.
func TestMouseIsInertUnderOverlaysAndOtherViews(t *testing.T) {
	m, p := scriptedModel(t)
	c := m.lay.Col("ready")
	if c == nil || len(c.Cards) == 0 {
		t.Fatal("no ready cards laid out")
	}
	card := c.Cards[0]
	click := tea.MouseClickMsg{X: card.X + 2, Y: card.Y + 1, Button: tea.MouseLeft}

	m.enterEdit()
	m.Update(click)
	if m.drag.armed {
		t.Error("a click on the edit overlay must not arm a board drag")
	}
	m.exitEdit()

	press(m, "a")
	m.Update(click)
	if m.drag.armed {
		t.Error("a click under the quick-add modal must not arm a board drag")
	}
	press(m, "esc")

	m.fullHelp = true
	m.Update(click)
	if m.drag.armed {
		t.Error("a click under the full help must not arm a board drag")
	}
	m.fullHelp = false

	m.view = viewTable
	m.Update(click)
	if m.drag.armed {
		t.Error("a click in the table view must not arm a BOARD drag")
	}
	m.view = viewBoard

	// Mid-gesture takeover: the release under an overlay must not commit.
	m.Update(click)
	if !m.drag.armed {
		t.Fatal("a plain board click must arm")
	}
	m.Update(tea.MouseMotionMsg{X: card.X + 12, Y: card.Y + 6, Button: tea.MouseLeft})
	m.fullHelp = true
	m.Update(tea.MouseReleaseMsg{X: card.X + 12, Y: card.Y + 6, Button: tea.MouseLeft})
	if m.drag.armed {
		t.Error("the release under the overlay must reset the drag")
	}
	if len(p.calls) != 0 {
		t.Errorf("store saw %v — a gesture cut by an overlay must not persist", p.calls)
	}
}

// bubbletea's raw mode delivers ctrl+c as an ordinary keystroke; every modal
// text input used to swallow it, leaving Esc as the only (undiscoverable)
// way out.
func TestCtrlCQuitsFromEveryModal(t *testing.T) {
	m, _ := scriptedModel(t)

	assertQuits := func(where string) {
		t.Helper()
		c := m.onKey(ctrlC())
		if c == nil {
			t.Fatalf("%s: ctrl+c produced no Cmd", where)
		}
		if _, ok := c().(tea.QuitMsg); !ok {
			t.Fatalf("%s: ctrl+c produced %T, want quit", where, c())
		}
	}

	press(m, "/")
	assertQuits("filter bar")
	m.mode = modeNormal

	press(m, "a")
	assertQuits("quick-add modal")
	m.mode, m.add = modeNormal, nil

	m.selectID("a", false)
	m.enterEdit()
	assertQuits("edit menu")
	m.openField(fieldTitle, m.b.Task("a"))
	assertQuits("edit text input")
	m.exitEdit()
}

// `r` used to fire a reload concurrently with in-flight writes; the snapshot
// then lost to the guard in onReloadDone and "reloading…" stayed on screen
// forever.
func TestReloadKeyRefusesWhileWritesAreInFlight(t *testing.T) {
	m, _ := scriptedModel(t)
	if _, c, err := m.commitMove("a", "ready", "ready", 0, 3); err != nil || c == nil {
		t.Fatal(err)
	}
	if c := m.onNormalKey(keyMsg("r")); c != nil {
		t.Fatal("r must refuse while a write is in flight")
	}
	if !strings.Contains(m.status, "writes in flight") {
		t.Errorf("status = %q — the refusal must be visible", m.status)
	}
}

// The rollback re-read is the only rollback there is: its own failure must
// say the board is showing an unsaved edit, not a generic "reload failed".
func TestRollbackReloadFailureIsHonest(t *testing.T) {
	m, _ := scriptedModel(t)
	m.onReloadDone(reloadDoneMsg{rollback: true, err: errors.New("furrow ls timed out")})
	if !m.statusErr || !strings.Contains(m.status, "unsaved") {
		t.Errorf("status = %q — a failed rollback must warn about the unsaved edit", m.status)
	}
}

// The measurer caches card heights per theme; the background-color answer
// used to swap the theme without rebinding it, splitting render geometry
// from hit-test geometry for the whole session.
func TestThemeSwapRebindsTheMeasurer(t *testing.T) {
	m, _ := scriptedModel(t)
	m.Update(tea.BackgroundColorMsg{Color: color.White}) // light terminal
	if m.ms.th != m.th {
		t.Error("the measurer must be rebound to the theme the renderer uses")
	}
}

// --- follow-up round: findings from the independent review OF the fixes ---

// A queued quick add has no optimistic effect, so when an earlier write's
// failure flushes the queue, the status message is the ONLY trace the typed
// title ever existed.
func TestFlushedQueueTailIsNamedInTheFailure(t *testing.T) {
	m, p := scriptedModel(t)
	p.moveErr = errors.New("no")

	_, cmd, err := m.commitMove("a", "ready", "ready", 0, 3)
	if err != nil || cmd == nil {
		t.Fatal(err)
	}
	press(m, "a")
	m.add.input.SetValue("消えては困る一枚")
	if _, c := m.Update(keyMsg("enter")); c != nil {
		t.Fatal("the add must queue behind the in-flight move")
	}

	rb := m.onPersistDone(cmd().(persistDoneMsg))
	if !strings.Contains(m.status, "dropped 1 queued") || !strings.Contains(m.status, "消えては困る一枚") {
		t.Errorf("status = %q — the flushed add must be named, it has no other trace", m.status)
	}
	if rb == nil {
		t.Error("the failed move still needs its rollback re-read")
	}
}

// An add that itself fails (with nothing optimistic flushed) must not fire a
// rollback re-read — there is nothing to roll back, and the re-read's own
// failure message would claim an unsaved edit that does not exist.
func TestFailedAddAloneSkipsTheRollbackReread(t *testing.T) {
	m, p := scriptedModel(t)
	p.addErr = errors.New("refused")
	press(m, "a")
	m.add.input.SetValue("だめな一枚")
	_, cmd := m.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("an idle queue must fire the add")
	}
	if rb := m.onPersistDone(cmd().(persistDoneMsg)); rb != nil {
		t.Error("a failed add with an empty tail must not schedule a rollback re-read")
	}
	if !m.statusErr || strings.Contains(m.status, "rolling back") {
		t.Errorf("status = %q — fail loudly, but do not claim a rollback", m.status)
	}
}

// The edit overlay deliberately opens the peek; the wheel must keep
// scrolling it there (the modality guard used to sit above the inPeek
// branch, making a long body unreadable while editing).
func TestPeekWheelScrollsUnderTheEditOverlay(t *testing.T) {
	m, _ := scriptedModel(t)
	m.selectID("a", false)
	m.enterEdit()
	m.vp.SetContent(strings.Repeat("行\n", 200))
	px, py, _, _ := m.peekBox()
	m.Update(tea.MouseWheelMsg{X: px + 2, Y: py + 2, Button: tea.MouseWheelDown})
	if m.vp.YOffset() == 0 {
		t.Error("the wheel must scroll the peek while the edit overlay is up")
	}
}

// A lifted card is not a modal overlay: the wheel keeps scrolling columns in
// move mode so the destination can be found.
func TestBoardWheelStillWorksInMoveMode(t *testing.T) {
	m := boardModel(t, 140, 12) // short enough that a column overflows
	var lane string
	for _, l := range m.b.Lanes() {
		if c := m.lay.Col(l.Name); c != nil && c.Hidden > 0 {
			lane = l.Name
			break
		}
	}
	if lane == "" {
		t.Skip("no overflowing column at this size")
	}
	m.enterMove()
	c := m.lay.Col(lane)
	before := c.Scroll
	m.Update(tea.MouseWheelMsg{X: c.X + 2, Y: boardTop + 2, Button: tea.MouseWheelDown})
	if m.scroll[lane] != before+1 {
		t.Errorf("scroll = %d, want %d — move mode must not freeze the wheel", m.scroll[lane], before+1)
	}
}

// ctrl+c must work even on the keystroke where a reconcile has just dropped
// the edited task (editTask closes the overlay and used to swallow it).
func TestCtrlCQuitsWhenTheEditedTaskVanished(t *testing.T) {
	m, _ := scriptedModel(t)
	m.selectID("a", false)
	m.enterEdit()
	m.b.Task("a").ID = "zz" // simulate the reconcile losing the id
	c := m.onKey(ctrlC())
	if c == nil {
		t.Fatal("ctrl+c after the task vanished produced no Cmd")
	}
	if _, ok := c().(tea.QuitMsg); !ok {
		t.Fatalf("got %T, want quit", c())
	}
}
