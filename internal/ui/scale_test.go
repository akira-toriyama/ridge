package ui

import (
	"fmt"
	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
	"strings"
	"testing"
)

// scaledTasks inflates the 33-task fixture to n tasks by cloning it with fresh
// ids, so the board has a realistic per-lane depth. The real central board is
// 658 tasks; the fixture is 33, so nothing here had measured what a full
// board costs to render.
func scaledTasks(n int) []*board.Task {
	base := memstore.New().Board().Tasks()
	out := make([]*board.Task, 0, n)
	for i := 0; len(out) < n; i++ {
		for _, t := range base {
			if len(out) >= n {
				break
			}
			c := *t
			if i > 0 {
				c.ID = fmt.Sprintf("%s-%d", t.ID, i)
				c.Deps = nil // clones must not duplicate the original's dep edges
				c.Priority = t.Priority + i*1000
			}
			out = append(out, &c)
		}
	}
	return out
}

func scaledModel(n, w, h int) *Model {
	m := New(memstore.NewWith(board.NewBoard(scaledTasks(n))), Options{})
	m.w, m.h = w, h
	m.recompute()
	m.relayout()
	return m
}

// BenchmarkViewAtBoardSize measures one full frame render. A TUI has to repaint
// on every keystroke and on every mouse-motion event during a drag, so the
// budget is a frame time comfortably under ~16ms; anything approaching that
// makes dragging feel heavy.
func BenchmarkViewAtBoardSize(b *testing.B) {
	for _, n := range []int{24, 100, 658, 2000} {
		b.Run(fmt.Sprintf("tasks=%d", n), func(b *testing.B) {
			m := scaledModel(n, 150, 40)
			b.ReportAllocs()
			for b.Loop() {
				_ = m.View()
			}
		})
	}
}

// BenchmarkRecomputeAtBoardSize measures the filter+group pass that runs
// whenever the board or the query changes (not on every keystroke).
func BenchmarkRecomputeAtBoardSize(b *testing.B) {
	for _, n := range []int{24, 658, 2000} {
		b.Run(fmt.Sprintf("tasks=%d", n), func(b *testing.B) {
			m := scaledModel(n, 150, 40)
			b.ReportAllocs()
			for b.Loop() {
				m.recompute()
			}
		})
	}
}

// TestScalesToARealBoard is the assertion, not just a measurement: a full-size
// board must still render a sane frame.
//
// Both original assertions were tautologies — `len(m.b.Tasks()) != 658` is true
// by construction and a composited frame is never the empty string — so a
// 658-task board that rendered ZERO cards in every lane passed. It asserts the
// board's actual job now: a lane with tasks shows cards, and those cards carry
// their ids.
func TestScalesToARealBoard(t *testing.T) {
	m := scaledModel(658, 150, 40)
	out := ansiStrip(m.View().Content)

	populated := 0
	for i := range m.lay.Cols {
		c := &m.lay.Cols[i]
		if len(c.Tasks) == 0 {
			continue
		}
		populated++
		if len(c.Cards) == 0 {
			t.Errorf("lane %s holds %d tasks and rendered no cards at all", c.Lane.Name, len(c.Tasks))
			continue
		}
		// Lane headers alone are not a board: the first card's id must be on
		// screen.
		if id := c.Tasks[c.Scroll].ID; !strings.Contains(out, id) {
			t.Errorf("lane %s renders %d cards but %s is not in the frame", c.Lane.Name, len(c.Cards), id)
		}
	}
	if populated == 0 {
		t.Fatal("a 658-task board produced no populated lane")
	}
}
