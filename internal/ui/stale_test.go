package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// driftStore serves the fixture as a LIVE store whose records and members can
// move from outside the model between two of its reads — the shape of another
// session's furrow write, which the drill's refutation found ridge acting over
// (t-2wa3). Reload applies onReload to the served board and rebuilds nothing,
// so the drift is what the re-read finds.
type driftStore struct {
	board.Provider
	records  map[string]string
	onReload func(b *board.Board)
	reloads  int
	reopen   error
	// epicRemove is EpicRemove's scripted refusal on the APPLY (the preview
	// still answers), the shape of a box withdrawn under the gate.
	epicRemove error
	// rmPreview is the scripted refusal of the delete PREVIEW, a task's or a
	// box's: the shape of a target another writer already removed.
	rmPreview error
	// fixture makes the store answer Live() false, as memstore does.
	fixture bool
}

func newDriftStore() *driftStore {
	return &driftStore{Provider: memstore.New(), records: map[string]string{}}
}

func (d *driftStore) Live() bool { return !d.fixture }
func (d *driftStore) ReadBody(id string) (string, error) {
	if r, ok := d.records[id]; ok {
		return r, nil
	}
	return d.Provider.ReadBody(id)
}

func (d *driftStore) Reload() error {
	d.reloads++
	if d.onReload != nil {
		d.onReload(d.Board())
	}
	return nil
}

func (d *driftStore) EpicRemove(id string, o board.RemoveOptions) (board.RemoveReport, error) {
	if o.Apply && d.epicRemove != nil {
		return board.RemoveReport{}, d.epicRemove
	}
	if !o.Apply && d.rmPreview != nil {
		return board.RemoveReport{}, d.rmPreview
	}
	return d.Provider.EpicRemove(id, o)
}

func (d *driftStore) Remove(ids []string, o board.RemoveOptions) (board.RemoveReport, error) {
	if !o.Apply && d.rmPreview != nil {
		return board.RemoveReport{}, d.rmPreview
	}
	return d.Provider.Remove(ids, o)
}

func (d *driftStore) EpicReopen(id string) error {
	if d.reopen != nil {
		return d.reopen
	}
	return d.Provider.EpicReopen(id)
}

func driftOverlay(t *testing.T, d *driftStore, id string) *Model {
	t.Helper()
	m := New(d, Options{})
	m.w, m.h = 240, 50
	m.recompute()
	m.relayout()
	m.toggleSlice()
	m.sliceField = sliceEpic
	m.enterEpic(id)
	if m.epic == nil {
		t.Fatalf("the overlay did not open on %s", id)
	}
	return m
}

// `e` hands $EDITOR the store's record, not the loaded snapshot: another
// session's note appended since the load is in the buffer the editor opens.
func TestEditorOpensOnTheStoreRecordNotTheSnapshot(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	d := newDriftStore()
	m := driftOverlay(t, d, "e-fw2m")
	m.epic.menuIdx = int(epicFieldBody)
	press(m, "enter")
	moved := m.b.Epic("e-fw2m").Body + "\n他セッションが追記した段落\n"
	d.records["e-fw2m"] = moved
	if c := m.onEpicBodyKey(tea.KeyPressMsg{Code: 'e', Text: "e"}, m.b.Epic("e-fw2m")); c == nil {
		t.Fatal("e must return the editor command")
	}
	files, _ := filepath.Glob(filepath.Join(tmp, "furrow-poc-e-fw2m-*.md"))
	if len(files) != 1 {
		t.Fatalf("one temp buffer expected, found %v", files)
	}
	got, err := os.ReadFile(files[0])
	if err != nil || string(got) != moved {
		t.Errorf("the editor must open on the store's record:\n got %q\nwant %q (%v)", got, moved, err)
	}
}

// The save is fenced on the text the editor was handed: a record that moved
// meanwhile comes back stale with the buffer kept on disk; one that did not
// applies and the buffer is removed.
func TestEditorResultIsStaleWhenTheRecordMoved(t *testing.T) {
	d := newDriftStore()
	path := filepath.Join(t.TempDir(), "buf.md")
	if err := os.WriteFile(path, []byte("typed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := d.Board().Epic("e-fw2m").Body
	d.records["e-fw2m"] = base + "\nmoved\n"
	msg := editorResult(d, "e-fw2m", base, path, nil)
	if !msg.stale || msg.kept != path || msg.body != "typed\n" || msg.err != nil {
		t.Errorf("a moved record must come back stale with the buffer named: %+v", msg)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("the typed text must be kept on disk when refused")
	}
	d.records["e-fw2m"] = base
	msg = editorResult(d, "e-fw2m", base, path, nil)
	if msg.stale || msg.body != "typed\n" || msg.err != nil {
		t.Errorf("an unmoved record must apply: %+v", msg)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("an applied buffer's temp file is removed")
	}
	// A record gone from the store is stale too — furrow's edit would refuse
	// it, and the text is still the only copy.
	if err := os.WriteFile(path, []byte("typed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if msg := editorResult(d, "e-ghost", "", path, nil); !msg.stale || msg.kept != path {
		t.Errorf("a vanished record must come back stale: %+v", msg)
	}
}

// A buffer the editor left as it was handed is not a save: no apply, no
// queued write, the temp file removed — and that holds when the record
// moved meanwhile, since nothing was typed that the move could lose. `furrow
// edit --body` stamps `updated` for an identical body too, so the write
// ridge sent took a stale task off `furrow revisit` (t-zq7m, measured on a
// copy of the ridge-test store). The board is re-read all the same: the
// editor showed the store's record, which the board may be behind.
func TestAnUntouchedEditorBufferWritesNothing(t *testing.T) {
	d := newDriftStore()
	base := d.Board().Epic("e-fw2m").Body
	path := filepath.Join(t.TempDir(), "buf.md")
	for _, record := range []string{base, base + "\nmoved\n"} {
		if err := os.WriteFile(path, []byte(base), 0o600); err != nil {
			t.Fatal(err)
		}
		d.records["e-fw2m"] = record
		msg := editorResult(d, "e-fw2m", base, path, nil)
		if !msg.same || msg.stale || msg.err != nil {
			t.Errorf("an untouched buffer must come back same: %+v", msg)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Error("an untouched buffer's temp file is removed")
		}
	}

	d.records["e-fw2m"] = base
	m := driftOverlay(t, d, "e-fw2m")
	for _, rollingBack := range []bool{false, true} {
		m.rollingBack = rollingBack
		reloads := d.reloads
		_, c := m.Update(editorDoneMsg{id: "e-fw2m", same: true})
		if len(m.pending) != 0 || m.inflight || m.heldBody != nil {
			t.Errorf("rollingBack=%v: an untouched buffer must neither queue nor be held", rollingBack)
		}
		runCmd(m, c)
		if d.reloads != reloads+1 {
			t.Errorf("rollingBack=%v: the board must be re-read, reloads %d → %d", rollingBack, reloads, d.reloads)
		}
		if m.statusErr || !strings.Contains(m.status, "e-fw2m body unchanged — nothing written") {
			t.Errorf("rollingBack=%v: status = %q (err=%v)", rollingBack, m.status, m.statusErr)
		}
	}
	if m.b.Epic("e-fw2m").Body != base {
		t.Error("the body moved")
	}
}

// A stale result is refused on the status line, names the kept file, writes
// nothing, queues nothing, and re-reads the board so the moved record is
// what the stage shows.
func TestStaleEditorResultIsRefusedAndNamesTheKeptFile(t *testing.T) {
	d := newDriftStore()
	m := driftOverlay(t, d, "e-fw2m")
	before := m.b.Epic("e-fw2m").Body
	_, c := m.Update(editorDoneMsg{id: "e-fw2m", body: "# 箱\n\n書き直した\n", stale: true, kept: "/tmp/kept-e-fw2m.md"})
	if !m.statusErr || !strings.Contains(m.status, "/tmp/kept-e-fw2m.md") || !strings.Contains(m.status, "changed while the editor was open") {
		t.Errorf("status = %q (err=%v), want the refusal naming the kept file", m.status, m.statusErr)
	}
	if m.b.Epic("e-fw2m").Body != before || len(m.pending) != 0 || m.inflight {
		t.Error("a stale result must neither apply nor queue")
	}
	if c == nil {
		t.Fatal("the refusal must hand back the re-read")
	}
	runCmd(m, c)
	if d.reloads != 1 {
		t.Errorf("the board must be re-read after the refusal, reloads=%d", d.reloads)
	}
}

// The close gate counts from a re-read of a live store: the gate opens
// reading, ⏎ is refused until the read lands, and the count it then shows is
// the store's — three members another session moved back into work were
// "0 still open" on the snapshot (t-2wa3).
func TestCloseGateCountsFromAReRead(t *testing.T) {
	d := newDriftStore()
	m := driftOverlay(t, d, "e-fw2m")
	before := len(m.b.OpenMembers("e-fw2m"))
	d.onReload = func(b *board.Board) {
		if _, err := b.MoveTo("t-2tbn", "backlog", 0); err != nil {
			t.Fatal(err)
		}
	}
	m.epic.menuIdx = int(epicFieldClosed)
	_, read := m.Update(keyMsg("enter"))
	if m.epic.stage != stageGate || m.epic.closeFresh || read == nil {
		t.Fatalf("the gate must open reading on a live store: stage=%d fresh=%v cmd=%v", m.epic.stage, m.epic.closeFresh, read != nil)
	}
	if !strings.Contains(m.status, "re-reading the board") || !strings.Contains(frame(m), "re-reading the board") {
		t.Errorf("the gate must say it is reading: status=%q", m.status)
	}
	_, c := m.Update(keyMsg("enter"))
	if c != nil || m.statusErr || !strings.Contains(m.status, "re-reading") || m.epic.stage != stageGate || len(m.pending) != 0 {
		t.Errorf("⏎ before the read lands must be refused as a note: cmd=%v err=%v status=%q stage=%d", c != nil, m.statusErr, m.status, m.epic.stage)
	}
	m.Update(read())
	if d.reloads != 1 || !m.epic.closeFresh {
		t.Fatalf("the read must land and mark the gate fresh: reloads=%d fresh=%v", d.reloads, m.epic.closeFresh)
	}
	// The line moves on with the landing — the refusal was a note, not an
	// error the stage note would have to preserve.
	want := fmt.Sprintf("%d still open", before+1)
	if !strings.Contains(m.status, want) || !strings.Contains(frame(m), want) || strings.Contains(m.status, "re-reading") {
		t.Errorf("the gate must count the re-read board (%s): status=%q", want, m.status)
	}
	_, c = m.Update(keyMsg("enter"))
	if c == nil || !strings.Contains(m.status, "epic done e-fw2m — waiting for furrow") {
		t.Errorf("⏎ on the fresh gate must queue the close: cmd=%v status=%q", c != nil, m.status)
	}
}

// The fixture's snapshot IS its store, so the gate is fresh at once; and a
// closed box's row (reopen) reads its own state and never waits for a read.
func TestCloseGateIsFreshOnTheFixture(t *testing.T) {
	m := boxOverlay(t, "e-fw2m")
	m.epic.menuIdx = int(epicFieldClosed)
	_, c := m.Update(keyMsg("enter"))
	if c != nil || !m.epic.closeFresh || !strings.Contains(m.status, "still open") {
		t.Errorf("the fixture's gate must open fresh: cmd=%v fresh=%v status=%q", c != nil, m.epic.closeFresh, m.status)
	}
	press(m, "esc")
	d := newDriftStore()
	m = driftOverlay(t, d, "e-3v8p")
	m.epic.menuIdx = int(epicFieldClosed)
	_, c = m.Update(keyMsg("enter"))
	if c != nil || !m.epic.closeFresh || !strings.Contains(m.status, "reopen e-3v8p") {
		t.Errorf("reopen must not wait for a read: cmd=%v fresh=%v status=%q", c != nil, m.epic.closeFresh, m.status)
	}
}

// A write in flight blocks the read the way it blocks `r`: the gate says so
// and refuses ⏎, and no reload is fired under the queue.
func TestCloseGateRefusesToReadUnderAWriteInFlight(t *testing.T) {
	d := newDriftStore()
	m := driftOverlay(t, d, "e-fw2m")
	m.epic.menuIdx = int(epicFieldStanding)
	press(m, "enter", "enter")
	if !m.queueBusy() {
		t.Fatal("setup: the standing write must be in flight")
	}
	m.epic.menuIdx = int(epicFieldClosed)
	_, c := m.Update(keyMsg("enter"))
	if c != nil || m.epic.closeFresh || !strings.Contains(m.status, "a write is still in flight") || d.reloads != 0 {
		t.Errorf("the gate must not read under the queue: cmd=%v fresh=%v status=%q reloads=%d", c != nil, m.epic.closeFresh, m.status, d.reloads)
	}
}

// `e` does not read while a write is in flight: ridge's own queued note
// leaves the file behind the screen until it lands, so the editor would
// open without it and the save be refused as its own drift (review finding
// on the real board, an `a` then `e` inside the write's window).
func TestEditorRefusesToOpenUnderAWriteInFlight(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	d := newDriftStore()
	m := driftOverlay(t, d, "e-fw2m")
	m.epic.menuIdx = int(epicFieldBody)
	press(m, "enter", "a")
	m.epic.input.SetValue("いま書いた段落")
	press(m, "enter")
	if !m.queueBusy() {
		t.Fatal("setup: the note must be in flight")
	}
	if c := m.onEpicBodyKey(tea.KeyPressMsg{Code: 'e', Text: "e"}, m.b.Epic("e-fw2m")); c != nil || !m.statusErr || !strings.Contains(m.status, "a write is still in flight") {
		t.Errorf("e must wait for the write: cmd=%v status=%q", c != nil, m.status)
	}
	if files, _ := filepath.Glob(filepath.Join(tmp, "furrow-poc-*")); len(files) != 0 {
		t.Errorf("no editor buffer may be written under the queue: %v", files)
	}
	// The task path shares the guard.
	drainPersists(m, t)
	press(m, "esc", "esc")
	m2 := boardModel(t, 240, 50)
	press(m2, "n")
	m2.edit.input.SetValue("いま書いた段落")
	press(m2, "enter")
	if !m2.queueBusy() {
		t.Fatal("setup: the task note must be in flight")
	}
	if c := m2.onKey(keyMsg("e")); c != nil || !strings.Contains(m2.status, "a write is still in flight") {
		t.Errorf("a task's e must wait for the write too: cmd=%v status=%q", c != nil, m2.status)
	}
	drainPersists(m2, t)
}

// A refused store-first delete re-reads the board the way a refused box
// write does: the rm gate is the overlay's other store-first ⏎ (t-2wa3).
func TestARefusedDeleteReReadsTheBoard(t *testing.T) {
	d := newDriftStore()
	d.epicRemove = fmt.Errorf(`unknown epic "e-fw2m" (epic-not-found)`)
	m := driftOverlay(t, d, "e-fw2m")
	m.epic.menuIdx = int(epicFieldDelete)
	if _, c := m.Update(keyMsg("enter")); c != nil {
		m.Update(c()) // the live store's preview read lands
	}
	if m.epic.rm.report == nil {
		t.Fatalf("setup: the preview must have landed: %+v", m.epic.rm)
	}
	press(m, "enter")
	if m.epic.rm.report != nil && !m.epic.rm.report.References.Empty() {
		press(m, "enter") // arm --force, then the second ⏎ deletes
	}
	cmd := m.firePersist()
	if cmd == nil {
		t.Fatal("the delete must be queued")
	}
	_, after := m.Update(cmd())
	if !m.statusErr || !strings.Contains(m.status, "epic-not-found") {
		t.Fatalf("the refusal must be reported: status=%q", m.status)
	}
	if after == nil {
		t.Fatal("the refusal must hand back the re-read")
	}
	runCmd(m, after)
	if d.reloads != 1 {
		t.Errorf("the board must be re-read after the refusal, reloads=%d", d.reloads)
	}
}

// A refused delete PREVIEW re-reads the board too, for a box and for a task:
// furrow refusing to preview a target the board still shows is the same
// news as a refused apply, and the gate's ⏎ never reaches the apply's
// reloadOnFail from there (t-dbvy: a box removed elsewhere stayed on the
// board with its members filed under it).
func TestARefusedDeletePreviewReReadsTheBoard(t *testing.T) {
	t.Run("box", func(t *testing.T) {
		d := newDriftStore()
		d.rmPreview = fmt.Errorf(`unknown epic "e-fw2m" (epic-not-found)`)
		m := driftOverlay(t, d, "e-fw2m")
		m.epic.menuIdx = int(epicFieldDelete)
		_, c := m.Update(keyMsg("enter"))
		if c == nil {
			t.Fatal("a live store's preview is read off the UI thread")
		}
		_, after := m.Update(c())
		if !m.statusErr || !strings.Contains(m.status, "epic-not-found") {
			t.Fatalf("the refusal must be reported: status=%q", m.status)
		}
		if after == nil {
			t.Fatal("the refused preview must hand back the re-read")
		}
		runCmd(m, after)
		if d.reloads != 1 {
			t.Errorf("reloads = %d, want 1", d.reloads)
		}
		if !m.statusErr || !strings.Contains(m.status, "epic-not-found") {
			t.Errorf("the re-read must not take the refusal off the status row: %q", m.status)
		}
	})
	// The fixture's Reload is the discard of the session's edits: a refused
	// preview there must not fire it.
	t.Run("fixture", func(t *testing.T) {
		d := newDriftStore()
		d.fixture = true
		d.rmPreview = fmt.Errorf(`unknown epic "e-fw2m" (epic-not-found)`)
		m := driftOverlay(t, d, "e-fw2m")
		m.epic.menuIdx = int(epicFieldDelete)
		_, c := m.Update(keyMsg("enter"))
		runCmd(m, c)
		if d.reloads != 0 || !m.statusErr {
			t.Errorf("reloads = %d status = %q", d.reloads, m.status)
		}
	})
	t.Run("task", func(t *testing.T) {
		d := newDriftStore()
		d.rmPreview = fmt.Errorf("1 of 1 ids not found (not-found)")
		m := New(d, Options{})
		m.w, m.h = 240, 50
		m.recompute()
		m.relayout()
		if !m.selectID("t-9sa6", false) {
			t.Fatal("setup: select")
		}
		m.enterEdit()
		m.edit.menuIdx = int(fieldDelete)
		_, c := m.Update(keyMsg("enter"))
		if c == nil {
			t.Fatal("a live store's preview is read off the UI thread")
		}
		_, after := m.Update(c())
		if after == nil {
			t.Fatal("the refused preview must hand back the re-read")
		}
		runCmd(m, after)
		if d.reloads != 1 || !m.statusErr || !strings.Contains(m.status, "not-found") {
			t.Errorf("reloads = %d status = %q", d.reloads, m.status)
		}
	})
}

// The lifecycle row is one gate over two writes. A re-read that finds the
// box closed by another session must not turn an open "close" gate into a
// "reopen" gate under the user's hands: the ⏎ meant for the close reopened
// the box (t-r4at). The gate backs out and says what moved.
func TestCloseGateBacksOutWhenTheBoxWasClosedElsewhere(t *testing.T) {
	d := newDriftStore()
	m := driftOverlay(t, d, "e-fw2m")
	d.onReload = func(b *board.Board) { b.Epic("e-fw2m").Closed = board.Now() }
	m.epic.menuIdx = int(epicFieldClosed)
	_, read := m.Update(keyMsg("enter"))
	if read == nil || m.epic.stage != stageGate {
		t.Fatal("setup: the gate must open reading")
	}
	m.Update(read())
	if m.epic == nil || m.epic.stage != stageMenu {
		t.Fatalf("the gate must back out to the menu: %+v", m.epic)
	}
	if m.statusErr || !strings.HasSuffix(m.status, "box e-fw2m is closed now — this gate wrote nothing; ⏎ on the row reopens it") {
		t.Errorf("status = %q (err=%v)", m.status, m.statusErr)
	}
	if strings.Contains(frame(m), "reopen this box") {
		t.Error("no reopen gate may be open")
	}
	if len(m.pending) != 0 || m.inflight {
		t.Error("nothing may be queued")
	}

	// The other direction: a reopen gate over a box that a re-read finds
	// open. An unread refusal stays at the head of the line.
	m.epic.menuIdx = int(epicFieldClosed)
	press(m, "enter")
	if m.epic.stage != stageGate || !strings.Contains(frame(m), "reopen this box") {
		t.Fatalf("setup: the reopen gate must open (stage %d)", m.epic.stage)
	}
	d.onReload = func(b *board.Board) { b.Epic("e-fw2m").Closed = time.Time{} }
	m.fail("an unread refusal")
	runCmd(m, m.reloadCmd(""))
	if m.epic.stage != stageMenu || !m.statusErr ||
		m.status != "an unread refusal · box e-fw2m is open now — this gate wrote nothing; ⏎ on the row closes it" {
		t.Errorf("stage %d status = %q (err=%v)", m.epic.stage, m.status, m.statusErr)
	}
}

// A re-read that takes the overlay's target off the board closes the
// overlay and says so, for a box and for a task. Left to the next key the
// overlay stayed in charge undrawn — the mode badge over a bare board, one
// key of any kind swallowed (t-kehv). An unread refusal leads the line: a
// stale $EDITOR save names its kept file nowhere else.
func TestAReReadThatDropsTheOverlaysTargetClosesIt(t *testing.T) {
	t.Run("box", func(t *testing.T) {
		d := newDriftStore()
		m := driftOverlay(t, d, "e-fw2m")
		if _, err := d.EpicRemove("e-fw2m", board.RemoveOptions{Force: true, Apply: true}); err != nil {
			t.Fatal(err)
		}
		m.fail("e-fw2m: kept at /tmp/kept.md")
		m.Update(reloadDoneMsg{})
		if m.epic != nil || m.mode == modeEpic {
			t.Fatalf("the overlay must close with the re-read: epic=%v mode=%v", m.epic != nil, m.mode)
		}
		if !m.statusErr || !strings.HasPrefix(m.status, "e-fw2m: kept at /tmp/kept.md · ") || !strings.HasSuffix(m.status, "e-fw2m left the board — the overlay closed") {
			t.Errorf("status = %q", m.status)
		}
	})
	t.Run("task", func(t *testing.T) {
		d := newDriftStore()
		m := New(d, Options{})
		m.w, m.h = 240, 50
		m.recompute()
		m.relayout()
		if !m.selectID("t-9sa6", false) {
			t.Fatal("setup: select")
		}
		m.enterEdit()
		if _, err := d.Remove([]string{"t-9sa6"}, board.RemoveOptions{Force: true, Apply: true}); err != nil {
			t.Fatal(err)
		}
		m.Update(reloadDoneMsg{label: "reloaded", ms: 3})
		if m.edit != nil || m.mode != modeNormal {
			t.Fatalf("the overlay must close with the re-read: edit=%v mode=%v", m.edit != nil, m.mode)
		}
		if !m.statusErr || m.status != "t-9sa6 left the board — the overlay closed" {
			t.Errorf("status = %q", m.status)
		}
	})
}

// A sync's verdict on the row is said nowhere else: the orphan line joins it.
// The new-box modal has no target to lose and stays open through a re-read.
func TestTheOrphanLineKeepsASyncVerdictAndSparesTheNewBoxModal(t *testing.T) {
	d := newDriftStore()
	m := driftOverlay(t, d, "e-fw2m")
	if _, err := d.EpicRemove("e-fw2m", board.RemoveOptions{Force: true, Apply: true}); err != nil {
		t.Fatal(err)
	}
	m.Update(reloadDoneMsg{label: "synced", ms: 3, note: "NOT published 1: t-abcd"})
	if want := "synced · 3ms · NOT published 1: t-abcd · e-fw2m left the board — the overlay closed"; m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}

	m = boardModel(t, 240, 50)
	press(m, "s")
	m.sliceField = sliceEpic
	press(m, "A")
	if m.epic == nil || !m.epic.creating {
		t.Fatal("setup: A must open the new-box modal")
	}
	m.Update(reloadDoneMsg{})
	if m.epic == nil || !m.epic.creating {
		t.Error("a re-read closed the new-box modal")
	}
}

// A re-read that a refusal fired and that then fails keeps the refusal at
// the head of the line.
func TestAFailedReReadKeepsTheRefusalThatFiredIt(t *testing.T) {
	d := newDriftStore()
	m := driftOverlay(t, d, "e-fw2m")
	m.fail("e-fw2m: kept at /tmp/kept.md")
	m.Update(reloadDoneMsg{err: fmt.Errorf("reading a box body")})
	if !m.statusErr || !strings.HasPrefix(m.status, "e-fw2m: kept at /tmp/kept.md · reload: reading a box body") {
		t.Errorf("status = %q", m.status)
	}
}

// A refused store-first epic write re-reads the board: the refusal (a box
// withdrawn elsewhere) says the overlay's board is not the store's, and
// without the re-read ridge kept a box furrow no longer listed (t-2wa3).
func TestARefusedEpicWriteReReadsTheBoard(t *testing.T) {
	d := newDriftStore()
	d.reopen = fmt.Errorf(`unknown epic "e-3v8p" (epic-not-found)`)
	m := driftOverlay(t, d, "e-3v8p")
	m.epic.menuIdx = int(epicFieldClosed)
	press(m, "enter", "enter")
	cmd := m.firePersist()
	if cmd == nil {
		t.Fatal("the reopen must be queued")
	}
	_, after := m.Update(cmd())
	if !m.statusErr || !strings.Contains(m.status, "epic-not-found") {
		t.Fatalf("the refusal must be reported: status=%q", m.status)
	}
	if after == nil {
		t.Fatal("the refusal must hand back the re-read")
	}
	runCmd(m, after)
	if d.reloads != 1 {
		t.Errorf("the board must be re-read after the refusal, reloads=%d", d.reloads)
	}
}

// runCmd executes a Cmd the way the program loop would, one batch level deep,
// feeding every message back to the model.
func runCmd(m *Model, c tea.Cmd) {
	if c == nil {
		return
	}
	switch msg := c().(type) {
	case tea.BatchMsg:
		for _, sub := range msg {
			runCmd(m, sub)
		}
	default:
		m.Update(msg)
	}
}

// The gate's three not-fresh lines have headless frames: each names its
// state in the gate's pane and on the status row.
func TestCloseGateStatesHaveHeadlessFrames(t *testing.T) {
	for demo, want := range map[string]string{
		"epicclosereading": closeGateReading,
		"epicclosebusy":    closeGateBusy,
		"epicclosefailed":  closeGateFailed,
	} {
		out, err := New(memstore.New(), Options{}).Dump(240, 50, demo, true)
		if err != nil {
			t.Fatalf("%s: %v", demo, err)
		}
		if strings.Count(out, want) < 2 {
			t.Errorf("-demo %s must show %q in the gate and on the status row:\n%s", demo, want, out)
		}
	}
}
