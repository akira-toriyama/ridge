package main

import (
	"errors"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// scriptedProvider exercises the optimistic queue without a furrow binary:
// it records every persist in call order and serves a rebuilt "store truth"
// on Reload, so a rollback is observable as the board reverting.
type scriptedProvider struct {
	mu      sync.Mutex
	truth   func() *Board
	current *Board
	calls   []string
	moves   []scriptedMove
	moveErr error
}

type scriptedMove struct{ id, lane, before, after string }

func newScriptedProvider(truth func() *Board) *scriptedProvider {
	return &scriptedProvider{truth: truth, current: truth()}
}

func (p *scriptedProvider) Board() *Board {
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
func (p *scriptedProvider) Live() bool  { return true }

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

func (p *scriptedProvider) PersistCheck(id string, _ int, _ bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "check "+id)
	return nil
}

func (p *scriptedProvider) PersistBody(id, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "body "+id)
	return nil
}

func scriptedBoard() *Board {
	return NewBoard([]*Task{
		{ID: "a", Title: "a", Status: "ready", Priority: 10},
		{ID: "b", Title: "b", Status: "ready", Priority: 20},
		{ID: "c", Title: "c", Status: "ready", Priority: 30},
		{ID: "z", Title: "z", Status: "backlog", Priority: 10},
	})
}

func scriptedModel(t *testing.T) (*Model, *scriptedProvider) {
	t.Helper()
	p := newScriptedProvider(scriptedBoard)
	m := New(p)
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

	moved, cmd1, err := m.commitMove("a", "ready", "ready", 0, 3)
	if err != nil || !moved || cmd1 == nil {
		t.Fatalf("first move: moved=%v cmd=%v err=%v", moved, cmd1, err)
	}
	if got := laneIDs(m.b, "ready"); got != "b,c,a" {
		t.Fatalf("optimistic order = %s, want b,c,a", got)
	}

	// A second gesture while the first write is in flight: queued, no new Cmd.
	moved, cmd2, err := m.commitMove("c", "ready", "ready", 1, 0)
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
// it is dropped and the store re-read IS the rollback.
func TestPersistFailureRollsBackFromTheStore(t *testing.T) {
	m, p := scriptedModel(t)
	p.moveErr = errors.New("schema gate says no")

	_, cmd, err := m.commitMove("a", "ready", "ready", 0, 3)
	if err != nil || cmd == nil {
		t.Fatal(err)
	}
	if got := laneIDs(m.b, "ready"); got != "b,c,a" {
		t.Fatalf("optimistic order = %s", got)
	}

	rb := m.onPersistDone(cmd().(persistDoneMsg))
	if rb == nil {
		t.Fatal("a failed persist must schedule the rollback re-read")
	}
	if !m.statusErr {
		t.Error("a failed persist must surface as an error, not a shrug")
	}
	m.onReloadDone(rb().(reloadDoneMsg))
	if got := laneIDs(m.b, "ready"); got != "a,b,c" {
		t.Errorf("after rollback ready = %s, want the store's a,b,c", got)
	}
	if len(m.pending) != 0 || m.inflight {
		t.Error("the queue must be empty after a rollback")
	}
}

// A reload snapshot that lands while writes are still queued is stale by
// definition: applying it would yank the newer optimistic edits back.
func TestReloadIsDeferredWhileWritesArePending(t *testing.T) {
	m, _ := scriptedModel(t)

	_, cmd, err := m.commitMove("a", "ready", "ready", 0, 3)
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
