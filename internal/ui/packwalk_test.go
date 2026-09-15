package ui

import (
	"fmt"
	"testing"
)

// The packed walk over layouts NO BOARD REACHES. Measured 2026-09-15 by
// instrumenting stepPacked and running the whole suite: 49 calls, every one of
// them at 1 or 2 columns. So nothing exercised the empty-column skip, a
// three-column walk, or an equidistant tie — the three places the rule is
// actually subtle. The skip in particular is unreachable in production at all:
// packColumns only opens a column to place a block in it, so a packed grid
// never has a hole. It is tested because it is written, not because a board
// can produce it.
//
// Built by hand rather than from a board: this is the layout arithmetic, and a
// board shaped to produce it would be a second fixture to keep honest.
//
// bite-exempt: it pins behaviour that already existed. boxLayout.step and
// mapLayout.step answered exactly this before they were merged into
// stepPacked — measured over 76,992 (shape, cursor, direction) triples, main
// and branch byte-identical, and the two originals agreed with each other in
// every one.
func TestThePackedWalkOnLayoutsTheFixtureCannotProduce(t *testing.T) {
	// cells are (column, row) pairs; the key of cell i is "k<i>".
	for _, tc := range []struct {
		name   string
		cells  [][2]int
		cols   int
		from   string
		dx, dy int
		want   string
	}{
		{"down takes the nearest row below in the same column",
			[][2]int{{0, 0}, {0, 9}, {0, 3}}, 1, "k0", 0, +1, "k2"},
		{"up takes the nearest row above, not the topmost",
			[][2]int{{0, 0}, {0, 9}, {0, 3}}, 1, "k1", 0, -1, "k2"},
		{"down at the bottom of a column stays put",
			[][2]int{{0, 0}, {0, 3}}, 1, "k1", 0, +1, "k1"},
		{"dy never leaves the column",
			[][2]int{{0, 0}, {1, 1}}, 2, "k0", 0, +1, "k0"},

		// The skip loop: the fixture never packs an empty column.
		{"right skips an empty column rather than stopping dead",
			[][2]int{{0, 0}, {2, 3}}, 3, "k0", +1, 0, "k1"},
		{"right skips two empty columns",
			[][2]int{{0, 4}, {4, 4}}, 5, "k0", +1, 0, "k1"},
		{"left skips an empty column the same way",
			[][2]int{{0, 0}, {2, 3}}, 3, "k1", -1, 0, "k0"},
		{"right off the last column stays put",
			[][2]int{{0, 0}, {1, 0}}, 2, "k1", +1, 0, "k1"},

		// Crossing picks by absolute distance, and a tie goes to the earlier row.
		{"right lands on the nearest row by absolute distance",
			[][2]int{{0, 5}, {1, 0}, {1, 4}, {1, 9}}, 2, "k0", +1, 0, "k2"},
		{"an equidistant row above beats one below",
			[][2]int{{0, 2}, {1, 3}, {1, 1}}, 2, "k0", +1, 0, "k1"},

		// Degenerate inputs the layout can hand over after a re-pack.
		{"an unknown cursor falls back to the first row",
			[][2]int{{0, 7}, {0, 2}}, 1, "gone", 0, +1, "k0"},
		{"an unknown cursor on an empty pack answers itself",
			nil, 1, "gone", 0, +1, "gone"},
		{"a zero step answers itself",
			[][2]int{{0, 0}, {0, 1}}, 1, "k0", 0, 0, "k0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Both overviews must answer alike: that is the whole reason the
			// walk has one home. The box overview keys rows by repo+id and the
			// dep map by task id, so the two are built separately here.
			box := &boxLayout{Cols: tc.cols, rowAt: map[string]int{}}
			mp := &mapLayout{Cols: tc.cols, rowAt: map[string]int{}}
			for i, c := range tc.cells {
				k := fmt.Sprintf("k%d", i)
				box.rowAt[k] = len(box.Rows)
				box.Rows = append(box.Rows, boxRow{Key: k, ID: k, Col: c[0], Y: c[1]})
				mp.rowAt[k] = len(mp.Rows)
				mp.Rows = append(mp.Rows, mapRow{ID: k, Col: c[0], Y: c[1]})
			}
			if got := box.step(tc.from, tc.dx, tc.dy); got != tc.want {
				t.Errorf("box overview: step(%q, %d, %d) = %q, want %q", tc.from, tc.dx, tc.dy, got, tc.want)
			}
			if got := mp.step(tc.from, tc.dx, tc.dy); got != tc.want {
				t.Errorf("dep map: step(%q, %d, %d) = %q, want %q", tc.from, tc.dx, tc.dy, got, tc.want)
			}
		})
	}
}
