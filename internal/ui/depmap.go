package ui

import "github.com/akira-toriyama/ridge/internal/board"

// The dependency MAP's layout engine — pure, deterministic, and with no
// knowledge of lipgloss, the theme or the terminal. depmapview.go draws what
// this file decides. board.Clusters decided the STRUCTURE; this file only
// decides where each cluster's panel lands.
//
// The shape is EVERY CLUSTER AT ONCE, as indented lists packed into columns.
// It is deliberately NOT a drawn graph, and that is the layout research's
// central finding rather than a shortcut: at this scale naming the blocker
// inline ("←t-jkqt,t-nysn") is unambiguous, costs one row per node, and lets a
// node reachable by two paths appear ONCE — which is the single weakness of
// the tree view. Lines between boxes buy nothing here and cost the width the
// Japanese titles need.
//
// The counterpart view is the ego graph (graph.go): that one answers "what
// surrounds THIS task", this one answers "what is the board tangled into".

const (
	// mapPanelMinW is the narrowest a cluster panel may be. Below it a row is
	// all marker and id with no room for a Japanese title — median 82 display
	// cells on the real board — so the map would degrade into a list of ids.
	mapPanelMinW = 64
	mapPanelGap  = 2 // blank cells between two columns
	mapMaxCols   = 5
	mapPanelSep  = 1 // blank row between two panels stacked in one column
	mapPanelHdr  = 1 // the "── #1  8 nodes · depth 4 ──" rule
	mapPanelFoot = 1 // the per-cluster stat line

	// mapMaxIndent caps how far a deep node is pushed right. The real board's
	// longest chain is 5 edges, so this never bites there; it exists so a
	// pathological chain eats its own title instead of the whole panel.
	mapMaxIndent = 8
	mapIndentW   = 2

	// mapSelGutter is the width of the left bar that marks the selected row.
	// A gutter rather than a background: the row is already a composition of
	// styled fragments (marker, id chip, title, blocker names), and wrapping
	// that in one more style would fight every reset inside it.
	mapSelGutter = 2
)

// mapRow is one node's placed line — the unit the cursor moves over.
type mapRow struct {
	ID    string
	Panel int
	Col   int
	Y     int // absolute row inside the packed canvas
}

// mapIndent is a node's leading indent in display cells, spelled ONCE: the
// renderer and any future hit-test must agree, and the graph view's own comment
// records what disagreeing costs.
func mapIndent(depth int) int {
	if depth > mapMaxIndent {
		depth = mapMaxIndent
	}
	return depth * mapIndentW
}

// mapPanel is one cluster's placed block.
type mapPanel struct {
	Cluster board.Cluster
	Num     int // 1-based; the "#3" in the panel header
	Col     int
	Y       int
	H       int
}

// mapLayout is one frame's worth of map geometry.
type mapLayout struct {
	Scope  board.ClusterScope
	Panels []mapPanel
	Rows   []mapRow
	rowAt  map[string]int // a task is in at most one cluster, so id is a key

	Cols int
	ColW int
	W, H int
}

// Row is the placed row for a task id, or nil when it is in no cluster.
func (l *mapLayout) Row(id string) *mapRow {
	i, ok := l.rowAt[id]
	if !ok {
		return nil
	}
	return &l.Rows[i]
}

// Empty reports a board with no dependency edges in scope at all. The view
// says so in words rather than drawing an empty grid and leaving the reader to
// wonder what broke.
func (l *mapLayout) Empty() bool { return len(l.Panels) == 0 }

// mapPanelH is a cluster panel's height: the rule, one row per node, the stat
// line. It is arithmetic rather than a measurement because every one of those
// rows is composed to exactly ColW cells by pad() — unlike a card, whose
// height depends on how its title wraps.
func mapPanelH(c board.Cluster) int { return mapPanelHdr + len(c.Nodes) + mapPanelFoot }

// packMap lays the clusters into columns.
//
// Column-major, not a shortest-column fit: the reading order of an overview
// has to be "down this column, then the next one", and a masonry pack scatters
// #4 above #3. So the only free choice is the height each column is allowed to
// reach, and packMap SEARCHES for the smallest one that still fits in the
// columns available — the classic "split a sequence into k contiguous parts,
// minimising the largest part".
//
// It is a search rather than the obvious total/cols because that average is
// not always achievable: a column holding k panels pays k-1 separators, not
// its share of all n-1, so the average can sit BELOW every feasible split. The
// greedy then starts a new column after the first panel every time, runs out
// of columns, and stacks the whole remainder in the last one. Measured before
// this was a search: 9 equal clusters at 400 columns packed 1/1/1/1/5, 44 rows
// tall against a 39-row canvas, with four columns blank below row 9.
func packMap(scope board.ClusterScope, clusters []board.Cluster, avail int) *mapLayout {
	if avail < 1 {
		avail = 1
	}
	maxCols := 1
	for maxCols < mapMaxCols && (maxCols+1)*mapPanelMinW+maxCols*mapPanelGap <= avail {
		maxCols++
	}
	// Never more columns than clusters. Width is the scarce resource a
	// Japanese title competes for, so a board with one big tangle must spend
	// all 240-400 cells on it rather than reserving empty columns and
	// truncating every row to a fraction of the screen.
	if len(clusters) > 0 && maxCols > len(clusters) {
		maxCols = len(clusters)
	}

	heights := make([]int, len(clusters))
	total, tallest := 0, 0
	for i, c := range clusters {
		heights[i] = mapPanelH(c)
		if i > 0 {
			total += mapPanelSep
		}
		total += heights[i]
		if heights[i] > tallest {
			tallest = heights[i]
		}
	}

	// fill runs the greedy at one target height and returns each panel's
	// column. A column always takes at least one panel, so the walk always
	// terminates; a target below `tallest` simply needs one column per panel.
	fill := func(target int) []int {
		cols := make([]int, len(clusters))
		col, h := 0, 0
		for i, ph := range heights {
			if h > 0 && h+mapPanelSep+ph > target {
				col++
				h = 0
			}
			if h > 0 {
				h += mapPanelSep
			}
			h += ph
			cols[i] = col
		}
		return cols
	}
	used := func(cols []int) int {
		if len(cols) == 0 {
			return 0
		}
		return cols[len(cols)-1] + 1
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
	placed := fill(lo)

	// Spend the width on the columns actually used. Reserving a column for a
	// panel that never arrives narrows every row for nothing.
	cols := maxInt(1, used(placed))
	colW := (avail - (cols-1)*mapPanelGap) / cols
	if colW < 1 {
		colW = 1
	}

	l := &mapLayout{Scope: scope, Cols: cols, ColW: colW, W: avail, rowAt: map[string]int{}}
	colH := make([]int, cols)
	for i, c := range clusters {
		col := placed[i]
		y := colH[col]
		if y > 0 {
			y += mapPanelSep
		}
		for j, n := range c.Nodes {
			l.rowAt[n.ID] = len(l.Rows)
			l.Rows = append(l.Rows, mapRow{
				ID:    n.ID,
				Panel: len(l.Panels),
				Col:   col,
				Y:     y + mapPanelHdr + j,
			})
		}
		l.Panels = append(l.Panels, mapPanel{Cluster: c, Num: i + 1, Col: col, Y: y, H: heights[i]})
		colH[col] = y + heights[i]
	}
	for _, h := range colH {
		if h > l.H {
			l.H = h
		}
	}
	return l
}

// step walks the cursor. dy moves within a column, dx crosses to the nearest
// row of a neighbouring column — the same "keep the position, change the axis"
// rule the graph's node walk uses, so the two full-screen views move alike.
func (l *mapLayout) step(from string, dx, dy int) string {
	cur := l.Row(from)
	if cur == nil {
		if len(l.Rows) == 0 {
			return from
		}
		return l.Rows[0].ID
	}
	if dy != 0 {
		best, bestD := "", 1<<30
		for _, r := range l.Rows {
			if r.Col != cur.Col {
				continue
			}
			if dy > 0 && r.Y <= cur.Y {
				continue
			}
			if dy < 0 && r.Y >= cur.Y {
				continue
			}
			if d := abs(r.Y - cur.Y); d < bestD {
				best, bestD = r.ID, d
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
	for c := cur.Col + dx; c >= 0 && c < l.Cols; c += dx {
		best, bestD := "", 1<<30
		for _, r := range l.Rows {
			if r.Col != c {
				continue
			}
			if d := abs(r.Y - cur.Y); d < bestD {
				best, bestD = r.ID, d
			}
		}
		if best != "" {
			return best
		}
	}
	return from
}
