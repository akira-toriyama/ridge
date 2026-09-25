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

func TestLiveDumpSettlesTheStartupVerdict(t *testing.T) {
	p := newScriptedProvider(scriptedBoard)
	p.qIDs = []string{"a"}
	m := New(p, Options{Filter: "lane:ready"})
	if m.startupCmd == nil {
		t.Fatal("a live store must leave the verdict as a Cmd for Init or Dump")
	}
	out, err := m.Dump(240, 40, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.queries) != 1 || p.queries[0] != "lane:ready" {
		t.Fatalf("store reads = %v, want the one startup query", p.queries)
	}
	if n := m.countVisible(); n != 1 {
		t.Errorf("visible = %d, want 1 — the verdict must be applied before the frame", n)
	}
	if m.startupCmd != nil {
		t.Error("the settled Cmd was left for an Init that never comes")
	}
	if !strings.Contains(out, "lane:ready") {
		t.Errorf("the frame does not carry the query in its filter row:\n%s", out)
	}
}

func TestLiveDumpSettlesTheRevisitLens(t *testing.T) {
	p := newScriptedProvider(scriptedBoard)
	p.qIDs = []string{"a"}
	m := New(p, Options{Revisit: true})
	if _, err := m.Dump(240, 40, "", true); err != nil {
		t.Fatal(err)
	}
	if len(p.queries) != 1 || p.queries[0] != "revisit:" {
		t.Fatalf("store reads = %v, want one revisit read with no query", p.queries)
	}
	if m.countVisible() != 1 || m.revisitWhy["a"] == nil {
		t.Errorf("visible = %d, why[a]=%v — the lens must be applied before the frame",
			m.countVisible(), m.revisitWhy["a"])
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
