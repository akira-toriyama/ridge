package ui

import (
	"strings"
	"testing"

	lg "charm.land/lipgloss/v2"
)

// A graph node shows a fixed number of title lines. When the title needs more,
// the node has to SAY so: the ellipsis marks the cut. It used to be applied to
// the wrapped LINE, which wrapLines already fits inside the node, so it only
// ever appeared when a line landed on exactly the inner width — which a
// Japanese title, wrapping one cell early on a double-width glyph, almost
// never does. The frame then showed a title that simply stopped.
func TestGraphNodeMarksATruncatedJapaneseTitle(t *testing.T) {
	const w, h = 240, 40
	m := boardModel(t, w, h)
	if err := m.demoState("graph"); err != nil {
		t.Fatalf("demo graph: %v", err)
	}
	frame := ansiStrip(m.View().Content)

	// The focused node is t-jv3j, whose fixture title is 77 cells wide and
	// cannot fit the node's title lines at this size.
	task := m.b.Task("t-jv3j")
	if task == nil {
		t.Fatal("the fixture no longer carries t-jv3j")
	}
	// The bottom strip names the task under the cursor IN FULL, so the node
	// is identified by its own box border, not by the title alone.
	head := string([]rune(task.Title)[:6])
	var found string
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, head) && strings.ContainsAny(line, "┃│") {
			found = line
			break
		}
	}
	if found == "" {
		t.Fatalf("no graph NODE line carries the head of %q", task.Title)
	}
	if strings.Contains(found, task.Title) {
		t.Fatalf("the node now holds the whole %d-cell title, so this test no longer "+
			"exercises the cut — widen the title or narrow the node", lg.Width(task.Title))
	}
	if !strings.Contains(found, "…") {
		t.Errorf("the node cut %q but drew no ellipsis:\n%s", task.Title, found)
	}
}

// capLines is the one spelling of "cap the block and mark the cut", shared by
// the card and the graph node. The mark belongs to the dropped TEXT, so it
// must appear whenever lines were dropped — at every inner width, not only
// where the last kept line happens to measure exactly inner.
func TestCapLinesMarksEveryCut(t *testing.T) {
	titles := []string{
		"行程表 v2 — 阿蘇→高千穂を買い出しと温泉の寄り道込みで引き直す（雨天代替つき）",
		"予約の総ざらい — 温泉・レンタル品・雨天予備日をまとめて確定し、宿とレンタカーの取り消し期限を一覧にする",
		"a plain english title long enough to wrap across several lines at any of these widths",
		"mixed 日本語 and english 混在 title that wraps unevenly depending on the width given",
	}
	for _, title := range titles {
		for inner := 8; inner <= 80; inner++ {
			for _, limit := range []int{1, 2, 3} {
				full := wrapLines(title, inner)
				got := capLines(append([]string(nil), full...), limit, inner)
				cut := len(full) > limit
				marked := len(got) > 0 && strings.HasSuffix(got[len(got)-1], "…")
				if cut && !marked {
					t.Fatalf("inner=%d limit=%d: %d lines dropped with no mark: %q", inner, limit, len(full)-limit, got[len(got)-1])
				}
				if !cut && marked && !strings.HasSuffix(title, "…") {
					t.Fatalf("inner=%d limit=%d: nothing was cut but the block is marked: %q", inner, limit, got[len(got)-1])
				}
				for i, l := range got {
					if lg.Width(l) > inner {
						t.Fatalf("inner=%d limit=%d line %d is %d cells: %q", inner, limit, i, lg.Width(l), l)
					}
				}
			}
		}
	}
}
