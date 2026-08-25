package ui

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// DebugLog is the -debuglog recorder: one JSONL line per event, in five
// layers — input (raw key/mouse), mode (mode/view transitions), apply (what a
// gesture enqueued against the store), persist (how each write, exec and
// re-read landed) and status (every note/fail the status line showed).
// Together they answer "I did X and the board showed Y" from the file alone,
// which is the whole point: a bug report attaches the file instead of a
// hand-retyped repro.
//
// What the file contains, stated bluntly because it is meant to be handed to
// someone: EVERY keystroke the terminal delivered — titles and filter text
// typed into modals included — plus positions, queue labels and status-line
// prose. What never enters is a task body: bodies are edited in $EDITOR,
// outside the event loop, and the apply/persist labels name their task by id.
//
// The nil *DebugLog is the disabled recorder — every method no-ops — so call
// sites never branch on whether -debuglog was set.
type DebugLog struct {
	mu sync.Mutex
	w  io.Writer
}

// NewDebugLog wraps an already-open sink and immediately writes the
// session/start marker, so it is the file's first line by construction — the
// flag's contract is append-open, and on a live store the load execs fire
// BEFORE the ui exists; a marker emitted any later would land those execs on
// the previous session's side of the delimiter (found in review).
//
// Opening the file is the caller's job — this package writes frames and
// events, never the filesystem.
func NewDebugLog(w io.Writer) *DebugLog {
	d := &DebugLog{w: w}
	d.event("session", "start", nil)
	return d
}

// event writes one JSON object per line. json.Marshal sorts map keys, so the
// envelope (kind/layer/t) interleaves alphabetically with the fields. A
// marshal or write failure is swallowed — a debug line must never take the
// session down; the worst case is a shorter log.
func (d *DebugLog) event(layer, kind string, fields map[string]any) {
	if d == nil {
		return
	}
	// The stamp is taken under the lock: events from the exec goroutine and
	// the UI thread must land in the file in the same order as their `t`s.
	d.mu.Lock()
	defer d.mu.Unlock()
	ev := map[string]any{
		"t":     time.Now().Format(time.RFC3339Nano),
		"layer": layer,
		"kind":  kind,
	}
	for k, v := range fields {
		// The envelope wins a collision — a fields key silently rewriting
		// `layer` would corrupt every reader's first grouping.
		if k == "t" || k == "layer" || k == "kind" {
			continue
		}
		ev[k] = v
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_, _ = d.w.Write(append(b, '\n'))
}

// Exec records one furrow exec — op and wall time, the same pair -perflog
// persists as TSV. Public because it is called from the store's perf hook,
// which runs on the write goroutine (hence the mutex on event).
func (d *DebugLog) Exec(op string, dur time.Duration) {
	d.event("persist", "exec", map[string]any{"op": op, "ms": dur.Milliseconds()})
}

// dbgInput records the input layer: the raw events the terminal delivered,
// before any dispatch. Internal ticks (filter debounce, drag autoscroll) are
// deliberately absent — they are consequences, and the apply/persist layers
// record the consequences that matter.
func (m *Model) dbgInput(msg tea.Msg) {
	if m.dbg == nil {
		return
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m.dbg.event("input", "key", map[string]any{"key": msg.String()})
	case tea.MouseClickMsg:
		m.dbg.event("input", "click", map[string]any{"x": msg.X, "y": msg.Y, "button": msg.Button.String()})
	case tea.MouseMotionMsg:
		m.dbg.event("input", "motion", map[string]any{"x": msg.X, "y": msg.Y, "button": msg.Button.String()})
	case tea.MouseReleaseMsg:
		m.dbg.event("input", "release", map[string]any{"x": msg.X, "y": msg.Y, "button": msg.Button.String()})
	case tea.MouseWheelMsg:
		m.dbg.event("input", "wheel", map[string]any{"x": msg.X, "y": msg.Y, "button": msg.Button.String()})
	case tea.WindowSizeMsg:
		m.dbg.event("input", "resize", map[string]any{"w": msg.Width, "h": msg.Height})
	}
}

// dbgTransitions diffs mode and view across one Update. Recording the DIFF at
// the single funnel, rather than instrumenting every `m.mode =` assignment,
// keeps a new mode's transitions from shipping unlogged — though its NAME
// still needs a String arm; an unmapped constant logs as "unknown".
func (m *Model) dbgTransitions(preMode mode, preView viewKind) {
	if m.dbg == nil {
		return
	}
	if m.mode != preMode {
		m.dbg.event("mode", "mode", map[string]any{"from": preMode.String(), "to": m.mode.String()})
	}
	if m.view != preView {
		m.dbg.event("mode", "view", map[string]any{"from": preView.String(), "to": m.view.String()})
	}
}
