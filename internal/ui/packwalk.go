package ui

// Cursor walking over a packed grid, shared by the two full-screen overviews.
//
// Where pack.go decides WHERE a block goes, this decides where the cursor goes
// next once the rows are placed: dy to the nearest row below or above inside
// the same column, dx to the nearest row by absolute distance in the nearest
// non-empty column. Both overviews' rows carry the same three facts (a key,
// the column, the absolute row), which is all the walk reads — the dep map
// keys its rows by task id and the box overview by repo+id, and neither
// meaning reaches here.
//
// The graph shares HALF of this and is deliberately not a caller. graphMove's
// across walk (graphview.go) is the same search this one's dx half is — same
// sentinel, same strict tie-break, same skip-the-empty-rank loop, and measured
// to agree on 119,828 random ego layouts. Its ALONG walk is not: it steps by
// slot index past the layout's dummy nodes, it swaps axes with the
// orientation, and a nil cursor does nothing there instead of falling back to
// the first row. Folding it in needs a skip predicate and a second walk mode,
// so the cost lands on this file rather than on the graph.

// packedAt is the three facts the walk reads off a packed row. Named `At`
// rather than `Cell` because a cell in this package is a display cell, which
// this is not.
type packedAt struct {
	Key    string
	Col, Y int
}

// packedRow is a row the walk can read. Implemented by value on boxRow and
// mapRow, so a layout's []Row satisfies it without copying the slice.
type packedRow interface{ at() packedAt }

// stepPacked walks from `from` by one step, returning the key it lands on —
// or `from` unchanged when the step would leave the grid. cur is the walk's
// starting row, looked up by the caller through its own rowAt map: a scan here
// would be O(n) and, where two rows share a key, would find the first where
// the map holds the last.
//
// A nil cur falls back to the first row rather than to boxLayout.First(),
// which answers "" on an empty pack where the walk must answer `from`. (The
// dep map has no First() at all.)
//
// cur must be a row of `rows`. Nothing enforces it — both callers are
// one-line methods that read their own layout, and a guard for a third caller
// that does not exist yet would be machinery for its own sake.
func stepPacked[R packedRow](rows []R, ncols int, cur *R, from string, dx, dy int) string {
	if cur == nil {
		if len(rows) == 0 {
			return from
		}
		return rows[0].at().Key
	}
	at := (*cur).at()

	if dy != 0 {
		best, bestD := "", 1<<30
		for _, r := range rows {
			c := r.at()
			if c.Col != at.Col {
				continue
			}
			if (dy > 0 && c.Y <= at.Y) || (dy < 0 && c.Y >= at.Y) {
				continue
			}
			if d := abs(c.Y - at.Y); d < bestD {
				best, bestD = c.Key, d
			}
		}
		if best == "" {
			return from
		}
		return best
	}
	if dx == 0 {
		return from
	}

	// Skip columns that hold no rows rather than stopping dead on one.
	for col := at.Col + dx; col >= 0 && col < ncols; col += dx {
		best, bestD := "", 1<<30
		for _, r := range rows {
			c := r.at()
			if c.Col != col {
				continue
			}
			if d := abs(c.Y - at.Y); d < bestD {
				best, bestD = c.Key, d
			}
		}
		if best != "" {
			return best
		}
	}
	return from
}
