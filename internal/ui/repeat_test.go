package ui

import (
	"errors"
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

// lineWith is the first frame line containing needle, "" when none does. A
// bare id is not a safe needle — the title bar's latency readout and the
// status line name ids too — so callers pass the card's "id repo" pair.
func lineWith(out, needle string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, needle) {
			return l
		}
	}
	return ""
}

// metaLine is the card's meta line: the id followed by its repo chip.
func metaLine(out string, task *board.Task) string {
	return lineWith(out, task.ID+" "+task.ShortRepo())
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
		line := metaLine(frame(m), m.b.Task(tc.id))
		if line == "" {
			t.Fatalf("%s is selected but its meta line is not in the frame", tc.id)
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

// The words are furrow's seriesLine (internal/cli/cmd_mutate.go), byte for
// byte on the spent form; the one divergence is the due, which furrow prints
// as the instant in the board's calendar and ridge as the local day every
// other surface spells dates in.
func TestRepeatLineMirrorsFurrowsSeriesLineExceptTheDay(t *testing.T) {
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

// `d` on a recurring task: the store's reply names the successor, and once the
// write lands the status line must carry it AFTER the gesture's own line —
// "unblocked N task(s)" is said nowhere else, and the write lands ~100ms
// after it was written, so a note that replaced it would erase it unread.
func TestDoneAnnouncesTheSuccessorAfterTheGesturesOwnNote(t *testing.T) {
	m, p := scriptedModel(t)
	due := time.Date(2026, 10, 8, 14, 59, 59, 0, time.UTC)
	p.repeat = &board.RepeatReport{Created: "t-succ", Due: due}
	day := due.In(board.Zone()).Format("2006-01-02")

	// c waits on b, so closing b unblocks one task — the gesture line that
	// must survive the landing.
	m.b.Task("c").Deps = []string{"b"}
	m.recompute()
	m.selectID("b", false)
	cmd := m.onNormalKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if cmd == nil {
		t.Fatal("done must return the persist Cmd")
	}
	if m.status != "closed b — unblocked 1 task(s)" {
		t.Fatalf("before the write lands the gesture's own note stands, got %q", m.status)
	}
	m.onPersistDone(cmd().(persistDoneMsg))
	want := "closed b — unblocked 1 task(s) · repeat: next due " + day + " (t-succ)"
	if m.status != want || m.statusErr {
		t.Errorf("status after the close landed = %q (err=%v), want %q", m.status, m.statusErr, want)
	}
}

// The same close by the other road, through the gesture's own commit path —
// a placement into the done lane is `furrow set -s done`, which advances the
// series exactly as `done` does. With no respace the label leads; with one,
// the respace note does.
func TestMoveIntoDoneAnnouncesTheSuccessorThroughCommitMove(t *testing.T) {
	m, p := scriptedModel(t)
	due := time.Date(2026, 10, 8, 14, 59, 59, 0, time.UTC)
	p.repeat = &board.RepeatReport{Created: "t-succ", Due: due}
	day := due.In(board.Zone()).Format("2006-01-02")

	m.note("before")
	moved, cmd, err := m.commitMove("b", "ready", "done", 0)
	if err != nil || !moved || cmd == nil {
		t.Fatalf("commitMove = %v %v %v", moved, cmd, err)
	}
	if m.status != "before" {
		t.Fatalf("a move with no respace writes no note, got %q", m.status)
	}
	m.onPersistDone(cmd().(persistDoneMsg))
	if want := "move b · repeat: next due " + day + " (t-succ)"; m.status != want {
		t.Errorf("status after the move landed = %q, want %q", m.status, want)
	}

	// Exhaust the gap in the done lane so the next placement respaces: the
	// respace note is the gesture's line, and the series report extends it.
	if _, err := m.b.MoveTo("c", "done", 1); err != nil {
		t.Fatal(err)
	}
	m.b.Task("b").Priority, m.b.Task("c").Priority = 20, 21
	m.recompute()
	moved, cmd, err = m.commitMove("a", "ready", "done", 1)
	if err != nil || !moved || cmd == nil {
		t.Fatalf("commitMove = %v %v %v", moved, cmd, err)
	}
	if !strings.HasPrefix(m.status, "respaced done (") {
		t.Fatalf("the exhausted gap must note the respace before the write, got %q", m.status)
	}
	respace := m.status
	m.onPersistDone(cmd().(persistDoneMsg))
	if want := respace + " · repeat: next due " + day + " (t-succ)"; m.status != want {
		t.Errorf("status after the respacing move landed = %q, want %q", m.status, want)
	}

	// A placement anywhere else answers no series, and the line must not
	// pretend one: the scripted store reports only for the done lane.
	m.note("before")
	moved, cmd, err = m.commitMove("z", "backlog", "ready", 0)
	if err != nil || !moved || cmd == nil {
		t.Fatalf("commitMove = %v %v %v", moved, cmd, err)
	}
	m.onPersistDone(cmd().(persistDoneMsg))
	if strings.Contains(m.status, "repeat") {
		t.Errorf("a plain move announced a series: %q", m.status)
	}
}

// A close of a task with no rule keeps the gesture's own note: the store
// answered nil, and "closed b · " with nothing after it would be a lie of shape.
func TestDoneWithoutARuleKeepsTheGesturesNote(t *testing.T) {
	m, _ := scriptedModel(t)
	m.selectID("b", false)
	cmd := m.onNormalKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	m.onPersistDone(cmd().(persistDoneMsg))
	if m.status != "closed b" {
		t.Errorf("status = %q, want the gesture's own closed note", m.status)
	}
}

// A refused close reports the refusal, never a series — and the rollback
// re-read hands the rule back: the optimistic close consumed it locally, and
// the store, having refused, still holds it.
func TestRefusedCloseReportsTheFailureAndTheRollbackRestoresTheRule(t *testing.T) {
	p := newScriptedProvider(func() *board.Board {
		b := scriptedBoard()
		b.Task("b").Repeat = "FREQ=WEEKLY"
		return b
	})
	m := New(p, Options{})
	m.w, m.h = 140, 40
	m.recompute()
	m.relayout()
	p.repeat = &board.RepeatReport{Created: "t-succ", Due: time.Date(2026, 10, 8, 14, 59, 59, 0, time.UTC)}
	p.doneErr = errors.New("boom")

	m.selectID("b", false)
	cmd := m.onNormalKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if m.b.Task("b").Repeat != "" {
		t.Fatal("the optimistic close must consume the rule before the write")
	}
	rb := m.onPersistDone(cmd().(persistDoneMsg))
	if !m.statusErr || !strings.Contains(m.status, "done b: boom") || strings.Contains(m.status, "repeat") {
		t.Errorf("a refused close must report the refusal alone, got %q (err=%v)", m.status, m.statusErr)
	}
	if rb == nil {
		t.Fatal("a refused optimistic write must return the rollback re-read")
	}
	m.onReloadDone(rb().(reloadDoneMsg))
	if got := m.b.Task("b"); got.Status != "ready" || got.Repeat != "FREQ=WEEKLY" {
		t.Errorf("after the rollback b is %s with repeat %q, want ready with the rule back", got.Status, got.Repeat)
	}
}

// The two headless frames: the rule on the card and in the peek, and the
// moment after a close landed — the successor with the mark and the status
// line naming it in furrow's words; the closed card among the earlier closes
// in the done lane, its priority kept, without the mark.
func TestRepeatDemosShowTheRuleAndTheSuccessor(t *testing.T) {
	m := New(memstore.New(), Options{})
	out, err := m.Dump(240, 50, "repeat", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, glyphRepeat+" repeats FREQ=WEEKLY") {
		t.Error("-demo repeat must open the peek on a task carrying a rule")
	}
	if m.curTask() == nil || m.curTask().ID != "t-9sa6" {
		t.Errorf("-demo repeat's cursor is on %v, want the fixture's first rule t-9sa6", m.curTask())
	}

	m = New(memstore.New(), Options{})
	out, err = m.Dump(240, 50, "repeatdone", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "closed t-9sa6 · repeat: next due") || !strings.Contains(out, "(t-next1)") {
		t.Errorf("-demo repeatdone must announce the successor in furrow's words; frame status: %q", m.status)
	}
	succ := m.b.Task("t-next1")
	if succ == nil {
		t.Fatal("the successor is not on the board")
	}
	if line := metaLine(out, succ); line == "" || !strings.Contains(line, glyphRepeat) {
		t.Errorf("the successor's card must carry the rule it inherited: %q", line)
	}
	closed := m.b.Task("t-9sa6")
	if closed.Status != m.b.DoneLane() || closed.Repeat != "" {
		t.Errorf("the closed subject must sit in the done lane with the rule consumed: %s %q", closed.Status, closed.Repeat)
	}
	// Born the way furrow births one: boxes unchecked, and its own slices —
	// ticking the closed card's box must not tick the successor's.
	for i, c := range succ.Checklist {
		if c.Done {
			t.Errorf("successor checklist item %d is ticked; furrow copies the list unticked", i)
		}
	}
	if len(closed.Checklist) > 0 {
		closed.Checklist[0].Done = true
		if succ.Checklist[0].Done {
			t.Error("the successor's checklist aliases the closed card's")
		}
	}

	// The closed card keeps its priority (`furrow done` renumbers nothing —
	// t-s5tj), so it sits among the earlier closes rather than at the lane's
	// tail, which is what puts it in the frame at 50 rows.
	if before := memstore.New().Board().Task("t-9sa6").Priority; closed.Priority != before {
		t.Errorf("the close renumbered t-9sa6: %d → %d", before, closed.Priority)
	}
	if done := m.b.LaneTasks(m.b.DoneLane()); done[len(done)-1].ID == "t-9sa6" {
		t.Error("the closed card sits at the done lane's tail; a kept priority sorts it among the earlier closes")
	}
	line := metaLine(out, closed)
	if line == "" {
		t.Fatal("at 50 rows the closed card's meta line must be in the frame")
	}
	if strings.Contains(line, glyphRepeat) {
		t.Errorf("the closed card must not keep the mark: %q", line)
	}
}
