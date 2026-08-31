package ui

import (
	"strings"
	"testing"
	"time"

	lg "charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// Every line a group contributes is composed to EXACTLY one column's width, so
// the columns beside it cannot drift.
//
// What this does NOT catch, stated because the obvious reading is wrong:
// measuring the title budget with len() instead of lg.Width does not fail here,
// because pad() re-pads every row afterwards — a len()-measured budget only
// truncates the title EARLIER. The width invariant is real; CJK truncation
// correctness is not what proves it.
func TestBoxGroupLinesAreExactlyOneColumnWide(t *testing.T) {
	for _, w := range []int{240, 320, 400} {
		for _, all := range []bool{false, true} {
			m := boardModel(t, w, 50)
			m.boxesAll = all
			l := m.buildBoxes()
			for _, g := range l.Groups {
				for j, line := range m.renderBoxGroup(g, l.ColW) {
					if got := lg.Width(ansiStrip(line)); got != l.ColW {
						t.Errorf("w=%d all=%t group %s line %d is %d cells, want %d: %q",
							w, all, g.Repo, j, got, l.ColW, ansiStrip(line))
					}
				}
			}
		}
	}
}

// A box naming two repos is listed under BOTH, and the cursor must be able to
// tell the two placements apart — an id alone cannot, which is why the row key
// carries the repo.
func TestATwoRepoBoxIsListedUnderBothAndAddressableInEach(t *testing.T) {
	b := board.NewBoard(nil,
		board.EpicInfo{ID: "e-two", Title: "二つの repo にまたがる箱",
			Repos: []string{"a/one", "b/two"}, Total: 2, Done: 1},
	)
	l := packBoxes(b.EpicsAll(), false, 240)
	if len(l.Groups) != 2 || len(l.Rows) != 2 {
		t.Fatalf("groups=%d rows=%d, want one group and one row per repo", len(l.Groups), len(l.Rows))
	}
	if l.Row(boxKey("a/one", "e-two")) == nil || l.Row(boxKey("b/two", "e-two")) == nil {
		t.Error("both placements must be addressable")
	}
	// Two DISTINCT keys, asserted on the map itself: building the lookups with
	// boxKey and stopping there passes even if boxKey drops the repo, which is
	// the whole property under test (found by review).
	if len(l.rowAt) != 2 {
		t.Errorf("rowAt holds %d keys, want one per placement — the repo must be part of the key", len(l.rowAt))
	}
	// A repo listed twice is one placement, not two.
	dup := packBoxes([]board.EpicInfo{
		{ID: "e-dup", Title: "重複 repo", Repos: []string{"a/one", "a/one"}},
	}, false, 240)
	if len(dup.Rows) != 1 || len(dup.rowAt) != 1 {
		t.Errorf("a duplicated repo placed %d rows / %d keys, want 1 / 1", len(dup.Rows), len(dup.rowAt))
	}
	// Groups are alphabetical so the frame does not reshuffle when a box is
	// activated; inside a group furrow's own order stands.
	if l.Groups[0].Repo != "a/one" || l.Groups[1].Repo != "b/two" {
		t.Errorf("group order = %s,%s, want alphabetical", l.Groups[0].Repo, l.Groups[1].Repo)
	}
}

// The repo-less box has somewhere to go, last. furrow refuses to ACTIVATE one
// but will hold one, so an "every box" overview that dropped it would be
// lying by omission.
func TestARepoLessBoxLandsInItsOwnGroupLast(t *testing.T) {
	b := board.NewBoard(nil,
		board.EpicInfo{ID: "e-bare", Title: "repo の無い箱"},
		board.EpicInfo{ID: "e-repo", Title: "repo のある箱", Repos: []string{"z/z"}},
	)
	l := packBoxes(b.EpicsAll(), false, 240)
	if n := len(l.Groups); n != 2 || l.Groups[n-1].Repo != boxNoRepo {
		t.Fatalf("groups = %v, want the repo-less group last", groupRepos(l))
	}
	// The sentinel is a KEY, not a label — a repo literally named "(no repo)"
	// must not merge into it.
	l2 := packBoxes([]board.EpicInfo{
		{ID: "e-bare", Title: "repo 無し"},
		{ID: "e-odd", Title: "紛らわしい repo", Repos: []string{boxNoRepoLabel}},
	}, false, 240)
	if len(l2.Groups) != 2 {
		t.Errorf("groups = %v, want the sentinel and the look-alike repo kept apart", groupRepos(l2))
	}
	if l.Row(boxKey(boxNoRepo, "e-bare")) == nil {
		t.Error("the repo-less box must still be addressable")
	}
}

// The scope toggle is what makes a box closed in an earlier session reachable
// at all — the overview is the surface that can show every one of them at
// once, and `m` from here is the door to reopen.
func TestTheScopeToggleAddsTheClosedBoxesAndTheCursorSurvivesIt(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.openBoxes()
	open := m.buildBoxes()
	if open.Row(boxKey("tomo/kyushu-trip", "e-2b7h")) != nil {
		t.Fatal("the default scope must not carry a closed box")
	}
	press(m, "z")
	all := m.buildBoxes()
	m.boxesLay = all
	if all.Row(boxKey("tomo/kyushu-trip", "e-2b7h")) == nil {
		t.Fatal("z must widen the population to the closed boxes")
	}

	// Narrowing again with the cursor ON a closed row must move the cursor,
	// not leave it pointing at a row that is no longer packed.
	m.boxesSel = boxKey("tomo/kyushu-trip", "e-2b7h")
	press(m, "z")
	l := m.buildBoxes()
	m.clampBoxesSel(l)
	if l.Row(m.boxesSel) == nil {
		t.Errorf("the cursor survived as %q, which the narrowed pack does not hold", m.boxesSel)
	}
}

// The key must be BOUND, not merely reachable by calling openBoxes: a staged
// call keeps passing after the binding is deleted, which is the trap the
// epicnew demo's comment records.
func TestTheBoxOverviewKeysAreActuallyBound(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "E")
	if m.view != viewBoxes {
		t.Fatalf("E did not open the box overview, view=%v", m.view)
	}
	press(m, "esc")
	if m.view != viewBoard {
		t.Errorf("esc did not leave the overview, view=%v", m.view)
	}
	// And the arrows walk it, which is the other half a "the view opened"
	// assertion misses.
	press(m, "E")
	m.renderBoxes() // the handlers walk the pack the last frame built
	before := m.boxesSel
	press(m, "down")
	if m.boxesSel == before {
		t.Error("↓ did not move the cursor inside the overview")
	}
}

// Drill-down is the slice term the panel already emits — no second filtering
// mechanism, and none is needed for a closed box either, which is one of the
// reasons the closed population earns its place.
func TestDrillDownEmitsTheEpicSliceTermForOpenAndClosedBoxes(t *testing.T) {
	for _, tc := range []struct{ name, repo, id string }{
		{"open", "tomo/kyushu-trip", "e-fw2m"},
		{"closed", "tomo/kyushu-trip", "e-2b7h"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := boardModel(t, 240, 50)
			m.boxesAll = true
			m.openBoxes()
			m.renderBoxes()
			m.boxesSel = boxKey(tc.repo, tc.id)
			press(m, "enter")

			if m.view != viewBoard {
				t.Errorf("⏎ must return to the board, view=%v", m.view)
			}
			if m.sliceField != sliceEpic || m.sliceVal != tc.id {
				t.Errorf("slice = %v/%q, want the epic axis on %s", m.sliceField, m.sliceVal, tc.id)
			}
			if got, want := m.sliceTerm(), "epic:"+tc.id; got != want {
				t.Errorf("slice term = %q, want %q", got, want)
			}
		})
	}
}

// `m` opens the box overlay on the row under the cursor — the path that makes
// `reopen` reachable from here.
func TestManageOpensTheOverlayOnTheSelectedBox(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.boxesAll = true
	m.openBoxes()
	m.renderBoxes()
	m.boxesSel = boxKey("tomo/kyushu-trip", "e-2b7h")
	press(m, "m")
	if m.mode != modeEpic || m.epic == nil || m.epic.id != "e-2b7h" {
		t.Fatalf("m must open the overlay on the selected box, mode=%v epic=%+v", m.mode, m.epic)
	}
}

// An empty population says so in words rather than drawing an empty grid and
// leaving the reader to wonder what broke.
func TestAnEmptyOverviewSaysSo(t *testing.T) {
	m := New(memstore.NewWith(board.NewBoard(nil)), Options{})
	m.w, m.h = 240, 50
	m.recompute()
	m.relayout()
	m.openBoxes()
	if out := ansiStrip(m.renderBoxes()); !strings.Contains(out, "no open boxes") {
		t.Errorf("an empty overview must explain itself:\n%s", out)
	}
}

// A short terminal must not panic. The dep map set this precedent after a real
// one, and the two views share the same band arithmetic.
func TestTheOverviewSurvivesAShortTerminal(t *testing.T) {
	for _, h := range []int{7, 10, 24} {
		m := New(memstore.New(), Options{})
		if _, err := m.Dump(240, h, "boxesall", true); err != nil {
			t.Fatalf("rows=%d: %v", h, err)
		}
	}
}

// The two demo frames must carry what they exist to show, or the headless
// verification is a frame that renders and proves nothing.
func TestBoxOverviewDemoFramesCarryWhatTheyExistFor(t *testing.T) {
	for _, tc := range []struct {
		demo string
		want []string
	}{
		// Grouping, the active marker, furrow's derived numbers, and the four
		// epic dep edges the whole board has, inline.
		{"boxes", []string{"boxes by repo", "scope open", "tomo/kyushu-trip",
			glyphEpicActive + " e-fw2m", "6/18", "←e-p3dx", "1 stuck"}},
		// The widened scope and the closed row, which has no other frame.
		{"boxesall", []string{"scope open + closed", "1 closed",
			glyphDone + " e-2b7h", "closed 2026-07-15"}},
	} {
		t.Run(tc.demo, func(t *testing.T) {
			out := strings.Join(dumpFrame(t, 240, 44, tc.demo), "\n")
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("-demo %s lost %q", tc.demo, want)
				}
			}
		})
	}
}

// Group counts are furrow's numbers summed, never recomputed from member
// tasks: recomputing would be the front-end logic this repo exists not to
// have, and it would disagree with the rows the same frame drew.
func TestGroupTotalsAreTheServedNumbersSummed(t *testing.T) {
	b := board.NewBoard(nil,
		board.EpicInfo{ID: "e-a", Title: "一", Repos: []string{"r/r"}, Done: 2, Total: 5},
		board.EpicInfo{ID: "e-b", Title: "二", Repos: []string{"r/r"}, Done: 1, Total: 3,
			Closed: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)},
	)
	l := packBoxes(b.EpicsAll(), true, 240)
	if len(l.Groups) != 1 || l.Groups[0].Done != 3 || l.Groups[0].Total != 8 {
		t.Errorf("group totals = %d/%d, want 3/8", l.Groups[0].Done, l.Groups[0].Total)
	}
}

func groupRepos(l *boxLayout) []string {
	out := make([]string, 0, len(l.Groups))
	for _, g := range l.Groups {
		out = append(out, g.Repo)
	}
	return out
}

// The pack at the real board's scale, recorded as numbers rather than as a
// feeling. 153 boxes over 31 repos is what `furrow epic ls -r "" --all` served
// on 2026-08-28; the widths are this repo's supported range (240 floor, 400
// target). A change to boxPanelMinW or boxMaxCols moves these, and moving them
// silently is how an overview turns into a column of ids.
//
// One repo per box here, so 153 rows. The real board has a few two-repo boxes
// and packs to 173 PLACEMENTS — taller than any supported canvas, which is why
// this view scrolls and says so in its header rather than claiming to fit on
// one screen.
func TestThePackAtTheRealBoardsScale(t *testing.T) {
	var es []board.EpicInfo
	for i := 0; i < 153; i++ {
		es = append(es, board.EpicInfo{
			ID: "e-" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			// Median title on the real board is 11 display cells, p90 67.
			Title: "テストの箱のタイトル",
			Repos: []string{"akira-toriyama/repo" + string(rune('a'+i%31))},
		})
	}
	for _, tc := range []struct{ w, cols, colW int }{
		{240, 4, 58},
		{320, 6, 51},
		{400, 6, 64},
	} {
		l := packBoxes(es, true, tc.w-2)
		if l.Cols != tc.cols || l.ColW != tc.colW {
			t.Errorf("w=%d packed %d columns of %d cells, want %d of %d",
				tc.w, l.Cols, l.ColW, tc.cols, tc.colW)
		}
		if len(l.Groups) != 31 {
			t.Errorf("w=%d grouped into %d repos, want 31 — a dropped group is a hidden box", tc.w, len(l.Groups))
		}
		// Every box is placed exactly once per repo it names, and none is lost
		// to the column search.
		if len(l.Rows) != 153 {
			t.Errorf("w=%d placed %d rows, want 153", tc.w, len(l.Rows))
		}
	}
}

// Every view that can hand the keyboard to a modal must also DRAW it. That is
// what modalLayers exists for and it had no test: this view shipped `m` →
// epic overlay while compositing nothing, so the overlay ate every keystroke
// and rendered not one pixel — and the suite stayed green because the only
// assertion was on m.mode. The graph and the dep map are absent on purpose:
// neither has a key that opens a modal.
func TestEveryViewThatOpensAModalAlsoDrawsIt(t *testing.T) {
	for _, tc := range []struct {
		view string
		open func(*Model)
	}{
		{"board", func(m *Model) { sliceOnEpicAxis(t, m, "e-c4mt") }},
		{"boxes", func(m *Model) {
			m.openBoxes()
			m.renderBoxes()
			m.boxesSel = boxKey("tomo/kyushu-trip", "e-c4mt")
		}},
	} {
		t.Run(tc.view, func(t *testing.T) {
			m := boardModel(t, 240, 50)
			tc.open(m)
			press(m, "m")
			if m.mode != modeEpic {
				t.Fatalf("the overlay did not take the keyboard in the %s view", tc.view)
			}
			out := frame(m)
			for _, want := range []string{"box e-c4mt", "standing", "pinned", "meta"} {
				if !strings.Contains(out, want) {
					t.Errorf("%s view: the overlay owns the keyboard but %q is not on screen", tc.view, want)
				}
			}
		})
	}
}

// Leaving the overlay must not hand the keyboard to a panel that is off
// screen either — `s` then `esc` then `E` leaves sliceOpen true underneath the
// overview, and exitEpic used to return the keyboard to it unconditionally.
func TestLeavingTheOverlayInTheOverviewDoesNotWakeTheHiddenPanel(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.toggleSlice() // the panel stays OPEN behind everything after this
	press(m, "esc")
	press(m, "E")
	m.renderBoxes()
	m.boxesSel = boxKey("tomo/kyushu-trip", "e-c4mt")
	press(m, "m")
	press(m, "esc")
	if m.mode == modeSlice {
		t.Fatal("the keyboard went back to the slice panel, which this view does not draw")
	}
	// Upwards: e-c4mt is the last row of its group, so ↓ would be a legitimate
	// no-op and would prove nothing.
	sel := m.boxesSel
	press(m, "up")
	if m.boxesSel == sel {
		t.Error("the overview did not get the keyboard back")
	}
}

// The header's counts are per BOX, not per placement: a box in two repos is
// drawn twice and must be counted once, or every aggregate on a fleet board
// reads high.
func TestHeaderCountsABoxOnceHoweverManyReposItNames(t *testing.T) {
	b := board.NewBoard(nil,
		board.EpicInfo{ID: "e-both", Title: "二重", Repos: []string{"a/a", "b/b"},
			Active: true, Done: 1, Total: 2},
	)
	m := New(memstore.NewWith(b), Options{})
	m.w, m.h = 240, 50
	m.recompute()
	m.relayout()
	l := m.buildBoxes()
	out := ansiStrip(m.boxHeader(l, false))
	for _, want := range []string{"1 boxes", "1 active", "1/2 tasks done"} {
		if !strings.Contains(out, want) {
			t.Errorf("header = %q, want %q — a two-repo box is one box", out, want)
		}
	}
}

// Every key the help overlay advertises for this view has to move something,
// or the section is an assertion the frame does not keep.
func TestEveryAdvertisedOverviewKeyActsOnSomething(t *testing.T) {
	setup := func() *Model {
		m := boardModel(t, 240, 20) // short enough that the pack scrolls
		m.boxesAll = true
		m.openBoxes()
		m.renderBoxes()
		m.boxesSel = m.boxesLay.Rows[0].Key
		return m
	}
	// ← / → cross columns, and the fixture has two repos, so both directions
	// have somewhere to go.
	m := setup()
	if press(m, "right"); m.boxesSel == m.boxesLay.Rows[0].Key {
		t.Error("→ did not cross to the other column")
	}
	if press(m, "left"); m.boxesSel != m.boxesLay.Rows[0].Key {
		t.Error("← did not come back")
	}
	// G lands on the LAST row, not the first.
	m = setup()
	press(m, "G")
	if last := m.boxesLay.Rows[len(m.boxesLay.Rows)-1].Key; m.boxesSel != last {
		t.Errorf("G landed on %q, want the last row %q", m.boxesSel, last)
	}
	press(m, "g")
	if m.boxesSel != m.boxesLay.Rows[0].Key {
		t.Error("g did not return to the first row")
	}
	// ^d pages by rows; on a column with more rows than half a canvas it must
	// move, and it must stop rather than wrap at the end.
	m = setup()
	m.boxesSel = boxKey("tomo/kyushu-trip", "e-fw2m")
	before := m.boxesSel
	press(m, "ctrl+d")
	if m.boxesSel == before {
		t.Error("^d did not page down")
	}
	press(m, "ctrl+u")
	if m.boxesSel != before {
		t.Errorf("^u did not page back to %q, landed on %q", before, m.boxesSel)
	}
}

// The strip is the whole answer to "a row can only show a truncated title", so
// every block it promises has to be there — and none of them had a test.
func TestTheStripCarriesTheBoxsWholeRecord(t *testing.T) {
	b := board.NewBoard(nil,
		board.EpicInfo{ID: "e-full", Title: "全部入りの箱",
			Goal: "終わりの条件をここに書く", Repos: []string{"a/a"}, Labels: []string{"gear"},
			Meta: map[string]string{"origin": "会議"}, Done: 1, Total: 3, Stuck: true,
			Deps: []string{"e-open", "e-shut", "e-gone"}, OpenDeps: []string{"e-open"}},
		board.EpicInfo{ID: "e-open", Title: "開いている依存", Done: 1, Total: 2},
		board.EpicInfo{ID: "e-shut", Title: "閉じた依存",
			Closed: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)},
	)
	m := New(memstore.NewWith(b), Options{})
	m.w, m.h = 240, 50
	m.recompute()
	m.relayout()
	out := ansiStrip(m.boxStrip(b.Epic("e-full"), 8))
	for _, want := range []string{
		"e-full", "全部入りの箱", "goal 終わりの条件", "1/3 done", "STUCK",
		"repos a/a", "labels gear", "meta origin",
		"waits on", "e-open (1/2) 開いている依存", "e-shut", "(closed)", "e-gone (missing)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the strip lost %q:\n%s", want, out)
		}
	}
	// And it must occupy exactly the band the full-screen layout subtracted
	// for it — the same rows taskStrip does, since the two share that band.
	// Height() is a MINIMUM in lipgloss, so an unbounded body silently grows
	// past it and fitFrame clips the status line off instead.
	full := boardModel(t, 240, 50)
	for _, h := range []int{2, 4, 8, 12} {
		want := len(strings.Split(full.taskStrip(full.b.Task("t-y4st"), false, h), "\n"))
		if got := len(strings.Split(m.boxStrip(b.Epic("e-full"), h), "\n")); got != want {
			t.Errorf("a strip given %d rows rendered %d, taskStrip renders %d", h, got, want)
		}
	}
}

// The dep tag may NOT be blockerTag: that one colours by the TASK graph, where
// an epic id is never Known, so every one of the board's four epic edges
// rendered in the "unresolved defect" colour — the one thing the tag's own
// contract says it may never do. Colour, not text, is the whole signal here,
// so the assertion has to read the escape codes.
func TestTheEpicDepTagIsColouredByEpicResolutionNotTheTaskGraph(t *testing.T) {
	b := board.NewBoard(nil,
		board.EpicInfo{ID: "e-live", Title: "開いた依存を持つ", Repos: []string{"a/a"},
			Deps: []string{"e-open"}, OpenDeps: []string{"e-open"}},
		board.EpicInfo{ID: "e-settled", Title: "閉じた依存だけ", Repos: []string{"a/a"},
			Deps: []string{"e-shut"}},
		board.EpicInfo{ID: "e-broken", Title: "解決できない依存", Repos: []string{"a/a"},
			Deps: []string{"e-gone"}},
		board.EpicInfo{ID: "e-open", Title: "開いている", Repos: []string{"b/b"}},
		board.EpicInfo{ID: "e-shut", Title: "閉じている", Repos: []string{"b/b"},
			Closed: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)},
	)
	m := New(memstore.NewWith(b), Options{})
	m.w, m.h = 240, 50
	m.recompute()
	m.relayout()

	for _, tc := range []struct {
		id, dep string
		want    string
	}{
		{"e-broken", "e-gone", m.th.danger.Render("x")}, // furrow lints this epic-dep-missing, at ERROR
		{"e-live", "e-open", m.th.warn.Render("x")},     // a live wait
		{"e-settled", "e-shut", m.th.dim.Render("x")},   // settled
	} {
		// Through the RENDERED ROW, not boxDepTag directly: the defect this
		// pins was the row calling the wrong tag function, which a direct call
		// cannot see.
		row := m.boxRowLine("a/a", *b.Epic(tc.id), 120)
		prefix := strings.TrimSuffix(tc.want, "x"+ansiReset(tc.want))
		i := strings.Index(row, "←"+tc.dep)
		if i < 0 {
			t.Fatalf("%s: no ←%s tag in the row %q", tc.id, tc.dep, ansiStrip(row))
		}
		if j := strings.LastIndex(row[:i], "\x1b["); j < 0 || !strings.HasPrefix(row[j:], prefix) {
			t.Errorf("%s ←%s is styled %q, want the style of %q",
				tc.id, tc.dep, row[maxInt(0, i-24):i], tc.want)
		}
	}
}

// ansiReset returns whatever trailing reset sequence a styled string carries,
// so a test can compare the OPENING sequence alone.
func ansiReset(styled string) string {
	if i := strings.LastIndex(styled, "\x1b["); i >= 0 {
		return styled[i:]
	}
	return ""
}
