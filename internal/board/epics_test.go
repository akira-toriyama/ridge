package board

import (
	"testing"
	"time"
)

// The read is `epic ls --all`, so a Board carries both populations and the
// split is the contract: Epics() is what a surface OFFERS, EpicsAll() is what
// it can RESOLVE. Everything below is one of those two sentences.
func epicBoard() *Board {
	return NewBoard(nil,
		EpicInfo{ID: "e-open1", Title: "開いている箱"},
		EpicInfo{ID: "e-shut", Title: "閉じた箱", Closed: time.Date(2026, 7, 15, 9, 12, 7, 0, time.UTC)},
		EpicInfo{ID: "e-open2", Title: "もう一つ開いている箱"},
	)
}

func TestEpicsIsTheOpenPopulationAndEpicsAllIsEverything(t *testing.T) {
	b := epicBoard()

	if got := len(b.EpicsAll()); got != 3 {
		t.Errorf("EpicsAll() = %d boxes, want all 3", got)
	}
	open := b.Epics()
	if len(open) != 2 || open[0].ID != "e-open1" || open[1].ID != "e-open2" {
		t.Errorf("Epics() = %v, want the two open boxes in furrow's order", epicIDs(open))
	}
	// Order is furrow's, and Epics() keeps it rather than re-sorting: the
	// active box leads `epic ls` and the slice panel's first row is meant to
	// be it.
	if all := b.EpicsAll(); all[1].ID != "e-shut" {
		t.Errorf("EpicsAll() reordered the read: %v", epicIDs(all))
	}
}

// The reason Epics() must stay the narrow one, stated as a test because the
// compiler cannot state it: the task edit overlay picks a box to file under by
// INDEXING this slice, so a closed box admitted here would both offer a
// finished box as a destination and shift every id after it.
func TestClosedBoxesNeverReachTheIndexedPopulation(t *testing.T) {
	b := epicBoard()
	for i, e := range b.Epics() {
		if !e.Closed.IsZero() {
			t.Errorf("Epics()[%d] = %s is closed", i, e.ID)
		}
	}
}

// Resolution is the other half: a dep or a membership pointing at a closed box
// must come back as that box, not as nil. nil is what made "(closed)" an
// unclaimable state before the --all read.
func TestEpicResolvesClosedBoxesToo(t *testing.T) {
	b := epicBoard()
	e := b.Epic("e-shut")
	if e == nil {
		t.Fatal("a closed box must still resolve by id")
	}
	if e.Closed.IsZero() {
		t.Error("the closing stamp must survive the read")
	}
	if b.Epic("e-nope") != nil {
		t.Error("an id no read serves must stay nil — that is a different state from closed")
	}
}

// Both accessors are built once, not filtered per call: the slice panel
// rebuilds its rows every frame. Two calls must be the same slice, not two
// equal ones.
func TestEpicPopulationsAreBuiltOnce(t *testing.T) {
	b := epicBoard()
	a1, a2 := b.Epics(), b.Epics()
	if len(a1) != len(a2) || (len(a1) > 0 && &a1[0] != &a2[0]) {
		t.Error("Epics() must hand back the same precomputed slice each call")
	}
	c1, c2 := b.EpicsAll(), b.EpicsAll()
	if len(c1) != len(c2) || (len(c1) > 0 && &c1[0] != &c2[0]) {
		t.Error("EpicsAll() must hand back the same precomputed slice each call")
	}
}

func epicIDs(es []EpicInfo) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.ID)
	}
	return out
}
