package memstore

import (
	"sync"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The fixture's epic writes are STORE-FIRST: unlike the Persist* no-ops they
// really apply, because nothing applied them locally. What they must NOT do is
// touch the derived half (Done/Total/Stuck/OpenDeps, all furrow's) or mutate a
// board already handed out.

func fixtureBox(t *testing.T, p *Store, id string) *board.EpicInfo {
	t.Helper()
	e := p.Board().Epic(id)
	if e == nil {
		t.Fatalf("%s is not a box on the fixture board", id)
	}
	return e
}

func TestEpicSetAppliesTheStoredFieldsOnly(t *testing.T) {
	p := New()
	before := *fixtureBox(t, p, "e-c4mt")

	title, goal := "冬キャンプ 2026-27 — 改題", "新しいゴール"
	yes := true
	if err := p.EpicSet("e-c4mt", board.EpicPatch{
		Title: &title, Goal: &goal, Standing: &yes,
		AddLabels: []string{"onsen"}, RmLabels: []string{"gear"},
		AddRepos: []string{"tomo/joubisai"},
		SetMeta:  map[string]string{"origin": "上書き", "new": "値"},
		RmMeta:   []string{"season"},
	}); err != nil {
		t.Fatalf("epic set: %v", err)
	}

	got := fixtureBox(t, p, "e-c4mt")
	switch {
	case got.Title != title:
		t.Errorf("title = %q, want %q", got.Title, title)
	case got.Goal != goal:
		t.Errorf("goal = %q, want %q", got.Goal, goal)
	case !got.Standing:
		t.Error("standing did not land")
	case !containsStr(got.Labels, "onsen") || containsStr(got.Labels, "gear"):
		t.Errorf("labels = %v, want [onsen]", got.Labels)
	case !containsStr(got.Repos, "tomo/joubisai") || !containsStr(got.Repos, "tomo/kyushu-trip"):
		t.Errorf("repos = %v, want both", got.Repos)
	case got.Meta["origin"] != "上書き" || got.Meta["new"] != "値":
		t.Errorf("meta = %v", got.Meta)
	}
	if _, still := got.Meta["season"]; still {
		t.Error("--rm-meta left the key behind")
	}

	// The derived half is furrow's, and an `epic set` does not change it.
	if got.Done != before.Done || got.Total != before.Total || got.Stuck != before.Stuck {
		t.Errorf("a set touched the derived values: %d/%d stuck=%v, want %d/%d stuck=%v",
			got.Done, got.Total, got.Stuck, before.Done, before.Total, before.Stuck)
	}
	if len(got.OpenDeps) != len(before.OpenDeps) {
		t.Errorf("a set touched OpenDeps: %v, want %v", got.OpenDeps, before.OpenDeps)
	}

	// An empty patch is refused, exactly as furrow refuses `epic set` with no
	// change flag.
	if err := p.EpicSet("e-c4mt", board.EpicPatch{}); err == nil {
		t.Error("an empty patch was accepted")
	}
	if err := p.EpicSet("e-nope", board.EpicPatch{Goal: &goal}); err == nil {
		t.Error("a set on an unknown box was accepted")
	}
}

// The flag must move, or no -demo could show an activation — and the slot must
// be held, or the fixture reaches a board furrow refuses to produce.
func TestEpicActivateMovesTheFlagAndHoldsTheSlot(t *testing.T) {
	p := New()

	// e-c4mt shares e-fw2m's repo, and e-fw2m is the fixture's active box.
	if err := p.EpicActivate("e-c4mt", ""); err == nil {
		t.Error("a second active box for tomo/kyushu-trip was accepted — " +
			"that is a board furrow cannot produce")
	}
	if fixtureBox(t, p, "e-c4mt").Active {
		t.Error("the refused activate set the flag anyway")
	}

	// Freeing the slot makes the same activate land.
	prev, err := p.EpicDeactivate("e-fw2m")
	if err != nil {
		t.Fatalf("deactivate: %v", err)
	}
	if fixtureBox(t, p, "e-fw2m").Active {
		t.Error("deactivate did not clear the flag")
	}
	// The fixture keeps no activation log, so it must not invent a suggestion.
	if prev.ID != "" {
		t.Errorf("previous = %+v, want empty — the fixture has no activation log to compute one from", prev)
	}
	if err := p.EpicActivate("e-c4mt", "テスト: 理由も受け取る"); err != nil {
		t.Fatalf("activate after the slot was freed: %v", err)
	}
	if !fixtureBox(t, p, "e-c4mt").Active {
		t.Error("activate did not set the flag")
	}

	if err := p.EpicActivate("e-nope", ""); err == nil {
		t.Error("activating an unknown box was accepted")
	}

	// A box naming no repo has no slot to hold, so furrow refuses it outright.
	bare, err := p.EpicAdd("repo を持たない箱", board.EpicAddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.EpicActivate(bare, ""); err == nil {
		t.Error("a repo-less box was activated")
	}
}

func TestEpicDepAddAndRm(t *testing.T) {
	p := New()
	if err := p.EpicDepAdd("e-9wtv", "e-p3dx"); err != nil {
		t.Fatalf("dep add: %v", err)
	}
	if got := fixtureBox(t, p, "e-9wtv").Deps; len(got) != 1 || got[0] != "e-p3dx" {
		t.Errorf("deps = %v, want [e-p3dx]", got)
	}
	// Idempotent, like furrow's own add.
	if err := p.EpicDepAdd("e-9wtv", "e-p3dx"); err != nil {
		t.Errorf("a repeated dep add must be a no-op, got %v", err)
	}
	if got := fixtureBox(t, p, "e-9wtv").Deps; len(got) != 1 {
		t.Errorf("deps = %v, want one edge after the repeat", got)
	}
	if err := p.EpicDepAdd("e-9wtv", "e-9wtv"); err == nil {
		t.Error("a box was allowed to wait on itself")
	}

	// The edge worth removing most is the one whose target is NOT on this
	// board: a closed or archived box leaves exactly that behind. e-2b7h is the
	// fixture's closed-dep shape.
	if err := p.EpicDepRm("e-c4mt", "e-2b7h"); err != nil {
		t.Fatalf("removing a dangling epic dep: %v", err)
	}
	if got := fixtureBox(t, p, "e-c4mt").Deps; containsStr(got, "e-2b7h") {
		t.Errorf("deps = %v, want e-2b7h gone", got)
	}
	if err := p.EpicDepRm("e-c4mt", "e-2b7h"); err == nil {
		t.Error("removing an absent edge was accepted; furrow refuses it")
	}
}

// A new box is never active, and its id does not collide with a task's.
func TestEpicAddServesTheBox(t *testing.T) {
	p := New()
	id, err := p.EpicAdd("新しい箱", board.EpicAddOptions{
		Goal: "ゴール", Labels: []string{"gear"}, Repos: []string{"tomo/kyushu-trip"},
	})
	if err != nil {
		t.Fatalf("epic add: %v", err)
	}
	got := fixtureBox(t, p, id)
	if got.Title != "新しい箱" || got.Goal != "ゴール" || got.Active {
		t.Errorf("added box = %+v", got)
	}
	if _, err := p.EpicAdd("   ", board.EpicAddOptions{}); err == nil {
		t.Error("a blank title was accepted")
	}

	// Reload discards it: rebuild() replays only added TASKS, and an epic edit
	// is an edit — Reload is the operation that discards edits. (An add's
	// survival exists so the fixture cannot lie about a created CARD.)
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if p.Board().Epic(id) != nil {
		t.Errorf("%s survived a reload; the fixture documents epic writes as session-only", id)
	}
}

// Board.Epics() hands out its internal slice and NewStoreBoard indexes into it
// by address, so an in-place epic edit would mutate the snapshot the UI thread
// is rendering — the one thing the port forbids.
func TestEpicWritesSwapInAFreshBoard(t *testing.T) {
	p := New()
	held := p.Board()
	// VALUES, not a struct copy: `before := *held.Epic(...)` copies the struct
	// but its Meta field still points at the SAME map, so comparing
	// got.Meta[k] against before.Meta[k] compares a map with itself and a
	// shallow cloneEpics rides green.
	wantGoal := held.Epic("e-c4mt").Goal
	wantOrigin := held.Epic("e-c4mt").Meta["origin"]
	wantDeps := len(held.Epic("e-c4mt").Deps)
	if wantOrigin == "" {
		t.Fatal("setup: e-c4mt must carry a meta origin for the aliasing check to mean anything")
	}

	goal := "書き換えたゴール"
	if err := p.EpicSet("e-c4mt", board.EpicPatch{Goal: &goal}); err != nil {
		t.Fatal(err)
	}
	if got := held.Epic("e-c4mt"); got.Goal != wantGoal {
		t.Errorf("the handed-out board changed under the reader: goal is now %q", got.Goal)
	}
	if p.Board() == held {
		t.Error("the store kept serving the same board object; a write must swap a fresh one in")
	}

	// The Meta MAP and the Deps SLICE are the two aliasing traps a shallow copy
	// leaves behind.
	if err := p.EpicSet("e-c4mt", board.EpicPatch{SetMeta: map[string]string{"origin": "別の値"}}); err != nil {
		t.Fatal(err)
	}
	if got := held.Epic("e-c4mt").Meta["origin"]; got != wantOrigin {
		t.Errorf("the handed-out board's meta map is aliased: origin is now %q, was %q", got, wantOrigin)
	}
	if err := p.EpicDepAdd("e-c4mt", "e-9wtv"); err != nil {
		t.Fatal(err)
	}
	if got := len(held.Epic("e-c4mt").Deps); got != wantDeps {
		t.Errorf("the handed-out board's deps slice is aliased: %d edges, was %d", got, wantDeps)
	}
}

// Two writes to the same box must both land. The persist queue serializes
// writes today, so this is not reachable from the model — but the port makes no
// such promise to a provider, and a read-modify-write split across the lock
// (clone outside, swap inside) silently drops whichever edit lost the race.
// Add holds its lock across the whole sequence; so must these.
func TestConcurrentEpicSetsDoNotLoseAnEdit(t *testing.T) {
	p := New()
	labels := []string{"gear", "bbq", "onsen", "bento"}

	var wg sync.WaitGroup
	for _, l := range labels {
		wg.Add(1)
		go func(label string) {
			defer wg.Done()
			if err := p.EpicSet("e-9wtv", board.EpicPatch{AddLabels: []string{label}}); err != nil {
				t.Errorf("set %s: %v", label, err)
			}
		}(l)
	}
	wg.Wait()

	got := fixtureBox(t, p, "e-9wtv")
	for _, l := range labels {
		if !containsStr(got.Labels, l) {
			t.Errorf("label %q was lost: labels = %v", l, got.Labels)
		}
	}
}

func TestEpicWriteDoesNotRaceWithAReader(t *testing.T) {
	p := New()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			b := p.Board()
			for _, e := range b.Epics() {
				_ = e.Title
				_ = e.MetaKeys()
				_ = b.ActiveHolder(e.ID)
			}
		}
	}()

	// e-9wtv shares the fixture active box's repo, so free the slot first —
	// otherwise every activate below is a refusal and the loop proves nothing.
	if _, err := p.EpicDeactivate("e-fw2m"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		goal := "並行に書くゴール"
		if err := p.EpicSet("e-c4mt", board.EpicPatch{Goal: &goal}); err != nil {
			t.Fatalf("set %d: %v", i, err)
		}
		if err := p.EpicActivate("e-9wtv", ""); err != nil {
			t.Fatalf("activate %d: %v", i, err)
		}
		if _, err := p.EpicDeactivate("e-9wtv"); err != nil {
			t.Fatalf("deactivate %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
}
