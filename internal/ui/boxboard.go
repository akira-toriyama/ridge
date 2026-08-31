package ui

import (
	"sort"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The BOX OVERVIEW's layout engine — pure, deterministic, and with no
// knowledge of lipgloss, the theme or the terminal. boxboardview.go draws what
// this file decides.
//
// The shape is EVERY BOX AT ONCE, grouped by repo, packed into columns. It is
// deliberately not a graph, and that is a measurement rather than a shortcut:
// on the real board (2026-08-28) there are 153 boxes, 117 open and 36 closed,
// carrying **four** epic-to-epic dep edges between them, across two boxes. An
// ego graph or a connected-component layout over four edges draws a nearly
// empty picture and buries the 149 boxes that are the actual content. The four
// edges ride inline as the dep map's `←id` tag, which costs nothing.
//
// Repo is the grouping because it is the axis every box has (zero repo-less
// boxes on the real board, 31 repos) and because furrow allows at most ONE
// active box per repo — so the ▶ markers read down the frame as "what each
// repo is working out of right now", which is the question the view exists to
// answer.
//
// A box naming two repos is listed under BOTH. Deduplicating it would make one
// of the two repos silently incomplete, and the row's own repo is not visible
// from inside a group whose header already names one.

const (
	// boxPanelMinW is the narrowest a repo group may be. Epic titles measure
	// median 11 display cells, p90 67, max 139 on the real board — much shorter
	// than task titles, which is why this is well below the dep map's 64.
	boxPanelMinW = 48
	boxPanelGap  = 2 // blank cells between two columns
	boxMaxCols   = 6
	boxPanelSep  = 1 // blank row between two groups stacked in one column
	boxPanelHdr  = 1 // the "── owner/repo  N ──" rule

	// boxSelGutter is the width of the left bar marking the selected row, for
	// the reason mapSelGutter gives: the row is a composition of styled
	// fragments and wrapping it in one more style fights every reset inside.
	boxSelGutter = 2
)

// boxNoRepo is the group a repo-less box lands in. furrow refuses to ACTIVATE
// one, but it will happily hold one, so the view must have somewhere to put it
// rather than dropping it out of an "every box" overview.
//
// The empty string is the KEY, not the label: a repo really named "(no repo)"
// would otherwise merge into it. Nothing furrow serves is repo-named "", since
// a repo is owner/repo.
const boxNoRepo = ""

// boxNoRepoLabel is what the group's header says.
const boxNoRepoLabel = "(no repo)"

// boxRow is one box's placed line — the unit the cursor moves over.
//
// Key, not ID, is the cursor's identity: a box naming two repos is placed
// twice, so an id alone cannot say which of the two rows the cursor is on.
type boxRow struct {
	Key   string
	ID    string
	Repo  string
	Group int
	Col   int
	Y     int // absolute row inside the packed canvas
}

// boxKey is the cursor identity for one placement, spelled once so the layout,
// the walk and the view cannot disagree. Repo and id are both stable across a
// re-pack; a row INDEX is not.
func boxKey(repo, id string) string { return repo + "\x00" + id }

// boxGroup is one repo's placed block.
type boxGroup struct {
	Repo  string
	Boxes []board.EpicInfo
	Done  int // member tasks closed, summed over the group — furrow's numbers
	Total int
	Col   int
	Y     int
	H     int
}

// boxLayout is one frame's worth of overview geometry.
type boxLayout struct {
	All    bool // the closed boxes are in this pack
	Groups []boxGroup
	Rows   []boxRow
	rowAt  map[string]int

	Cols int
	ColW int
	W, H int
}

// Row is the placed row for a cursor key, or nil when it is not in this pack.
func (l *boxLayout) Row(key string) *boxRow {
	i, ok := l.rowAt[key]
	if !ok {
		return nil
	}
	return &l.Rows[i]
}

// Empty reports a board with no boxes at all in scope — said in words rather
// than drawn as an empty grid.
func (l *boxLayout) Empty() bool { return len(l.Rows) == 0 }

// First is the key the cursor lands on when it has none, or when a scope change
// removed the row it was on.
func (l *boxLayout) First() string {
	if len(l.Rows) == 0 {
		return ""
	}
	return l.Rows[0].Key
}

// packBoxes groups the population by repo and lays the groups into columns.
//
// Repo ORDER is alphabetical, with the repo-less group last. Not
// activity-ordered: an overview whose rows move when a box is activated is
// disorienting, and the ▶ markers are what carry the activity anyway. Inside a
// group the order is furrow's own (`epic ls` is active-first), so the box a
// repo is working out of leads its group.
func packBoxes(boxes []board.EpicInfo, all bool, avail int) *boxLayout {
	byRepo := map[string][]board.EpicInfo{}
	for _, e := range boxes {
		if len(e.Repos) == 0 {
			byRepo[boxNoRepo] = append(byRepo[boxNoRepo], e)
			continue
		}
		seen := make(map[string]bool, len(e.Repos))
		for _, r := range e.Repos {
			// Deduped: two identical repos would place the box twice under ONE
			// key, leaving the second placement unreachable and both rows
			// drawing the selection gutter.
			if seen[r] {
				continue
			}
			seen[r] = true
			byRepo[r] = append(byRepo[r], e)
		}
	}
	repos := make([]string, 0, len(byRepo))
	for r := range byRepo {
		if r != boxNoRepo {
			repos = append(repos, r)
		}
	}
	sort.Strings(repos)
	// Appended AFTER the sort, never sorted with them: the sentinel is "" and
	// would otherwise lead the frame.
	if _, ok := byRepo[boxNoRepo]; ok {
		repos = append(repos, boxNoRepo)
	}

	groups := make([]boxGroup, 0, len(repos))
	heights := make([]int, 0, len(repos))
	for _, r := range repos {
		g := boxGroup{Repo: r, Boxes: byRepo[r]}
		for _, e := range g.Boxes {
			g.Done += e.Done
			g.Total += e.Total
		}
		groups = append(groups, g)
		heights = append(heights, boxPanelHdr+len(g.Boxes))
	}

	placed, cols, colW := packColumns(heights, packSpec{
		Sep: boxPanelSep, Gap: boxPanelGap, MinW: boxPanelMinW, MaxCols: boxMaxCols, Avail: avail,
	})

	l := &boxLayout{All: all, Cols: cols, ColW: colW, W: avail, rowAt: map[string]int{}}
	colH := make([]int, cols)
	for i := range groups {
		col := placed[i]
		y := colH[col]
		if y > 0 {
			y += boxPanelSep
		}
		for j, e := range groups[i].Boxes {
			key := boxKey(groups[i].Repo, e.ID)
			l.rowAt[key] = len(l.Rows)
			l.Rows = append(l.Rows, boxRow{
				Key: key, ID: e.ID, Repo: groups[i].Repo,
				Group: i, Col: col, Y: y + boxPanelHdr + j,
			})
		}
		groups[i].Col, groups[i].Y, groups[i].H = col, y, heights[i]
		colH[col] = y + heights[i]
	}
	l.Groups = groups
	for _, h := range colH {
		if h > l.H {
			l.H = h
		}
	}
	return l
}

// step walks the cursor: dy within a column, dx to the nearest row of a
// neighbouring column. The same rule the dep map and the graph use, so the
// three full-screen views move alike.
func (l *boxLayout) step(from string, dx, dy int) string {
	cur := l.Row(from)
	if cur == nil {
		if len(l.Rows) == 0 {
			return from
		}
		return l.Rows[0].Key
	}
	if dy != 0 {
		best, bestD := "", 1<<30
		for _, r := range l.Rows {
			if r.Col != cur.Col {
				continue
			}
			if (dy > 0 && r.Y <= cur.Y) || (dy < 0 && r.Y >= cur.Y) {
				continue
			}
			if d := abs(r.Y - cur.Y); d < bestD {
				best, bestD = r.Key, d
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
				best, bestD = r.Key, d
			}
		}
		if best != "" {
			return best
		}
	}
	return from
}
