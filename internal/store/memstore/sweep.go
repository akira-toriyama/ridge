package memstore

import (
	"fmt"
	"slices"
	"sort"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The fixture's sweep (board.Provider). The previews are computed from the
// board the way furrow computes them — done tasks older than the age guard,
// open tasks with edges to done ones — and the writes move ids between the
// board and a session-scoped archive set. It exists so -dump / -demo and
// tests have a Sweep to show; nothing outside the fixture path may lean on
// its rules (the live adapter asks furrow).

// fixtureOlderThanDays mirrors furrow's default `archive.older_than_days`.
const fixtureOlderThanDays = 30

// shape applies the session's sweep state to a freshly built board: archived
// tasks leave it, pruned edges are dropped. Called by rebuild on every Reload
// so the writes survive the way quick adds do.
func (p *Store) shape(b *board.Board) *board.Board {
	p.mu.Lock()
	archived := p.archived
	pruned := p.pruned
	p.mu.Unlock()
	if len(archived) == 0 && len(pruned) == 0 {
		return b
	}
	tasks := make([]*board.Task, 0, len(b.Tasks()))
	for _, t := range b.Tasks() {
		if archived[t.ID] {
			continue
		}
		if gone := pruned[t.ID]; len(gone) > 0 {
			c := cloneTask(*t)
			c.Deps = slices.DeleteFunc(c.Deps, func(d string) bool { return gone[d] })
			t = &c
		}
		tasks = append(tasks, t)
	}
	return board.NewStoreBoard(b.Lanes(), tasks, cloneEpics(b.EpicsAll()), b.Writable(), b.SchemaState())
}

func sweepTaskOf(t *board.Task) board.SweepTask {
	return board.SweepTask{ID: t.ID, Title: t.Title, Repos: append([]string(nil), t.Repos...), Closed: t.Closed}
}

// SweepPreview computes the three previews from the current snapshot
// (board.Provider). Archivable: done-lane tasks closed more than
// fixtureOlderThanDays ago, oldest first. DoneDeps: tasks outside the done
// lane whose deps name a done-lane task. UnknownKeys: none — the fixture has
// no shard bytes to park a key in. Archived: the session's retired tasks,
// newest closed first, as `ls --archived` orders them.
func (p *Store) SweepPreview() (board.Sweep, error) {
	b := p.snapshot()
	done := b.DoneLane()
	now := nowFn()
	s := board.Sweep{OlderThanDays: fixtureOlderThanDays}
	for _, t := range b.Tasks() {
		if t.Status == done && !t.Closed.IsZero() && now.Sub(t.Closed).Hours() > float64(fixtureOlderThanDays*24) {
			s.Archivable = append(s.Archivable, sweepTaskOf(t))
		}
	}
	sort.SliceStable(s.Archivable, func(i, j int) bool { return s.Archivable[i].Closed.Before(s.Archivable[j].Closed) })
	for _, t := range b.Tasks() {
		if t.Status == done {
			continue
		}
		var sat []string
		for _, d := range t.Deps {
			if dt := b.Task(d); dt != nil && dt.Status == done {
				sat = append(sat, d)
			}
		}
		if len(sat) > 0 {
			s.DoneDeps = append(s.DoneDeps, board.TidyDoneDep{ID: t.ID, Deps: sat})
		}
	}
	p.mu.Lock()
	archived := p.archived
	p.mu.Unlock()
	if len(archived) > 0 {
		for _, t := range p.base().Tasks() {
			if archived[t.ID] {
				s.Archived = append(s.Archived, sweepTaskOf(t))
			}
		}
		sort.SliceStable(s.Archived, func(i, j int) bool { return s.Archived[i].Closed.After(s.Archived[j].Closed) })
	}
	return s, nil
}

// Archive retires ids (board.Provider). Mirrors furrow's refusals: an unknown
// id is not-found, a task outside the done lane is refused, and either
// refuses the WHOLE list (nothing moves).
func (p *Store) Archive(ids []string) error {
	if err := p.gate(); err != nil {
		return err
	}
	if err := board.ValidateSweepIDs("archive", ids); err != nil {
		return err
	}
	b := p.snapshot()
	done := b.DoneLane()
	for _, id := range ids {
		t := b.Task(id)
		if t == nil {
			return fmt.Errorf("task not found: %s", id)
		}
		if t.Status != done {
			return fmt.Errorf("only done-lane tasks can be archived by id; %s is in %q (move it to done first)", id, t.Status)
		}
	}
	p.mu.Lock()
	if p.archived == nil {
		p.archived = map[string]bool{}
	}
	for _, id := range ids {
		p.archived[id] = true
	}
	p.mu.Unlock()
	return p.Reload()
}

// Unarchive restores ids (board.Provider). All-or-nothing like furrow: an id
// not in the archive fails the list; an id already on the board is refused.
func (p *Store) Unarchive(ids []string) error {
	if err := p.gate(); err != nil {
		return err
	}
	if err := board.ValidateSweepIDs("unarchive", ids); err != nil {
		return err
	}
	b := p.snapshot()
	p.mu.Lock()
	archived := p.archived
	p.mu.Unlock()
	var missing []string
	for _, id := range ids {
		if b.Task(id) != nil {
			return fmt.Errorf("%s is not archived — it is already on the hot board", id)
		}
		if !archived[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%d of %d ids not found in the archive — nothing was restored (%v)", len(missing), len(ids), missing)
	}
	p.mu.Lock()
	for _, id := range ids {
		delete(p.archived, id)
	}
	p.mu.Unlock()
	return p.Reload()
}

// Tidy prunes one class (board.Provider). done-deps drops every satisfied
// edge the preview named; unknown-keys is a no-op here because the fixture
// parks none — reported as applied, the way furrow reports an empty class.
func (p *Store) Tidy(class board.TidyClass) error {
	if err := p.gate(); err != nil {
		return err
	}
	switch class {
	case board.TidyDoneDeps:
		s, _ := p.SweepPreview()
		p.mu.Lock()
		if p.pruned == nil {
			p.pruned = map[string]map[string]bool{}
		}
		for _, d := range s.DoneDeps {
			set := p.pruned[d.ID]
			if set == nil {
				set = map[string]bool{}
				p.pruned[d.ID] = set
			}
			for _, dep := range d.Deps {
				set[dep] = true
			}
		}
		p.mu.Unlock()
		return p.Reload()
	case board.TidyUnknownKeys:
		return nil
	}
	return fmt.Errorf("tidy: unknown class %d", int(class))
}
