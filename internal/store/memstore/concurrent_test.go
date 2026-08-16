package memstore

import (
	"sync"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The port contract: "Persists run OFF the UI thread, inside a tea.Cmd, so a
// provider must never mutate a Board it has handed out: Reload builds a fresh
// one and swaps." Add used to append straight into the board the UI was
// rendering, which is an unsynchronised write on every -mock quick add. The
// race detector only ever saw it green because nothing drove a write
// concurrently with a read.
func TestAddDoesNotRaceWithAReader(t *testing.T) {
	p := New()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// The "UI thread": walk the handed-out snapshot the way a render does.
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
			for _, task := range b.Tasks() {
				_ = task.Title
				_ = b.LaneTasks(task.Status)
			}
		}
	}()

	// The persist queue: one write at a time, off the UI thread.
	for i := 0; i < 20; i++ {
		if _, err := p.Add("追加されたタスク", board.AddOptions{}); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()
}

// Add must not grow a board it already handed out.
func TestAddSwapsInAFreshBoardRatherThanGrowingTheOldOne(t *testing.T) {
	p := New()
	held := p.Board()
	before := len(held.Tasks())

	if _, err := p.Add("新しいタスク", board.AddOptions{}); err != nil {
		t.Fatal(err)
	}

	if got := len(held.Tasks()); got != before {
		t.Errorf("the snapshot already handed out grew from %d to %d tasks", before, got)
	}
	if got := len(p.Board().Tasks()); got != before+1 {
		t.Errorf("the new snapshot has %d tasks, want %d", got, before+1)
	}
}

// Reload's documented job is to discard session edits. Add used to rewire the
// rebuild closure to return the CURRENT board, so after one quick add every
// move, close and field edit survived a reload too — and `r` said the opposite
// on screen. The add itself is the deliberate exception.
func TestReloadDiscardsSessionEditsButKeepsTheAdd(t *testing.T) {
	p := New()
	b := p.Board()
	if len(b.Tasks()) == 0 {
		t.Fatal("the fixture is empty")
	}

	id, err := p.Add("追加は残る", board.AddOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// A session edit to a pre-existing fixture task, applied the way the model
	// applies it: straight onto the snapshot.
	victim := p.Board().Tasks()[0]
	original := victim.Title
	victim.Title = "セッション中に書き換えた"

	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}

	after := p.Board()
	if after.Task(id) == nil {
		t.Errorf("the reload dropped the quick add %s — the fixture would be lying about it having happened", id)
	}
	if got := after.Tasks()[0].Title; got != original {
		t.Errorf("the reload kept a session edit: title is %q, want the pristine %q", got, original)
	}
}

// Two reloads must not hand back aliased tasks: editing the added card after
// one reload would otherwise leak into the next.
func TestRebuiltAddsAreNotAliasedAcrossReloads(t *testing.T) {
	p := New()
	id, err := p.Add("原本", board.AddOptions{Label: "ui"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	first := p.Board().Task(id)
	if first == nil {
		t.Fatal("the add did not survive the reload")
	}
	first.Title = "書き換え"
	first.Labels[0] = "書き換え"

	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	second := p.Board().Task(id)
	if second == nil {
		t.Fatal("the add did not survive the second reload")
	}
	if second.Title != "原本" {
		t.Errorf("an edit made after the first reload leaked into the second: %q", second.Title)
	}
	if len(second.Labels) > 0 && second.Labels[0] != "ui" {
		t.Errorf("the label slice is aliased across reloads: %q", second.Labels[0])
	}
}

// A gated board refuses writes, and the rebuilt board must stay gated —
// hard-coding writable:true would plant the exact lie NewGated exists to model.
func TestGatedStoreStaysGatedAcrossReload(t *testing.T) {
	p := NewGated("board-behind")
	if p.Board().Writable() {
		t.Fatal("a gated board reports writable")
	}
	if _, err := p.Add("拒否される", board.AddOptions{}); err == nil {
		t.Error("a gated store accepted an add")
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if p.Board().Writable() {
		t.Error("the reloaded board lost its schema gate")
	}
}

// A quick add is not a reload. The first attempt at the Reload contract built
// the post-add board from the PRISTINE base, so adding a card in -mock threw
// away every move and edit made in the session — reproduced here before the
// fix.
func TestAddKeepsSessionEdits(t *testing.T) {
	p := New()
	b := p.Board()
	victim := b.Tasks()[0]
	if _, err := b.MoveTo(victim.ID, "ready", 0); err != nil {
		t.Fatal(err)
	}

	id, err := p.Add("新しいカード", board.AddOptions{})
	if err != nil {
		t.Fatal(err)
	}

	after := p.Board()
	if after.Task(id) == nil {
		t.Errorf("the add did not reach the new snapshot")
	}
	if got := after.Task(victim.ID).Status; got != "ready" {
		t.Errorf("the add discarded a session move: %s is back in %s", victim.ID, got)
	}
}

// NewWith hands back the caller's own board, so an add appended during the
// first rebuild is still present on the second. Without a guard every reload
// stacked another copy of the same id.
func TestAddsAreNotDuplicatedByRepeatedReloadsOnAHandedInBoard(t *testing.T) {
	b := board.NewBoard([]*board.Task{{ID: "t-1", Status: "backlog", Priority: 10}})
	p := NewWith(b)

	id, err := p.Add("一度だけ現れるべき", board.AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := p.Reload(); err != nil {
			t.Fatal(err)
		}
	}

	n := 0
	for _, task := range p.Board().Tasks() {
		if task.ID == id {
			n++
		}
	}
	if n != 1 {
		t.Errorf("after three reloads %s appears %d times", id, n)
	}
}
