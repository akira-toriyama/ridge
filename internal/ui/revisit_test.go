package ui

import (
	"errors"
	"strings"
	"testing"

	lg "charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// The revisit lens is a verdict from `furrow revisit -q`, not a filter term:
// `f` narrows the board to the flagged OPEN tasks, keeps furrow's reasons for
// the peek, and shows a chip in the filter row while it lasts. A second `f`
// hands the board back exactly as it was.
func TestRevisitLensNarrowsToFurrowsFlaggedSet(t *testing.T) {
	m := boardModel(t, 200, 40)
	all := m.countVisible()

	press(m, "f")
	if !m.revisitOn {
		t.Fatal("f must turn the lens on")
	}
	shown := m.countVisible()
	if shown == 0 || shown >= all {
		t.Fatalf("lens shows %d of %d; it must narrow the board without emptying it", shown, all)
	}
	if !strings.Contains(m.status, "revisit lens on") {
		t.Errorf("the note must say what the lens shows, got %q", m.status)
	}
	for _, task := range m.b.Tasks() {
		if !m.taskVisible(task) {
			continue
		}
		// furrow skips every terminal lane (config DefaultTerminal), so the
		// fixture's icebox draft must NOT survive — IsDone was the first
		// cut's rule, and it let that card through (found by review).
		if st := task.Status; st == "done" || st == "icebox" || st == "waiting" {
			t.Errorf("%s sits in terminal lane %s yet survived the lens", task.ID, st)
		}
		if len(m.revisitWhy[task.ID]) == 0 {
			t.Errorf("%s is visible with no reason — every survivor carries furrow's why", task.ID)
		}
	}
	frame, err := m.Dump(200, 40, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(frame, glyphRevisit+" revisit") {
		t.Errorf("the filter row must carry the lens chip while it is on:\n%s", frame)
	}

	// The peek names the reasons: t-jv3j's dep t-t38k is done, and the
	// fixture is months old, so both signals must read verbatim.
	if !m.selectID("t-jv3j", false) {
		t.Fatal("t-jv3j must survive the lens")
	}
	m.peekOpen = true
	out := ansiStrip(m.peekContent(70))
	for _, want := range []string{glyphRevisit + " revisit", "dep_done dep t-t38k is done", "stale no update in"} {
		if !strings.Contains(out, want) {
			t.Errorf("peek lacks %q:\n%s", want, out)
		}
	}

	press(m, "f")
	if m.revisitOn || m.revisitWhy != nil || m.qMatched != nil {
		t.Errorf("the second f must drop the lens whole: on=%v why=%d matched=%v",
			m.revisitOn, len(m.revisitWhy), m.qMatched != nil)
	}
	if got := m.countVisible(); got != all {
		t.Errorf("after the lens %d visible, want the original %d", got, all)
	}
}

// With a typed query the lens is `revisit -q <query>`: furrow ANDs the two,
// so the visible set is inside BOTH the query's verdict and the flagged set.
func TestRevisitLensANDsTheTypedQuery(t *testing.T) {
	m := boardModel(t, 200, 40)
	m.applyFilter("is:blocked")
	blocked := map[string]bool{}
	for id := range m.qMatched {
		blocked[id] = true
	}
	press(m, "f")
	if m.countVisible() == 0 {
		t.Fatal("the fixture has blocked tasks worth a fresh look; the lens emptied the board")
	}
	for _, task := range m.b.Tasks() {
		if m.taskVisible(task) && !blocked[task.ID] {
			t.Errorf("%s is visible but not blocked — the lens must keep the typed query", task.ID)
		}
	}
	if !strings.Contains(m.qRaw, "is:blocked") {
		t.Errorf("the lens must not edit the query text, got %q", m.qRaw)
	}
}

// -revisit is a view setting like -filter: on the fixture the verdict is
// already applied in the opening frame, and on a live store the lens's read
// rides Init the way the startup filter does (hardening_test pins that one;
// dropping this Cmd would open the TUI with the chip on and the board full).
func TestRevisitOptionOpensWithTheLensOn(t *testing.T) {
	m := New(memstore.New(), Options{Revisit: true, Filter: "is:blocked"})
	m.w, m.h = 200, 40
	m.recompute()
	if !m.revisitOn || m.qMatched == nil || len(m.revisitWhy) == 0 {
		t.Fatalf("fixture opening state on=%v matched=%v why=%d; want the lens on with its verdict applied",
			m.revisitOn, m.qMatched != nil, len(m.revisitWhy))
	}

	p := newScriptedProvider(scriptedBoard)
	p.qIDs = []string{"a"}
	live := New(p, Options{Revisit: true})
	if live.startupFilter == nil {
		t.Fatal("the lens must leave its verdict Cmd for Init on a live store")
	}
	live.Update(live.startupFilter())
	if len(p.queries) != 1 || p.queries[0] != "revisit:" {
		t.Fatalf("store reads = %v, want one revisit read with no query", p.queries)
	}
	if live.countVisible() != 1 || live.revisitWhy["a"] == nil {
		t.Errorf("visible = %d, why[a]=%v — the startup verdict must apply the lens",
			live.countVisible(), live.revisitWhy["a"])
	}
}

// `i` stamps the review clock and only that: Reviewed moves, Updated does
// not, the write reaches the store path, and the peek shows the stamp.
func TestReviewKeyStampsReviewedAndLeavesUpdated(t *testing.T) {
	m := boardModel(t, 200, 40)
	if !m.selectID("t-9sa6", false) {
		t.Fatal("t-9sa6 is not on the fixture board")
	}
	before := *m.b.Task("t-9sa6")
	if !before.Reviewed.IsZero() {
		t.Fatal("t-9sa6 must start unreviewed for this test to mean anything")
	}
	press(m, "i")
	got := m.b.Task("t-9sa6")
	if got.Reviewed.IsZero() {
		t.Fatal("i did not stamp Reviewed")
	}
	if !got.Updated.Equal(before.Updated) {
		t.Errorf("i moved Updated %v -> %v; a review changes no content", before.Updated, got.Updated)
	}
	if !strings.Contains(m.status, "reviewed t-9sa6") {
		t.Errorf("status = %q, want the stamp named", m.status)
	}
	drainPersists(m, t)
	m.peekOpen = true
	if out := ansiStrip(m.peekContent(70)); !strings.Contains(out, "reviewed 0m ago") {
		t.Errorf("the peek's stamps line must show the review clock:\n%s", out)
	}
}

// The rollback window refuses the stamp like every other write — and BEFORE
// the local apply, so the board does not show a clock the store never got.
func TestReviewKeyRefusedWhileRollingBack(t *testing.T) {
	m := boardModel(t, 200, 40)
	if !m.selectID("t-9sa6", false) {
		t.Fatal("t-9sa6 is not on the fixture board")
	}
	m.rollingBack = true
	press(m, "i")
	if !m.b.Task("t-9sa6").Reviewed.IsZero() || len(m.pending) != 0 {
		t.Errorf("the refusal must be total: reviewed=%v pending=%d",
			m.b.Task("t-9sa6").Reviewed, len(m.pending))
	}
	if !strings.Contains(m.status, "review t-9sa6 dropped") {
		t.Errorf("status = %q, want the refusal named", m.status)
	}
}

// The two peek lines the lens adds (the third stamp and the reason line)
// must fit the box at every size, with CJK titles around them; the general
// box-fit sweep never turns the lens on, so it cannot see them.
func TestRevisitPeekLinesFitTheirBox(t *testing.T) {
	for _, size := range [][2]int{{240, 50}, {140, 40}, {100, 30}, {80, 24}, {60, 20}} {
		m := boardModel(t, size[0], size[1])
		press(m, "f")
		if !m.selectID("t-jv3j", false) {
			t.Fatal("t-jv3j must survive the lens")
		}
		m.peekOpen = true
		m.syncPeek()
		_, _, w, _ := m.peekBox()
		inner := maxInt(10, w-4)
		out := ansiStrip(m.peekContent(inner))
		if !strings.Contains(out, "reviewed ") || !strings.Contains(out, glyphRevisit+" revisit") {
			t.Fatalf("%dx%d peek lacks the stamp or the reason line:\n%s", size[0], size[1], out)
		}
		for i, line := range strings.Split(out, "\n") {
			if lw := lg.Width(line); lw > inner {
				t.Errorf("%dx%d peek line %d is %d cells, box inner width is %d: %q",
					size[0], size[1], i, lw, inner, line)
			}
		}
	}
}

// -revisit must not write over the read-only warning, which is set once per
// session and restored by nothing (the -roadmap precedent, and the regression
// CLAUDE.md records shipping once).
func TestRevisitOptionKeepsTheReadOnlyWarning(t *testing.T) {
	m := New(memstore.NewGated("board-behind"), Options{Revisit: true})
	if !m.revisitOn {
		t.Fatal("-revisit must turn the lens on")
	}
	if !strings.Contains(m.status, "read-only") {
		t.Errorf("status %q lost the read-only warning to the lens", m.status)
	}
}

// -revisit and -demo revisit compose: the demo asserts the lens ON rather than
// toggling it, or the pair cancelled out into an unfiltered "revisit" frame.
func TestRevisitOptionComposesWithTheRevisitDemo(t *testing.T) {
	m := New(memstore.New(), Options{Revisit: true})
	frame, err := m.Dump(200, 40, "revisit", true)
	if err != nil {
		t.Fatal(err)
	}
	if !m.revisitOn || !strings.Contains(frame, glyphRevisit+" revisit") {
		t.Errorf("on=%v; the demo frame must still carry the lens:\n%s", m.revisitOn, frame)
	}
}

// Turning the lens off tears down what it owned even when the last re-query
// was refused: the reasons (applyVerdict keeps the last good verdict on an
// error, so nothing else clears them) and the jump pins when nothing narrows
// the board any more.
func TestRevisitOffDropsReasonsAndPins(t *testing.T) {
	p := &liveQueryProvider{b: board.NewBoard([]*board.Task{
		{ID: "a", Title: "a", Status: "backlog"},
		{ID: "b", Title: "b", Status: "backlog"},
	}), ids: []string{"a"}}
	m := New(p, Options{})
	m.w, m.h = 120, 30
	m.recompute()
	c := pressKey(m, 'f')
	if c == nil {
		t.Fatal("the lens on a live store must fire a read")
	}
	m.Update(c())
	if len(m.revisitWhy) != 1 {
		t.Fatalf("why = %v, want the scripted reason for a", m.revisitWhy)
	}
	m.pinned["b"] = true // a jump past the lens
	p.err = errors.New("furrow: unknown qualifier")
	c = pressKey(m, 'f')
	if c != nil {
		m.Update(c())
	}
	if m.revisitOn || m.revisitWhy != nil || len(m.pinned) != 0 {
		t.Errorf("after f off: on=%v why=%v pinned=%v — the lens must tear down whole", m.revisitOn, m.revisitWhy, m.pinned)
	}
	m.peekOpen = true
	if out := ansiStrip(m.peekContent(60)); strings.Contains(out, glyphRevisit) {
		t.Errorf("the peek still shows a reason line with the lens off:\n%s", out)
	}
}

// The full-screen views mute filtered rows instead of dropping them, and the
// lens is a filter like any other: with no typed query the map must still
// count what the lens hides.
func TestRevisitLensMutesTheFullScreenViews(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "f")
	hidden := 0
	for _, task := range m.b.Tasks() {
		if m.taskHidden(task.ID) {
			hidden++
		}
	}
	if hidden == 0 {
		t.Fatal("taskHidden counts nothing under the lens while the board hides tasks")
	}
	if hidden+m.countVisible() != len(m.b.Tasks()) {
		t.Errorf("hidden %d + visible %d != %d tasks — the two predicates disagree",
			hidden, m.countVisible(), len(m.b.Tasks()))
	}
}
