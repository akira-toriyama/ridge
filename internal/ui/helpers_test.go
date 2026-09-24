package ui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// Shared constructors for the package's tests, and nothing else. The only
// t.Fatal here is a setup guard (advTallModel refusing a size at which its
// column does not fold); no test's subject is asserted in this file.
//
// boardModel / logicModel serve the 34-task fixture through memstore. The
// adv* boards are 2-40 card synthetic boards for a test whose precondition
// the fixture does not promise — an empty lane, a column that folds, one open
// blocker, two clusters that pack side by side, one cluster taller than any
// canvas, a chain with a blocker above and a dependant below its middle under
// a box with a title, a done member of a box; short ASCII titles that make the
// arithmetic readable. emptyProvider answers every query with nothing, so a
// test that filters must use advModel (memstore) instead, and its Reload
// swaps in an EMPTY board — the state a reload test wants to see survive. A
// test whose subject does not need the fixture's shape builds its board from
// these rather than guarding with t.Skip: one task added to the fixture once
// silenced three tests and broke 21 (t-38fm).

func boardModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := logicModel(t, w, h)
	m.relayout()
	return m
}

// logicModel is boardModel without the layout pass, for tests that only
// exercise the board/column logic (commitMove, cursor arithmetic) and never
// read m.lay. relayout is 97% of boardModel's cost (measured: 1.90ms of
// 1.96ms), and a test that builds 1,361 models paid 39s of a 141s -race run
// for layouts it never looked at.
func logicModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := New(memstore.New(), Options{})
	m.w, m.h = w, h
	m.recompute()
	return m
}

type emptyProvider struct{ b *board.Board }

func (p *emptyProvider) Board() *board.Board            { return p.b }
func (p *emptyProvider) Reload() error                  { p.b = board.NewBoard(nil); return nil }
func (p *emptyProvider) Sync() error                    { return fmt.Errorf("no store") }
func (p *emptyProvider) Query(string) ([]string, error) { return nil, nil }
func (p *emptyProvider) Live() bool                     { return false }
func (p *emptyProvider) PersistMove(_, _, _, _ string) (board.MoveReport, error) {
	return board.MoveReport{}, nil
}
func (p *emptyProvider) PersistDone(_ string) (*board.RepeatReport, error)  { return nil, nil }
func (p *emptyProvider) PersistCheck(_ string, _ int, _ bool) error         { return nil }
func (p *emptyProvider) PersistBody(_, _ string) error                      { return nil }
func (p *emptyProvider) PersistNote(_, _ string) error                      { return nil }
func (p *emptyProvider) PersistReview(_ string) error                       { return nil }
func (p *emptyProvider) Revisit(string) ([]board.Revisit, error)            { return nil, nil }
func (p *emptyProvider) PersistFields(_ string, _ board.FieldPatch) error   { return nil }
func (p *emptyProvider) PersistCheckAdd(_, _ string) error                  { return nil }
func (p *emptyProvider) PersistCheckRm(_ string, _ int) error               { return nil }
func (p *emptyProvider) PersistCheckReword(_ string, _ int, _ string) error { return nil }
func (p *emptyProvider) PersistDepAdd(_, _ string) error                    { return nil }
func (p *emptyProvider) PersistDepRm(_, _ string) error                     { return nil }
func (p *emptyProvider) EpicSet(string, board.EpicPatch) error              { return nil }
func (p *emptyProvider) EpicActivate(_, _ string) error                     { return nil }
func (p *emptyProvider) EpicDepAdd(_, _ string) error                       { return nil }
func (p *emptyProvider) EpicDepRm(_, _ string) error                        { return nil }

func (p *emptyProvider) EpicAdd(string, board.EpicAddOptions) (string, error) {
	return "", fmt.Errorf("no store")
}

func (p *emptyProvider) EpicDeactivate(string) (board.EpicPrevious, error) {
	return board.EpicPrevious{}, nil
}

func (p *emptyProvider) EpicDone(string) (board.EpicPrevious, error) {
	return board.EpicPrevious{}, nil
}
func (p *emptyProvider) EpicReopen(string) error { return nil }
func (p *emptyProvider) Add(string, board.AddOptions) (string, error) {
	return "", fmt.Errorf("no store")
}
func (p *emptyProvider) SweepPreview() (board.Sweep, error) { return board.Sweep{}, nil }
func (p *emptyProvider) Archive([]string) error             { return nil }
func (p *emptyProvider) Unarchive([]string) error           { return nil }
func (p *emptyProvider) Tidy(board.TidyClass) error         { return nil }

// advModel serves a synthetic board through memstore, so the filter answers
// on it (emptyProvider's Query answers nothing).
func advModel(t *testing.T, b *board.Board, w, h int) *Model {
	t.Helper()
	m := New(memstore.NewWith(b), Options{})
	m.w, m.h = w, h
	m.recompute()
	m.relayout()
	return m
}

// A tiny synthetic board: short ASCII titles so several cards fit a column and
// the arithmetic is easy to read.
func advSmallBoard() *board.Board {
	var ts []*board.Task
	for i, id := range []string{"a1", "a2", "a3"} {
		ts = append(ts, &board.Task{ID: id, Title: id, Status: "ready", Priority: (i + 1) * 10})
	}
	for i, id := range []string{"b1", "b2", "b3", "b4"} {
		ts = append(ts, &board.Task{ID: id, Title: id, Status: "backlog", Priority: (i + 1) * 10})
	}
	return board.NewBoard(ts)
}

func advSmallModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := New(&emptyProvider{b: advSmallBoard()}, Options{})
	m.w, m.h = w, h
	m.recompute()
	m.relayout()
	return m
}

// advTallBoard overflows one column: twelve short cards in backlog, so a
// short terminal folds it, and the first two carry a label the filter can
// keep. The shipped fixture folds only at some sizes, and every test that
// asked it to skipped in silence the day it stopped.
func advTallBoard() *board.Board {
	var ts []*board.Task
	for i := 1; i <= 12; i++ {
		id := fmt.Sprintf("c%02d", i)
		task := &board.Task{ID: id, Title: id, Status: "backlog", Priority: i * 10}
		if i <= 2 {
			task.Labels = []string{"keep"}
		}
		ts = append(ts, task)
	}
	// r1 keeps the label too, so a filter to label:keep leaves the cursor
	// where a test parked it.
	ts = append(ts, &board.Task{ID: "r1", Title: "r1", Status: "ready", Priority: 10, Labels: []string{"keep"}})
	return board.NewBoard(ts)
}

// advTallModel is advTallBoard at a size where backlog folds, and it fails
// the test outright when it does not: the fold is the whole point.
func advTallModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := advModel(t, advTallBoard(), w, h)
	col := m.lay.Col("backlog")
	if col == nil {
		t.Fatalf("setup: no backlog column at %dx%d", w, h)
	}
	if col.Hidden == 0 || len(col.Cards) == 0 {
		t.Fatalf("setup: advTallBoard does not fold at %dx%d (cards=%d hidden=%d)",
			w, h, len(col.Cards), col.Hidden)
	}
	return m
}

// advDepBoard is one open blocker: d1 waits on d2, both in backlog.
func advDepBoard() *board.Board {
	return board.NewBoard([]*board.Task{
		{ID: "d1", Title: "d1", Status: "backlog", Priority: 10, Deps: []string{"d2"}},
		{ID: "d2", Title: "d2", Status: "backlog", Priority: 20},
	})
}

// advMapBoard is what the dep map's walks need and the fixture does not
// promise: two OPEN clusters (m1,m3 wait on m2; m4 waits on m5), which at 240
// columns pack side by side so a sideways walk has a column to cross; one
// finished pair (m6 waited on m7, both done), present at scope=all and gone at
// scope=open; and m8, in no cluster at all, so opening the map on it lands on
// a fallback row nobody chose.
func advMapBoard() *board.Board {
	return board.NewBoard([]*board.Task{
		{ID: "m1", Title: "waits on two", Status: "backlog", Priority: 10, Deps: []string{"m2"}},
		{ID: "m2", Title: "blocks one and three", Status: "backlog", Priority: 20},
		{ID: "m3", Title: "also waits on two", Status: "backlog", Priority: 30, Deps: []string{"m2"}},
		{ID: "m4", Title: "waits on five", Status: "ready", Priority: 10, Deps: []string{"m5"}},
		{ID: "m5", Title: "blocks four", Status: "backlog", Priority: 40},
		{ID: "m6", Title: "finished after seven", Status: "done", Priority: 10, Deps: []string{"m7"}},
		{ID: "m7", Title: "finished first", Status: "done", Priority: 20},
		{ID: "m8", Title: "no edges at all", Status: "backlog", Priority: 50},
	})
}

// advChainBoard is one cluster of n tasks, each waiting on the next, so the
// packed map is n+2 rows tall and overflows any canvas shorter than that —
// the state the map's scroll and paging tests need at a short terminal.
func advChainBoard(n int) *board.Board {
	ts := make([]*board.Task, 0, n)
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("k%02d", i)
		task := &board.Task{ID: id, Title: id, Status: "backlog", Priority: i * 10}
		if i < n {
			task.Deps = []string{fmt.Sprintf("k%02d", i+1)}
		}
		ts = append(ts, task)
	}
	return board.NewBoard(ts)
}

// advGraphBoard is a three-task chain with structure in both directions
// around g2 — g3 blocks it, g1 waits on it — and g2 is filed under a box
// whose title the graph strip has to resolve.
func advGraphBoard() *board.Board {
	return board.NewBoard([]*board.Task{
		{ID: "g1", Title: "waits on the middle", Status: "backlog", Priority: 10, Deps: []string{"g2"}},
		{ID: "g2", Title: "the middle of the chain", Status: "backlog", Priority: 20, Deps: []string{"g3"}, Epic: "e-gbox"},
		{ID: "g3", Title: "blocks the middle", Status: "backlog", Priority: 30},
	}, board.EpicInfo{ID: "e-gbox", Title: "graph strip resolves this"})
}

// advSwimBoard is two boxes across two lanes plus a finished member: backlog
// holds two tasks of box a — so a walk down the seeded band reaches a second
// row before any header — and one of box b, so the same walk then crosses a
// band header; s4 is done inside box a, which the open scope drops.
func advSwimBoard() *board.Board {
	return board.NewBoard([]*board.Task{
		{ID: "s1", Title: "s1", Status: "backlog", Priority: 10, Epic: "e-sa"},
		{ID: "s2", Title: "s2", Status: "backlog", Priority: 20, Epic: "e-sb"},
		{ID: "s3", Title: "s3", Status: "ready", Priority: 10, Epic: "e-sa"},
		{ID: "s4", Title: "s4", Status: "done", Priority: 10, Epic: "e-sa"},
		{ID: "s5", Title: "s5", Status: "backlog", Priority: 30, Epic: "e-sa"},
	}, board.EpicInfo{ID: "e-sa", Title: "box a"}, board.EpicInfo{ID: "e-sb", Title: "box b"})
}

func ids(ts []*board.Task) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.ID
	}
	return out
}

func indexOf(ts []*board.Task, id string) int {
	for i, t := range ts {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func pinIDs(p map[string]bool) []string {
	var out []string
	for k := range p {
		out = append(out, k)
	}
	return out
}

func keyCodeFor(s string) rune {
	switch s {
	case "enter":
		return tea.KeyEnter
	case "esc":
		return tea.KeyEscape
	case "space":
		return tea.KeySpace
	}
	return []rune(s)[0]
}

func keyTextFor(s string) string {
	switch s {
	case "enter", "esc", "space":
		return ""
	}
	return s
}
