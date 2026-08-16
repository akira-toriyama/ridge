package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

// DemoNames is every -demo state, spelled once. The flag's usage string, the
// unknown-name error and the tests all read this slice, because the list was
// duplicated in three places and adding two states updated two of them —
// `ridge -h` then advertised eight of ten (independent review of PR #22).
var DemoNames = []string{"move", "drag", "add", "edit", "editpick", "editinput", "graph", "help", "slice", "sort", "filter", "filterchips", "fail"}

// Options configures a freshly-constructed Model. The zero value is the
// default TUI: dark palette, board view, no filter.
type Options struct {
	Light  bool   // light palette
	Filter string // initial filter query
	Table  bool   // open on the table view
	Peek   bool   // open with the detail side-peek
	Tree   bool   // open with the dep-tree overlay (implies Peek)
	LoadMS int    // real-store load time, for the startup note
}

// New builds the Model the program runs.
func New(p board.Provider, o Options) *Model {
	m := newModel(p)
	if o.Light {
		m.th = newTheme(false)
	}
	if o.Filter != "" {
		m.ti.SetValue(o.Filter)
		// On a live store applyFilter returns the debounce tick that will
		// eventually fetch the verdict; a constructor has no runtime to hand
		// it to, so Init carries it. Dropping it here made -filter a silent
		// no-op against the real store (the fixture answers synchronously,
		// which is why every headless frame hid the bug).
		m.startupFilter = m.applyFilter(o.Filter)
	}
	if o.Table {
		m.view = viewTable
	}
	if o.Peek || o.Tree {
		m.peekOpen = true
		m.treeOpen = o.Tree
	}
	// What the read cost is the one thing the opening frame knows and the
	// screen does not show anywhere else. The keys that used to be tacked on
	// here (`r reload · R sync · ? help`) were a third partial key list.
	//
	// The read-only case says NOTHING, on purpose. newModel has already put
	// "board is read-only … writes will fail until `furrow upgrade`" in the
	// status, and that warning is set exactly once per session — nothing
	// restores it later, so anything written over it is gone for good. An
	// earlier revision of this function overwrote it with "fixture · N tasks",
	// which was worse than losing it: on a live store gated by the schema
	// check, "fixture" is the one word that means nothing you do touches disk
	// (independent review of PR #21, blocker 1).
	switch {
	case !m.b.Writable():
	case p.Live():
		m.note("loaded %d tasks in %dms", len(m.b.Tasks()), o.LoadMS)
	default:
		m.note("fixture · %d tasks", len(m.b.Tasks()))
	}
	return m
}

// Dump renders one frame at w x h — the headless verification surface — and
// returns it, optionally stripped of ANSI so the output is diffable. demo
// puts the model into a transient mid-gesture state first.
func (m *Model) Dump(w, h int, demo string, plain bool) (string, error) {
	m.w, m.h = w, h
	m.help.SetWidth(w)
	m.recompute()
	m.relayout()
	if err := m.demoState(demo); err != nil {
		return "", err
	}
	out := m.View().Content
	if plain {
		out = ansiStrip(out)
	}
	return out, nil
}

// demoState puts the model into a transient state that a single -dump frame
// could not otherwise reach, because it only exists mid-gesture. Without this,
// "does the drop indicator render?" is a question only a human at a terminal
// can answer — and the house rule is that everything is provable headless.
func (m *Model) demoState(kind string) error {
	switch kind {
	case "":
		return nil

	case "help":
		// The `?` overlay: the app's one full key listing.
		m.fullHelp = true

	case "move":
		// Keyboard move mode mid-gesture: the lifted card and its drop target.
		m.curLane = m.b.LaneIndex("backlog")
		m.setPos(1)
		m.enterMove()
		m.dropLane, m.dropIdx = "ready", 1
		m.followDrop()

	case "drag":
		// A mouse drag mid-gesture: the ghost, the drop indicator, the source
		// card's shadow.
		src := m.lay.Col("backlog")
		dst := m.lay.Col("ready")
		if src == nil || dst == nil || len(src.Cards) < 2 {
			return fmt.Errorf("demo drag: the board is too small at this size")
		}
		grab := src.Cards[1]
		m.Update(tea.MouseClickMsg{X: grab.X + 3, Y: grab.Y + 1, Button: tea.MouseLeft})
		m.Update(tea.MouseMotionMsg{X: dst.X + 8, Y: dst.Top + 4, Button: tea.MouseLeft})

	case "slice":
		// Panel open + focused, sliced to the ui label: the inset board, the
		// selected row and the composed verdict all land in one frame.
		m.toggleSlice()
		m.sliceField = sliceLabel
		rows := m.sliceRows()
		for i, r := range rows {
			if r.value == "bbq" {
				m.sliceIdx = i
			}
		}
		if c := m.selectSlice(sliceLabel, "bbq"); c != nil {
			_ = c
		}

	case "add":
		// A filtered board, so the modal PROVES the context inheritance: the
		// filter's label lands in the chips, not silently on the task.
		m.ti.SetValue("label:bbq")
		m.applyFilter("label:bbq")
		m.relayout()
		if c := m.enterAdd(); c != nil {
			_ = c
		}
		m.add.input.SetValue("盤面から起票するタスク")

	case "edit":
		// Open the field-edit overlay on a task with a checklist AND labels
		// and advance straight into the checklist sub-editor — the stage with
		// a cursor, which is the one a still frame can say something about.
		// The menu rows themselves are NOT exercised by this demo; they are
		// covered by unit tests instead.
		if !m.selectID("t-9sa6", false) {
			return fmt.Errorf("demo edit: t-9sa6 is not on the fixture board")
		}
		m.enterEdit()
		if m.edit == nil {
			return fmt.Errorf("demo edit: the edit menu did not open")
		}
		m.edit.menuIdx = int(fieldChecklist)
		m.openField(fieldChecklist, m.b.Task("t-9sa6"))
		m.edit.listIdx = 1

	case "editpick":
		// The 1..5 picker (value / effort). With editinput below, one of the
		// two SUB-EDITOR stages -dump could not reach (t-36yr): both exist
		// only between two keystrokes of a live overlay, so a regression that
		// blanked them could ship unseen. stageMenu is still -dump-less on
		// purpose — due_test frames it directly, and a menu is not a
		// mid-keystroke state.
		if !m.selectID("t-9sa6", false) {
			return fmt.Errorf("demo editpick: t-9sa6 is not on the fixture board")
		}
		m.enterEdit()
		if m.edit == nil {
			return fmt.Errorf("demo editpick: the edit menu did not open")
		}
		m.edit.menuIdx = int(fieldValue)
		if c := m.openField(fieldValue, m.b.Task("t-9sa6")); c != nil {
			_ = c
		}

	case "editinput":
		// The retitle input, focused and pre-seeded with the task's CJK
		// title: one frame proves the prompt, the seeded value (its tail —
		// the cursor sits at the end) and the apply/back keys.
		if !m.selectID("t-9sa6", false) {
			return fmt.Errorf("demo editinput: t-9sa6 is not on the fixture board")
		}
		m.enterEdit()
		if m.edit == nil {
			return fmt.Errorf("demo editinput: the edit menu did not open")
		}
		m.edit.menuIdx = int(fieldTitle)
		if c := m.openField(fieldTitle, m.b.Task("t-9sa6")); c != nil {
			_ = c
		}

	case "graph":
		// Root the graph on a task that actually HAS both directions, so the
		// frame proves the layout rather than a degenerate single node.
		m.curLane = m.b.LaneIndex("backlog")
		for i, t := range m.cols["backlog"] {
			if len(t.Deps) > 0 && len(m.g.Blocks(t.ID)) > 0 {
				m.setPos(i)
				break
			}
		}
		m.openGraph()

	case "sort":
		// The table sorted by due ascending: the ▲ marker in the header, the
		// dated fixture tasks on top, the undated majority below them — the
		// three sort facts one frame can prove.
		m.view = viewTable
		m.setSort(sortDue, true)

	case "filter":
		// The modal filter input with text in it: the ⟨FILTER⟩ badge, the
		// prompt holding the keyboard, and the board already narrowed behind
		// it. Reachable only mid-keystroke otherwise.
		m.mode = modeFilter
		m.ti.SetValue("lane:backlog is:blocked")
		m.ti.Focus()
		_ = m.applyFilter(m.ti.Value())

	case "filterchips":
		// The filter row under maximum load: table view sorted, an epic slice
		// active, and the input holding the keyboard. The fixed-width input
		// used to push the sort readout off the row in exactly this state
		// (t-a54p), and no other demo can produce it — the sort chip needs the
		// table, the slice chip needs a selection, and the input only pads the
		// row while it is focused mid-keystroke.
		m.view = viewTable
		m.setSort(sortUpdated, false)
		m.toggleSlice()
		m.sliceField = sliceEpic
		for i, r := range m.sliceRows() {
			if r.value == "e-fw2m" {
				m.sliceIdx = i
			}
		}
		if c := m.selectSlice(sliceEpic, "e-fw2m"); c != nil {
			_ = c
		}
		m.mode = modeFilter
		m.ti.SetValue("lane:backlog is:blocked")
		m.ti.Focus()
		_ = m.applyFilter(m.ti.Value())

	case "fail":
		// A refused write. The ⚠ styling has its own colour and its own row,
		// and nothing else in the demo set renders an error at all.
		// onPersistDone sets lastPersist BEFORE it branches on the error, so a
		// real refusal always carries the latency readout too. Leaving it
		// empty rendered a frame the app cannot actually be in.
		m.lastPersist = "move t-jv3j 96ms"
		m.fail("t-jv3j: the store refused the write — the board is rolling back")
		m.rollingBack = true

	default:
		return fmt.Errorf("unknown -demo %q (want %s)", kind, strings.Join(DemoNames, "|"))
	}
	m.relayout()
	return nil
}
