package memstore

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The withdrawals over the fixture (board.Provider Remove / EpicRemove):
// `furrow rm` mirrored as far as a -dump frame and a test need — the
// references furrow would list (dep edges, live [[id]] links in other
// bodies, members, epic dep edges), the refusal while any stand, and what
// --force severs. Assets: the fixture holds none, so the transfer is always
// empty. Session-only like the sweep: a removal survives Reload (shape), and
// furrow's "the id is never reused" holds because the fixture mints none.
// Done/Total of a box that lost a member stay as declared — they are
// furrow-derived and the fixture recomputes them after no write.

// referencesTo is what still points at the targets, in board order: the
// task-side edges and links first, then the boxes' dep edges. References
// among the targets themselves never count (furrow: a chain removes in one
// call). Two limits, both unobservable on the fixture: furrow walks the
// boxes' bodies for [[links]] too (board.EpicInfo carries no body), and it
// reads a link out of the prose (core.ExtractLinks) where this counts a
// `[[id]]` inside a code span as well.
func referencesTo(b *board.Board, targets map[string]bool) board.References {
	var r board.References
	ids := slices.Sorted(maps.Keys(targets))
	for _, t := range b.Tasks() {
		if targets[t.ID] {
			continue
		}
		for _, d := range t.Deps {
			if targets[d] {
				r.Deps = append(r.Deps, board.RefEdge{From: t.ID, To: d})
			}
		}
		if t.Epic != "" && targets[t.Epic] {
			r.Members = append(r.Members, board.RefMember{Task: t.ID, Epic: t.Epic})
		}
		for _, id := range ids {
			if strings.Contains(t.Body, "[["+id+"]]") {
				r.Links = append(r.Links, board.RefLink{Body: t.ID, To: id})
			}
		}
	}
	for _, e := range b.EpicsAll() {
		if targets[e.ID] {
			continue
		}
		for _, d := range e.Deps {
			if targets[d] {
				r.EpicDeps = append(r.EpicDeps, board.RefEdge{From: e.ID, To: d})
			}
		}
	}
	return r
}

// removeWith is shape's second pass: the withdrawn tasks and boxes leave the
// board, and what --force severed stays severed — a dep edge naming a removed
// task is dropped, a member of a removed box is unfiled, a box's dep on a
// removed box is dropped, and a live [[id]] link to a removed id reads as the
// bare id (the prose keeps its words, as furrow's de-link does).
func removeWith(b *board.Board, removed map[string]bool) *board.Board {
	if len(removed) == 0 {
		return b
	}
	tasks := make([]*board.Task, 0, len(b.Tasks()))
	for _, t := range b.Tasks() {
		if removed[t.ID] {
			continue
		}
		c := cloneTask(*t)
		c.Deps = slices.DeleteFunc(c.Deps, func(d string) bool { return removed[d] })
		if removed[c.Epic] {
			c.Epic = ""
		}
		for id := range removed {
			c.Body = strings.ReplaceAll(c.Body, "[["+id+"]]", id)
		}
		tasks = append(tasks, &c)
	}
	epics := make([]board.EpicInfo, 0, len(b.EpicsAll()))
	for _, e := range cloneEpics(b.EpicsAll()) {
		if removed[e.ID] {
			continue
		}
		e.Deps = slices.DeleteFunc(e.Deps, func(d string) bool { return removed[d] })
		e.OpenDeps = slices.DeleteFunc(e.OpenDeps, func(d string) bool { return removed[d] })
		epics = append(epics, e)
	}
	return board.NewStoreBoard(b.Lanes(), tasks, epics, b.Writable(), b.SchemaState())
}

// referencedErr is furrow's refusal in its words (app/remove.go
// referencedErr, re-read on dev 2026-09-27): the summary, the two ways out
// and what --force does, then the subject the envelope carries.
func referencedErr(subject string, refs board.References) error {
	return fmt.Errorf("still referenced — %s; drop the references first, or pass --force to sever them "+
		"(dep edges dropped, [[links]] de-linked to the bare id, members unfiled) (referenced: %s)",
		refs.Summary(), subject)
}

// Remove is `furrow rm <ids>` over the fixture (board.Provider): all-or-
// nothing on a miss, the refusal while referenced unless Force — on the dry
// run too, as furrow refuses it (board.Provider says where that was
// measured) — the preview on Apply=false, the removal otherwise.
func (p *Store) Remove(ids []string, o board.RemoveOptions) (board.RemoveReport, error) {
	if err := board.ValidateSweepIDs("rm", ids); err != nil {
		return board.RemoveReport{}, err
	}
	if o.Apply {
		if err := p.gate(); err != nil {
			return board.RemoveReport{}, err
		}
	}
	b := p.snapshot()
	targets := make(map[string]bool, len(ids))
	rep := board.RemoveReport{DryRun: !o.Apply, Force: o.Force}
	for _, id := range ids {
		t := b.Task(id)
		if t == nil {
			return board.RemoveReport{}, fmt.Errorf("task not found: %s", id)
		}
		targets[id] = true
		rep.Tasks = append(rep.Tasks, board.RemovedTask{ID: t.ID, Title: t.Title, Repeat: t.Repeat, Due: t.Due})
	}
	rep.References = referencesTo(b, targets)
	if !rep.References.Empty() && !o.Force {
		return board.RemoveReport{}, referencedErr(strings.Join(ids, ","), rep.References)
	}
	if !o.Apply {
		return rep, nil
	}
	p.withdraw(ids)
	return rep, nil
}

// EpicRemove is `furrow epic rm <id>` over the fixture (board.Provider).
func (p *Store) EpicRemove(id string, o board.RemoveOptions) (board.RemoveReport, error) {
	if err := board.ValidateSweepIDs("epic rm", []string{id}); err != nil {
		return board.RemoveReport{}, err
	}
	if o.Apply {
		if err := p.gate(); err != nil {
			return board.RemoveReport{}, err
		}
	}
	b := p.snapshot()
	e := b.Epic(id)
	if e == nil {
		return board.RemoveReport{}, fmt.Errorf("unknown epic %q", id)
	}
	rep := board.RemoveReport{DryRun: !o.Apply, Force: o.Force, Epic: &board.RemovedEpic{ID: e.ID, Title: e.Title, Active: e.Active}}
	rep.References = referencesTo(b, map[string]bool{id: true})
	if !rep.References.Empty() && !o.Force {
		return board.RemoveReport{}, referencedErr(id, rep.References)
	}
	if !o.Apply {
		return rep, nil
	}
	p.withdraw([]string{id})
	return rep, nil
}

// withdraw records the ids as removed and reshapes the CURRENT snapshot —
// never Reload, the fixture's discard operation (sweep.go says why).
func (p *Store) withdraw(ids []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.removed == nil {
		p.removed = map[string]bool{}
	}
	for _, id := range ids {
		p.removed[id] = true
	}
	p.b = removeWith(p.b, p.removed)
}
