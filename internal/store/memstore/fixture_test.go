package memstore

import (
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

func fixtureGraph(t *testing.T) (*board.Board, *board.Graph) {
	t.Helper()
	b := New().Board()
	return b, board.NewGraph(b)
}

func TestGraphAgreesWithFixtureFacts(t *testing.T) {
	b, g := fixtureGraph(t)

	// e-fw2m is the fixture's one epic ENTITY — no lane, no card — and the 18
	// member tasks point at it. Done/Total mirror `furrow epic ls`, so they
	// must agree with the member lanes the fixture actually holds.
	e := b.Epic("e-fw2m")
	if e == nil {
		t.Fatal("e-fw2m is the fixture's epic entity")
	}
	var members, membersDone int
	for _, task := range b.Tasks() {
		if task.Epic != e.ID {
			continue
		}
		members++
		if g.IsDone(task.ID) {
			membersDone++
		}
	}
	if members != e.Total || membersDone != e.Done {
		t.Errorf("epic reports %d/%d, members measure %d/%d", e.Done, e.Total, membersDone, members)
	}
	if e.Total != 18 {
		t.Errorf("e-fw2m has %d members, want 18", e.Total)
	}

	// t-jv3j depends on t-ehk7 (open) and t-t38k (done).
	got := g.BlockedBy("t-jv3j")
	if len(got) != 1 || got[0] != "t-ehk7" {
		t.Errorf("BlockedBy(t-jv3j) = %v, want [t-ehk7]", got)
	}

	// No dangling deps and no cycles in the fixture — the same facts measured
	// over the real 658-task board.
	for _, task := range b.Tasks() {
		for _, d := range task.Deps {
			if !g.Known(d) {
				t.Errorf("%s has a dangling dep %s", task.ID, d)
			}
		}
	}
}
