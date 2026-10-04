package ui

import "fmt"

// A re-read can take the row under a full-screen view's cursor: another
// session closed, removed or un-dated it. Each view's clamp then put the
// cursor on its first row without a word, and the next ⏎ acted on a row
// nobody chose — the roadmap opened the top row's due input, the swimlane
// sliced to the first band (t-v8j6, on a copy of the ridge-test store). The
// seat is taken BEFORE the board is swapped, from the layout the cursor was
// walked on, and re-seated after: the surviving row fewest vertical steps
// away, the forward one on a tie, and the landing note says where the
// cursor went. Rows the vertical walk cannot reach from the lost one are
// left to the view's clamp, which the note then admits.
//
// A landing does not set the view's `moved`: whether the cursor is carried
// to the board on close stays what the user's own walk made it. The lost
// row may have been the opening fallback, and carrying its neighbour would
// move the board cursor off a task the user never left.

// fullScreenSeat is the cursor of the full-screen view on screen and the
// rows a vertical walk from it reaches, nearest first.
type fullScreenSeat struct {
	view viewKind
	sel  string
	name string // sel as the note names it
	near []string
}

// seatLimit bounds the walk; a view's step is monotone along its axis
// (halfPage), so the bound only guards a layout that broke that.
const seatLimit = 4096

func (m *Model) takeSeat() fullScreenSeat {
	s := fullScreenSeat{view: m.view}
	var step func(from string, dy int) string
	switch m.view {
	case viewRoadmap:
		if l := m.road.lay; l != nil {
			s.sel, s.name = m.road.sel, m.road.sel
			step = l.step
		}
	case viewMap:
		if l := m.depmap.lay; l != nil {
			s.sel, s.name = m.depmap.sel, m.depmap.sel
			step = func(from string, dy int) string { return l.step(from, 0, dy) }
		}
	case viewBoxes:
		if l := m.boxes.lay; l != nil {
			s.sel = m.boxes.sel
			if r := l.Row(s.sel); r != nil {
				s.name = r.ID
			}
			step = func(from string, dy int) string { return l.step(from, 0, dy) }
		}
	case viewSwim:
		if l := m.swim.lay; l != nil {
			s.sel, s.name = m.swim.sel, swimSeatName(l, m.swim.sel)
			lane := m.swim.lane
			step = func(from string, dy int) string {
				k, _ := l.step(from, lane, 0, dy)
				return k
			}
		}
	case viewGraph:
		s.sel, s.name = m.graph.sel, m.graph.sel
	}
	if step == nil || s.sel == "" {
		return s
	}
	walk := func(dy int) []string {
		var out []string
		cur := s.sel
		for i := 0; i < seatLimit; i++ {
			n := step(cur, dy)
			if n == cur || n == "" {
				break
			}
			out = append(out, n)
			cur = n
		}
		return out
	}
	fwd, back := walk(+1), walk(-1)
	for i := 0; i < len(fwd) || i < len(back); i++ {
		if i < len(fwd) {
			s.near = append(s.near, fwd[i])
		}
		if i < len(back) {
			s.near = append(s.near, back[i])
		}
	}
	return s
}

// swimSeatName names a swimlane key for the note: the task's id, or the
// band a header stands for.
func swimSeatName(l *swimLayout, key string) string {
	if id := l.IDOf(key); id != "" {
		return id
	}
	if b := l.BandOf(key); b >= 0 {
		return "band " + l.Bands[b].Label
	}
	return key
}

// reseat puts the cursor back after the board was swapped and returns what
// the landing note owes the user, "" when the row is still there.
func (m *Model) reseat(s fullScreenSeat) string {
	if s.view != m.view || s.sel == "" {
		return ""
	}
	var has func(key string) bool
	var set func(key string) string // lands the cursor, returns its name
	what, empty := "", false
	switch m.view {
	case viewRoadmap:
		l := m.buildRoad()
		what, empty = "roadmap", l.Empty()
		has = func(k string) bool { return l.Row(k) != nil }
		set = func(k string) string { m.road.sel = k; return k }
	case viewMap:
		l := m.buildMap()
		what, empty = "dep map", l.Empty()
		has = func(k string) bool { return l.Row(k) != nil }
		set = func(k string) string { m.depmap.sel = k; return k }
	case viewBoxes:
		l := m.buildBoxes()
		what, empty = "overview", l.Empty()
		has = func(k string) bool { return l.Row(k) != nil }
		set = func(k string) string { m.boxes.sel = k; return l.Row(k).ID }
	case viewSwim:
		l := m.buildSwim()
		what, empty = "swimlane", l.Empty()
		has = func(k string) bool { _, ok := l.Pos(k); return ok }
		set = func(k string) string { m.swim.sel = k; return swimSeatName(l, k) }
	case viewGraph:
		return m.reseatGraph(s)
	default:
		return ""
	}
	if has(s.sel) {
		return ""
	}
	for _, k := range s.near {
		if has(k) {
			return fmt.Sprintf("%s left the %s — the cursor is on %s", s.name, what, set(k))
		}
	}
	if empty {
		return fmt.Sprintf("%s left the %s — no row is left", s.name, what)
	}
	return fmt.Sprintf("%s left the %s — the cursor is on its first row", s.name, what)
}

// reseatGraph: a graph whose root left the board has nothing to be rooted
// on — it stayed "rooted on" the missing id, one unresolved node in an
// otherwise empty frame, answering ⏎ with "already the root" — so it closes.
// A selection the new layout lacks goes back to the root, which is where the
// clamp puts it; the note is what was missing. Membership is the layout's,
// not the board's: an unresolved dep is a node with no task behind it.
func (m *Model) reseatGraph(s fullScreenSeat) string {
	if m.b.Task(m.graph.focus) == nil {
		root := m.graph.focus
		// Nothing for closeGraph to carry back: its selection is read off
		// the old layout, and a pin on an id the board lost never clears.
		m.graph.sel = ""
		m.closeGraph()
		note := fmt.Sprintf("the graph's root %s left the board — the graph closed", root)
		if m.view == viewMap && m.buildMap().Row(m.depmap.sel) == nil {
			// Back on the map it was opened from, whose own cursor may be
			// the same lost task: the clamp picks the first row, which is
			// not a row the user walked to.
			m.depmap.moved = false
			note += "; the map's cursor is on its first row"
		}
		return note
	}
	if s.sel != m.graph.focus && m.buildGraph().Node(s.sel) == nil {
		m.graph.sel = m.graph.focus
		return fmt.Sprintf("%s left the graph — the selection is back on the root", s.name)
	}
	return ""
}
