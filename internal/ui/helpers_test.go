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
// adv* boards are 2-13 card synthetic boards for a test whose precondition
// the fixture does not promise — an empty lane, a column that folds, one open
// blocker, short ASCII titles that make the arithmetic readable. emptyProvider
// answers every query with nothing, so a test that filters must use advModel
// (memstore) instead, and its Reload swaps in an EMPTY board — the state a
// reload test wants to see survive. A test whose subject does not need the
// fixture's shape builds its board from these rather than guarding with
// t.Skip: one task added to the fixture once silenced three tests and broke
// 21 (t-38fm).

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
func (p *emptyProvider) PersistMove(_, _, _, _ string) ([]string, error) {
	return nil, nil
}
func (p *emptyProvider) PersistDone(_ string) error                         { return nil }
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
