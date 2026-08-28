package ui

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"

	tea "charm.land/bubbletea/v2"
)

// scriptedProvider exercises the optimistic queue without a furrow binary:
// it records every persist in call order and serves a rebuilt "store truth"
// on Reload, so a rollback is observable as the board reverting.
type scriptedProvider struct {
	mu        sync.Mutex
	truth     func() *board.Board
	current   *board.Board
	calls     []string
	moves     []scriptedMove
	queries   []string
	qIDs      []string // Query's scripted verdict
	moveErr   error
	addErr    error
	addFailAt int
	addCalls  int
	// epicErr is returned by the epicFailAt'th epic write (1-based; 0 = never);
	// epicPrev is what EpicDeactivate suggests.
	epicErr    error
	epicFailAt int
	epicCalls  int
	epicPrev   board.EpicPrevious
}

type scriptedMove struct{ id, lane, before, after string }

func newScriptedProvider(truth func() *board.Board) *scriptedProvider {
	return &scriptedProvider{truth: truth, current: truth()}
}

func (p *scriptedProvider) Board() *board.Board {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.current
}

func (p *scriptedProvider) Reload() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = p.truth()
	return nil
}

func (p *scriptedProvider) Sync() error { return nil }

func (p *scriptedProvider) Query(raw string) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queries = append(p.queries, raw)
	return p.qIDs, nil
}

func (p *scriptedProvider) PersistFields(_ string, _ board.FieldPatch) error   { return nil }
func (p *scriptedProvider) PersistCheckAdd(_, _ string) error                  { return nil }
func (p *scriptedProvider) PersistCheckReword(_ string, _ int, _ string) error { return nil }
func (p *scriptedProvider) PersistDepAdd(_, _ string) error                    { return nil }
func (p *scriptedProvider) PersistDepRm(_, _ string) error                     { return nil }
func (p *scriptedProvider) Live() bool                                         { return true }

func (p *scriptedProvider) PersistCheckRm(id string, i int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, fmt.Sprintf("checkrm %s[%d]", id, i))
	return nil
}

// addFailAt is 1-based and 0 means "every call fails when addErr is set" — the
// shape most tests want. Scripting a specific call matters for the drain where
// one add LANDS and the next is refused: the Cmd runs long after the enqueue, so
// flipping addErr between enqueues would refuse both.
func (p *scriptedProvider) Add(title string, _ board.AddOptions) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "add "+title)
	p.addCalls++
	if p.addErr != nil && (p.addFailAt == 0 || p.addCalls == p.addFailAt) {
		return "", p.addErr
	}
	return "t-new", nil
}

func (p *scriptedProvider) PersistMove(id, lane, beforeID, afterID string) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "move "+id)
	p.moves = append(p.moves, scriptedMove{id, lane, beforeID, afterID})
	return nil, p.moveErr
}

func (p *scriptedProvider) PersistDone(id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "done "+id)
	return nil
}

func (p *scriptedProvider) PersistCheck(id string, i int, done bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, fmt.Sprintf("check %s[%d]=%v", id, i, done))
	return nil
}

func (p *scriptedProvider) PersistBody(id, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "body "+id)
	return nil
}

func (p *scriptedProvider) PersistNote(id, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "note "+id)
	return nil
}

// The epic family is store-first: it records the call and, when the scenario
// asks for it, refuses. epicFailAt is 1-based, so a test can script "the first
// lands, the second is refused" — the drain shape the landed re-read exists for.
func (p *scriptedProvider) epicCall(label string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, label)
	p.epicCalls++
	if p.epicErr != nil && p.epicCalls == p.epicFailAt {
		return p.epicErr
	}
	return nil
}

func (p *scriptedProvider) EpicAdd(title string, _ board.EpicAddOptions) (string, error) {
	if err := p.epicCall("epicadd " + title); err != nil {
		return "", err
	}
	return "e-new", nil
}

func (p *scriptedProvider) EpicSet(id string, _ board.EpicPatch) error {
	return p.epicCall("epicset " + id)
}

func (p *scriptedProvider) EpicActivate(id, reason string) error {
	return p.epicCall("epicactivate " + id + " reason=" + reason)
}

func (p *scriptedProvider) EpicDeactivate(id string) (board.EpicPrevious, error) {
	if err := p.epicCall("epicdeactivate " + id); err != nil {
		return board.EpicPrevious{}, err
	}
	return p.epicPrev, nil
}

// The lifecycle pair rides the same script: done answers the previous-active
// suggestion the way deactivate does, reopen answers nothing but the verdict.
func (p *scriptedProvider) EpicDone(id string) (board.EpicPrevious, error) {
	if err := p.epicCall("epicdone " + id); err != nil {
		return board.EpicPrevious{}, err
	}
	return p.epicPrev, nil
}

func (p *scriptedProvider) EpicReopen(id string) error {
	return p.epicCall("epicreopen " + id)
}

func (p *scriptedProvider) EpicDepAdd(id, dep string) error {
	return p.epicCall("epicdep " + id + " " + dep)
}

func (p *scriptedProvider) EpicDepRm(id, dep string) error {
	return p.epicCall("epicdeprm " + id + " " + dep)
}

func scriptedBoard() *board.Board {
	return board.NewBoard([]*board.Task{
		{ID: "a", Title: "a", Status: "ready", Priority: 10, Checklist: []board.ChecklistItem{
			{Text: "one"}, {Text: "two"}, {Text: "three", Done: true},
		}},
		{ID: "b", Title: "b", Status: "ready", Priority: 20},
		{ID: "c", Title: "c", Status: "ready", Priority: 30},
		{ID: "z", Title: "z", Status: "backlog", Priority: 10},
	})
}

func scriptedModel(t *testing.T) (*Model, *scriptedProvider) {
	t.Helper()
	p := newScriptedProvider(scriptedBoard)
	m := New(p, Options{})
	m.w, m.h = 140, 40
	m.recompute()
	m.relayout()
	return m, p
}

// Two gestures in a row must persist strictly in order, one in flight at a
// time — the second write's --before anchor descends from the first one's
// outcome, so concurrency here would invert intent.
func TestPersistQueueSerializesInOrder(t *testing.T) {
	m, p := scriptedModel(t)

	moved, cmd1, err := m.commitMove("a", "ready", "ready", 3)
	if err != nil || !moved || cmd1 == nil {
		t.Fatalf("first move: moved=%v cmd=%v err=%v", moved, cmd1, err)
	}
	if got := laneIDs(m.b, "ready"); got != "b,c,a" {
		t.Fatalf("optimistic order = %s, want b,c,a", got)
	}

	// A second gesture while the first write is in flight: queued, no new Cmd.
	moved, cmd2, err := m.commitMove("c", "ready", "ready", 0)
	if err != nil || !moved {
		t.Fatalf("second move: moved=%v err=%v", moved, err)
	}
	if cmd2 != nil {
		t.Fatal("second persist must queue behind the in-flight one, not fire")
	}
	if got := laneIDs(m.b, "ready"); got != "c,b,a" {
		t.Fatalf("optimistic order = %s, want c,b,a", got)
	}
	if len(p.calls) != 0 {
		t.Fatalf("store saw %v before any Cmd ran — persists must live in Cmds, not Update", p.calls)
	}

	// Drain: first Cmd completes, its handler fires the queued second.
	next := m.onPersistDone(cmd1().(persistDoneMsg))
	if next == nil {
		t.Fatal("draining must fire the queued write")
	}
	rec := m.onPersistDone(next().(persistDoneMsg))
	if strings.Join(p.calls, ";") != "move a;move c" {
		t.Errorf("persist order = %v", p.calls)
	}
	if rec == nil {
		t.Error("a drained queue on a live provider must reconcile")
	}

	// The anchors describe the optimistic board at each gesture's own time.
	if p.moves[0] != (scriptedMove{"a", "ready", "", "c"}) {
		t.Errorf("first anchors = %+v, want after c", p.moves[0])
	}
	if p.moves[1] != (scriptedMove{"c", "ready", "b", ""}) {
		t.Errorf("second anchors = %+v, want before b", p.moves[1])
	}
}

// A failed persist means the optimistic board lied: everything queued behind
// it is dropped and the store re-read IS the rollback. The second queued
// gesture is load-bearing: with only one write in the test, the flush line
// (`m.pending = nil`) was never exercised, and the "rollback keeps the queued
// writes" mutation survived the suite.
func TestPersistFailureRollsBackFromTheStore(t *testing.T) {
	m, p := scriptedModel(t)
	p.moveErr = errors.New("schema gate says no")

	_, cmd, err := m.commitMove("a", "ready", "ready", 3)
	if err != nil || cmd == nil {
		t.Fatal(err)
	}
	if got := laneIDs(m.b, "ready"); got != "b,c,a" {
		t.Fatalf("optimistic order = %s", got)
	}
	// A second gesture, queued behind the doomed one: its anchors descend
	// from a board state the store will refuse, so it must be flushed.
	if _, queued, err := m.commitMove("c", "ready", "ready", 0); err != nil || queued != nil {
		t.Fatalf("second move must queue silently: cmd=%v err=%v", queued, err)
	}

	rb := m.onPersistDone(cmd().(persistDoneMsg))
	if rb == nil {
		t.Fatal("a failed persist must schedule the rollback re-read")
	}
	if !m.statusErr {
		t.Error("a failed persist must surface as an error, not a shrug")
	}
	if len(m.pending) != 0 {
		t.Fatalf("the queued tail must be flushed with the failure, pending=%d", len(m.pending))
	}
	m.onReloadDone(rb().(reloadDoneMsg))
	if got := laneIDs(m.b, "ready"); got != "a,b,c" {
		t.Errorf("after rollback ready = %s, want the store's a,b,c", got)
	}
	if len(m.pending) != 0 || m.inflight {
		t.Error("the queue must be empty after a rollback")
	}
	if got := strings.Join(p.calls, ";"); got != "move a" {
		t.Errorf("store saw %q — the flushed write must never reach it", got)
	}
}

// A reload snapshot that lands while writes are still queued is stale by
// definition: applying it would yank the newer optimistic edits back.
func TestReloadIsDeferredWhileWritesArePending(t *testing.T) {
	m, _ := scriptedModel(t)

	_, cmd, err := m.commitMove("a", "ready", "ready", 3)
	if err != nil || cmd == nil {
		t.Fatal(err)
	}
	before := m.b
	m.onReloadDone(reloadDoneMsg{label: "reloaded"})
	if m.b != before {
		t.Fatal("a reload landing mid-queue must not swap the board")
	}
	if got := laneIDs(m.b, "ready"); got != "b,c,a" {
		t.Errorf("optimistic order lost: %s", got)
	}
}

// The done gesture goes through the same queue as everything else.
func TestDoneAppliesLocallyThenQueues(t *testing.T) {
	m, p := scriptedModel(t)
	m.selectID("b", false)

	cmd := m.onNormalKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if m.b.Task("b").Status != "done" {
		t.Fatalf("b is %s, want done before the store write", m.b.Task("b").Status)
	}
	if m.b.Task("b").Closed.IsZero() {
		t.Error("the optimistic close must stamp Closed")
	}
	if cmd == nil {
		t.Fatal("done must return the persist Cmd")
	}
	m.onPersistDone(cmd().(persistDoneMsg))
	if strings.Join(p.calls, ";") != "done b" {
		t.Errorf("store saw %v", p.calls)
	}
}

func laneIDs(b *board.Board, lane string) string {
	var ids []string
	for _, t := range b.LaneTasks(lane) {
		ids = append(ids, t.ID)
	}
	return strings.Join(ids, ",")
}
