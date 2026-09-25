package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/views"
)

// The constructor and headless surface: DemoNames, Options, New (with
// noteLoad, its startup note) and Dump — what internal/cli calls to build a
// Model and to render one frame of it. The rest of the exported surface is
// the tea.Model contract (Init, Update in model.go; View in view.go) and the
// -debuglog recorder (debuglog.go). The -demo harness Dump hands a state
// name to is demo.go, kept out of this file so the API is read without
// wading through it; the two once shared 900 lines and the harness's churn
// (t-xy0c).

// DemoNames is every -demo state, spelled once. The flag's usage string, the
// unknown-name error and the tests all read this slice, because the list was
// duplicated in three places and adding two states updated two of them —
// `ridge -h` then advertised eight of ten.
var DemoNames = []string{"move", "drag", "add", "adddraft", "edit", "editpick", "editinput", "editdeps", "editrefs", "note", "refs", "graph", "graphall", "map", "mapall", "mapfiltered", "help", "slice", "sliceepic", "sort", "filter", "filterchips", "revisit", "epicdeps", "epic", "epiclist", "epicreason", "epicconfirm", "epicshut", "epicdone", "epicreopen", "sliceepicall", "sliceepicclosed", "epicnew", "boxes", "boxesall", "roadmapweek", "roadmapmonth", "swim", "swimopen", "swimrepo", "swimall", "views", "viewsroad", "viewsmany", "sweep", "sweepconfirm", "sweeprestore", "sweepwait", "fail", "unlaned", "repeat", "repeatdone"}

// Options configures a freshly-constructed Model. The zero value is the
// default TUI: dark palette, board view, no filter.
type Options struct {
	Light  bool   // light palette
	Filter string // initial filter query
	Table  bool   // open on the table view
	// Roadmap opens on the roadmap view — a view setting like Table, not a
	// -demo name, so it composes with the demos and with the interactive TUI
	// (`ridge -roadmap` against the real store is the "what expires this
	// week" glance the view exists for). Day zoom; the week/month axes are
	// the roadmapweek/roadmapmonth demos.
	Roadmap bool
	// GraphLR opens the dependency graph with its layers running left to
	// right. It is a view SETTING, not a transient gesture, so it is a flag
	// like Table rather than a -demo name — which also means it composes with
	// every graph demo instead of needing a mirrored copy of each.
	GraphLR bool
	// Revisit opens with the revisit lens on — a view setting like Table,
	// not a -demo name, so `ridge -revisit` is the real-store "what is worth
	// a fresh look" glance and `-dump -revisit` its headless frame.
	Revisit bool
	// Graph / Map / Boxes / Swim / Sweep open on that full-screen view — an
	// opening setting like Roadmap, the flag twin of the S / T / E / W / X
	// keys — so a live dump reaches every view without the -demo harness
	// (which Dump refuses on a live store). The CLI refuses two opening
	// views at once; the graph roots on the opening cursor (the first card
	// of the first lane with work) and stays on the board when there is
	// none.
	Graph, Map, Boxes, Swim, Sweep bool

	Peek   bool // open with the detail side-peek
	Tree   bool // open with the dep-tree overlay (implies Peek)
	LoadMS int  // real-store load time, for the startup note
	// Debug is the -debuglog recorder over an already-open sink (nil = off).
	// The caller opens the file: this package never touches the filesystem.
	Debug *DebugLog

	// The saved-view tabs (viewtabs.go). The caller loads and saves the
	// file — this package sees data and a closure, so fixture sessions
	// (whose frames must not vary with the machine's views.toml, and which
	// must not be able to WRITE it) simply leave both empty. ViewWarnings
	// carries views.Load's clamp reports into the status line.
	Views        []views.View
	SaveViews    func([]views.View) error
	ViewWarnings []string
}

// New builds the Model the program runs.
func New(p board.Provider, o Options) *Model {
	m := newModel(p, o.Debug)
	if o.Light {
		m.th = newTheme(false)
	}
	m.views = o.Views
	m.saveViews = o.SaveViews
	if o.Filter != "" {
		m.ti.SetValue(o.Filter)
		// On a live store applyFilter returns the debounce tick that will
		// eventually fetch the verdict; a constructor has no runtime to hand
		// it to, so Init carries it. Dropping it here made -filter a silent
		// no-op against the real store (the fixture answers synchronously,
		// which is why every headless frame hid the bug).
		m.startupCmd = m.applyFilter(o.Filter)
	}
	if o.Table {
		m.view = viewTable
	}
	// The full-screen openers' start* halves, NOT open*: the note-free
	// twins. An opener's status line would land exactly where the read-only
	// warning below protects itself by writing nothing, and `-readonly
	// -roadmap` lost the warning once that way. The fallback sentence a
	// start* returns (a seed the view has no row for) is dropped too — the
	// load note overwrites the status right after anyway.
	switch {
	case o.Roadmap:
		m.startRoadmap()
	case o.Graph:
		m.startGraph()
	case o.Map:
		m.startMap(m.cursorID())
	case o.Boxes:
		m.startBoxes()
	case o.Swim:
		m.startSwim()
	case o.Sweep:
		// The preview read is a Cmd on a live store, like the verdicts
		// below: Init or Dump runs it.
		m.startupCmd = tea.Batch(m.startupCmd, m.startSweep())
	}
	if o.GraphLR {
		m.graph.orient = orientLeftRight
	}
	if o.Revisit {
		// setRevisit, NOT toggleRevisit: the note-free half, so the read-only
		// warning below survives (`-readonly -revisit` lost it — the same
		// regression -roadmap's startRoadmap comment records). The same Init
		// hand-off as -filter: on a live store the verdict is a Cmd, and only
		// the fixture answers inside the constructor.
		m.startupCmd = tea.Batch(m.startupCmd, m.setRevisit(true))
	}
	if o.Peek || o.Tree {
		m.peekOpen = true
		m.treeOpen = o.Tree
	}
	// The board snapshot, after the flags above shaped it, so the log states
	// its own baseline (-table starts on the table). Not the session marker:
	// that is NewDebugLog's first line, because on a live store the load execs
	// fire before this constructor runs.
	m.dbg.event("session", "board", map[string]any{
		"live": p.Live(), "tasks": len(m.b.Tasks()), "view": m.view.String(),
	})
	// What the read cost is the one thing the opening frame knows and the
	// screen does not show anywhere else. No key hints: they would be a
	// third partial key list.
	//
	// The read-only case says NOTHING, on purpose. newModel has already put
	// "board is read-only … writes will fail until `furrow upgrade`" in the
	// status, and that warning is set exactly once per session — nothing
	// restores it later, so anything written over it is gone for good — and
	// "fixture · N tasks" over it would be worse than losing it: on a live store
	// gated by the schema check, "fixture" is the one word that means nothing
	// you do touches disk.
	m.noteLoad(p.Live(), o.LoadMS)
	// A clamped views.toml is actionable and rare, so it outranks the load
	// note above — but never the read-only warning, which is set exactly
	// once per session and restored by nothing (the Writable guard is that
	// warning's, not this one's).
	if len(o.ViewWarnings) > 0 && m.b.Writable() {
		m.fail("views.toml: %s", strings.Join(o.ViewWarnings, " · "))
	}
	return m
}

// noteLoad is the startup note. The count is Tasks(), and a task whose
// status names no lane is in it while every lane-driven surface draws it
// nowhere — so the gap is named, or "loaded N" is a truthful line over an
// untruthful board. Shared with the unlaned demo, which is the only headless
// way to see the clause: the fixture has no such task.
func (m *Model) noteLoad(live bool, loadMS int) {
	unlaned := ""
	if n := len(m.b.Unlaned()); n > 0 {
		unlaned = fmt.Sprintf(" · %d in no lane (status outside the board's lanes)", n)
	}
	switch {
	case !m.b.Writable():
	case live:
		m.note("loaded %d tasks in %dms%s", len(m.b.Tasks()), loadMS, unlaned)
	default:
		m.note("fixture · %d tasks%s", len(m.b.Tasks()), unlaned)
	}
}

// Dump renders one frame at w x h — the headless verification surface — and
// returns it, optionally stripped of ANSI so the output is diffable. demo
// puts the model into a transient mid-gesture state first; it is the
// fixture's instrument and is refused on a live store.
func (m *Model) Dump(w, h int, demo string, plain bool) (string, error) {
	if demo != "" && m.prov.Live() {
		// The harness's states are shaped around the fixture's synchronous
		// answers (the revisit demo insists on one; the sweep demos drop the
		// Cmd a live read is), and sweeprestore archives two tasks through
		// the provider — a WRITE on a real board. A live frame is the board
		// at rest, reached through the opening-view Options.
		return "", fmt.Errorf("-demo %s freezes a fixture state (the harness bypasses the persist queue; sweeprestore writes) — a live store is dumped at rest, through the view flags", demo)
	}
	m.w, m.h = w, h
	// -cols/-rows ARE the terminal here: geometry gated on a real size (the
	// roadmap's opening window) must not wait for a WindowSizeMsg that will
	// never come.
	m.sized = true
	m.help.SetWidth(w)
	m.recompute()
	m.relayout()
	// What the fixture answered inside New, a live store answers as Cmds
	// (the -filter / -revisit verdicts, the sweep's preview read). There is
	// no program loop here to run them, so Dump is the loop — without this
	// a live `-filter` frame showed the query over the unfiltered board.
	m.settle(m.startupCmd)
	m.startupCmd = nil
	if err := m.demoState(demo); err != nil {
		return "", err
	}
	out := m.View().Content
	if plain {
		out = ansiStrip(out)
	}
	return out, nil
}

// settle runs cmd to completion synchronously: its message goes to Update,
// a batch is unwrapped, and whatever Update returns is run in turn — a
// stand-in for the program loop where none runs (Dump on a live store, the
// tests). A debounce tick is waited out rather than skipped, so the path
// exercised is the program's own.
func (m *Model) settle(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m.settle(c)
		}
		return
	}
	_, next := m.Update(msg)
	m.settle(next)
}

// cursorID is the selected task's id, "" in an empty column — the seed the
// map and the roadmap open on, and what a table re-sort keeps.
func (m *Model) cursorID() string {
	if t := m.curTask(); t != nil {
		return t.ID
	}
	return ""
}
