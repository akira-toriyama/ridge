package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// Dump on a live store (the -live path). Two rules the fixture never
// exercised: the -demo harness is refused there (one of its states archives
// through the provider), and what a live store answers as Cmds — the
// -filter / -revisit verdicts, the sweep's preview read — is settled before
// the frame, where the fixture answered inside New. Without the settle a
// live -filter frame showed the query over the unfiltered board.

func TestDumpRefusesTheHarnessOnALiveStore(t *testing.T) {
	p := newScriptedProvider(scriptedBoard)
	m := New(p, Options{})
	if _, err := m.Dump(240, 40, "sweeprestore", true); err == nil {
		t.Fatal("a -demo on a live store must be refused: sweeprestore archives through the provider")
	}
	for _, c := range p.calls {
		if strings.HasPrefix(c, "archive") {
			t.Fatalf("the refused demo still reached the provider: %v", p.calls)
		}
	}
}

// The opening view seeds on the cursor, so the -filter verdict must land
// before it — on a live store too, where the verdict is an exec. Before the
// settle moved into New, `-live -graph -filter <nothing>` rooted the graph on
// a task the filter excluded while the fixture drew the board (found in
// review).
func TestLiveFilterNarrowsTheBoardBeforeTheOpeningViewSeeds(t *testing.T) {
	p := newScriptedProvider(scriptedBoard)
	p.qIDs = []string{"b"} // not the unfiltered cursor (the first card of the first lane with work)
	m := New(p, Options{Filter: "title:b", Graph: true})
	if m.view != viewGraph || m.graph.focus != "b" {
		t.Fatalf("view=%v focus=%q; want the graph rooted on the one task the filter left", m.view, m.graph.focus)
	}
	if len(p.queries) != 1 {
		t.Fatalf("store queries = %v, want the startup filter asked once", p.queries)
	}
	out, err := m.Dump(240, 40, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "⟨GRAPH⟩") || !strings.Contains(out, "rooted on b") {
		t.Errorf("the frame is not the graph rooted on b:\n%s", out)
	}
}

// With nothing under the cursor — here a filter that excludes every card —
// the graph cannot open, the board is drawn, and the frame says so instead
// of passing the board off as the requested view.
func TestGraphFlagWithNothingUnderTheCursorSaysSo(t *testing.T) {
	p := newScriptedProvider(scriptedBoard)
	p.qIDs = nil
	m := New(p, Options{Filter: "title:nothing", Graph: true})
	if m.view != viewBoard {
		t.Fatalf("view=%v; want the board (there is no task to root the graph on)", m.view)
	}
	if !m.statusErr || !strings.Contains(m.status, "graph not opened") {
		t.Errorf("status=%q err=%v; want the unopened graph named as a failure", m.status, m.statusErr)
	}

	// On a read-only board the warning is kept and the fact rides behind
	// it — the one board state that must stay checkable headless is not
	// where the swapped view goes unexplained.
	ro := New(memstore.NewGated("board-behind"), Options{Filter: "title:nothing", Graph: true})
	if !strings.Contains(ro.status, "read-only") || !strings.Contains(ro.status, "graph not opened") {
		t.Errorf("read-only status=%q; want the warning AND the unopened graph", ro.status)
	}
}

// A -filter furrow refuses keeps the last good verdict (none: the full
// board) and the graph roots on the unfiltered cursor — and the status line
// says the filter was refused, because the graph has no filter row to show
// ⚠ in (the board does; the graph said nothing, found in review).
func TestRefusedStartupFilterIsNamedInTheStatus(t *testing.T) {
	p := &liveQueryProvider{b: memstore.New().Board(), err: errors.New(`unknown qualifier "bogus"`)}
	m := New(p, Options{Filter: "bogus:zzz", Graph: true})
	if m.view != viewGraph {
		t.Fatalf("view = %v; want the graph on the unfiltered cursor", m.view)
	}
	if !m.statusErr || !strings.Contains(m.status, "-filter refused") || !strings.Contains(m.status, "bogus") {
		t.Errorf("status=%q err=%v; want the refusal named", m.status, m.statusErr)
	}

	// Two refusals at once (an empty board: the graph cannot open either)
	// share the one status row; written one after the other, the second
	// erased the first.
	empty := &liveQueryProvider{b: board.NewBoard(nil), err: errors.New(`unknown qualifier "bogus"`)}
	both := New(empty, Options{Filter: "bogus:zzz", Graph: true})
	if !strings.Contains(both.status, "-filter refused") || !strings.Contains(both.status, "graph not opened") {
		t.Errorf("status=%q; want both refusals on the row", both.status)
	}

	// A refused lens read names the flag that was typed.
	lens := &liveQueryProvider{b: memstore.New().Board(), err: errors.New("revisit is not supported")}
	rv := New(lens, Options{Revisit: true})
	if !strings.Contains(rv.status, "-revisit refused") {
		t.Errorf("status=%q; want the lens's refusal named after -revisit", rv.status)
	}
}

// -filter with -revisit is ONE read: `revisit -q` is the filtered verdict,
// so an `ls -q` fired first is dead work its successor fences out (measured
// as an extra exec on the live path, found in review).
func TestFilterWithRevisitAsksTheStoreOnce(t *testing.T) {
	p := newScriptedProvider(scriptedBoard)
	p.qIDs = []string{"a"}
	m := New(p, Options{Filter: "lane:ready", Revisit: true})
	if len(p.queries) != 1 || p.queries[0] != "revisit:lane:ready" {
		t.Fatalf("store reads = %v, want one revisit read carrying the query", p.queries)
	}
	if m.countVisible() != 1 || m.qRaw != "lane:ready" {
		t.Errorf("visible = %d, qRaw = %q — the one read must apply the filtered lens", m.countVisible(), m.qRaw)
	}
}

func TestLiveDumpSettlesTheSweepRead(t *testing.T) {
	p := newScriptedProvider(scriptedBoard)
	m := New(p, Options{Sweep: true})
	if m.view != viewSweep {
		t.Fatalf("view = %v, want the sweep", m.view)
	}
	if !m.sweep.loading || m.sweep.preview != nil {
		t.Fatalf("before Dump on a live store the read is pending: loading=%v preview=%v",
			m.sweep.loading, m.sweep.preview != nil)
	}
	out, err := m.Dump(240, 40, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if m.sweep.loading || m.sweep.preview == nil {
		t.Errorf("the preview read was not settled: loading=%v preview=%v", m.sweep.loading, m.sweep.preview != nil)
	}
	if strings.Contains(out, "reading") {
		t.Errorf("the frame still says the previews are being read:\n%s", out)
	}
}
