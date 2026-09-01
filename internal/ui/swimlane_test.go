package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

// swimModel is a board with the swimlane open, at the default axis and scope.
func swimModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := boardModel(t, w, h)
	m.openSwim()
	m.relayout()
	return m
}

// A band exists because a task carries its value, not because the vocabulary
// lists it: the fixture holds boxes with no open members, and drawing an empty
// band for each would turn the frame into the vocabulary rather than the board.
// The sentinel is the one band that may not be sorted in — it is an absence,
// and it goes last so the read down the frame ends there.
func TestSwimBandsComeFromPresenceAndTheSentinelIsLast(t *testing.T) {
	m := swimModel(t, 240, 50)
	l := m.buildSwim()
	if len(l.Bands) == 0 {
		t.Fatal("the fixture grouped into no bands at all")
	}
	for _, b := range l.Bands {
		if b.Total == 0 {
			t.Errorf("band %q was placed with no tasks in it", b.Label)
		}
	}
	if last := l.Bands[len(l.Bands)-1]; last.Key != "" {
		t.Errorf("the last band is %q, want the sentinel", last.Label)
	}
	for _, b := range l.Bands[:len(l.Bands)-1] {
		if b.Key == "" {
			t.Errorf("a second sentinel band %q was placed", b.Label)
		}
	}
}

// A task carrying two repos is DRAWN in both bands — dropping one would make
// that repo's band silently incomplete, the box overview's rule — but it is
// one task, and the lane bar counts tasks. Summing the bands' counts into the
// bar is the bug this pins: the fixture's 17-task backlog read as 18.
func TestSwimCountsATwoValuedTaskOncePerLaneAndTwiceAcrossBands(t *testing.T) {
	m := swimModel(t, 240, 50)
	m.swimAxis = sliceRepo
	l := m.buildSwim()

	if l.Placed <= l.Tasks {
		t.Fatalf("the fixture has a two-repo task; placed=%d tasks=%d", l.Placed, l.Tasks)
	}
	li := -1
	for i, lane := range l.Lanes {
		if lane.Name == "backlog" {
			li = i
		}
	}
	if li < 0 {
		t.Fatal("no backlog lane on this board")
	}
	want := len(m.swimPopulation()["backlog"])
	if l.LaneCount[li] != want {
		t.Errorf("the lane bar says %d backlog tasks, the population holds %d",
			l.LaneCount[li], want)
	}
	sum := 0
	for _, b := range l.Bands {
		sum += b.Counts[li]
	}
	if sum <= want {
		t.Errorf("expected the bands to hold more PLACEMENTS (%d) than the lane holds tasks (%d)",
			sum, want)
	}

	// And the two-repo task is genuinely in both bands.
	in := 0
	for _, b := range l.Bands {
		for _, cell := range b.Cells {
			for _, task := range cell {
				if task.ID == "t-ehk7" {
					in++
				}
			}
		}
	}
	if in != 2 {
		t.Errorf("t-ehk7 carries two repos but was placed %d time(s)", in)
	}
}

// Folding must never move a number: the histogram is read DOWN the frame, and
// a count that shifts when a neighbouring band opens is a count you cannot
// compare. The two header lines may differ in exactly one thing — the
// disclosure marker.
func TestSwimFoldingChangesOnlyTheDisclosureMarker(t *testing.T) {
	m := swimModel(t, 240, 50)
	m.swimOpen = map[string]bool{}
	m.swimSel = ""
	l := m.buildSwim()
	m.swimLay = l
	m.clampSwimSel(l)

	bi := 0
	shut := ansiStrip(m.swimHeaderLine(l, bi))

	m.swimOpen[l.Bands[bi].Key] = true
	l2 := m.buildSwim()
	open := ansiStrip(m.swimHeaderLine(l2, bi))

	if strings.ReplaceAll(shut, swimFoldShut, swimFoldOpen) != open {
		t.Errorf("the header changed by more than its disclosure marker\nshut: %q\nopen: %q", shut, open)
	}
}

// The composed row must fit the width renderSwim pads to. Asserting it against
// swimGeometry's own arithmetic is what let the first cut ship a row one cell
// too wide at every width where the division came out even: renderSwim then
// truncated the last column and stamped an ellipsis over the lane's count. The
// budget is m.w-2, and the sweep is EVERY width in the design range, because
// the overflow depended on a remainder — 245 failed while 240 and 400 passed.
func TestSwimRowNeverOutgrowsTheFrame(t *testing.T) {
	for _, lanes := range []int{1, 5, 6, 7, 12, 20} {
		for w := 60; w <= 400; w++ {
			railW, colW, nvis := swimGeometry(w, lanes)
			got := railW + nvis*(swimLaneGap+colW)
			if budget := maxInt(1, w-2); got > budget {
				t.Fatalf("lanes=%d w=%d: a row composes %d cells into a %d-cell frame",
					lanes, w, got, budget)
			}
		}
	}
}

// Every line kind is composed from the same rail and the same per-lane
// segments, so the columns cannot drift as CJK titles get longer below them.
// The widths swept include 200 (the size the demo sweeps render at), the 240
// floor, and 245 — the narrowest width at which the old arithmetic overflowed.
func TestSwimColumnsAlignAcrossEveryLineKind(t *testing.T) {
	for _, w := range []int{200, 240, 245, 280, 320, 400} {
		m := swimModel(t, w, 50)
		l := m.buildSwim()
		m.swimLay = l
		// Unfold everything, so the sweep sees header lines, cell lines and
		// the blank separators in one frame.
		for _, b := range l.Bands {
			m.swimOpen[b.Key] = true
		}
		l = m.buildSwim()

		var lines []string
		lines = append(lines, m.swimBarRows(l)...)
		for _, ln := range l.Lines {
			lines = append(lines, m.swimLineText(l, ln))
		}
		want := l.RailW + len(l.Lanes)*(swimLaneGap+l.ColW)
		if budget := maxInt(1, w-2); want > budget {
			t.Fatalf("w=%d: the funnel composes %d cells into a %d-cell frame", w, want, budget)
		}
		for i, s := range lines {
			if s == "" {
				continue // the separator line is deliberately empty
			}
			if got := lg.Width(ansiStrip(s)); got != want {
				t.Errorf("w=%d line %d measures %d cells, want %d\n%q", w, i, got, want, ansiStrip(s))
			}
		}
	}
}

// The band's per-lane count is read against the lane bar's total directly
// above it, so the two must end on the same display column. A trailing space
// on one of them put every histogram digit one cell left of its heading.
func TestSwimHistogramSharesTheLaneBarsRightAnchor(t *testing.T) {
	for _, w := range []int{240, 245, 320} {
		m := swimModel(t, w, 50)
		l := m.buildSwim()
		m.swimLay = l
		bar := ansiStrip(m.swimBarRows(l)[0])
		hdr := ansiStrip(m.swimHeaderLine(l, 0))
		for i := range l.Lanes {
			end := l.RailW + (i+1)*(swimLaneGap+l.ColW) // one past this column
			if got := lastNonSpace(bar, end); got != lastNonSpace(hdr, end) {
				t.Errorf("w=%d lane %d: the bar's total ends at %d, the band's count at %d",
					w, i, got, lastNonSpace(hdr, end))
			}
		}
	}
}

// lastNonSpace is the display column of the last non-blank cell at or before x.
func lastNonSpace(s string, x int) int {
	col, last := 0, -1
	for _, r := range s {
		if col >= x {
			break
		}
		if r != ' ' {
			last = col
		}
		col += lg.Width(string(r))
	}
	return last
}

// `G` says "bottom", so it must reach the bottom. Landing on the last BAND's
// header leaves that band's rows below the fold with no key that reaches them.
func TestSwimBottomReachesTheLastRowNotTheLastHeader(t *testing.T) {
	m := swimModel(t, 240, 30)
	l := m.buildSwim()
	for _, b := range l.Bands {
		m.swimOpen[b.Key] = true
	}
	l = m.buildSwim()
	m.swimLay = l
	if l.Lines[len(l.Lines)-1].Kind == swimLineHeader {
		t.Skip("the last band is folded on this fixture; nothing below its header")
	}
	m.onSwimKey(keyMsg("G"))
	// Holding Down from here must not move: G already landed where the walk ends.
	before := m.swimSel
	m.swimMove(0, +1)
	if m.swimSel != before {
		t.Errorf("G landed on %q but a further Down reached %q", before, m.swimSel)
	}
	if l.IDOf(before) == "" {
		t.Errorf("G landed on a band header (%q) while task rows sit below it", before)
	}
}

// The status line is the only place this view says what it just did. ⏎ on the
// band the board is ALREADY sliced to un-slices it (selectSlice is a radio), so
// claiming a slice was applied would be the opposite of what happened.
func TestSwimSliceReportsAClearRatherThanAnEmptyTerm(t *testing.T) {
	m := swimModel(t, 240, 50)
	l := m.buildSwim()
	m.swimLay = l
	bi := -1
	for i, b := range l.Bands {
		if b.Key != "" {
			bi = i
			break
		}
	}
	if bi < 0 {
		t.Fatal("no non-sentinel band")
	}
	m.swimSel = swimKey(l.Bands[bi].Key, "")
	m.sliceToSwimBand(l)
	if m.sliceVal == "" {
		t.Fatal("the first press did not slice")
	}

	m.openSwim()
	l = m.buildSwim()
	m.swimLay = l
	m.swimSel = swimKey(l.Bands[bi].Key, "")
	m.sliceToSwimBand(l)
	if m.sliceVal != "" {
		t.Fatal("the second press did not clear the slice")
	}
	if strings.Contains(m.status, "sliced to  ") || !strings.Contains(m.status, "cleared") {
		t.Errorf("the board came back unsliced but the status says %q", m.status)
	}
}

// Seeding is all-or-nothing: a cursor the pack cannot hold (a done card at the
// open scope) must not leave its band unfolded with nothing selected in it.
func TestSwimSeedingADoneCardLeavesTheFrameFolded(t *testing.T) {
	m := boardModel(t, 240, 50)
	var done *board.Task
	for _, tk := range m.b.Tasks() {
		if m.g.IsDone(tk.ID) && tk.Epic != "" {
			done = tk
			break
		}
	}
	if done == nil {
		t.Skip("the fixture has no done task in a box")
	}
	if !m.selectID(done.ID, true) {
		t.Fatalf("could not put the board cursor on %s", done.ID)
	}
	m.openSwim()
	l := m.buildSwim()
	for _, b := range l.Bands {
		if b.Open {
			t.Errorf("band %q opened for a task the scope drops", b.Label)
		}
	}
	if want := swimKey(done.Epic, ""); m.swimSel != want {
		t.Errorf("the cursor landed on %q, want the band header %q", m.swimSel, want)
	}
}

// The vocabulary is closed over the population: a task whose box no longer
// resolves still lands in a band, under its raw id. Anything else drops it out
// of every band, out of the title bar's count and out of the lane bar at once.
func TestSwimGivesAnUnresolvedBoxItsOwnBand(t *testing.T) {
	m := swimModel(t, 240, 50)
	var victim *board.Task
	for _, tk := range m.b.Tasks() {
		if !m.g.IsDone(tk.ID) {
			victim = tk
			break
		}
	}
	if victim == nil {
		t.Fatal("no open task on the fixture")
	}
	victim.Epic = "e-gone"
	m.recompute()

	l := m.buildSwim()
	found := false
	for _, b := range l.Bands {
		for _, cell := range b.Cells {
			for _, tk := range cell {
				if tk.ID == victim.ID {
					found = true
					if b.Key != "e-gone" {
						t.Errorf("%s landed in band %q, want its own raw-id band", tk.ID, b.Key)
					}
				}
			}
		}
	}
	if !found {
		t.Errorf("%s carries an unresolvable box and vanished from every band", victim.ID)
	}
}

// A column too narrow to carry a marker and a syllable is not a column: the
// pack drops lanes from the end rather than shrinking every one of them below
// the floor, and the frame SAYS which ones are missing. A silent drop would
// make the view claim to be the whole board while showing four sevenths of it.
func TestSwimDropsLanesItCannotDrawAndSaysSo(t *testing.T) {
	// Twelve lanes still fit at the 240 floor (15 cells each); twenty do not,
	// and twenty is what proves the pack drops rather than shrinks.
	lanes := make([]board.Lane, 20)
	for i := range lanes {
		lanes[i] = board.Lane{Name: string(rune('a' + i))}
	}
	_, colW, nvis := swimGeometry(240, len(lanes))
	if nvis >= len(lanes) {
		t.Fatalf("%d lanes at 240 columns should not all fit, got %d", len(lanes), nvis)
	}
	if colW < swimLaneMinW {
		t.Errorf("a drawn column is %d cells, below the %d floor", colW, swimLaneMinW)
	}

	m := swimModel(t, 80, 40)
	l := m.buildSwim()
	if !l.Clipped() {
		t.Fatal("80 columns cannot hold six lanes; the pack says it did")
	}
	if out := frame(m); !strings.Contains(out, "lanes 1-") {
		t.Error("the frame drops lanes without saying so")
	}
}

// The cursor carries a desired COLUMN, because a band header spans every lane
// and so cannot say which one a vertical walk was descending. Without it,
// walking down through a header dumps the cursor into lane 0 and the reader
// loses the column they were reading.
func TestSwimCursorKeepsItsColumnAcrossABandHeader(t *testing.T) {
	m := swimModel(t, 240, 50)
	l := m.buildSwim()
	m.swimLay = l

	// A lane with tasks in two different bands, so a walk crosses a header.
	var lane, first, second = -1, -1, -1
	for li := range l.Lanes {
		bands := []int{}
		for bi, b := range l.Bands {
			if len(b.Cells[li]) > 0 {
				bands = append(bands, bi)
			}
		}
		if len(bands) >= 2 {
			lane, first, second = li, bands[0], bands[1]
			break
		}
	}
	if lane < 0 {
		t.Skip("this fixture has no lane populated in two bands")
	}
	for _, b := range l.Bands {
		m.swimOpen[b.Key] = true
	}
	l = m.buildSwim()
	m.swimLay = l
	m.swimSel = l.KeyAt(first, lane, 0)
	m.swimLane = lane
	if m.swimSel == "" {
		t.Fatal("no cell to start the walk from")
	}

	// Down until the next band's first cell in the SAME column is reached.
	for i := 0; i < 200; i++ {
		if m.swimSel == l.KeyAt(second, lane, 0) {
			return
		}
		before := m.swimSel
		m.swimMove(0, +1)
		if m.swimSel == before {
			break
		}
	}
	t.Errorf("walking down never reached band %d's column %d; ended on %q",
		second, lane, m.swimSel)
}

// Folding parks the cursor on the band's own header. Its cells are about to
// stop existing, and a cursor left on one is clamped to the first band on the
// next frame — which reads as the view scrolling away on its own.
func TestSwimFoldParksTheCursorOnTheBandHeader(t *testing.T) {
	m := swimModel(t, 240, 50)
	l := m.buildSwim()
	m.swimLay = l
	bi := -1
	for i, b := range l.Bands {
		if b.Open && b.Rows > 0 {
			bi = i
		}
	}
	if bi < 0 {
		t.Fatal("openSwim did not unfold the seeded band")
	}
	// Stand on a cell of that band.
	for li := range l.Lanes {
		if k := l.KeyAt(bi, li, 0); k != "" {
			m.swimSel, m.swimLane = k, li
			break
		}
	}
	m.toggleSwimFold(l)
	if want := swimKey(l.Bands[bi].Key, ""); m.swimSel != want {
		t.Errorf("after folding the cursor is on %q, want the band header %q", m.swimSel, want)
	}
	if m.swimOpen[l.Bands[bi].Key] {
		t.Error("the band is still unfolded")
	}
}

// ⏎ emits the -q term the band stands for — the box overview's drill-down,
// which is the only filtering mechanism this repo owns. On the sentinel there
// is no `field:value` to emit, so it names the query furrow DOES answer rather
// than issuing one that means something else.
func TestSwimSliceEmitsTheAxisTermAndRefusesTheSentinel(t *testing.T) {
	m := swimModel(t, 240, 50)
	l := m.buildSwim()
	m.swimLay = l

	bi := -1
	for i, b := range l.Bands {
		if b.Key != "" {
			bi = i
			break
		}
	}
	if bi < 0 {
		t.Fatal("no non-sentinel band")
	}
	m.swimSel = swimKey(l.Bands[bi].Key, "")
	m.sliceToSwimBand(l)
	if m.view != viewBoard {
		t.Error("slicing left the swimlane up")
	}
	if m.sliceField != sliceEpic || m.sliceVal != l.Bands[bi].Key {
		t.Errorf("sliced to %s:%s, want epic:%s", m.sliceField, m.sliceVal, l.Bands[bi].Key)
	}

	m2 := swimModel(t, 240, 50)
	l2 := m2.buildSwim()
	m2.swimLay = l2
	m2.swimSel = swimKey("", "")
	m2.sliceToSwimBand(l2)
	if !m2.statusErr {
		t.Error("the sentinel band was sliced instead of refused")
	}
	if !strings.Contains(m2.status, "no:epic") {
		t.Errorf("the refusal does not name the query to type: %q", m2.status)
	}
	if m2.view != viewSwim {
		t.Error("a refused slice left the view")
	}
}

// The filter MUTES here and does not shrink — the contract the dep map and the
// roadmap keep. A band count that moved with a query would stop being a fact
// about the board, and the frame reports the hidden rows instead.
func TestSwimFilterMutesRatherThanShrinkingTheBands(t *testing.T) {
	m := swimModel(t, 240, 50)
	before := m.buildSwim()

	m.applyFilter("label:bbq")
	m.relayout()
	after := m.buildSwim()

	if len(after.Bands) != len(before.Bands) || after.Tasks != before.Tasks {
		t.Errorf("the filter changed the population: %d bands/%d tasks -> %d/%d",
			len(before.Bands), before.Tasks, len(after.Bands), after.Tasks)
	}
	if m.swimHiddenCount(after) == 0 {
		t.Fatal("this filter hides nothing; the test proves nothing")
	}
	if out := frame(m); !strings.Contains(out, "hidden by the filter") {
		t.Error("the frame does not report what the filter hid")
	}
}

// Only a cursor the USER moved is carried back to the board: opening the view
// seeds a selection nobody chose, and following THAT back is a silent
// re-selection — the frametruth class this repo already pins for the map and
// the roadmap.
func TestSwimCarriesOnlyAMovedCursorBackToTheBoard(t *testing.T) {
	m := swimModel(t, 240, 50)
	was := ""
	if tk := m.curTask(); tk != nil {
		was = tk.ID
	}
	m.closeSwim()
	if got := ""; m.curTask() != nil {
		got = m.curTask().ID
		if got != was {
			t.Errorf("an untouched swimlane moved the board cursor from %s to %s", was, got)
		}
	}

	m = swimModel(t, 240, 50)
	l := m.buildSwim()
	m.swimLay = l
	for i := 0; i < 40; i++ {
		m.swimMove(0, +1)
		if id := l.IDOf(m.swimSel); id != "" && id != was {
			m.closeSwim()
			if m.curTask() == nil || m.curTask().ID != id {
				t.Errorf("the board cursor did not follow the walk to %s", id)
			}
			return
		}
	}
	t.Skip("the walk never left the seeded task")
}

// Re-grouping is not filtering. swimAxis is separate from sliceField on
// purpose: cycling the grouping of a read-only view must not rewrite the
// query the board is under.
func TestSwimAxisCycleLeavesTheActiveSliceAlone(t *testing.T) {
	m := swimModel(t, 240, 50)
	m.selectSlice(sliceLabel, "bbq")
	m.view = viewSwim
	field, val := m.sliceField, m.sliceVal

	m.cycleSwimAxis(+1)
	m.cycleSwimAxis(+1)
	if m.sliceField != field || m.sliceVal != val {
		t.Errorf("cycling the group axis moved the slice from %s:%s to %s:%s",
			field, val, m.sliceField, m.sliceVal)
	}
	if m.swimAxis == field && m.swimAxis == sliceEpic {
		t.Error("the group axis did not move")
	}
}

// `z` is the population knob every full-screen view spends it on. At the
// default scope the done lane is empty BY DEFINITION, and the column is still
// drawn: a grid whose columns move when the scope changes is a grid you can no
// longer read against the board.
func TestSwimScopeAddsTheDoneLaneAndTheColumnStaysEitherWay(t *testing.T) {
	m := swimModel(t, 240, 50)
	open := m.buildSwim()
	doneIdx := -1
	for i, l := range open.Lanes {
		if l.Done {
			doneIdx = i
		}
	}
	if doneIdx < 0 {
		t.Fatal("the done lane is not drawn at the default scope")
	}
	if open.LaneCount[doneIdx] != 0 {
		t.Errorf("scope open still counts %d done tasks", open.LaneCount[doneIdx])
	}

	m.onSwimKey(keyMsg("z"))
	all := m.buildSwim()
	if len(all.Lanes) != len(open.Lanes) || all.ColW != open.ColW {
		t.Errorf("z reflowed the grid: %d lanes of %d -> %d of %d",
			len(open.Lanes), open.ColW, len(all.Lanes), all.ColW)
	}
	if all.LaneCount[doneIdx] == 0 {
		t.Error("scope all shows no done tasks")
	}
}

// `W` is the whole gesture: it opens the view and it closes it again, and the
// key is BOUND (a demo can reach a state a keystroke cannot).
func TestSwimOpensAndClosesOnItsOwnKey(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.onNormalKey(tea.KeyPressMsg{Code: 'W', Text: "W"})
	if m.view != viewSwim {
		t.Fatal("W did not open the swimlane")
	}
	m.onSwimKey(tea.KeyPressMsg{Code: 'W', Text: "W"})
	if m.view != viewBoard {
		t.Error("W did not close the swimlane")
	}
}
