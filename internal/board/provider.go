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

	// Query evaluates a furrow -q expression against the store and returns
	// the matching task ids ("" matches everything). The grammar's one
	// definition lives furrow-side (t-ehk7); ridge passes the string through
	// and intersects the ids with its own Board() snapshot. All-or-nothing:
	// a query furrow refuses returns an error and NO ids.
	Query(q string) ([]string, error)

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

	// PersistFields records id's already-applied metadata edit. Everything
	// set-shaped in the patch lands in ONE `furrow set` write; Title and the
	// repo edits are their own commands (retitle / repo), so a mixed patch
	// may cost up to three writes — the UI edits one field per gesture, so
	// in practice it is one.
	PersistFields(id string, p FieldPatch) error

	// PersistCheckAdd records an already-appended checklist item.
	PersistCheckAdd(id, text string) error

	// PersistCheckRm records checklist item i's already-applied deletion.
	PersistCheckRm(id string, i int) error

	// PersistCheckReword records checklist item i's already-applied rewording.
	PersistCheckReword(id string, i int, text string) error
}

// FieldPatch is one already-applied metadata edit. nil means "untouched";
// the zero value of a pointed-to field means "clear" (furrow --clear-value /
// --clear-effort / --clear-due / -e "").
type FieldPatch struct {
	Value     *int // 1..5; 0 clears
	Effort    *int // 1..5; 0 clears
	AddLabels []string
	RmLabels  []string
	Epic      *string // e- id; "" unfiles
	Due       *string // furrow date forms incl. the +1d snooze; "" clears
	Title     *string
	AddRepos  []string // full owner/repo
	RmRepos   []string
}
