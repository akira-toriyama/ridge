package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

// The repeat surfaces (t-93xr). furrow #331 made a task recur; ridge read
// neither the rule nor the close's series report, so no frame could say a
// task repeats and a close's successor appeared with no announcement.

func TestRepeatGlyphIsOneCell(t *testing.T) {
	if w := lg.Width(glyphRepeat); w != 1 {
		t.Errorf("glyphRepeat measures %d cells; the table's due column and the card's meta line budget one", w)
	}
}

// lineWith is the first frame line naming id, "" when none does.
func lineWith(out, id string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, id) {
			return l
		}
	}
	return ""
}

func TestRepeatCardCarriesTheMarkOnItsMetaLine(t *testing.T) {
	m := boardModel(t, 240, 50)
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"t-9sa6", true},  // weekly
		{"t-ehk7", true},  // monthly
		{"t-jv3j", false}, // dated, no rule
	} {
		// Selecting scrolls the card into the column's window: backlog is
		// deeper than 50 rows show.
		if !m.selectID(tc.id, false) {
			t.Fatalf("%s is not on the fixture board", tc.id)
		}
		line := lineWith(frame(m), tc.id)
		if line == "" {
			t.Fatalf("%s is selected but not in the frame", tc.id)
		}
		if got := strings.Contains(line, glyphRepeat); got != tc.want {
			t.Errorf("%s meta line carries %s = %v, want %v: %q", tc.id, glyphRepeat, got, tc.want, line)
		}
	}
}

func TestRepeatPeekPrintsTheRuleAndTheSeriesStart(t *testing.T) {
	m := boardModel(t, 240, 50)
	if !m.selectID("t-9sa6", false) {
		t.Fatal("t-9sa6 is not on the fixture board")
	}
	m.peekOpen = true
	m.syncPeek()
	out := frame(m)
	since := m.b.Task("t-9sa6").RepeatAnchor.In(board.Zone()).Format("2006-01-02")
	if want := glyphRepeat + " repeats FREQ=WEEKLY (since " + since + ")"; !strings.Contains(out, want) {
		t.Errorf("the peek must print the rule and its series start as %q", want)
	}
	// A task without a rule gets no line at all — not an empty "repeats".
	if !m.selectID("t-jv3j", false) {
		t.Fatal("t-jv3j is not on the fixture board")
	}
	m.syncPeek()
	if strings.Contains(frame(m), "repeats") {
		t.Error("a task with no rule must not print a repeats line")
	}
}

func TestRepeatLineSpeaksFurrowsWords(t *testing.T) {
	due := time.Date(2026, 10, 8, 14, 59, 59, 0, time.UTC)
	day := due.In(board.Zone()).Format("2006-01-02")
	for _, tc := range []struct {
		rep  board.RepeatReport
		want string
	}{
		{board.RepeatReport{Created: "t-succ", Due: due}, "repeat: next due " + day + " (t-succ)"},
		{board.RepeatReport{Created: "t-succ", Due: due, Skipped: 2}, "repeat: next due " + day + " (t-succ) — 2 occurrence(s) skipped"},
		{board.RepeatReport{Completed: true}, "repeat: series complete — no further occurrences"},
		{board.RepeatReport{Completed: true, Skipped: 1}, "repeat: series complete — no further occurrences — 1 occurrence(s) skipped"},
	} {
		if got := repeatLine(&tc.rep); got != tc.want {
			t.Errorf("repeatLine(%+v) = %q, want %q", tc.rep, got, tc.want)
		}
	}
}

// `d` on a recurring task: the store's reply names the successor, and the
// status line must carry it once the write lands — the card itself only
// arrives with the re-read, so this line is the id's first appearance.
func TestDoneAnnouncesTheSuccessorTheStoreReported(t *testing.T) {
	m, p := scriptedModel(t)
	due := time.Date(2026, 10, 8, 14, 59, 59, 0, time.UTC)
	p.repeat = &board.RepeatReport{Created: "t-succ", Due: due}
	m.selectID("b", false)

	cmd := m.onNormalKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if cmd == nil {
		t.Fatal("done must return the persist Cmd")
	}
	if !strings.HasPrefix(m.status, "closed b") {
		t.Fatalf("before the write lands the gesture's own note stands, got %q", m.status)
	}
	m.onPersistDone(cmd().(persistDoneMsg))
	want := "done b · repeat: next due " + due.In(board.Zone()).Format("2006-01-02") + " (t-succ)"
	if m.status != want || m.statusErr {
		t.Errorf("status after the close landed = %q (err=%v), want %q", m.status, m.statusErr, want)
	}
}

// The same close by the other road — a placement into the done lane is
// `furrow set -s done`, which advances the series exactly as `done` does.
func TestMoveIntoDoneAnnouncesTheSuccessorToo(t *testing.T) {
	m, p := scriptedModel(t)
	due := time.Date(2026, 10, 8, 14, 59, 59, 0, time.UTC)
	p.repeat = &board.RepeatReport{Created: "t-succ", Due: due}

	if _, err := m.b.MoveTo("b", "done", 0); err != nil {
		t.Fatal(err)
	}
	m.recompute()
	cmd := m.persistPlacement("b", "done")
	m.onPersistDone(cmd().(persistDoneMsg))
	if want := "move b · repeat: next due " + due.In(board.Zone()).Format("2006-01-02") + " (t-succ)"; m.status != want {
		t.Errorf("status after the move landed = %q, want %q", m.status, want)
	}

	// A placement anywhere else answers no series, and the line must not
	// pretend one: the scripted store reports only for the done lane.
	m.note("before")
	if _, err := m.b.MoveTo("a", "ready", 0); err != nil {
		t.Fatal(err)
	}
	m.recompute()
	cmd = m.persistPlacement("a", "ready")
	m.onPersistDone(cmd().(persistDoneMsg))
	if strings.Contains(m.status, "repeat") {
		t.Errorf("a plain move announced a series: %q", m.status)
	}
}

// A close of a task with no rule keeps the gesture's own note: the store
// answered nil, and "done b · " with nothing after it would be a lie of shape.
func TestDoneWithoutARuleKeepsTheGesturesNote(t *testing.T) {
	m, _ := scriptedModel(t)
	m.selectID("b", false)
	cmd := m.onNormalKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m.onPersistDone(cmd().(persistDoneMsg))
	if !strings.HasPrefix(m.status, "closed b") || strings.Contains(m.status, "repeat") {
		t.Errorf("status = %q, want the gesture's own closed note", m.status)
	}
}

// The two headless frames: the rule on the card and in the peek, and the
// moment after a close landed — closed card without the mark, successor with
// it, the status line naming the successor in furrow's words.
func TestRepeatDemosShowTheRuleAndTheSuccessor(t *testing.T) {
	m := New(memstore.New(), Options{})
	out, err := m.Dump(240, 50, "repeat", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, glyphRepeat+" repeats FREQ=WEEKLY") {
		t.Error("-demo repeat must open the peek on a task carrying a rule")
	}

	m = New(memstore.New(), Options{})
	out, err = m.Dump(240, 50, "repeatdone", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "done t-9sa6 · repeat: next due") || !strings.Contains(out, "(t-next1)") {
		t.Errorf("-demo repeatdone must announce the successor in furrow's words; frame status: %q", m.status)
	}
	succ := lineWith(out, "t-next1")
	if succ == "" || !strings.Contains(succ, glyphRepeat) {
		t.Errorf("the successor's card must carry the rule it inherited: %q", succ)
	}
	closed := m.b.Task("t-9sa6")
	if closed.Status != m.b.DoneLane() || closed.Repeat != "" {
		t.Errorf("the closed subject must sit in the done lane with the rule consumed: %s %q", closed.Status, closed.Repeat)
	}
	if strings.Contains(lineWith(out, "t-9sa6 "), glyphRepeat) {
		t.Error("the closed card must not keep the mark")
	}
}
