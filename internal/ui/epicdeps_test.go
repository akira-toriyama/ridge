package ui

import (
	"strings"
	"testing"
)

// The peek resolves the selected task's epic deps — furrow's "open after
// those close" edge, in `furrow epic dep --list`'s own "waits on" wording.
// t-y4st's box (e-c4mt) declares one dep the board resolves (e-fw2m, open)
// and one it cannot (e-2b7h): the open-only epic read means an unresolvable
// dep IS a dep on a closed epic, rendered as satisfied, never as an error.
func TestPeekResolvesTheEpicsOwnDeps(t *testing.T) {
	m := boardModel(t, 240, 50)
	if !m.selectID("t-y4st", false) {
		t.Fatal("t-y4st is not on the fixture board")
	}
	press(m, "space")
	out := frame(m)

	if !strings.Contains(out, "epic waits on") {
		t.Fatal("the peek must carry the epic's dep line")
	}
	if !strings.Contains(out, "e-fw2m (6/18) 九州") {
		t.Error("an OPEN dep must resolve to id, progress, title — progress first, so a truncated CJK title cannot eat it")
	}
	if !strings.Contains(out, "e-2b7h (closed)") {
		t.Error("a dep absent from the open-epic read is a closed dep — say so")
	}
}

// The line exists only when there is an edge to report: a dep-less epic and
// an unfiled task both stay without it — an empty "waits on" would read as a
// broken lookup, not as an answer.
func TestPeekOmitsTheDepLineWithoutEpicDeps(t *testing.T) {
	for _, id := range []string{"t-kv82", "t-ehk7"} { // e-p3dx member; unfiled
		m := boardModel(t, 240, 50)
		if !m.selectID(id, false) {
			t.Fatalf("%s is not on the fixture board", id)
		}
		press(m, "space")
		if strings.Contains(frame(m), "waits on") {
			t.Errorf("%s: no epic dep to report, so no line", id)
		}
	}
}
