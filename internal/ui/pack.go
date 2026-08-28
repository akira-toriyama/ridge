package ui

// Column packing, shared by the two full-screen overviews. Pure arithmetic
// over block HEIGHTS: it knows nothing about what a block holds, which is the
// point — the dep map packs cluster panels and the box overview packs repo
// groups, and a second copy of the search below would be a second place for
// its one subtle bug to come back.
//
// Column-major, not a shortest-column fit: the reading order of an overview
// has to be "down this column, then the next one", and a masonry pack scatters
// block 4 above block 3. So the only free choice is the height each column is
// allowed to reach, and this SEARCHES for the smallest one that still fits in
// the columns available — the classic "split a sequence into k contiguous
// parts, minimising the largest part".
//
// It is a search rather than the obvious total/cols because that average is
// not always achievable: a column holding k blocks pays k-1 separators, not
// its share of all n-1, so the average can sit BELOW every feasible split. The
// greedy then starts a new column after the first block every time, runs out
// of columns, and stacks the whole remainder in the last one. Measured before
// this was a search: 9 equal clusters at 400 columns packed 1/1/1/1/5, 44 rows
// tall against a 39-row canvas, with four columns blank below row 9.

// packSpec is the geometry a caller brings to the pack.
type packSpec struct {
	Sep     int // blank rows between two blocks stacked in one column
	Gap     int // blank cells between two columns
	MinW    int // the narrowest a column may be
	MaxCols int
	Avail   int // display cells available across
}

// packColumns assigns each block a column index and answers the resulting
// column count and width. Blocks keep their order: cols[i] is non-decreasing.
func packColumns(heights []int, s packSpec) (cols []int, ncols, colW int) {
	avail := s.Avail
	if avail < 1 {
		avail = 1
	}
	maxCols := 1
	for maxCols < s.MaxCols && (maxCols+1)*s.MinW+maxCols*s.Gap <= avail {
		maxCols++
	}
	// Never more columns than blocks. Width is the scarce resource a Japanese
	// title competes for, so a board with one big block must spend all 240-400
	// cells on it rather than reserving empty columns and truncating every row
	// to a fraction of the screen.
	if len(heights) > 0 && maxCols > len(heights) {
		maxCols = len(heights)
	}

	total, tallest := 0, 0
	for i, h := range heights {
		if i > 0 {
			total += s.Sep
		}
		total += h
		if h > tallest {
			tallest = h
		}
	}

	// fill runs the greedy at one target height and returns each block's
	// column. A column always takes at least one block, so the walk always
	// terminates; a target below `tallest` simply needs one column per block.
	fill := func(target int) []int {
		out := make([]int, len(heights))
		col, h := 0, 0
		for i, bh := range heights {
			if h > 0 && h+s.Sep+bh > target {
				col++
				h = 0
			}
			if h > 0 {
				h += s.Sep
			}
			h += bh
			out[i] = col
		}
		return out
	}
	used := func(c []int) int {
		if len(c) == 0 {
			return 0
		}
		return c[len(c)-1] + 1
	}

	lo, hi := tallest, maxInt(tallest, total)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if used(fill(mid)) <= maxCols {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	cols = fill(lo)

	// Spend the width on the columns actually used. Reserving a column for a
	// block that never arrives narrows every row for nothing.
	ncols = maxInt(1, used(cols))
	colW = (avail - (ncols-1)*s.Gap) / ncols
	if colW < 1 {
		colW = 1
	}
	return cols, ncols, colW
}
