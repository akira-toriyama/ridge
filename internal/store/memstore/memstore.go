// Package memstore is the in-memory store adapter: it serves the built-in
// fixture (or any handed-in board, for tests) and never touches a real
// .furrow store. The board the model mutates IS the store, so every persist
// is a validated no-op — which is exactly what keeps tests, -dump and -demo
// deterministic.
package memstore

import (
	"fmt"

	"github.com/akira-toriyama/ridge/internal/board"
)

type Store struct {
	b       *board.Board
	rebuild func() *board.Board
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

func (p *Store) Board() *board.Board { return p.b }

func (p *Store) Reload() error { p.b = p.rebuild(); return nil }

func (p *Store) Sync() error { return fmt.Errorf("the fixture has no store to sync") }

func (p *Store) Live() bool { return false }

func (p *Store) PersistMove(id, lane, _, _ string) ([]string, error) {
	if p.b.Task(id) == nil {
		return nil, fmt.Errorf("unknown task %q", id)
	}
	if p.b.Lane(lane) == nil {
		return nil, fmt.Errorf("unknown lane %q", lane)
	}
	return nil, nil
}

func (p *Store) PersistDone(id string) error {
	if p.b.Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

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

func (p *Store) PersistBody(id, _ string) error {
	if p.b.Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

var _ board.Provider = (*Store)(nil)
