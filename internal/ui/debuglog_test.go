package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// debugLines parses the recorder's sink back into events, failing the test on
// any line that is not one JSON object — the file's whole value is that jq
// can read it.
func debugLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var evs []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("not JSONL: %q: %v", line, err)
		}
		for _, k := range []string{"t", "layer", "kind"} {
			if _, ok := ev[k]; !ok {
				t.Fatalf("event missing %q: %q", k, line)
			}
		}
		evs = append(evs, ev)
	}
	return evs
}

// kinds flattens events to "layer/kind" for subsequence assertions.
func kinds(evs []map[string]any) []string {
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i] = ev["layer"].(string) + "/" + ev["kind"].(string)
	}
	return out
}

// assertSubsequence checks that want appears in got, in order, gaps allowed —
// the log records more than any one test asserts (ticks, transitions), and
// pinning exact positions would couple the test to that noise.
func assertSubsequence(t *testing.T, got, want []string) {
	t.Helper()
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	if i != len(want) {
		t.Fatalf("log lacks subsequence %v (matched %d)\ngot: %v", want, i, got)
	}
}

// The four layers land from one scripted session: a resize, a move-mode
// round trip, a close, its persist completion, and a mouse click.
func TestDebugLogRecordsAllFourLayers(t *testing.T) {
	var buf bytes.Buffer
	m := New(memstore.New(), Options{Debug: NewDebugLog(&buf)})
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 60})

	m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})           // lift → modeMove
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})            // cancel → modeNormal
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"}) // close → enqueue
	if cmd == nil {
		t.Fatal("d on a selected task returned no persist cmd")
	}
	m.Update(cmd()) // the fixture write lands synchronously

	m.Update(tea.MouseClickMsg{X: 3, Y: 3, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: 3, Y: 3, Button: tea.MouseLeft})

	evs := debugLines(t, &buf)
	assertSubsequence(t, kinds(evs), []string{
		"session/start", // NewDebugLog's own first line
		"session/board", // the model exists and got the recorder
		"input/resize",
		"input/key", "mode/mode", // m → normal→move
		"input/key", "mode/mode", // esc → move→normal
		"input/key", "status/note", // d → "closed t-x" via the status funnel
		"apply/enqueue", // …then the write is queued
		"persist/done",
		"input/click",
		"input/release",
	})

	// The apply label names the task by id — the file must let a reader tie
	// the gesture to its subject without the board in front of them.
	for _, ev := range evs {
		if ev["layer"] == "apply" && ev["kind"] == "enqueue" {
			if !strings.HasPrefix(ev["label"].(string), "done t-") {
				t.Fatalf("apply/enqueue label = %q, want done t-<id>", ev["label"])
			}
			return
		}
	}
	t.Fatal("no apply/enqueue event found")
}

// A refused write records the failure, the dropped tail, and the refusals
// issued while the rollback window is open — the exact sequence a "my edit
// vanished" report needs.
func TestDebugLogRecordsFailureAndRollbackRefusal(t *testing.T) {
	var buf bytes.Buffer
	m := New(memstore.New(), Options{Debug: NewDebugLog(&buf)})
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 60})

	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"}); cmd == nil {
		t.Fatal("d returned no persist cmd")
	}
	// The store's answer, scripted: the queued write failed.
	m.Update(persistDoneMsg{label: "done t-x", ms: 7, err: errors.New("boom")})
	// Any write inside the rollback window must be refused — and recorded.
	m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})

	evs := debugLines(t, &buf)
	assertSubsequence(t, kinds(evs),
		[]string{"apply/enqueue", "persist/fail", "status/fail", "apply/refused"})
	for _, ev := range evs {
		if ev["layer"] == "persist" && ev["kind"] == "fail" && ev["err"] != "boom" {
			t.Fatalf("persist/fail err = %v, want boom", ev["err"])
		}
		if ev["kind"] == "refused" && ev["why"] != "rolling-back" {
			t.Fatalf("apply/refused why = %v, want rolling-back", ev["why"])
		}
	}
}

// Exec is the store-hook entry: the -perflog pair, as a persist/exec event.
// It lands AFTER the session marker even though the load execs fire before
// the ui exists — NewDebugLog writes the marker at construction, which is
// what keeps an appended file's sessions splittable on session/start.
func TestDebugLogExecCarriesOpAndMillis(t *testing.T) {
	var buf bytes.Buffer
	NewDebugLog(&buf).Exec("set", 42*time.Millisecond)
	evs := debugLines(t, &buf)
	if len(evs) != 2 || evs[0]["kind"] != "start" {
		t.Fatalf("got %v, want session/start then persist/exec", evs)
	}
	ex := evs[1]
	if ex["layer"] != "persist" || ex["kind"] != "exec" || ex["op"] != "set" || ex["ms"] != float64(42) {
		t.Fatalf("exec event = %v, want persist/exec op=set ms=42", ex)
	}
}

// The reload family: a landing, a failure, and — the one with no on-screen
// counterpart at all — a snapshot skipped because a write was queued behind
// it. That skip is the ghost a "the board didn't update" report hinges on.
func TestDebugLogRecordsReloadFamily(t *testing.T) {
	var buf bytes.Buffer
	m := New(memstore.New(), Options{Debug: NewDebugLog(&buf)})
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 60})

	m.Update(reloadDoneMsg{label: "synced", ms: 3})
	m.Update(reloadDoneMsg{label: "reloaded", ms: 4, err: errors.New("nope")})
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"}); cmd == nil {
		t.Fatal("d returned no persist cmd")
	}
	m.Update(reloadDoneMsg{label: "late", ms: 5}) // queued write behind it → skip

	assertSubsequence(t, kinds(debugLines(t, &buf)), []string{
		"persist/reload", "persist/reloadfail", "apply/enqueue", "persist/reloadskip",
	})
}

// The whole pipe — terminal bytes → parser → Update → recorder — through a
// real tea.Program, driven by the same synthetic SGR encoding the e2e suite
// uses. This is what proves a bug report's file matches what the terminal
// actually sent, not just what a unit test injected.
func TestDebugLogEndToEndThroughARealProgram(t *testing.T) {
	var buf bytes.Buffer
	m := New(memstore.New(), Options{Debug: NewDebugLog(&buf)})

	var in bytes.Buffer
	in.WriteString(mousePress(5, 5))
	in.WriteString(mouseRelease(5, 5))
	in.WriteString("q")

	var out bytes.Buffer
	if _, err := tea.NewProgram(m,
		tea.WithInput(&in), tea.WithOutput(&out),
		tea.WithoutSignals(), tea.WithWindowSize(240, 60),
	).Run(); err != nil {
		t.Fatalf("program: %v", err)
	}

	assertSubsequence(t, kinds(debugLines(t, &buf)), []string{
		"session/start", "input/click", "input/release", "input/key",
	})
}

// The constructor's own status must reach the log: the read-only warning is
// set exactly once per session and never restored, and a recorder attached
// after newModel missed it — a -readonly -debuglog bug report could not
// explain its own status line (found by review).
func TestDebugLogRecordsTheReadOnlyWarning(t *testing.T) {
	var buf bytes.Buffer
	m := New(memstore.NewGated("board-behind"), Options{Debug: NewDebugLog(&buf)})
	if !strings.Contains(m.status, "read-only") {
		t.Fatalf("status = %q — the gated fixture did not warn", m.status)
	}
	for _, ev := range debugLines(t, &buf) {
		if ev["layer"] == "status" && ev["kind"] == "fail" &&
			strings.Contains(ev["text"].(string), "read-only") {
			return
		}
	}
	t.Error("the read-only warning never reached the status layer")
}

// The nil recorder is the OFF switch: every emit site calls through
// unconditionally, so nil must be safe on both the type and the model.
func TestNilDebugLogIsSafe(_ *testing.T) {
	var d *DebugLog
	d.event("x", "y", nil)
	d.Exec("op", time.Millisecond)
	m := New(memstore.New(), Options{}) // no recorder
	m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
}
