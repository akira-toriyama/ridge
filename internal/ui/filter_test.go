package ui

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// liveQueryProvider is a Live()=true provider whose Query is scripted, so the
// debounce/staleness machinery can be driven without a furrow binary and
// without wall-clock waits.
type liveQueryProvider struct {
	b *board.Board

	mu    sync.Mutex
	calls []string
	ids   []string
	err   error
}

func (p *liveQueryProvider) Board() *board.Board             { return p.b }
func (p *liveQueryProvider) Reload() error                   { return nil }
func (p *liveQueryProvider) Sync() (board.SyncReport, error) { return board.SyncReport{}, nil }
func (p *liveQueryProvider) Live() bool                      { return true }
func (p *liveQueryProvider) Query(q string) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, q)
	return p.ids, p.err
}
func (p *liveQueryProvider) queryCalls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.calls...)
}
func (p *liveQueryProvider) PersistMove(_, _, _, _ string) (board.MoveReport, error) {
	return board.MoveReport{}, nil
}
func (p *liveQueryProvider) PersistDone(_ string) (*board.RepeatReport, error)  { return nil, nil }
func (p *liveQueryProvider) PersistCheck(_ string, _ int, _ bool) error         { return nil }
func (p *liveQueryProvider) PersistBody(_, _ string) error                      { return nil }
func (p *liveQueryProvider) PersistNote(_, _ string) error                      { return nil }
func (p *liveQueryProvider) PersistReview(_ string) error                       { return nil }
func (p *liveQueryProvider) PersistFields(_ string, _ board.FieldPatch) error   { return nil }
func (p *liveQueryProvider) PersistCheckAdd(_, _ string) error                  { return nil }
func (p *liveQueryProvider) PersistCheckRm(_ string, _ int) error               { return nil }
func (p *liveQueryProvider) PersistCheckReword(_ string, _ int, _ string) error { return nil }
func (p *liveQueryProvider) PersistDepAdd(_, _ string) error                    { return nil }
func (p *liveQueryProvider) PersistDepRm(_, _ string) error                     { return nil }
func (p *liveQueryProvider) Add(string, board.AddOptions) (string, error)       { return "", nil }
func (p *liveQueryProvider) EpicSet(string, board.EpicPatch) error              { return nil }
func (p *liveQueryProvider) EpicActivate(_, _ string) error                     { return nil }
func (p *liveQueryProvider) EpicDepAdd(_, _ string) error                       { return nil }
func (p *liveQueryProvider) EpicDepRm(_, _ string) error                        { return nil }

// Revisit answers the same scripted ids, flagged, and logs the call under a
// `revisit:` prefix so a test can tell which read the lens fired.
func (p *liveQueryProvider) Revisit(q string) ([]board.Revisit, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, "revisit:"+q)
	if p.err != nil {
		return nil, p.err
	}
	rows := make([]board.Revisit, 0, len(p.ids))
	for _, id := range p.ids {
		rows = append(rows, board.Revisit{ID: id, Reasons: []board.RevisitReason{{Code: "stale", Detail: "scripted"}}})
	}
	return rows, nil
}

func (p *liveQueryProvider) EpicAdd(string, board.EpicAddOptions) (string, error) {
	return "e-new", nil
}

func (p *liveQueryProvider) EpicDeactivate(string) (board.EpicPrevious, error) {
	return board.EpicPrevious{}, nil
}

func (p *liveQueryProvider) EpicDone(string) (board.EpicClose, error) {
	return board.EpicClose{}, nil
}
func (p *liveQueryProvider) EpicReopen(string) error { return nil }

func liveModel(t *testing.T) (*Model, *liveQueryProvider) {
	t.Helper()
	p := &liveQueryProvider{b: memstore.New().Board()}
	m := New(p, Options{})
	m.w, m.h = 140, 40
	m.recompute()
	m.relayout()
	return m, p
}

func TestLiveFilterDebouncesToTheNewestKeystroke(t *testing.T) {
	m, p := liveModel(t)

	cmd := m.applyFilter("lane:r")
	if cmd == nil {
		t.Fatal("a live filter keystroke must arm the debounce")
	}
	stale := m.qSeq
	if calls := p.queryCalls(); len(calls) != 0 {
		t.Fatalf("the store was queried before the debounce fired: %v", calls)
	}

	// A newer keystroke lands before the first tick fires.
	if cmd = m.applyFilter("lane:re"); cmd == nil {
		t.Fatal("the newer keystroke must re-arm the debounce")
	}
	fresh := m.qSeq

	// The stale tick fires: it must NOT query the store.
	if c := m.onFilterTick(filterTickMsg{seq: stale}); c != nil {
		t.Error("a stale tick must die, not query the store")
	}
	// The fresh tick fires: exactly one query, for the newest text.
	c := m.onFilterTick(filterTickMsg{seq: fresh})
	if c == nil {
		t.Fatal("the newest tick must query the store")
	}
	msg := c()
	if calls := p.queryCalls(); len(calls) != 1 || calls[0] != "lane:re" {
		t.Fatalf("store queries = %v, want exactly [lane:re]", calls)
	}
	res, ok := msg.(filterResultMsg)
	if !ok {
		t.Fatalf("query cmd returned %T", msg)
	}
	m.Update(res)
	if m.qMatched == nil {
		t.Error("the verdict was not applied")
	}
}

func TestStaleFilterResultIsDroppedWhole(t *testing.T) {
	m, _ := liveModel(t)

	m.applyFilter("one")
	stale := m.qSeq
	m.applyFilter("two")

	// The verdict for the OLD text arrives after the new keystroke.
	m.Update(filterResultMsg{seq: stale, ids: []string{"t-jv3j"}})
	if m.qMatched != nil {
		t.Error("a stale verdict must be dropped whole, not shown under newer text")
	}
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"t-jv3j"}})
	if !m.qMatched["t-jv3j"] {
		t.Error("the current verdict must land")
	}
	if n := m.countVisible(); n != 1 {
		t.Errorf("verdict of 1 id shows %d cards", n)
	}
}

func TestLiveRefusalKeepsTheLastGoodVerdict(t *testing.T) {
	m, p := liveModel(t)

	m.applyFilter("lane:ready")
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"t-n2fc"}})
	if n := m.countVisible(); n != 1 {
		t.Fatalf("good verdict shows %d, want 1", n)
	}

	p.err = fmt.Errorf("value: needs a comparison (query-invalid)")
	m.applyFilter("lane:ready value:>")
	m.Update(filterResultMsg{seq: m.qSeq, err: p.err})
	if n := m.countVisible(); n != 1 {
		t.Errorf("a refusal must keep the last good verdict: %d visible", n)
	}
	if !strings.Contains(m.qErr, "query-invalid") {
		t.Errorf("the refusal must be surfaced verbatim, got %q", m.qErr)
	}

	// The next clean verdict clears the refusal.
	m.applyFilter("lane:ready")
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"t-n2fc"}})
	if m.qErr != "" {
		t.Errorf("a clean verdict must clear the refusal, got %q", m.qErr)
	}
}

func TestReloadRequeriesTheActiveFilter(t *testing.T) {
	m, p := liveModel(t)

	m.applyFilter("is:blocked")
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"t-jv3j"}})
	before := len(p.queryCalls())

	// A silent reconcile re-read lands: the verdict is stale, so the model
	// must ask again — without the keystroke debounce.
	_, cmd := m.Update(reloadDoneMsg{})
	if cmd == nil {
		t.Fatal("a reload under an active filter must requery the store")
	}
	if msg := cmd(); msg != nil {
		m.Update(msg)
	}
	calls := p.queryCalls()
	if len(calls) != before+1 || calls[len(calls)-1] != "is:blocked" {
		t.Errorf("reload queries = %v, want one more 'is:blocked'", calls)
	}
}

func TestClearingTheFilterNeedsNoStoreRoundTrip(t *testing.T) {
	m, p := liveModel(t)

	m.applyFilter("lane:ready")
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"t-n2fc"}})
	n := len(p.queryCalls())
	if cmd := m.applyFilter(""); cmd != nil {
		t.Error("clearing the filter must not need the store")
	}
	if len(p.queryCalls()) != n {
		t.Error("clearing the filter queried the store")
	}
	if m.countVisible() != len(m.b.Tasks()) {
		t.Error("clearing the filter must show the whole board")
	}
}

func (p *liveQueryProvider) SweepPreview() (board.Sweep, error) { return board.Sweep{}, nil }
func (p *liveQueryProvider) Archive([]string) error             { return nil }
func (p *liveQueryProvider) Unarchive([]string) error           { return nil }
func (p *liveQueryProvider) Tidy(board.TidyClass) error         { return nil }

// The `b` (blocked-only) toggle does a raw string ReplaceAll on the query, so a
// NEGATED is:blocked term leaves a stray "-" token behind.
func TestAdvBlockedToggleCorruptsANegatedQuery(t *testing.T) {
	m := boardModel(t, 140, 40)
	m.applyFilter("-is:blocked")
	m.ti.SetValue("-is:blocked")
	before := m.countVisible()
	m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if m.qRaw == "-" {
		t.Errorf("pressing b on %q left the query %q (a bare '-' bare-word term); "+
			"visible went %d -> %d", "-is:blocked", m.qRaw, before, m.countVisible())
	}
}

// The toggle must cut the query where furrow's lexer does (board.QFields):
// strings.Fields split a value holding U+3000 or NBSP, and the rejoin handed
// back two terms the user never typed (t-j39t).
func TestDropBlockedTokenSplitsOnlyWhereTheLexerDoes(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
		had       bool
	}{
		{"", "", false},
		{"is:blocked", "", true},
		{"label:ui is:blocked", "label:ui", true},
		{"is:blocked label:ui", "label:ui", true},
		{"label:ui\tis:blocked\tepic:e-1", "label:ui epic:e-1", true},
		{"label:全角　空白 is:blocked", "label:全角　空白", true},
		{"label:nb\u00a0sp is:blocked", "label:nb\u00a0sp", true},
		{`label:"needs review" is:blocked`, `label:"needs review"`, true},
		// Only the exact token: a negation is not the toggle's term, and a
		// token glued to a quote is not either. Quoting is not honoured beyond
		// that (board.QFields): a whitespace-delimited is:blocked INSIDE a
		// quoted phrase is cut too, as it always was.
		{"-is:blocked", "-is:blocked", false},
		{`title:"is:blocked" is:blocked`, `title:"is:blocked"`, true},
	} {
		got, had := dropBlockedToken(tc.raw)
		if got != tc.want || had != tc.had {
			t.Errorf("dropBlockedToken(%q) = (%q, %v), want (%q, %v)", tc.raw, got, had, tc.want, tc.had)
		}
	}
}

// The same fact through the key: `b` twice must hand the typed query back
// unchanged, wide space included.
func TestBlockedToggleRoundTripsAValueHoldingAWideSpace(t *testing.T) {
	m := boardModel(t, 140, 40)
	const q = "label:全角　空白"
	m.applyFilter(q)
	m.ti.SetValue(q)
	m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if want := q + " is:blocked"; m.qRaw != want {
		t.Fatalf("first b: query %q, want %q", m.qRaw, want)
	}
	m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if m.qRaw != q {
		t.Errorf("second b: query %q, want %q back", m.qRaw, q)
	}
}

// Typing a filter that hides the selected card silently leaves the cursor on a
// DIFFERENT task, and every subsequent destructive key (d = done, x = check,
// enter = move) acts on that one.
func TestAdvFilterCanSilentlyRepointTheCursor(t *testing.T) {
	// Five backlog tasks, one of them blocked: the filter must hide the card
	// under the cursor and leave exactly one for it to land on. Built rather
	// than taken from the fixture, whose blocked tasks are its own shape.
	ts := []*board.Task{{ID: "b1", Title: "b1", Status: "backlog", Priority: 10, Deps: []string{"b5"}}}
	for i := 2; i <= 5; i++ {
		id := fmt.Sprintf("b%d", i)
		ts = append(ts, &board.Task{ID: id, Title: id, Status: "backlog", Priority: i * 10})
	}
	m := advModel(t, board.NewBoard(ts), 140, 40)
	m.curLane = m.b.LaneIndex("backlog")
	m.setPos(3)
	before := m.curTask()
	if before == nil {
		t.Fatal("no selection")
	}
	m.applyFilter("is:blocked")
	after := m.curTask()
	if after != nil && after.ID != before.ID {
		t.Logf("selection moved %s -> %s after filtering (expected); the danger is "+
			"that nothing tells the user", before.ID, after.ID)
	}
	// Now the real defect: pressing `d` closes whatever the cursor landed on.
	if after == nil {
		t.Fatal("setup: is:blocked hid every task, but b1 waits on b5")
	}
	m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if m.b.Task(after.ID).Status != "done" {
		t.Fatal("d did not close the selection")
	}
	if before.ID != after.ID && !strings.Contains(m.status, after.ID) {
		t.Errorf("closed %s but the status line says %q", after.ID, m.status)
	}
}

func TestAdvUnknownIsValueEmptiesTheBoardWhileClaimingToBeNonFatal(t *testing.T) {
	m := boardModel(t, 140, 40)
	total := m.countVisible()
	m.applyFilter("is:bogus")
	// -q semantics: the store REFUSES the query; the last good verdict (all
	// tasks) stays on screen and the refusal is surfaced.
	if m.countVisible() != total {
		t.Errorf("a refused query must keep the last good verdict: %d -> %d visible",
			total, m.countVisible())
	}
	if m.qErr == "" {
		t.Error("the refusal must be surfaced in qErr")
	}
}
