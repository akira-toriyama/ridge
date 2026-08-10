package ui

import (
	"testing"

	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// The two filter-lifecycle defects from t-74y3: the startup -filter armed
// nothing on a live provider (its async query Cmd was discarded, so the bar
// showed a query the board never ran), and a verdict landing mid-keyboard-move
// reshaped the columns under the aimed slot, so the commit wrote a different
// position than the status line promised.

func TestStartupFilterFiresOnALiveStore(t *testing.T) {
	p := &liveQueryProvider{b: memstore.New().Board()}
	m := New(p, Options{Filter: "lane:ready"})
	m.w, m.h = 140, 40
	m.recompute()
	m.relayout()

	if m.startupFilter == nil {
		t.Fatal("-filter on a live provider must arm the async query — dropping the Cmd left every task visible")
	}
	// Drive the armed chain the way Init's runner would: debounce tick, then
	// the query Cmd it fires.
	msg := m.startupFilter()
	_, cmd := m.Update(msg)
	if cmd == nil {
		t.Fatal("the startup tick must fire the store query")
	}
	m.Update(cmd())
	if calls := p.queryCalls(); len(calls) != 1 || calls[0] != "lane:ready" {
		t.Fatalf("store queries = %v, want the startup filter", calls)
	}
	if m.qMatched == nil {
		t.Fatal("the startup verdict must land")
	}
}

func TestMoveHoldsTheVerdictUntilTheMoveResolves(t *testing.T) {
	m, _ := scriptedModel(t)

	m.applyFilter("title:x")
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"a", "b", "c", "z"}})
	if got := len(m.cols["ready"]); got != 3 {
		t.Fatalf("setup: ready shows %d, want 3", got)
	}

	press(m, "enter") // lift the cursor's card — keyboard move mode
	if m.mode != modeMove {
		t.Fatalf("enter must lift the card, mode=%v", m.mode)
	}

	// A shrinking verdict lands mid-aim: it must NOT reshape the columns the
	// drop slot is measured against.
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"a"}})
	if got := len(m.cols["ready"]); got != 3 {
		t.Fatalf("mid-move verdict reshaped the board: ready shows %d, want 3 held", got)
	}
	if m.heldVerdict == nil {
		t.Fatal("the mid-move verdict must be held for the exit")
	}

	press(m, "esc") // cancel the move — the held verdict applies now
	if got := len(m.cols["ready"]); got != 1 {
		t.Fatalf("after the move exits the held verdict must land: ready shows %d, want 1", got)
	}
	if m.heldVerdict != nil {
		t.Fatal("the hold must be consumed on exit")
	}
}

func TestStaleHeldVerdictIsDroppedOnRelease(t *testing.T) {
	m, _ := scriptedModel(t)
	m.applyFilter("title:x")
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"a", "b", "c", "z"}})

	press(m, "enter")
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"a"}}) // held
	m.applyFilter("title:xy")                                  // newer keystroke re-queries: hold is stale
	press(m, "esc")
	if got := len(m.cols["ready"]); got != 3 {
		t.Fatalf("a stale held verdict must be dropped, ready shows %d, want 3", got)
	}
}
