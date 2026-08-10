package board

// Provider is the seam between the UI and the task store — the port; the
// adapters live in internal/store (furrowstore for the real CLI/JSON store,
// memstore for the fixture).
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
