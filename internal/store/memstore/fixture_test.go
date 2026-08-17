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

	// OpenDeps is hand-written the way Done/Total is, so the same rot check
	// applies: over THIS fixture's fully-listed population it must equal
	// what furrow would derive — the deps that resolve to a served (open)
	// epic. A mismatch means one side of the hand-kept pair was edited alone.
	for _, e := range b.Epics() {
		open := map[string]bool{}
		for _, d := range e.OpenDeps {
			open[d] = true
			if !containsExact(e.Deps, d) {
				t.Errorf("%s: open dep %s is not in Deps at all", e.ID, d)
			}
		}
		for _, d := range e.Deps {
			if (b.Epic(d) != nil) != open[d] {
				t.Errorf("%s: dep %s — hand-written OpenDeps disagrees with the served epic set", e.ID, d)
			}
		}
	}
	// The edges cover every rendering state the UI distinguishes: a wait on
	// an OPEN box, a dep furrow already resolved away (outside open_deps AND
	// absent from the open-only read — the closed-dep shape), and a stuck
	// epic below.
	if e := b.Epic("e-c4mt"); len(e.OpenDeps) != 1 || e.OpenDeps[0] != "e-fw2m" {
		t.Errorf("e-c4mt OpenDeps = %v, want [e-fw2m] — e-2b7h stays satisfied", e.OpenDeps)
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

	// furrow allows at most ONE active box per repo, so a fixture with two for
	// the same repo is a board furrow cannot produce — and it would make the
	// epic overlay's "slot held by" line answer arbitrarily.
	held := map[string]string{}
	for _, e := range b.Epics() {
		if !e.Active {
			continue
		}
		if len(e.Repos) == 0 {
			t.Errorf("%s is active with no repo; furrow refuses to activate one", e.ID)
		}
		for _, r := range e.Repos {
			if other, dup := held[r]; dup {
				t.Errorf("%s and %s are both active for %s — furrow allows one", other, e.ID, r)
			}
			held[r] = e.ID
		}
	}
	if len(held) == 0 {
		t.Error("the fixture must keep one active box — the ▶ marker's only site, " +
			"and the only box `deactivate` is reachable on")
	}

	// The activate gesture needs a REFUSAL site too: a second box sharing an
	// active box's repo. Without it the precondition line the overlay renders
	// ("slot held by …") is unreachable headless.
	clash := false
	for _, e := range b.Epics() {
		if e.Active {
			continue
		}
		for _, r := range e.Repos {
			if _, taken := held[r]; taken {
				clash = true
			}
		}
	}
	if !clash {
		t.Error("no inactive box shares an active box's repo — the activate-clash " +
			"precondition has no fixture site")
	}

	// An epic's repos must be repos the board actually knows, or the overlay's
	// repo toggle list (board vocab ∪ the box's own) would show a repo no task
	// has and no slice can select.
	vocab := map[string]bool{}
	for _, task := range b.Tasks() {
		for _, r := range task.Repos {
			vocab[r] = true
		}
	}
	for _, e := range b.Epics() {
		for _, r := range e.Repos {
			if !vocab[r] {
				t.Errorf("%s names repo %q, which no fixture task carries", e.ID, r)
			}
		}
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
