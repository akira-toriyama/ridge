package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

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
		m.applyFilter(o.Filter)
	}
	if o.Table {
		m.view = viewTable
	}
	if o.Peek || o.Tree {
		m.peekOpen = true
		m.treeOpen = o.Tree
	}
	if p.Live() && m.b.Writable() {
		m.note("loaded %d tasks in %dms · r reload · R sync · ? help",
			len(m.b.Tasks()), o.LoadMS)
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
		m.fullHelp = true

	case "move":
		m.curLane = m.b.LaneIndex("backlog")
		m.setPos(1)
		m.enterMove()
		m.dropLane, m.dropIdx = "ready", 1
		m.followDrop()

	case "drag":
		src := m.lay.Col("backlog")
		dst := m.lay.Col("ready")
		if src == nil || dst == nil || len(src.Cards) < 2 {
			return fmt.Errorf("demo drag: the board is too small at this size")
		}
		grab := src.Cards[1]
		m.Update(tea.MouseClickMsg{X: grab.X + 3, Y: grab.Y + 1, Button: tea.MouseLeft})
		m.Update(tea.MouseMotionMsg{X: dst.X + 8, Y: dst.Top + 4, Button: tea.MouseLeft})

	case "edit":
		// Open the field-edit overlay on a task with a checklist AND labels,
		// so the menu row values and the checklist cursor are all exercised.
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

	default:
		return fmt.Errorf("unknown -demo %q (want move|drag|edit|graph|help)", kind)
	}
	m.relayout()
	return nil
}
