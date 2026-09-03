package ui

import (
	"fmt"
	"strings"
	"sync"
	"testing"

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

func (p *liveQueryProvider) Board() *board.Board { return p.b }
func (p *liveQueryProvider) Reload() error       { return nil }
func (p *liveQueryProvider) Sync() error         { return nil }
func (p *liveQueryProvider) Live() bool          { return true }
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
func (p *liveQueryProvider) PersistMove(_, _, _, _ string) ([]string, error)    { return nil, nil }
func (p *liveQueryProvider) PersistDone(_ string) error                         { return nil }
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

func (p *liveQueryProvider) EpicDone(string) (board.EpicPrevious, error) {
	return board.EpicPrevious{}, nil
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
