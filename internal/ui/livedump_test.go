package ui

import (
	"strings"
	"testing"
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
