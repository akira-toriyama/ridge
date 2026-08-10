// Package memstore is the in-memory store adapter: it serves the built-in
// fixture (or any handed-in board, for tests) and never touches a real
// .furrow store. The board the model mutates IS the store, so every persist
// is a validated no-op — which is exactly what keeps tests, -dump and -demo
// deterministic.
package memstore

import (
	"fmt"
	"strings"

	"github.com/akira-toriyama/ridge/internal/board"
)

// Store is the fixture-backed board.Provider.
type Store struct {
	b       *board.Board
	rebuild func() *board.Board
	addSeq  int
}

// New serves the fixture snapshot.
func New() *Store {
	f := func() *board.Board { return board.NewBoard(fixtureTasks(), fixtureEpics()...) }
	return &Store{b: f(), rebuild: f}
}

// NewWith serves an arbitrary in-memory board (tests). Reload keeps serving
// the same board: there is no pristine source to rebuild from.
func NewWith(b *board.Board) *Store {
	return &Store{b: b, rebuild: func() *board.Board { return b }}
}

// Board returns the current snapshot (board.Provider).
func (p *Store) Board() *board.Board { return p.b }

// Reload rebuilds the pristine board, discarding session edits
// (board.Provider).
func (p *Store) Reload() error { p.b = p.rebuild(); return nil }

// Sync always fails: there is no store behind the fixture (board.Provider).
func (p *Store) Sync() error { return fmt.Errorf("the fixture has no store to sync") }

// Live is false: the board the model mutates IS the store (board.Provider).
func (p *Store) Live() bool { return false }

// PersistMove validates the ids and records nothing (board.Provider).
func (p *Store) PersistMove(id, lane, _, _ string) ([]string, error) {
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
	if p.b.Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

// PersistCheck validates the item and records nothing (board.Provider).
func (p *Store) PersistCheck(id string, i int, _ bool) error {
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
	if p.b.Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

// PersistCheckAdd validates the id and records nothing (board.Provider).
func (p *Store) PersistCheckAdd(id, _ string) error {
	if p.b.Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

// PersistCheckRm validates the item and records nothing (board.Provider).
func (p *Store) PersistCheckRm(id string, i int) error {
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

// Add appends a task to the CURRENT board (board.Provider) — the fixture has
// no external store, so the board the model mutates is also where an add
// lands, and Reload keeps serving it (discarding it would make the mock lie
// about the add having happened).
func (p *Store) Add(title string, o board.AddOptions) (string, error) {
	if strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("a title cannot be empty")
	}
	lane := o.Lane
	if lane == "" {
		lane = p.b.Lanes()[0].Name
	}
	if p.b.Lane(lane) == nil {
		return "", fmt.Errorf("unknown lane %q", lane)
	}
	p.addSeq++
	t := &board.Task{
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
	last := p.b.LaneTasks(lane)
	if len(last) > 0 {
		t.Priority = last[len(last)-1].Priority + 10
	} else {
		t.Priority = 10
	}
	p.b.Append(t)
	b := p.b
	p.rebuild = func() *board.Board { return b }
	return t.ID, nil
}
