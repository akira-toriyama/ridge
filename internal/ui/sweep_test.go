package ui

import (
	"errors"

	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The row model: every section has a header, an empty section carries one
// non-selectable line, and the cursor walk skips both.
func TestSweepRowsSkipHeadersAndEmptyLines(t *testing.T) {
	s := &board.Sweep{
		Archivable: []board.SweepTask{{ID: "t-a"}, {ID: "t-b"}},
		Archived:   []board.SweepTask{{ID: "t-z"}},
	}
	rows := sweepRows(s)
	if got := sweepFirst(rows); got != sweepKey(sweepArchive, "t-a") {
		t.Fatalf("first = %q", got)
	}
	k := sweepFirst(rows)
	k = sweepStep(rows, k, +1)
	if k != sweepKey(sweepArchive, "t-b") {
		t.Errorf("step 1 = %q", k)
	}
	// Two empty sections (done-deps, unknown-keys) sit between t-b and t-z:
	// one step crosses their headers and empty lines.
	k = sweepStep(rows, k, +1)
	if k != sweepKey(sweepArchived, "t-z") {
		t.Errorf("step over the empty sections = %q", k)
	}
	if got := sweepStep(rows, k, +1); got != k {
		t.Errorf("stepping past the end moved to %q", got)
	}
	if got := sweepLast(rows); got != k {
		t.Errorf("last = %q", got)
	}
	// nil preview: four headers, four empty lines, nothing selectable.
	if rows := sweepRows(nil); len(rows) != 8 || sweepFirst(rows) != "" {
		t.Errorf("nil preview rows = %d, first = %q", len(rows), sweepFirst(rows))
	}
}

// X opens the sweep on the fixture: the previews are synchronous, so the
// frame carries the archive candidates and the satisfied dep edges at once,
// and the empty sections say so.
func TestSweepOpensWithTheFixturePreviews(t *testing.T) {
	m := boardModel(t, 240, 40)
	press(m, "X")
	if m.view != viewSweep {
		t.Fatal("X did not open the sweep")
	}
	if m.sweep == nil || len(m.sweep.Archivable) == 0 || len(m.sweep.DoneDeps) == 0 {
		t.Fatalf("fixture previews missing: %+v", m.sweep)
	}
	out := frame(m)
	for _, want := range []string{"⟨SWEEP⟩", "archivable (closed >30d)", "with satisfied deps", "no unknown keys parked", "the archive is empty", "t-t38k"} {
		if !strings.Contains(out, want) {
			t.Errorf("frame lacks %q", want)
		}
	}
	press(m, "esc")
	if m.view != viewBoard {
		t.Error("esc did not return to the board")
	}
}

// The archive write: x skips a row, ⏎ arms the gate naming the rest, a
// stray key cancels it, ⏎ ⏎ applies — and after the store-first write lands
// the archived rows leave the board and appear in the archive section.
func TestSweepArchiveGateSkipCancelAndApply(t *testing.T) {
	m := boardModel(t, 240, 40)
	press(m, "X")
	first := m.sweepSel
	all := sweepArchiveSet(m.sweep, nil)
	skipped := strings.TrimPrefix(first, sweepKey(sweepArchive, ""))

	press(m, "x")
	if !m.sweepSkip[skipped] {
		t.Fatalf("x did not skip %s", skipped)
	}
	if got := sweepArchiveSet(m.sweep, m.sweepSkip); len(got) != len(all)-1 {
		t.Fatalf("archive set after skip = %d, want %d", len(got), len(all)-1)
	}
	press(m, "enter")
	if m.sweepGate == nil {
		t.Fatal("⏎ did not arm the gate")
	}
	if !strings.Contains(frame(m), "⏎ confirms: furrow archive") {
		t.Error("the header does not carry the gate line")
	}
	press(m, "down")
	if m.sweepGate != nil {
		t.Fatal("a stray key must cancel the gate")
	}
	if m.sweepSel != first {
		t.Error("the cancelling key must not also move the cursor")
	}

	press(m, "enter", "enter")
	if m.sweepGate != nil {
		t.Fatal("the second ⏎ must consume the gate")
	}
	drainPersists(m, t)
	if m.b.Task(skipped) == nil {
		t.Errorf("the skipped %s must stay on the board", skipped)
	}
	for _, id := range all {
		if id == skipped {
			continue
		}
		if m.b.Task(id) != nil {
			t.Errorf("%s was archived but is still on the board", id)
		}
	}
	if m.sweep == nil || len(m.sweep.Archived) != len(all)-1 {
		t.Fatalf("the preview did not re-read after the write: %+v", m.sweep)
	}
	if !strings.Contains(frame(m), "archived — the archive store") || strings.Contains(frame(m), "the archive is empty") {
		t.Error("the archive section must list the retired tasks now")
	}
	// x is an ARCHIVE-row key: on any other section it explains and changes nothing.
	m.sweepSel = sweepKey(sweepArchived, all[1])
	marks := len(m.sweepSkip)
	press(m, "x")
	if len(m.sweepSkip) != marks {
		t.Error("x on an archived row must not mark a skip")
	}
	// Restore one: ⏎ ⏎ on the archived row brings it back to the done lane.
	press(m, "enter", "enter")
	drainPersists(m, t)
	if tk := m.b.Task(all[1]); tk == nil || tk.Status != m.b.DoneLane() {
		t.Errorf("unarchive did not bring %s back to the done lane: %+v", all[1], tk)
	}
}

// Tidy done-deps prunes every satisfied edge in one write; the board's deps
// and the preview both reflect it after the landing.
func TestSweepTidyDoneDepsPrunesTheClass(t *testing.T) {
	m := boardModel(t, 240, 40)
	press(m, "X")
	if len(m.sweep.DoneDeps) == 0 {
		t.Fatal("fixture has no satisfied dep edges to tidy")
	}
	victim := m.sweep.DoneDeps[0]
	m.sweepSel = sweepKey(sweepDoneDeps, victim.ID)
	press(m, "enter")
	if m.sweepGate == nil || !strings.Contains(m.sweepGate.what, "--done-deps") {
		t.Fatalf("gate = %+v", m.sweepGate)
	}
	press(m, "enter")
	drainPersists(m, t)
	tk := m.b.Task(victim.ID)
	for _, d := range victim.Deps {
		for _, have := range tk.Deps {
			if have == d {
				t.Errorf("%s still depends on done %s after tidy", victim.ID, d)
			}
		}
	}
	if len(m.sweep.DoneDeps) != 0 {
		t.Errorf("preview still lists %d tasks with satisfied deps", len(m.sweep.DoneDeps))
	}
}

// Every archive row skipped = nothing to send: the gate refuses to arm rather
// than queue a write furrow would answer with the id-less SWEEP.
func TestSweepRefusesAnEmptyArchiveSet(t *testing.T) {
	m := boardModel(t, 240, 40)
	press(m, "X")
	for _, tk := range m.sweep.Archivable {
		if m.sweepSkip == nil {
			m.sweepSkip = map[string]bool{}
		}
		m.sweepSkip[tk.ID] = true
	}
	press(m, "enter")
	if m.sweepGate != nil {
		t.Fatal("an all-skipped archive set must not arm a gate")
	}
	if !strings.Contains(m.status, "every archive row is skipped") {
		t.Errorf("status = %q", m.status)
	}
}

// A refused sweep write reports and rolls nothing back (store-first), and the
// second gesture while one is in flight is refused with the sweep's own word.
func TestSweepWriteRefusalIsStoreFirst(t *testing.T) {
	m, p := scriptedModel(t)
	p.epicErr, p.epicFailAt = errors.New("scripted refusal"), 1
	m.view = viewSweep
	m.sweep = &board.Sweep{Archivable: []board.SweepTask{{ID: "a"}}}
	m.sweepSel = sweepKey(sweepArchive, "a")
	press(m, "enter", "enter")
	if !m.inflight {
		t.Fatal("the write did not queue")
	}
	press(m, "enter", "enter")
	if !strings.Contains(m.status, "a sweep write is still in flight") {
		t.Errorf("status = %q, want the in-flight refusal", m.status)
	}
	drainPersists(m, t)
	if m.rollingBack {
		t.Error("a refused store-first write must not open the rollback window")
	}
	if !m.statusErr || !strings.Contains(m.status, "sweep: archive 1 task(s)") {
		t.Errorf("status = %q, want the refusal named", m.status)
	}
}

// A sweep write on the fixture must not discard the session's other edits:
// memstore's writes reshape the CURRENT snapshot, never Reload (the discard
// operation). Review of #88 measured a keyboard move reverting under an
// archive before this.
func TestSweepWriteKeepsTheSessionsOtherEditsOnTheFixture(t *testing.T) {
	m := boardModel(t, 240, 40)
	if _, err := m.b.MoveTo("t-ehk7", "ready", 0); err != nil {
		t.Fatal(err)
	}
	press(m, "X", "enter", "enter")
	drainPersists(m, t)
	if tk := m.b.Task("t-ehk7"); tk == nil || tk.Status != "ready" {
		t.Errorf("the archive write reverted the session's move: %+v", tk)
	}
	if len(m.sweep.Archived) == 0 {
		t.Error("the archive write did not land")
	}
}

// ctrl+c under an open gate quits (after the drain) instead of merely
// cancelling the gate — the escape hatch every other surface keeps.
func TestSweepGateLetsCtrlCThrough(t *testing.T) {
	m := boardModel(t, 240, 40)
	press(m, "X", "enter")
	if m.sweepGate == nil {
		t.Fatal("no gate")
	}
	c := m.onSweepKey(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if c == nil {
		t.Error("ctrl+c under the gate returned no quit command")
	}
}

// X while a write is queued defers the read (it would race the queue's furrow
// process) — and the drain then delivers it, on the refusal path that reloads
// nothing as much as on success. Measured before the fix: a refused write with
// nothing unread left the sweep at "nothing to sweep" for good.
func TestSweepDeferredReadArrivesWhenTheDrainEnds(t *testing.T) {
	m, p := scriptedModel(t)
	p.epicErr, p.epicFailAt = errors.New("scripted refusal"), 1
	// A store-first write in flight, then X.
	m.mode = modeNormal
	if c := m.epicWrite("epic set e-1", func(prov board.Provider) error { return prov.EpicSet("e-1", board.EpicPatch{Goal: new(string)}) }); c == nil {
		t.Fatal("the epic write did not queue")
	}
	press(m, "X")
	if m.view != viewSweep || m.sweep != nil || !m.sweepLoading {
		t.Fatalf("X under a queued write: view=%v sweep=%v loading=%v", m.view, m.sweep != nil, m.sweepLoading)
	}
	if out := frame(m); !strings.Contains(out, "reading the previews") || strings.Contains(out, "nothing old enough") || !strings.Contains(out, "not read yet") {
		t.Errorf("the deferred frame must say it is reading, not that there is nothing to sweep")
	}
	// Drive the loop the way the program does: every Cmd Update returns is
	// run and its message fed back, so the deferred read the refusal branch
	// batches in actually executes (drainPersists discards those Cmds).
	pump(m, m.firePersist())
	if m.inflight || len(m.pending) > 0 {
		t.Fatal("the queue did not drain")
	}
	if m.sweep == nil || m.sweepLoading {
		t.Errorf("the drain (a refused write, nothing unread) did not deliver the read: sweep=%v loading=%v", m.sweep != nil, m.sweepLoading)
	}
}

// pump runs cmd, feeds its message to Update and recurses on what comes back,
// unwrapping batches — a synchronous stand-in for the program loop.
func pump(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			pump(m, c)
		}
		return
	}
	_, next := m.Update(msg)
	pump(m, next)
}

// A board re-read that FAILS under a deferred sweep read is the one drain end
// that reads nothing: the header must stop claiming a read is in flight and
// name r instead (measured before: "reading the previews…" forever).
func TestSweepStalledReadNamesTheWayOut(t *testing.T) {
	m, _ := scriptedModel(t)
	m.view = viewSweep
	m.sweepLoading = true
	m.sweep = nil
	m.Update(reloadDoneMsg{label: "reload", err: errors.New("furrow ls timed out")})
	if m.sweepLoading {
		t.Error("the failed re-read left the loading claim up")
	}
	if out := frame(m); !strings.Contains(out, "previews not read — r reads them") || strings.Contains(out, "reading the previews") {
		t.Errorf("frame after a failed re-read must name r, got a stale claim")
	}
}
