package ui

import "testing"

// wrapIdx is every overlay cursor's arithmetic: both ends wrap, and an empty
// list parks the cursor on 0 rather than leaving a stale index in place.
func TestWrapIdx(t *testing.T) {
	cases := []struct{ i, n, want int }{
		{0, 3, 0},
		{2, 3, 2},
		{3, 3, 0},
		{-1, 3, 2},
		{5, 3, 2},
		{-1, 0, 0},
		{4, 0, 0},
		{0, 1, 0},
		{-1, 1, 0},
	}
	for _, c := range cases {
		if got := wrapIdx(c.i, c.n); got != c.want {
			t.Errorf("wrapIdx(%d, %d) = %d, want %d", c.i, c.n, got, c.want)
		}
	}
}
