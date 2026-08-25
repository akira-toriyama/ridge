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
	// epicSeq names boxes created this session (`e-newN`), the way addSeq names
	// tasks. Epic writes are not replayed on Reload, so nothing else is kept.
	epicSeq int
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
func (p *Store) Board() *board.Board { return p.snapshot() }

// snapshot is the ONE read of the current board. Every Persist* and Query runs
// on the persist queue's goroutine while the UI thread renders, so none of them
// may touch the field directly.
func (p *Store) snapshot() *board.Board {
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
		// NewWith's base is the caller's OWN board, handed back as-is, so an
		// add appended to it on the first rebuild is still there on the
		// second. Without this guard every reload stacked another copy of the
		// same id.
		if b.Task(added[i].ID) != nil {
			continue
		}
		t := cloneTask(added[i])
		b.Append(&t)
	}
	return b
}

// withTask copies a board's task list, appends one task and returns a NEW
// board. The original is left untouched — it is the snapshot the UI thread is
// rendering.
func withTask(b *board.Board, extra *board.Task) *board.Board {
	tasks := append([]*board.Task(nil), b.Tasks()...)
	tasks = append(tasks, extra)
	return board.NewStoreBoard(b.Lanes(), tasks, cloneEpics(b.Epics()), b.Writable(), b.SchemaState())
}

// withEpics returns a NEW board carrying the given epic set. Board.Epics()
// hands out its internal slice and NewStoreBoard indexes into it by address, so
// an epic edit applied in place would mutate the very snapshot the UI thread is
// rendering — the one thing the port contract forbids a provider to do.
func withEpics(b *board.Board, epics []board.EpicInfo) *board.Board {
	return board.NewStoreBoard(b.Lanes(), b.Tasks(), epics, b.Writable(), b.SchemaState())
}

// cloneEpics deep-copies an epic set, Meta map included: a shallow copy would
// leave the new board's maps aliased to the old board's, so a later meta edit
// would reach backwards into a snapshot already handed out.
func cloneEpics(src []board.EpicInfo) []board.EpicInfo {
	out := make([]board.EpicInfo, len(src))
	for i, e := range src {
		e.Labels = append([]string(nil), e.Labels...)
		e.Repos = append([]string(nil), e.Repos...)
		e.Deps = append([]string(nil), e.Deps...)
		e.OpenDeps = append([]string(nil), e.OpenDeps...)
		if e.Meta != nil {
			meta := make(map[string]string, len(e.Meta))
			for k, v := range e.Meta {
				meta[k] = v
			}
			e.Meta = meta
		}
		out[i] = e
	}
	return out
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
	if p.snapshot().Task(id) == nil {
		return nil, fmt.Errorf("unknown task %q", id)
	}
	if p.snapshot().Lane(lane) == nil {
		return nil, fmt.Errorf("unknown lane %q", lane)
	}
	return nil, nil
}

// PersistDone validates the id and records nothing (board.Provider).
func (p *Store) PersistDone(id string) error {
	if err := p.gate(); err != nil {
		return err
	}
	if p.snapshot().Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

// PersistCheck validates the item and records nothing (board.Provider).
func (p *Store) PersistCheck(id string, i int, _ bool) error {
	if err := p.gate(); err != nil {
		return err
	}
	t := p.snapshot().Task(id)
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
	if p.snapshot().Task(id) == nil {
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
	// ONE snapshot for the whole evaluation: taking three would let a
	// concurrent add parse the query against one board and match against
	// another.
	b := p.snapshot()
	parsed := parseQuery(q, boardVocab(b))
	if len(parsed.problems) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(parsed.problems, "; "))
	}
	g := board.NewGraph(b)
	var ids []string
	for _, t := range b.Tasks() {
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
	if p.snapshot().Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

// PersistNote validates the id and records nothing (board.Provider) — the
// local apply (Board.AppendNote) already refused an empty text against the
// same board.
func (p *Store) PersistNote(id, _ string) error {
	if err := p.gate(); err != nil {
		return err
	}
	if p.snapshot().Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

// PersistCheckAdd validates the id and records nothing (board.Provider).
func (p *Store) PersistCheckAdd(id, _ string) error {
	if err := p.gate(); err != nil {
		return err
	}
	if p.snapshot().Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

// PersistCheckRm validates the item and records nothing (board.Provider).
func (p *Store) PersistCheckRm(id string, i int) error {
	if err := p.gate(); err != nil {
		return err
	}
	t := p.snapshot().Task(id)
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

// PersistDepAdd validates the ids and records nothing (board.Provider) — the
// local apply (Board.DepAdd) already enforced the acyclic/exists mirror
// against the same board.
func (p *Store) PersistDepAdd(id, dep string) error {
	if err := p.gate(); err != nil {
		return err
	}
	b := p.snapshot()
	if b.Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	if b.Task(dep) == nil {
		return fmt.Errorf("unknown dep %q", dep)
	}
	return nil
}

// PersistDepRm validates the TASK id only and records nothing
// (board.Provider). The dep id is deliberately unchecked: an archived dep
// leaves a dangling edge on the live board (the `?` row), and removing it is
// exactly the write furrow allows — requiring the dep to exist would refuse
// the one removal that matters.
func (p *Store) PersistDepRm(id, _ string) error {
	if err := p.gate(); err != nil {
		return err
	}
	if p.snapshot().Task(id) == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	return nil
}

// --- epic writes -----------------------------------------------------------
//
// These are STORE-FIRST (board.Provider's epic family): nothing was applied to
// the board, so unlike the Persist* no-ops the fixture must really apply them
// or every -dump, -demo and test would show a board that ignored the write.
//
// What it applies is deliberately narrow: the STORED fields only. It does not
// recompute OpenDeps from the served epic set (board.EpicInfo forbids that by
// name — the value is furrow-derived), does not touch Done/Total/Stuck, and does
// not mirror furrow's refusal MESSAGES. The -q evaluator next door licenses an
// approximation only where every rule was measured against a real store, and
// furrow's epic refusal prose has no such surface here, so the fixture stays a
// data store and lets the real adapter own the verdicts.
//
// Two refusals it does keep, both because the alternative is a board furrow
// cannot produce: the schema gate (a non-writable board whose writes all succeed
// is the fixture lying in the direction the gate exists to describe) and
// one-active-epic-per-repo (see EpicActivate).
//
// Session epic writes do NOT survive Reload: rebuild() replays only `added`
// tasks. That asymmetry with the quick add is deliberate here — an add's
// survival exists so the fixture cannot lie about a CREATED card, while an
// epic edit is an edit, and Reload is the operation that discards edits.

// editEpic is the ONE path every epic write takes: under the store lock it
// clones the epic set, hands fn the box to edit, and swaps a fresh board in.
//
// Two properties, both load-bearing. The clone means no write ever touches a
// board already handed out (the port forbids it, and Board.Epics() returns the
// internal slice that NewStoreBoard indexes by address). Holding the lock across
// the whole read-modify-write means an edit cannot be lost to an interleaving
// one — the same atomicity Add has. fn returning an error swaps NOTHING, so a
// refused edit leaves the board exactly as it was.
func (p *Store) editEpic(id string, fn func(*board.EpicInfo) error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	epics := cloneEpics(p.b.Epics())
	for i := range epics {
		if epics[i].ID != id {
			continue
		}
		if err := fn(&epics[i]); err != nil {
			return err
		}
		p.b = withEpics(p.b, epics)
		return nil
	}
	return fmt.Errorf("unknown epic %q", id)
}

// EpicAdd appends a box and swaps in a board that contains it
// (board.Provider). Never active on creation, like furrow's own add.
func (p *Store) EpicAdd(title string, o board.EpicAddOptions) (string, error) {
	if err := p.gate(); err != nil {
		return "", err
	}
	if strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("a title cannot be empty")
	}
	p.mu.Lock()
	p.epicSeq++
	e := board.EpicInfo{
		ID:     fmt.Sprintf("e-new%d", p.epicSeq),
		Title:  title,
		Goal:   o.Goal,
		Labels: append([]string(nil), o.Labels...),
		Repos:  append([]string(nil), o.Repos...),
	}
	p.b = withEpics(p.b, append(cloneEpics(p.b.Epics()), e))
	p.mu.Unlock()
	return e.ID, nil
}

// EpicSet applies the stored half of one `epic set` (board.Provider).
func (p *Store) EpicSet(id string, patch board.EpicPatch) error {
	if err := p.gate(); err != nil {
		return err
	}
	if patch.Empty() {
		return fmt.Errorf("nothing to set on %s", id)
	}
	return p.editEpic(id, func(e *board.EpicInfo) error {
		if patch.Title != nil {
			if strings.TrimSpace(*patch.Title) == "" {
				return fmt.Errorf("epic title must not be empty")
			}
			e.Title = *patch.Title
		}
		if patch.Goal != nil {
			e.Goal = *patch.Goal
		}
		for _, l := range patch.AddLabels {
			if l != "" && !containsStr(e.Labels, l) {
				e.Labels = append(e.Labels, l)
			}
		}
		for _, l := range patch.RmLabels {
			e.Labels = removeStr(e.Labels, l)
		}
		for _, r := range patch.AddRepos {
			if r != "" && !containsStr(e.Repos, r) {
				e.Repos = append(e.Repos, r)
			}
		}
		for _, r := range patch.RmRepos {
			e.Repos = removeStr(e.Repos, r)
		}
		for k, v := range patch.SetMeta {
			if e.Meta == nil {
				e.Meta = map[string]string{}
			}
			e.Meta[k] = v
		}
		for _, k := range patch.RmMeta {
			delete(e.Meta, k)
		}
		if patch.Standing != nil {
			e.Standing = *patch.Standing
		}
		if patch.Pinned != nil {
			e.Pinned = *patch.Pinned
		}
		return nil
	})
}

// EpicActivate sets the active flag (board.Provider). The reason is furrow's
// body-appended activation record and has nowhere to go in a fixture, so it is
// accepted and dropped.
//
// The one-active-per-repo rule IS enforced here — the one place this adapter
// mirrors a furrow refusal, and only because the alternative is worse: without
// it the fixture reaches a board furrow cannot produce (two active boxes for one
// repo), which every -demo, -dump and test would then render as if it were real.
// The check reads the served Active/Repos through Board.ActiveHolder, the same
// accessor the overlay's precondition line uses, so there is no second copy of
// the rule to rot. The MESSAGE deliberately does not imitate furrow's prose:
// callers branch on furrow's error kind, never on text.
func (p *Store) EpicActivate(id, _ string) error {
	if err := p.gate(); err != nil {
		return err
	}
	return p.editEpic(id, func(e *board.EpicInfo) error {
		if holder := p.b.ActiveHolder(id); holder != "" {
			return fmt.Errorf("another epic (%s) is already active for one of %s's repos", holder, id)
		}
		if len(e.Repos) == 0 {
			return fmt.Errorf("epic %s names no repo — a repo-less box has no slot to hold", id)
		}
		e.Active = true
		return nil
	})
}

// EpicDeactivate clears the active flag (board.Provider). It names no previous
// box: the suggestion is computed from furrow's activation log, which the
// fixture does not keep, and guessing one would be a fabricated suggestion.
func (p *Store) EpicDeactivate(id string) (board.EpicPrevious, error) {
	if err := p.gate(); err != nil {
		return board.EpicPrevious{}, err
	}
	err := p.editEpic(id, func(e *board.EpicInfo) error {
		e.Active = false
		return nil
	})
	return board.EpicPrevious{}, err
}

// EpicDepAdd appends an epic dep edge (board.Provider).
//
// The new edge joins OpenDeps too. That is NOT recomputing furrow's derivation —
// it is keeping the two hand-kept fields consistent for the one edge just added.
// Under ridge's open-only epic read every box on the board is open, so a wait on
// a served box is by definition still waiting; and an edge on an id this read
// does not serve cannot be proven satisfied either. Leaving it out instead made
// the deps list render a wait added one keystroke ago as "(satisfied)".
func (p *Store) EpicDepAdd(id, dep string) error {
	if err := p.gate(); err != nil {
		return err
	}
	if id == dep {
		return fmt.Errorf("%s cannot wait on itself", id)
	}
	return p.editEpic(id, func(e *board.EpicInfo) error {
		if !containsStr(e.Deps, dep) {
			e.Deps = append(e.Deps, dep)
		}
		if !containsStr(e.OpenDeps, dep) {
			e.OpenDeps = append(e.OpenDeps, dep)
		}
		return nil
	})
}

// EpicDepRm removes an epic dep edge (board.Provider). Like PersistDepRm it
// validates the SUBJECT only: an edge whose target is closed or archived is
// exactly the one worth removing, and requiring the target to resolve would
// refuse it.
func (p *Store) EpicDepRm(id, dep string) error {
	if err := p.gate(); err != nil {
		return err
	}
	return p.editEpic(id, func(e *board.EpicInfo) error {
		if !containsStr(e.Deps, dep) {
			return fmt.Errorf("%q is not a dependency of %s", dep, id)
		}
		e.Deps = removeStr(e.Deps, dep)
		// OpenDeps is furrow-derived and never recomputed here — but leaving a
		// removed edge in it would make the →N readout count an edge that no
		// longer exists, which is not "not recomputing", it is stale.
		e.OpenDeps = removeStr(e.OpenDeps, dep)
		return nil
	})
}

func containsStr(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// removeStr returns a NEW slice: the in-place compaction board.removeStr uses
// is safe there because the caller owns the task, but here the slice may still
// be reachable from a clone.
func removeStr(hay []string, needle string) []string {
	out := make([]string, 0, len(hay))
	for _, h := range hay {
		if h != needle {
			out = append(out, h)
		}
	}
	return out
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
	// A fresh board carrying the CURRENT session state plus the new card.
	// Building it from the pristine base instead would make a quick add
	// silently throw away every move and edit made this session — Reload is
	// the operation that discards, an add is not.
	added := cloneTask(t)
	p.b = withTask(cur, &added)
	p.mu.Unlock()
	return t.ID, nil
}
