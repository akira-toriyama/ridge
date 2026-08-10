package main

import "fmt"

// Provider is the seam between the UI and the task store.
//
// The division of labour: the MODEL owns the optimistic local apply — it
// mutates the Board() snapshot through the same furrow-semantics helpers
// (MoveTo, Close, ToggleCheck, SetBody) on the UI thread — and a Persist*
// method only records that already-applied change in the backing store.
// Persists run OFF the UI thread, inside a tea.Cmd, so a provider must never
// mutate a Board it has handed out: Reload builds a fresh one and swaps.
type Provider interface {
	// Board returns the current snapshot. The model mutates it optimistically
	// on the UI thread; the provider treats handed-out boards as frozen.
	Board() *Board

	// Reload re-reads the backing store into a fresh Board. For the mock the
	// backing store is the fixture, so a reload discards session edits.
	Reload() error

	// Sync runs the store's git sync (commit, pull --rebase, push). A
	// provider without a store returns an error.
	Sync() error

	// Live reports whether persists land in an external store whose truth
	// can drift from the in-memory board. The model reconciles (re-reads)
	// after its persist queue drains only when this is true — reconciling the
	// mock would just revert the session's own edits.
	Live() bool

	// PersistMove records id's already-applied placement: the lane plus at
	// most one anchor. beforeID wins when both are set; both empty means the
	// task is the lane's only card. renumbered reports the neighbours the
	// store renumbered when the sparse-priority gap was exhausted.
	PersistMove(id, lane, beforeID, afterID string) (renumbered []string, err error)

	// PersistDone records id's already-applied close.
	PersistDone(id string) error

	// PersistCheck records checklist item i's already-applied state. done is
	// the state AFTER the local toggle, so the write is idempotent.
	PersistCheck(id string, i int, done bool) error

	// PersistBody records id's already-applied body replacement.
	PersistBody(id, body string) error
}

// mockProvider serves the fixture. The board the model mutates IS the store,
// so every persist is a validated no-op — which is exactly what keeps tests
// and -dump deterministic.
type mockProvider struct{ b *Board }

func newMockProvider() *mockProvider { return &mockProvider{b: NewBoard(fixtureTasks())} }

func (p *mockProvider) Board() *Board { return p.b }

func (p *mockProvider) Reload() error { p.b = NewBoard(fixtureTasks()); return nil }

func (p *mockProvider) Sync() error { return fmt.Errorf("the fixture has no store to sync") }

func (p *mockProvider) Live() bool { return false }

func (p *mockProvider) PersistMove(id, lane, _, _ string) ([]string, error) {
	if p.b.Task(id) == nil {
		return nil, fmt.Errorf("unknown task %q", id)
	}
	if p.b.Lane(lane) == nil {
		return nil, fmt.Errorf("unknown lane %q", lane)
	}
	return nil, nil
}

func (p *mockProvider) PersistDone(id string) error {
	if p.b.Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

func (p *mockProvider) PersistCheck(id string, i int, _ bool) error {
	t := p.b.Task(id)
	if t == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	if i < 0 || i >= len(t.Checklist) {
		return fmt.Errorf("task %s has no checklist item %d", id, i)
	}
	return nil
}

func (p *mockProvider) PersistBody(id, _ string) error {
	if p.b.Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

var _ Provider = (*mockProvider)(nil)
