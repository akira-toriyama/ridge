// Package memstore is the in-memory store adapter: it serves the built-in
// fixture (or any handed-in board, for tests) and never touches a real
// .furrow store. The board the model mutates IS the store, so every persist
// is a validated no-op — which is exactly what keeps tests, -dump and -demo
// deterministic.
package memstore

import (
	"fmt"
	"strings"
	"sync"

	"github.com/akira-toriyama/ridge/internal/board"
)

// Store is the fixture-backed board.Provider.
//
// The mutex is not decoration: persists run OFF the UI thread inside a
// tea.Cmd, so Add used to append to the very board View() was rendering. The
// port contract is explicit — "a provider must never mutate a Board it has
// handed out: Reload builds a fresh one and swaps" — and furrowstore already
// honoured it. This one now does too: every write to b/added/addSeq is under
// the lock, and Add swaps a freshly built board in rather than growing the
// live one.
type Store struct {
	mu sync.Mutex

	b    *board.Board
	base func() *board.Board // the pristine source, rebuilt on every Reload
	// added holds quick adds BY VALUE so a reload can materialize each one
	// fresh. Keeping the *Task would make edits to an added card survive a
	// reload while every other card's were discarded — a worse contract than
	// either alternative.
	added  []board.Task
	addSeq int
	// writeErr is what every persist returns when the board is gated. A
	// non-writable board whose writes all SUCCEED is the fixture lying in the
	// one direction the gate exists to describe, so the refusal lives on the
	// store, not only in Board.Writable().
	writeErr error
}

// New serves the fixture snapshot.
func New() *Store {
	f := func() *board.Board { return board.NewBoard(fixtureTasks(), fixtureEpics()...) }
	return &Store{b: f(), base: f}
}

// NewWith serves an arbitrary in-memory board (tests). Reload keeps serving
// the same board: there is no pristine source to rebuild from.
func NewWith(b *board.Board) *Store {
	return &Store{b: b, base: func() *board.Board { return b }}
}

// NewGated serves the fixture as a board furrow would REFUSE to write to —
// the schema gate that `furrow board --json` reports as writable:false.
//
// It exists because that state was unreachable by hand: producing it for real
// needs a store on an older schema, so the one frame that carries the
// read-only warning could not be looked at, only unit-tested. A regression
// that deleted that warning therefore shipped to review unseen (t-04f8).
func NewGated(schema string) *Store {
	f := func() *board.Board {
		b := board.NewBoard(fixtureTasks(), fixtureEpics()...)
		return board.NewStoreBoard(b.Lanes(), b.Tasks(), b.Epics(), false, schema)
	}
	return &Store{
		b:    f(),
		base: f,
		// Measured against a real store forced onto an older schema: `furrow
		// board --json` reports writable:false and every set/done/check/add
		// exits 2 with schema-upgrade-required. A gated fixture that accepted
		// writes would render "writes will fail" and then let them all
		// through — the same class of lie as the fixture's -q guessing
		// instead of refusing (1139e1b).
		writeErr: fmt.Errorf("board is read-only (%s): furrow refuses writes until `furrow upgrade`", schema),
	}
}

// gate is the schema pre-flight every write goes through.
func (p *Store) gate() error { return p.writeErr }

// Board returns the current snapshot (board.Provider).
func (p *Store) Board() *board.Board {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.b
}

// Reload rebuilds the pristine board, discarding session edits
// (board.Provider).
//
// Quick adds are the one exception, and deliberately so: a reload that
// discarded the add would make the fixture lie about it having happened
// (PR #9). They are rebuilt from the value captured at Add time, so the ADD
// survives while edits made to it afterwards are discarded like everyone
// else's — the alternative silently exempted added cards from the reload.
func (p *Store) Reload() error {
	b := p.rebuild()
	p.mu.Lock()
	p.b = b
	p.mu.Unlock()
	return nil
}

// rebuild materializes the pristine board plus a fresh copy of every task
// added this session.
func (p *Store) rebuild() *board.Board {
	p.mu.Lock()
	added := make([]board.Task, len(p.added))
	copy(added, p.added)
	p.mu.Unlock()

	b := p.base()
	for i := range added {
		t := cloneTask(added[i])
		b.Append(&t)
	}
	return b
}

// cloneTask deep-copies the slice fields so a rebuilt board never aliases the
// captured value (or a previous rebuild).
func cloneTask(t board.Task) board.Task {
	t.Labels = append([]string(nil), t.Labels...)
	t.Repos = append([]string(nil), t.Repos...)
	t.Deps = append([]string(nil), t.Deps...)
	t.Refs = append([]string(nil), t.Refs...)
	t.Checklist = append([]board.ChecklistItem(nil), t.Checklist...)
	return t
}

// Sync always fails: there is no store behind the fixture (board.Provider).
func (p *Store) Sync() error { return fmt.Errorf("the fixture has no store to sync") }

// Live is false: the board the model mutates IS the store (board.Provider).
func (p *Store) Live() bool { return false }

// PersistMove validates the ids and records nothing (board.Provider).
func (p *Store) PersistMove(id, lane, _, _ string) ([]string, error) {
	if err := p.gate(); err != nil {
		return nil, err
	}
	if p.b.Task(id) == nil {
		return nil, fmt.Errorf("unknown task %q", id)
	}
	if p.b.Lane(lane) == nil {
		return nil, fmt.Errorf("unknown lane %q", lane)
	}
	return nil, nil
}

// PersistDone validates the id and records nothing (board.Provider).
func (p *Store) PersistDone(id string) error {
	if err := p.gate(); err != nil {
		return err
	}
	if p.b.Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

// PersistCheck validates the item and records nothing (board.Provider).
func (p *Store) PersistCheck(id string, i int, _ bool) error {
	if err := p.gate(); err != nil {
		return err
	}
	t := p.b.Task(id)
	if t == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	if i < 0 || i >= len(t.Checklist) {
		return fmt.Errorf("task %s has no checklist item %d", id, i)
	}
	return nil
}

// PersistBody validates the id and records nothing (board.Provider).
func (p *Store) PersistBody(id, _ string) error {
	if err := p.gate(); err != nil {
		return err
	}
	if p.b.Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

var _ board.Provider = (*Store)(nil)

// Query stands in for furrow's -q over the fixture (board.Provider) — see
// query.go for what it honours at furrow's semantics and what it refuses.
// All-or-nothing like furrow's exit 2: a refused query returns an error and
// no ids. The lane and repo vocabularies come from the board being served,
// because furrow validates those two at parse time.
func (p *Store) Query(q string) ([]string, error) {
	parsed := parseQuery(q, boardVocab(p.b))
	if len(parsed.problems) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(parsed.problems, "; "))
	}
	g := board.NewGraph(p.b)
	var ids []string
	for _, t := range p.b.Tasks() {
		if parsed.match(t, g) {
			ids = append(ids, t.ID)
		}
	}
	return ids, nil
}

// PersistFields validates the id and records nothing (board.Provider) — the
// local apply already validated the patch against the same board.
func (p *Store) PersistFields(id string, _ board.FieldPatch) error {
	if err := p.gate(); err != nil {
		return err
	}
	if p.b.Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

// PersistCheckAdd validates the id and records nothing (board.Provider).
func (p *Store) PersistCheckAdd(id, _ string) error {
	if err := p.gate(); err != nil {
		return err
	}
	if p.b.Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

// PersistCheckRm validates the item and records nothing (board.Provider).
func (p *Store) PersistCheckRm(id string, i int) error {
	if err := p.gate(); err != nil {
		return err
	}
	t := p.b.Task(id)
	if t == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	if i < 0 || i >= len(t.Checklist) {
		return fmt.Errorf("task %s has no checklist item %d", id, i)
	}
	return nil
}

// PersistCheckReword validates the item and records nothing (board.Provider).
func (p *Store) PersistCheckReword(id string, i int, _ string) error {
	return p.PersistCheckRm(id, i) // same bounds contract
}

// Add records a task and swaps in a board that contains it (board.Provider).
//
// It runs on the persist queue's goroutine while the UI thread renders the
// board it was handed, so it must not append to that board — it builds a fresh
// one and swaps it under the lock, the way furrowstore does. Reload keeps
// serving the add afterwards (discarding it would make the mock lie about the
// add having happened).
func (p *Store) Add(title string, o board.AddOptions) (string, error) {
	if err := p.gate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("a title cannot be empty")
	}

	p.mu.Lock()
	cur := p.b
	lane := o.Lane
	if lane == "" {
		lane = cur.Lanes()[0].Name
	}
	if cur.Lane(lane) == nil {
		p.mu.Unlock()
		return "", fmt.Errorf("unknown lane %q", lane)
	}
	p.addSeq++
	t := board.Task{
		ID:     fmt.Sprintf("t-new%d", p.addSeq),
		Title:  title,
		Status: lane,
		Epic:   o.Epic,
	}
	if o.Label != "" {
		t.Labels = []string{o.Label}
	}
	if o.Repo != "" {
		t.Repos = []string{o.Repo}
	}
	last := cur.LaneTasks(lane)
	if len(last) > 0 {
		t.Priority = last[len(last)-1].Priority + 10
	} else {
		t.Priority = 10
	}
	p.added = append(p.added, t)
	p.mu.Unlock()

	// A FRESH board, not an append to the one already on screen.
	b := p.rebuild()
	p.mu.Lock()
	p.b = b
	p.mu.Unlock()
	return t.ID, nil
}
