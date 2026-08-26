package ui

import (
	"strings"
	"testing"
)

// The draft display surfaces (t-v4pp). t-dg7k is the fixture's one draft;
// before it existed the peek's and the graph's "draft (no repo)" branches
// were unreachable from any frame, headless or otherwise.

func TestDraftCardCarriesTheMarker(t *testing.T) {
	m := boardModel(t, 240, 50)
	if !strings.Contains(frame(m), "t-dg7k draft") {
		t.Error("a repo-less card must say draft where the repo chip would sit")
	}
}

func TestDraftRowInTheTableNamesItself(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.view = viewTable
	out := frame(m)
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "t-dg7k") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("the draft is not in the table")
	}
	if !strings.Contains(line, "draft") {
		t.Errorf("the repo column must carry the draft marker, got %q", line)
	}
}

func TestDraftPeekSaysNoRepo(t *testing.T) {
	m := boardModel(t, 240, 50)
	if !m.selectID("t-dg7k", false) {
		t.Fatal("t-dg7k is not on the fixture board")
	}
	m.peekOpen = true
	m.syncPeek()
	if !strings.Contains(frame(m), "draft (no repo)") {
		t.Error("the peek must name the draft state, not render an absent repos line")
	}
}

func TestDraftGraphSaysNoRepo(t *testing.T) {
	m := boardModel(t, 240, 50)
	if !m.selectID("t-dg7k", false) {
		t.Fatal("t-dg7k is not on the fixture board")
	}
	m.openGraph()
	if !strings.Contains(frame(m), "draft (no repo)") {
		t.Error("the graph's meta line must name the draft state")
	}
}
