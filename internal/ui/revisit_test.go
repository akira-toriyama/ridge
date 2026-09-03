package ui

import (
	"strings"
	"testing"

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
		if m.g.IsDone(task.ID) {
			t.Errorf("%s is done yet survived the lens — revisit lists OPEN tasks", task.ID)
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
