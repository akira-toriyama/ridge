package ui

import (
	"github.com/akira-toriyama/ridge/internal/board"
)

// The SWIMLANE's layout engine — pure, deterministic, and with no knowledge of
// lipgloss, the theme or the terminal. swimlaneview.go draws what this file
// decides.
//
// The shape is the board's lanes across and one BAND per value of a grouping
// axis down: `furrow ls --tree` given a second dimension. A band is FOLDED by
// default and its header line carries a count per lane, so a folded frame is a
// histogram of the whole board — the real board's epic axis is one line per
// box plus the sentinel, a fifth of its open-task count. Unfolding drops that band's tasks
// into the same lane columns underneath, which is the state where the view is
// actually a grid. (openSwim opens the cursor's own band on entry, so `W`
// answers "where am I" too; every other band stays folded.)
//
// A band exists IFF at least one task in the population carries its value.
// Vocabulary order decides the band order (the view supplies it); presence
// decides membership. Ordering by count instead would make the frame move
// whenever a task changes lane, which is the disorientation packBoxes
// already refuses.
//
// WHY NO WRITES, and why this comment is here rather than in a commit
// message: commitMove's drop index is measured against the destination LANE
// AS DISPLAYED, and a band is a filtered SUBSET of a lane — so a `--before
// <id>` anchor taken from a band-local neighbour names a priority the board
// never showed. A task carrying two repos or two labels is in two bands at
// once, which has no answer at all in a reorder. K/J, drag and every mouse
// path are therefore refused, not postponed.

const (
	// The rail is the band label's home, and on task rows it is the selection
	// gutter and the band's continuation rule. It is what buys an epic title
	// real width: without it the label would have to fit before lane 0's
	// count, which is ~25 cells at the 240 floor.
	swimRailMinW = 18
	swimRailMaxW = 40
	swimLaneGap  = 1
	// swimLaneMinW is where a lane column stops being readable at all: two
	// cells of gutter, the marker, and a title worth truncating. Below it the
	// pack shows FEWER lanes and says so, rather than drawing every lane too
	// narrow to read.
	swimLaneMinW = 14
	swimBandSep  = 1 // one blank line under an UNFOLDED band, never under a folded one
)

// swimValue is one band's identity as the view resolved it: Key is the raw
// group value (the -q value; "" is the sentinel band), Label is what the
// header says, Marker leads the label for the axes that have a lifecycle
// (only epic does).
type swimValue struct {
	Key    string
	Label  string
	Marker string
}

// swimBand is one placed band.
type swimBand struct {
	swimValue
	Open   bool
	Total  int             // distinct tasks carrying this value, over EVERY lane
	Counts []int           // per VISIBLE lane
	Cells  [][]*board.Task // per VISIBLE lane, in board order
	Rows   int             // max over visible lanes; 0 while folded
	Y      int             // the header's line index in the canvas
	H      int             // lines this band owns, its trailing separator included
}

type swimLineKind int

const (
	swimLineHeader swimLineKind = iota
	swimLineCells
	swimLineGap
)

// swimLine is one canvas line. The kind is explicit rather than a sentinel
// stuffed into Row: boxRow's Key-not-index comment records what a sentinel in
// a position field costs when the pack is rebuilt underneath it.
type swimLine struct {
	Kind swimLineKind
	Band int
	Row  int // the row index within the band, for swimLineCells
}

// swimPos is where a selection key sits. Lane is an index into the layout's
// VISIBLE lanes, and is meaningless on a header — a band header spans every
// column, which is exactly why the cursor carries a desired lane of its own.
type swimPos struct {
	Band   int
	Lane   int
	Row    int
	Header bool
}

// swimLayout is one frame's geometry.
type swimLayout struct {
	Axis  sliceField
	All   bool         // done tasks are in this population
	Lanes []board.Lane // the lanes this frame DRAWS
	NLane int          // how many the board has, so a clipped frame can say so
	RailW int
	ColW  int
	Bands []swimBand
	Lines []swimLine

	Tasks  int // distinct tasks placed
	Placed int // placements — larger than Tasks when a task carries two values
	// tasks is the id set Tasks counts. Kept so the header's claims about the
	// population (the filter's hidden count) are made over the same set and
	// cannot drift into counting placements instead.
	tasks map[string]bool
	// LaneCount is TASKS per drawn lane, never the sum of the bands' counts:
	// a task carrying two repos is in two bands, and summing them told the
	// lane bar there were 18 tasks in a column holding 17.
	LaneCount []int

	at map[string]swimPos
}

// swimKey is the cursor identity for one placement: the band plus the task,
// because a task carrying two repos is placed in two bands and an id alone
// cannot say which. The empty id is the band's own header row.
func swimKey(band, id string) string { return band + "\x00" + id }

// swimKeys is the set of bands a task belongs to on this axis, deduped. A task
// with no value at all lands in the one sentinel band ("") rather than being
// dropped: the swimlane claims to be a partition of the population, and
// dropping the unfiled would lose close to four in ten of the real board's
// open tasks (2026-09-03: 118 of 313).
func swimKeys(t *board.Task, axis sliceField) []string {
	var vals []string
	switch axis {
	case sliceRepo:
		vals = t.Repos
	case sliceLabel:
		vals = t.Labels
	case sliceEpic:
		if t.Epic != "" {
			vals = []string{t.Epic}
		}
	}
	if len(vals) == 0 {
		return []string{""}
	}
	out := make([]string, 0, len(vals))
	seen := make(map[string]bool, len(vals))
	for _, v := range vals {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// swimGeometry decides the rail width, the column width, and how many lanes
// this width can hold. Lanes are dropped from the END rather than drawn below
// swimLaneMinW — a column too narrow to carry a marker and a syllable is not a
// column — and the caller states the clip in the header.
func swimGeometry(w int, lanes int) (railW, colW, nvis int) {
	avail := maxInt(1, w-2)
	railW = clamp(avail/5, swimRailMinW, swimRailMaxW)
	if railW > avail-swimLaneMinW-swimLaneGap {
		railW = maxInt(1, avail-swimLaneMinW-swimLaneGap)
	}
	nvis = maxInt(1, lanes)
	for {
		// nvis gaps, not nvis-1: swimRow emits one before EVERY lane, the first
		// included, because that separator divides the rail from lane 0. Paying
		// for one fewer overflowed the composed row by exactly one cell at every
		// width where the division came out even — 26 of them between 240 and
		// 400 — and renderSwim's truncate then ate the last column's count and
		// stamped an ellipsis on it. An alignment test that recomputes this same
		// formula cannot see that; it has to measure against the width the frame
		// pads to.
		colW = (avail - railW - nvis*swimLaneGap) / nvis
		if colW >= swimLaneMinW || nvis == 1 {
			break
		}
		nvis--
	}
	if colW < 1 {
		colW = 1
	}
	return railW, colW, nvis
}

// swimSpec is everything packSwim needs. Vocab carries the band ORDER — the
// view resolves it per axis from the same source each axis already uses, so
// this file never learns what an epic is.
type swimSpec struct {
	Axis  sliceField
	All   bool
	Vocab []swimValue
	Lanes []board.Lane
	Cols  map[string][]*board.Task // lane name -> the population, in board order
	Open  map[string]bool          // unfolded band keys
	W     int
}

// packSwim places every band and every line.
func packSwim(s swimSpec) *swimLayout {
	railW, colW, nvis := swimGeometry(s.W, len(s.Lanes))
	vis := s.Lanes
	if nvis < len(vis) {
		vis = vis[:nvis]
	}
	l := &swimLayout{
		Axis: s.Axis, All: s.All, Lanes: vis, NLane: len(s.Lanes),
		RailW: railW, ColW: colW, at: map[string]swimPos{},
	}

	// Membership first, over EVERY lane the board has: Total and the drop
	// count are claims about the population, not about the columns that fit.
	type bucket struct {
		cells [][]*board.Task
		total map[string]bool
	}
	buckets := map[string]*bucket{}
	for _, v := range s.Vocab {
		buckets[v.Key] = &bucket{cells: make([][]*board.Task, len(vis)), total: map[string]bool{}}
	}
	tasks := map[string]bool{}
	l.LaneCount = make([]int, len(vis))
	for li, lane := range s.Lanes {
		if li < len(vis) {
			l.LaneCount[li] = len(s.Cols[lane.Name])
		}
		for _, t := range s.Cols[lane.Name] {
			for _, k := range swimKeys(t, s.Axis) {
				b, ok := buckets[k]
				if !ok {
					// Unreachable: the view closes the vocabulary over the
					// population on every axis (repoVocab/labelVocab are built
					// from these same tasks, and swimVocab adds a band for an
					// epic id no box resolves). A guard, not an index panic —
					// but it must never silently drop a task: one whose box is
					// missing from the vocabulary would leave every band, the
					// title bar's count and the lane bar at once, and say so
					// nowhere.
					continue
				}
				b.total[t.ID] = true
				tasks[t.ID] = true
				l.Placed++
				if li < len(vis) {
					b.cells[li] = append(b.cells[li], t)
				}
				// A placement in a lane this width cannot draw stays in Total on
				// purpose: the band's number is a claim about the board, and the
				// header's "lanes 1-k of N" is what says a column is missing.
			}
		}
	}
	l.Tasks, l.tasks = len(tasks), tasks

	y := 0
	for _, v := range s.Vocab {
		b := buckets[v.Key]
		if len(b.total) == 0 {
			continue // presence, not vocabulary, decides that a band exists
		}
		band := swimBand{swimValue: v, Open: s.Open[v.Key], Total: len(b.total),
			Cells: b.cells, Counts: make([]int, len(vis)), Y: y}
		for i, cell := range b.cells {
			band.Counts[i] = len(cell)
			if band.Open && len(cell) > band.Rows {
				band.Rows = len(cell)
			}
		}
		bi := len(l.Bands)
		l.Lines = append(l.Lines, swimLine{Kind: swimLineHeader, Band: bi})
		l.at[swimKey(v.Key, "")] = swimPos{Band: bi, Header: true}
		for r := 0; r < band.Rows; r++ {
			l.Lines = append(l.Lines, swimLine{Kind: swimLineCells, Band: bi, Row: r})
			for li, cell := range b.cells {
				if r < len(cell) {
					l.at[swimKey(v.Key, cell[r].ID)] = swimPos{Band: bi, Lane: li, Row: r}
				}
			}
		}
		band.H = 1 + band.Rows
		if band.Rows > 0 {
			l.Lines = append(l.Lines, swimLine{Kind: swimLineGap, Band: bi})
			band.H += swimBandSep
		}
		y += band.H
		l.Bands = append(l.Bands, band)
	}
	return l
}

// Empty reports a population with nothing in it — said in words rather than
// drawn as an empty grid.
func (l *swimLayout) Empty() bool { return len(l.Bands) == 0 }

// Clipped reports that the frame is not showing every lane the board has.
func (l *swimLayout) Clipped() bool { return len(l.Lanes) < l.NLane }

// Pos resolves a cursor key, or false when this pack does not hold it.
func (l *swimLayout) Pos(key string) (swimPos, bool) {
	p, ok := l.at[key]
	return p, ok
}

// First is the key the cursor lands on when it has none, or when the pack was
// rebuilt without the row it was on.
func (l *swimLayout) First() string {
	if len(l.Bands) == 0 {
		return ""
	}
	return swimKey(l.Bands[0].Key, "")
}

// Last is First's mirror: the deepest key a downward walk reaches in the
// desired lane. The last BAND's header is not the bottom — an unfolded band's
// rows sit below it, and `G`, whose help line says "bottom", left them off
// screen. Walked by stepY's own rule, so `G` lands exactly where holding ↓
// would stop.
func (l *swimLayout) Last(lane int) (string, int) {
	lane = clamp(lane, 0, maxInt(0, len(l.Lanes)-1))
	for i := len(l.Lines) - 1; i >= 0; i-- {
		ln := l.Lines[i]
		switch ln.Kind {
		case swimLineHeader:
			return swimKey(l.Bands[ln.Band].Key, ""), lane
		case swimLineCells:
			if k := l.KeyAt(ln.Band, lane, ln.Row); k != "" {
				return k, lane
			}
		}
	}
	return l.First(), lane
}

// LineOf is the canvas line a key sits on — the SAME number the renderer
// places it at, so the scroll clamp can never disagree with the drawing.
func (l *swimLayout) LineOf(key string) (int, bool) {
	p, ok := l.at[key]
	if !ok {
		return 0, false
	}
	b := l.Bands[p.Band]
	if p.Header {
		return b.Y, true
	}
	return b.Y + 1 + p.Row, true
}

// KeyAt is the key of the cell at (band, lane, row), or "" when there is none.
func (l *swimLayout) KeyAt(band, lane, row int) string {
	if band < 0 || band >= len(l.Bands) || lane < 0 {
		return ""
	}
	b := l.Bands[band]
	if lane >= len(b.Cells) || row < 0 || row >= len(b.Cells[lane]) {
		return ""
	}
	return swimKey(b.Key, b.Cells[lane][row].ID)
}

// step walks the cursor. `lane` is the DESIRED column, carried alongside the
// key because a band header spans every column and therefore cannot say which
// one a vertical walk was descending; both are returned so the caller stores
// the pair.
//
// Vertically the canvas is walked as LINES: the next line that either is a
// band header or holds a task in the desired lane. That rule needs no special
// case for a band whose lanes are ragged — walking off the bottom of a short
// column simply lands on the next header rather than stopping dead.
func (l *swimLayout) step(from string, lane, dx, dy int) (string, int) {
	lane = clamp(lane, 0, maxInt(0, len(l.Lanes)-1))
	p, ok := l.at[from]
	if !ok {
		return l.First(), lane
	}
	if !p.Header {
		lane = p.Lane
	}
	if dy != 0 {
		return l.stepY(p, lane, dy), lane
	}
	if dx == 0 {
		return from, lane
	}
	return l.stepX(p, lane, dx)
}

func (l *swimLayout) stepY(p swimPos, lane, dy int) string {
	line, ok := l.LineOf(l.keyOf(p))
	if !ok {
		return l.First()
	}
	for i := line + dy; i >= 0 && i < len(l.Lines); i += dy {
		ln := l.Lines[i]
		switch ln.Kind {
		case swimLineHeader:
			return swimKey(l.Bands[ln.Band].Key, "")
		case swimLineCells:
			if k := l.KeyAt(ln.Band, lane, ln.Row); k != "" {
				return k
			}
		}
	}
	return l.keyOf(p)
}

// stepX moves to the nearest row of a neighbouring column, the rule the dep
// map and the box overview already walk by. On a HEADER it moves the desired
// column only: the header is one row spanning every lane, so there is nothing
// to move to, but the column it will descend into has to be choosable.
func (l *swimLayout) stepX(p swimPos, lane, dx int) (string, int) {
	if p.Header {
		next := clamp(lane+dx, 0, maxInt(0, len(l.Lanes)-1))
		return swimKey(l.Bands[p.Band].Key, ""), next
	}
	b := l.Bands[p.Band]
	for c := p.Lane + dx; c >= 0 && c < len(b.Cells); c += dx {
		if len(b.Cells[c]) == 0 {
			continue
		}
		row := clamp(p.Row, 0, len(b.Cells[c])-1)
		return swimKey(b.Key, b.Cells[c][row].ID), c
	}
	return l.keyOf(p), p.Lane
}

func (l *swimLayout) keyOf(p swimPos) string {
	if p.Header {
		return swimKey(l.Bands[p.Band].Key, "")
	}
	return l.KeyAt(p.Band, p.Lane, p.Row)
}

// BandOf is the band a key belongs to, -1 when the key is not in this pack.
func (l *swimLayout) BandOf(key string) int {
	if p, ok := l.at[key]; ok {
		return p.Band
	}
	return -1
}

// IDOf is the task a key names, "" for a band header.
func (l *swimLayout) IDOf(key string) string {
	p, ok := l.at[key]
	if !ok || p.Header {
		return ""
	}
	b := l.Bands[p.Band]
	return b.Cells[p.Lane][p.Row].ID
}

// Height is the whole canvas, in lines.
func (l *swimLayout) Height() int { return len(l.Lines) }
