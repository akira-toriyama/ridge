package ui

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// DebugLog is the -debuglog recorder: one JSONL line per event, in four
// layers — input (raw key/mouse), mode (mode/view transitions), apply (what a
// gesture enqueued against the store) and persist (how each write, exec and
// re-read landed). Together they answer "I did X and the board showed Y" from
// the file alone, which is the whole point: a bug report attaches the file
// instead of a hand-retyped repro.
//
// The nil *DebugLog is the disabled recorder — every method no-ops — so call
// sites never branch on whether -debuglog was set.
//
// Bodies never enter the log: apply/persist events carry the persist-queue
// label ("body t-x", "move t-y"), which names the task by id. The one
// free-text exception is a quick add's title, which is the gesture's own
// payload — a board title, not a body.
type DebugLog struct {
	mu sync.Mutex
	w  io.Writer
}

// NewDebugLog wraps an already-open sink. Opening the file is the caller's
// job — this package writes frames and events, never the filesystem.
func NewDebugLog(w io.Writer) *DebugLog { return &DebugLog{w: w} }

// event writes one line: {"t":...,"layer":...,"kind":...} plus fields.
// A marshal or write failure is swallowed — a debug line must never take the
// session down; the worst case is a shorter log.
func (d *DebugLog) event(layer, kind string, fields map[string]any) {
	if d == nil {
		return
	}
	ev := map[string]any{
		"t":     time.Now().Format(time.RFC3339Nano),
		"layer": layer,
		"kind":  kind,
	}
	for k, v := range fields {
		ev[k] = v
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
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
// is what keeps a new mode from shipping unlogged.
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
