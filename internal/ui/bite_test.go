package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// syncProvider is the fixture reported as LIVE, counting the two calls `R`
// makes so their ORDER can be asserted: sync then re-read, never the reverse.
type syncProvider struct {
	*memstore.Store
	syncs, reloads    int
	reloadedAfterSync bool
}

func newSyncProvider() *syncProvider { return &syncProvider{Store: memstore.New()} }

func (p *syncProvider) Live() bool { return true }

func (p *syncProvider) Sync() error {
	p.syncs++
	return nil
}

func (p *syncProvider) Reload() error {
	p.reloads++
	if p.syncs > 0 {
		p.reloadedAfterSync = true
	}
	return p.Store.Reload()
}

// Behaviour this package documents as load-bearing and did not test: deleting
// any of it left `go test ./...` green.

// --- the measurer's cache -----------------------------------------------------

// The cache is kept ACROSS frames on purpose — per-frame measurement cost
// 36ms/frame at 658 tasks — and dropped on rebind, because a stale height
// desynchronises the renderer from the hit-test. Neither half was pinned: the
// only test naming the measurer asserts pointer identity of the theme.
func TestMeasurerCacheOutlivesTheFrame(t *testing.T) {
	m := boardModel(t, 240, 60)
	m.View()

	c := m.lay.Col("backlog")
	if c == nil || len(c.Cards) == 0 {
		t.Fatal("expected a populated backlog column")
	}
	id := c.Tasks[c.Cards[0].Idx].ID
	k := measureKey{id: id, w: m.lay.ColW}
	measured, ok := m.ms.cache[k]
	if !ok {
		t.Fatalf("the first render did not cache a height for %s at width %d", id, m.lay.ColW)
	}

	// Poison it. A cache rebuilt every frame would overwrite this and the
	// layout would be unchanged — which is the 36ms/frame regression.
	m.ms.cache[k] = measured + 3
	m.relayout()

	c = m.lay.Col("backlog")
	var got int
	for _, b := range c.Cards {
		if c.Tasks[b.Idx].ID == id {
			got = b.H
		}
	}
	if got != measured+3 {
		t.Errorf("card %s measured %d after the cache was poisoned to %d — the cache is being rebuilt per frame",
			id, got, measured+3)
	}
}

// recompute() must drop the cache, and the layout that follows must agree with
// what the renderer actually draws — for the edited card AND for every card
// below it, since a height change shifts them all.
func TestRecomputeDropsTheCacheAndTheLayoutFollowsTheRender(t *testing.T) {
	m := boardModel(t, 240, 60)
	m.View()
	if len(m.ms.cache) == 0 {
		t.Fatal("nothing was measured")
	}

	c := m.lay.Col("backlog")
	if c == nil || len(c.Cards) < 2 {
		t.Fatal("need at least two visible backlog cards")
	}
	first := c.Tasks[c.Cards[0].Idx]

	// A retitle changes the wrapped line count, which changes the card height.
	first.Title = strings.Repeat("長いタイトルに書き換える", 6)
	m.recompute()

	if n := len(m.ms.cache); n != 0 {
		t.Errorf("recompute left %d cached heights; a stale one desynchronises the hit-test from the renderer", n)
	}

	m.relayout()
	c = m.lay.Col("backlog")
	for i, box := range c.Cards {
		task := c.Tasks[box.Idx]
		want := lg.Height(renderCard(task, m.g, m.th, c.W, cardNormal))
		if box.H != want {
			t.Errorf("card %d (%s) is laid out %d rows tall, renders %d", i, task.ID, box.H, want)
		}
		if i > 0 {
			prev := c.Cards[i-1]
			if wantY := prev.Y + prev.H + cardGapY; box.Y != wantY {
				t.Errorf("card %d (%s) sits at y=%d, want %d — the cards below the edit did not shift",
					i, task.ID, box.Y, wantY)
			}
		}
	}
}

// --- the drag's edge auto-scroll ----------------------------------------------

// The repeat tick is what makes holding still at a column edge keep scrolling:
// a terminal only emits motion when the pointer MOVES. The handler was 0%
// covered, so both its stale-seq guard and its self-re-arm could be deleted.
func TestEdgeAutoScrollKeepsTickingAndStopsAtTheEnd(t *testing.T) {
	m := boardModel(t, 240, 24) // short, so a column has cards below the fold
	var lane string
	for _, l := range m.b.Lanes() {
		if c := m.lay.Col(l.Name); c != nil && c.Hidden > 0 && len(c.Cards) > 0 {
			lane = l.Name
			break
		}
	}
	if lane == "" {
		t.Skip("no column has cards below the fold at this size")
	}
	c := m.lay.Col(lane)
	box := c.Cards[0]

	m.Update(tea.MouseClickMsg{X: box.X + 3, Y: box.Y + 1, Button: tea.MouseLeft})
	cmd := cmdOf(m.Update(tea.MouseMotionMsg{X: c.X + 3, Y: c.Bot - 1, Button: tea.MouseLeft}))
	if cmd == nil {
		t.Fatal("parking the pointer at the bottom edge armed no repeat tick")
	}
	if m.drag.scrollDir <= 0 {
		t.Fatalf("the bottom edge armed scrollDir=%d, want +1", m.drag.scrollDir)
	}

	// Follow the chain. It must keep re-arming while there is more to scroll
	// and stop exactly when there is not.
	steps := 0
	for cmd != nil && steps < 200 {
		msg := cmd()
		tick, ok := msg.(dragScrollMsg)
		if !ok {
			t.Fatalf("the auto-scroll tick produced a %T", msg)
		}
		cmd = cmdOf(m.Update(tick))
		steps++
	}
	if steps == 0 {
		t.Error("the tick chain stopped before scrolling once")
	}
	if steps >= 200 {
		t.Fatal("the tick chain never stopped — it re-arms past the end of the column")
	}
	if got := m.lay.Col(lane); got.Hidden != 0 {
		t.Errorf("the chain stopped with %d cards still below the fold", got.Hidden)
	}
}

// "there is never more than one live timer": a tick from a superseded pointer
// position must be dropped, not applied. The direction stays ARMED here — a
// test that merely moves off the edge is killed by the scrollDir check alone
// and proves nothing about the seq guard.
func TestASupersededAutoScrollTickIsDropped(t *testing.T) {
	m := boardModel(t, 240, 22)
	var lane string
	for _, l := range m.b.Lanes() {
		if c := m.lay.Col(l.Name); c != nil && c.Hidden > 3 && len(c.Cards) > 0 {
			lane = l.Name
			break
		}
	}
	if lane == "" {
		t.Skip("no column has enough cards below the fold at this size")
	}
	c := m.lay.Col(lane)
	box := c.Cards[0]

	m.Update(tea.MouseClickMsg{X: box.X + 3, Y: box.Y + 1, Button: tea.MouseLeft})

	// Park at the bottom edge: arms scrollDir=+1 and hands back tick #1.
	stale := cmdOf(m.Update(tea.MouseMotionMsg{X: c.X + 3, Y: c.Bot - 1, Button: tea.MouseLeft}))
	if stale == nil {
		t.Fatal("no tick was armed")
	}
	staleMsg, ok := stale().(dragScrollMsg)
	if !ok {
		t.Fatal("not a scroll tick")
	}

	// Another motion, still at the edge: bumps scrollSeq and supersedes tick
	// #1 while LEAVING the direction armed.
	if cmd := cmdOf(m.Update(tea.MouseMotionMsg{X: c.X + 4, Y: c.Bot - 1, Button: tea.MouseLeft})); cmd == nil {
		t.Fatal("the second motion disarmed the scroll; this test needs it live")
	}
	if m.drag.scrollDir == 0 {
		t.Fatal("the direction must still be armed for this to test the seq guard")
	}
	before := m.scroll[lane]

	if cmd := cmdOf(m.Update(staleMsg)); cmd != nil {
		t.Error("a superseded tick re-armed the timer — there is now more than one live")
	}
	if m.scroll[lane] != before {
		t.Errorf("a superseded tick scrolled %s from %d to %d", lane, before, m.scroll[lane])
	}
}

// --- `R` sync ------------------------------------------------------------------

// glossary: sync is `furrow sync` THEN a store re-read, never automatic, and
// `r` is the re-read alone. Every line of that branch — both guards and the
// ordering — was uncovered.
func TestSyncRunsTheStoreSyncThenReReads(t *testing.T) {
	p := newSyncProvider()
	m := New(p, Options{})
	m.w, m.h = 240, 60
	m.recompute()
	m.relayout()

	cmd := cmdOf(m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"}))
	if cmd == nil {
		t.Fatal("R produced no command on a live provider")
	}
	msg, ok := cmd().(reloadDoneMsg)
	if !ok {
		t.Fatalf("sync produced a %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("sync reported %v", msg.err)
	}
	if p.syncs != 1 {
		t.Errorf("Sync ran %d times, want 1", p.syncs)
	}
	if p.reloads != 1 {
		t.Errorf("Reload ran %d times after the sync, want 1", p.reloads)
	}
	if !p.reloadedAfterSync {
		t.Error("the re-read ran BEFORE the sync — it would pick up none of what the pull brought in")
	}
	if !strings.Contains(msg.label, "sync") {
		t.Errorf("the status label %q does not name the sync", msg.label)
	}
}

// A queued write must not be raced by a git sync.
func TestSyncIsRefusedWhileWritesAreInFlight(t *testing.T) {
	p := newSyncProvider()
	m := New(p, Options{})
	m.w, m.h = 240, 60
	m.recompute()
	m.relayout()
	m.inflight = true

	if cmd := cmdOf(m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})); cmd != nil {
		t.Error("R started a git sync while a write was still in flight")
	}
	if p.syncs != 0 {
		t.Errorf("Sync ran %d times during an in-flight write", p.syncs)
	}
	if m.status == "" {
		t.Error("the refusal said nothing")
	}
}

// --- ago() ---------------------------------------------------------------------

// Every fixture stamp sits ~24 days before the pinned clock, so the zero,
// minute, hour and >90-day branches had no coverage at all — and the two tests
// that touch ago() compare it against itself or against a 6-character
// substring, so a 100-day skew passed.
func TestAgoRendersEachBranchLiterally(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	fixedNow(t, now)

	old := now.Add(-91 * 24 * time.Hour)
	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"zero", time.Time{}, "never"},
		{"minutes", now.Add(-30 * time.Minute), "30m ago"},
		{"just under an hour", now.Add(-59 * time.Minute), "59m ago"},
		{"hours", now.Add(-5 * time.Hour), "5h ago"},
		{"just under a day", now.Add(-23 * time.Hour), "23h ago"},
		{"days", now.Add(-72 * time.Hour), "3d ago"},
		{"just under the cutoff", now.Add(-89 * 24 * time.Hour), "89d ago"},
		// Past 90 days it becomes an absolute date. Derived through the same
		// Format call so the expectation is timezone-independent.
		{"past the cutoff", old, old.Format("2006-01-02")},
	}
	for _, c := range cases {
		if got := ago(c.at); got != c.want {
			t.Errorf("%s: ago() = %q, want %q", c.name, got, c.want)
		}
	}
}

// --- card geometry at the widths the app actually negotiates --------------------

// The only multi-width per-card check sweeps terminal widths 56-160, which
// yield CARD widths of 24-36. At the declared 240-400 range boardCols yields
// 38-64 — a disjoint set. CJK wrapping is parity-sensitive (a spaceless
// Japanese title hard-wraps at w-2), which is exactly why the drift shows up on
// wide screens and not narrow ones.
func TestCardLinesFillTheirColumnAcrossTheDeclaredWidthRange(t *testing.T) {
	for w := 238; w <= 404; w++ {
		m := boardModel(t, w, 60)
		for i := range m.lay.Cols {
			c := &m.lay.Cols[i]
			for _, box := range c.Cards {
				task := c.Tasks[box.Idx]
				card := renderCard(task, m.g, m.th, c.W, cardNormal)
				lines := strings.Split(card, "\n")
				if len(lines) != box.H {
					t.Fatalf("w=%d lane=%s %s: laid out %d rows, renders %d",
						w, c.Lane.Name, task.ID, box.H, len(lines))
				}
				for j, l := range lines {
					if lg.Width(l) != c.W {
						t.Fatalf("w=%d lane=%s %s line %d measures %d cells, column is %d — the border shears",
							w, c.Lane.Name, task.ID, j, lg.Width(l), c.W)
					}
				}
			}
		}
	}
}

// cmdOf unwraps the (tea.Model, tea.Cmd) pair Update returns.
func cmdOf(_ tea.Model, cmd tea.Cmd) tea.Cmd { return cmd }

// The other half of `R`'s guard pair: the fixture has no store to sync, so the
// key must say so and run nothing. Deleting `if !m.prov.Live()` left every
// package green — memstore.Sync's own error is swallowed into a reloadDoneMsg
// nobody was asserting on.
func TestSyncIsRefusedOnTheFixture(t *testing.T) {
	p := &countingFixture{Store: memstore.New()}
	m := New(p, Options{})
	m.w, m.h = 240, 60
	m.recompute()
	m.relayout()

	if cmd := cmdOf(m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})); cmd != nil {
		t.Error("R started a git sync against the fixture, which has no store behind it")
	}
	if p.syncs != 0 {
		t.Errorf("Sync ran %d times on the fixture", p.syncs)
	}
	if !strings.Contains(m.status, "sync") {
		t.Errorf("the refusal does not mention syncing: %q", m.status)
	}
}

// countingFixture is the fixture with its Live() answer left alone, counting
// Sync so the refusal can be asserted as "never called".
type countingFixture struct {
	*memstore.Store
	syncs int
}

func (p *countingFixture) Sync() error {
	p.syncs++
	return p.Store.Sync()
}
