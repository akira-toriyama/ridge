package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

func frame(m *Model) string { return ansiStrip(m.View().Content) }

// badgeTokens is the mode→token declaration this test checks the FRAME against.
// It is deliberately a map keyed by the mode enum rather than by demo name: a
// mode with no entry here fails the sweep below instead of quietly rendering
// ⟨NORMAL⟩ over its own overlay, which is what a hand-written {demo, token}
// list allowed (the epic overlay would have passed it while badge-less).
var badgeTokens = map[mode]string{
	modeNormal: "⟨NORMAL⟩",
	modeMove:   "⟨MOVE⟩",
	modeFilter: "⟨FILTER⟩",
	modeEdit:   "⟨EDIT⟩",
	modeAdd:    "⟨ADD⟩",
	modeSlice:  "⟨SLICE⟩",
	modeEpic:   "⟨EPIC⟩",
}

// The mode badge is the ONLY place the current mode is named — the modes are
// otherwise invisible state (the complaint that opened t-8xk8). Swept over
// DemoNames, so every state a demo can pin must carry the right token, normal
// included, and a new mode cannot arrive without one.
func TestModeBadgeAlwaysNamesTheMode(t *testing.T) {
	for _, demo := range append([]string{""}, DemoNames...) {
		name := demo
		if name == "" {
			name = "board"
		}
		t.Run(name, func(t *testing.T) {
			m := boardModel(t, 200, 40)
			if demo != "" {
				if err := m.demoState(demo); err != nil {
					t.Fatalf("%v", err)
				}
			}
			// The badges that are not m.mode: a drag lives in dragState, and
			// the two full-screen views have title rows of their own.
			want := ""
			switch {
			case m.drag.moved:
				want = "⟨DRAG⟩"
			case m.view == viewGraph:
				want = "⟨GRAPH⟩"
			case m.view == viewMap:
				want = "⟨MAP⟩"
			case m.view == viewBoxes:
				want = "⟨BOXES⟩"
			case m.view == viewRoadmap:
				want = "⟨ROADMAP⟩"
			case m.view == viewSwim:
				want = "⟨SWIM⟩"
			default:
				var ok bool
				if want, ok = badgeTokens[m.mode]; !ok {
					t.Fatalf("mode %d has no badge token — add it to badgeTokens "+
						"and to modeBadge, or the frame names the wrong owner of the keyboard", m.mode)
				}
			}
			if out := frame(m); !strings.Contains(out, want) {
				t.Errorf("frame does not carry %s", want)
			}
		})
	}
}

// A modal that owns the keyboard must be VISIBLE in every view. The board and
// the table compose separate frames, and while the overlay list was written out
// twice the epic overlay was added to the board's only: in the table it held the
// whole keyboard — arrows, ⏎, and writes — while rendering nothing at all. Same
// class as the slice panel's own recorded failure.
func TestEveryModalRendersInBothViews(t *testing.T) {
	// want must be text only the overlay's BODY draws. Every overlay also names
	// itself in the status line, and matching that passed while the overlay
	// itself rendered nowhere — the exact bug this test exists to catch. The
	// status is cleared below as well, so a future marker cannot regress into
	// the same false pass.
	cases := []struct {
		mode  string
		want  string
		setup func(*Model)
	}{
		{"edit", "⏎ edit · esc close", func(m *Model) {
			m.selectID("t-9sa6", false)
			m.enterEdit()
		}},
		{"add", "add item", func(m *Model) {
			if c := m.enterAdd(); c != nil {
				_ = c
			}
		}},
		{"epic", "standing", func(m *Model) {
			m.toggleSlice()
			m.sliceField = sliceEpic
			m.enterEpic("e-c4mt")
		}},
	}
	for _, tc := range cases {
		for _, view := range []struct {
			name string
			kind viewKind
		}{{"board", viewBoard}, {"table", viewTable}} {
			t.Run(tc.mode+"/"+view.name, func(t *testing.T) {
				m := boardModel(t, 240, 50)
				m.view = view.kind
				tc.setup(m)
				m.status, m.statusErr = "", false
				m.relayout()
				if out := frame(m); !strings.Contains(out, tc.want) {
					t.Errorf("the %s overlay owns the keyboard but does not render in the %s view "+
						"(looking for %q)", tc.mode, view.name, tc.want)
				}
			})
		}
	}
}

// The `?` overlay is sectioned by mode, and the section you are in says so.
func TestHelpOverlayIsSectionedByMode(t *testing.T) {
	m := boardModel(t, 200, 50)
	if err := m.demoState("help"); err != nil {
		t.Fatal(err)
	}
	out := frame(m)
	if !strings.Contains(out, "normal mode — you are here") {
		t.Error("help must mark the current mode's section")
	}
	for _, sec := range []string{"move mode", "graph"} {
		if !strings.Contains(out, sec) {
			t.Errorf("help lost the %q section", sec)
		}
	}

	// From the graph the marker must follow.
	g := boardModel(t, 200, 50)
	if err := g.demoState("graph"); err != nil {
		t.Fatal(err)
	}
	g.fullHelp = true
	gout := frame(g)
	if !strings.Contains(gout, "graph — you are here") {
		t.Error("the graph's help must mark the graph section")
	}
	if strings.Contains(gout, "normal mode — you are here") {
		t.Error("the graph's help still marks normal mode as current")
	}

	// And from move mode — where `?` must WORK: the move-mode title row
	// advertises it, and this section is the only listing of the extremes.
	mv := boardModel(t, 200, 50)
	if err := mv.demoState("move"); err != nil {
		t.Fatal(err)
	}
	mv.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if !mv.fullHelp {
		t.Fatal("? must open the help from move mode")
	}
	mout := frame(mv)
	if !strings.Contains(mout, "move mode — you are here") {
		t.Error("move mode's help must mark the move-mode section")
	}
	// esc peels the overlay first and leaves the lift alone, same ordering as
	// the board and the graph.
	mv.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if mv.fullHelp {
		t.Error("esc must close the help before anything else")
	}
	if mv.mode != modeMove {
		t.Error("closing the help must not cancel the move")
	}
}

// The overlay must not ride into a modal, and its marker must never contradict
// the badge — `?` then a mode key was exactly the two-keystroke sequence that
// shipped a frame saying ⟨FILTER⟩ and "normal mode — you are here" at once.
func TestHelpOverlayNeverRidesIntoAModal(t *testing.T) {
	for _, tc := range []struct {
		key  rune
		mode mode
	}{
		{'/', modeFilter},
		{'a', modeAdd},
		{'s', modeSlice},
		{'m', modeMove}, // move keeps help REACHABLE (?) but never inherits it
	} {
		m := boardModel(t, 200, 50)
		m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
		if !m.fullHelp {
			t.Fatal("? did not open the help")
		}
		m.Update(tea.KeyPressMsg{Code: tc.key, Text: string(tc.key)})
		if m.mode != tc.mode {
			t.Fatalf("%q: mode = %v, want %v", tc.key, m.mode, tc.mode)
		}
		if m.fullHelp {
			t.Errorf("%q: the help overlay rode into mode %v", tc.key, tc.mode)
		}
	}
}

// At 240 columns — the repo's stated width floor — a stock 24-row (and even
// 20-row) terminal must keep the title bar: it carries the mode badge this
// overlay is sectioned around. The sectioned help once grew past it and
// covered row 0.
func TestHelpOverlayLeavesTheTitleRowAtStockHeights(t *testing.T) {
	// 12 and 16 sit where the h-1 cap + y floor are load-bearing on their own;
	// 20-24 is the window the unshelved sections regressed. Both mutations die.
	for _, h := range []int{12, 16, 20, 22, 24, 40} {
		m := boardModel(t, 240, h)
		if err := m.demoState("help"); err != nil {
			t.Fatal(err)
		}
		top := strings.SplitN(frame(m), "\n", 2)[0]
		if !strings.Contains(top, "furrow board") || !strings.Contains(top, "⟨NORMAL⟩") {
			t.Errorf("h=%d: the help overlay covers the title row: %q", h, top)
		}
	}
}

func TestMoveModeRendersALiftedCardAndADropIndicator(t *testing.T) {
	m := boardModel(t, 140, 30)
	if err := m.demoState("move"); err != nil {
		t.Fatal(err)
	}
	out := frame(m)

	// The lifted card switches to a double border — the card is visibly
	// detached, which is the whole affordance of GitHub's move mode.
	if !strings.Contains(out, "╔") {
		t.Error("the lifted card should be drawn with a distinct border")
	}
	// The drop indicator is drawn in the DESTINATION lane.
	if !strings.Contains(out, strings.Repeat(glyphDrop, 8)) {
		t.Error("no drop indicator in the frame")
	}
	dropCol := m.lay.Col(m.dropLane)
	y, ok := m.lay.dropY(m.dropLane, m.dropIdx)
	if !ok {
		t.Fatal("dropY failed")
	}
	line := strings.Split(out, "\n")[y]
	if !strings.Contains(line[minInt(dropCol.X, len(line)):], glyphDrop) {
		t.Errorf("the drop indicator is not on row %d of the %s column: %q", y, m.dropLane, line)
	}
	// The status bar has to say what is happening and how to get out.
	if !strings.Contains(out, "MOVE "+m.moveID) || !strings.Contains(out, "esc restore") {
		t.Error("move mode must announce itself and its exit in the status bar")
	}
	// And the footer must switch to the move keymap, not lie about the arrows.
	if strings.Contains(out, "jump to blocker") {
		t.Error("the footer still shows the normal keymap during a move")
	}
}

func TestDragRendersAGhostAndLeavesAShadow(t *testing.T) {
	m := boardModel(t, 140, 30)
	if err := m.demoState("drag"); err != nil {
		t.Fatal(err)
	}
	out := frame(m)
	id := m.drag.id
	if id == "" {
		t.Fatal("no drag in flight")
	}

	// The card appears TWICE: once as the shadow left in its original slot (so
	// nothing reflows under the cursor) and once as the ghost under the pointer.
	if n := strings.Count(out, id); n < 2 {
		t.Errorf("%s appears %d times; want a shadow AND a ghost", id, n)
	}
	if !strings.Contains(out, "DRAG "+id) {
		t.Error("the status bar must name the dragged card")
	}
	if !strings.Contains(out, "⟨DRAG⟩") {
		t.Error("the mode badge must show the drag")
	}

	// The ghost is a top-z layer: it must be drawn over the lane it is above.
	g := m.ghostLayer()
	if g == nil {
		t.Fatal("no ghost layer")
	}
	if g.GetZ() != zGhost {
		t.Errorf("ghost z = %d, want %d", g.GetZ(), zGhost)
	}
	// The grab offset must be preserved, or the card snaps under the cursor.
	if m.drag.grabDX == 0 && m.drag.grabDY == 0 {
		t.Error("the grab offset was not recorded")
	}
	// The grab offset itself, away from either clamp.
	if g.GetX() != m.drag.x-m.drag.grabDX {
		t.Errorf("ghost X = %d, want %d — the card snapped under the cursor",
			g.GetX(), m.drag.x-m.drag.grabDX)
	}

	// And the clamps, asserted as the PROPERTY they exist for rather than by
	// recomputing the formula. The old assertion re-derived X using the
	// since-deleted colOuterW — by then a constant with no production readers
	// — and at these coordinates neither bound bound, so it degenerated to the
	// line above and pinned nothing. Y had no assertion at all.
	//
	// The ghost is a layer, and a compositor grows its canvas to fit any
	// layer: one that overhangs widens the whole frame.
	m.drag.x, m.drag.y = m.w-1, m.h-1
	g = m.ghostLayer()
	if g == nil {
		t.Fatal("no ghost layer at the far corner")
	}
	card := renderCard(m.b.Task(m.drag.id), m.g, m.th, minInt(m.lay.ColW, maxInt(m.w, 1)), cardGhost)
	if right := g.GetX() + lg.Width(card); right > m.w {
		t.Errorf("the ghost reaches column %d on a %d-wide terminal — it would widen the frame", right, m.w)
	}
	if bottom := g.GetY() + lg.Height(card); bottom > m.h {
		t.Errorf("the ghost reaches row %d on a %d-tall terminal — it would grow the frame", bottom, m.h)
	}
	if g.GetX() < 0 || g.GetY() < 0 {
		t.Errorf("the ghost is at (%d,%d); a negative offset shifts the whole scene", g.GetX(), g.GetY())
	}
}

// Requirement 1 of dependency legibility: the peek resolves ids to titles and
// lanes, in BOTH directions. A raw id list is what the shard already gives you.
func TestPeekResolvesDependenciesBothWays(t *testing.T) {
	m := boardModel(t, 140, 40)
	m.peekOpen = true
	if !m.selectID("t-jv3j", false) {
		t.Fatal("could not select t-jv3j")
	}
	m.syncPeek()
	out := ansiStrip(m.peekContent(64))

	if !strings.Contains(out, "blocked by") || !strings.Contains(out, "blocks") {
		t.Fatal("both dependency directions must have a section")
	}
	// Forward edge, resolved to a title and a lane — not just "t-ehk7".
	if !strings.Contains(out, "t-ehk7") || !strings.Contains(out, "[backlog]") {
		t.Error("the forward dep is not resolved to a title + lane")
	}
	// A done dep is marked done rather than being silently dropped.
	if !strings.Contains(out, "t-t38k") || !strings.Contains(out, "[done]") {
		t.Error("a satisfied dep must still be listed, marked done")
	}
	// Reverse edges exist nowhere on disk; they have to be computed.
	for _, id := range m.g.Blocks("t-jv3j") {
		if !strings.Contains(out, id) {
			t.Errorf("reverse edge %s is missing from the peek", id)
		}
	}
	// Titles, not bare ids.
	if !strings.Contains(out, "キャンプ献立") {
		t.Error("dep lines must carry the blocker's title")
	}
}

func TestPeekMarksAnUnknownDepRatherThanHidingIt(t *testing.T) {
	b := board.NewBoard([]*board.Task{{ID: "a", Status: "ready", Title: "A", Deps: []string{"ghost"}}})
	m := New(memstore.NewWith(b), Options{})
	m.w, m.h = 120, 30
	m.peekOpen = true
	m.recompute()
	out := ansiStrip(m.peekContent(60))

	if !strings.Contains(out, glyphUnknown) || !strings.Contains(out, "not on this board") {
		t.Errorf("an unresolvable dep must be marked, not dropped:\n%s", out)
	}
}

func TestBlockedCardsAreMarkedNotHidden(t *testing.T) {
	m := boardModel(t, 140, 40)
	out := frame(m)

	// t-jv3j is blocked and must carry the glyph and a blocker count.
	if !strings.Contains(out, "t-jv3j") {
		t.Fatal("t-jv3j is not on the board")
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "t-jv3j") {
			if !strings.Contains(line, glyphBlocked+"1") {
				t.Errorf("the blocked card must show its blocker count: %q", line)
			}
			return
		}
	}
}

func TestActionableAndEpicChips(t *testing.T) {
	// 240 is the app's declared floor — and the width the CJK epic title
	// needs before the card chip has room for its first two glyphs (an
	// ASCII-led title used to squeeze into 140).
	m := boardModel(t, 240, 50)
	out := frame(m)

	// t-n2fc is the ready lane's one card; the glyph must sit on ITS title —
	// as the adjacent "▸ <title>" pair, because a terminal ROW carries every
	// column, so line-scoping still matched the neighbour column's glyph. A
	// frame-wide Contains stopped meaning anything once the in-progress
	// lane carried actionable cards too — the ready-is-never-actionable
	// mutant survived both loose forms.
	if !strings.Contains(out, "t-n2fc") {
		t.Fatal("t-n2fc is not on the board")
	}
	if !strings.Contains(out, glyphActionable+" 出発前夜") {
		t.Errorf("the actionable glyph (%q) is missing from t-n2fc's card", glyphActionable)
	}
	// An epic is an ENTITY, never a card: no lane holds e-fw2m, and member
	// cards carry a chip with the epic's RESOLVED title, not its raw id.
	if strings.Contains(out, "e-fw2m") {
		t.Error("the epic id leaked into the frame — members must show the resolved title chip")
	}
	if !strings.Contains(out, glyphEpic) {
		t.Errorf("the epic chip glyph (%q) is missing from the frame", glyphEpic)
	}
	if !strings.Contains(out, "九州") {
		t.Error("the epic chip must carry the epic's resolved title")
	}
}

// The WIP limit is RENDERED, never enforced — GitHub Projects parity.
func TestWIPLimitIsShownButNotEnforced(t *testing.T) {
	m := boardModel(t, 140, 40)
	// ready has WIP 2. Pile four tasks into it.
	var moved []string
	for _, task := range m.b.LaneTasks("backlog")[:4] {
		moved = append(moved, task.ID)
	}
	for _, id := range moved {
		if _, err := m.b.MoveTo(id, "ready", 0); err != nil {
			t.Fatal(err)
		}
	}
	m.reload()
	m.relayout()

	if n := len(m.b.LaneTasks("ready")); n < 5 {
		t.Fatalf("ready has %d tasks; the move should not have been refused", n)
	}
	out := frame(m)
	if !strings.Contains(out, "5/2"+glyphWIPOver) {
		t.Errorf("an over-limit column must be badged, got:\n%s", strings.Split(out, "\n")[rowColHdr])
	}
}

// -q is all-or-nothing (furrow exit 2): a refused query keeps the LAST GOOD
// verdict on screen and surfaces the refusal, because typing one more
// character must never blank the board.
func TestFilterRefusalKeepsTheLastGoodVerdict(t *testing.T) {
	m := boardModel(t, 140, 30)
	m.applyFilter("lane:ready")
	if n := m.countVisible(); n != 1 {
		t.Fatalf("lane:ready matched %d, want 1", n)
	}
	m.applyFilter("lane:ready nope:x")
	m.relayout()
	out := frame(m)

	if !strings.Contains(out, "unknown key") {
		t.Error("the refusal must be reported in the filter bar")
	}
	if n := m.countVisible(); n != 1 {
		t.Errorf("a refused query must keep the last good verdict: %d visible", n)
	}
}

func TestTableViewListsEveryVisibleTask(t *testing.T) {
	m := boardModel(t, 160, 40)
	m.view = viewTable
	out := frame(m)
	rows := m.tableRows()
	if len(rows) != 34 {
		t.Fatalf("expected all 34 tasks, got %d", len(rows))
	}
	// The cursor is a glyph, not only colour, so -plain can see it.
	if !strings.Contains(out, "▌ "+rows[0].ID) {
		t.Error("the table cursor must be visible without colour")
	}
}

// Regression: the graph canvas is padded and indented LINE BY LINE, so a
// multi-line node band appended as one element got the leading space on its top
// border only — every roof sat one cell right of its walls — and the
// undercounted height overflowed the frame until fitFrame clipped the strip and
// footer off. The focus box is the only heavy-bordered one, so its corners are
// unambiguous (edge routing uses light glyphs only).
func TestGraphBoxBordersAlignAndFrameKeepsItsFooter(t *testing.T) {
	m := boardModel(t, 240, 40)
	if err := m.demoState("graph"); err != nil {
		t.Fatal(err)
	}
	out := frame(m)
	lines := strings.Split(out, "\n")

	cell := func(line, sub string) int {
		i := strings.Index(line, sub)
		if i < 0 {
			return -1
		}
		return lg.Width(line[:i])
	}
	top, bottom := -1, -1
	for i, ln := range lines {
		if strings.Contains(ln, "┏") {
			top = i
		}
		if strings.Contains(ln, "┗") {
			bottom = i
		}
	}
	if top < 0 || bottom <= top {
		t.Fatal("no focus box in the frame")
	}
	left := cell(lines[top], "┏")
	if got := cell(lines[bottom], "┗"); got != left {
		t.Errorf("focus box shears: top-left at cell %d, bottom-left at %d", left, got)
	}
	if want, got := cell(lines[top], "┓"), cell(lines[bottom], "┛"); got != want {
		t.Errorf("focus box shears: top-right at cell %d, bottom-right at %d", want, got)
	}
	for i := top + 1; i < bottom; i++ {
		if got := cell(lines[i], "┃"); got != left {
			t.Errorf("line %d: wall at cell %d, roof corner at %d", i, got, left)
		}
	}
	if !strings.Contains(out, "graph rooted on") {
		t.Error("the status line was clipped off the graph frame")
	}
}

func TestEmptyLaneIsFocusableSoItCanReceiveADrop(t *testing.T) {
	m := boardModel(t, 140, 40)
	if len(m.cols["inbox"]) != 0 {
		t.Skip("inbox is not empty in this fixture")
	}
	m.curLane = m.b.LaneIndex("backlog")
	m.moveCursor(-1, 0)
	if m.curLaneName() != "inbox" {
		t.Errorf("focus = %s; an empty lane must be reachable, or it can never be a drop target", m.curLaneName())
	}
	if m.curTask() != nil {
		t.Error("an empty lane has no selection")
	}
	// Move mode into the empty lane must work.
	m.curLane = m.b.LaneIndex("backlog")
	m.setPos(0)
	id := m.curTask().ID
	m.enterMove()
	m.shiftDropLane(-1)
	if _, _, err := m.commitMove(id, "backlog", m.dropLane, m.dropIdx); err != nil {
		t.Fatal(err)
	}
	if m.b.Task(id).Status != "inbox" {
		t.Errorf("%s went to %s, want inbox", id, m.b.Task(id).Status)
	}
}
