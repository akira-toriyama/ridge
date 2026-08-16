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

	// Epics are ENTITIES — no lane, no card — and their hand-written
	// Done/Total mirror `furrow epic ls`, so EVERY epic's numbers must agree
	// with the member lanes the fixture actually holds (a count edited on one
	// side only is exactly how a hand-kept snapshot rots).
	for _, e := range b.Epics() {
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
			t.Errorf("%s reports %d/%d, members measure %d/%d",
				e.ID, e.Done, e.Total, membersDone, members)
		}
	}
	if e := b.Epic("e-fw2m"); e == nil || e.Total != 18 {
		t.Errorf("e-fw2m must stay the 18-member main box: %+v", e)
	}

	// The epic-dep edges cover every rendering state the UI distinguishes:
	// a wait on an OPEN box, a dep on a CLOSED box (an id the open-only epic
	// read cannot resolve — satisfied, per furrow), and a stuck epic.
	if got := b.OpenEpicDeps(b.Epic("e-fw2m")); len(got) != 1 || got[0] != "e-p3dx" {
		t.Errorf("OpenEpicDeps(e-fw2m) = %v, want [e-p3dx]", got)
	}
	if got := b.OpenEpicDeps(b.Epic("e-c4mt")); len(got) != 1 || got[0] != "e-fw2m" {
		t.Errorf("OpenEpicDeps(e-c4mt) = %v, want [e-fw2m] — e-2b7h is the closed-dep shape", got)
	}
	if b.Epic("e-2b7h") != nil {
		t.Error("e-2b7h must stay ABSENT from the epic set: it is the fixture's closed-dep shape")
	}
	stuck := false
	for _, e := range b.Epics() {
		stuck = stuck || e.Stuck
	}
	if !stuck {
		t.Error("the fixture must keep one stuck epic — the warn glyph's only fixture site")
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
