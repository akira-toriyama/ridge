package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/views"
)

// DemoNames is every -demo state, spelled once. The flag's usage string, the
// unknown-name error and the tests all read this slice, because the list was
// duplicated in three places and adding two states updated two of them —
// `ridge -h` then advertised eight of ten.
var DemoNames = []string{"move", "drag", "add", "adddraft", "edit", "editpick", "editinput", "editdeps", "editrefs", "note", "refs", "graph", "graphall", "map", "mapall", "mapfiltered", "help", "slice", "sliceepic", "sort", "filter", "filterchips", "revisit", "epicdeps", "epic", "epiclist", "epicreason", "epicconfirm", "epicshut", "epicdone", "epicreopen", "sliceepicall", "epicnew", "boxes", "boxesall", "roadmapweek", "roadmapmonth", "swim", "swimopen", "swimrepo", "swimall", "views", "viewsroad", "viewsmany", "fail"}

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
	Peek    bool // open with the detail side-peek
	Tree    bool // open with the dep-tree overlay (implies Peek)
	LoadMS  int  // real-store load time, for the startup note
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
		m.startupFilter = m.applyFilter(o.Filter)
	}
	if o.Table {
		m.view = viewTable
	}
	if o.Roadmap {
		// startRoadmap, NOT openRoadmap: the note-free half. openRoadmap's
		// status line would land exactly where the read-only warning below
		// protects itself by writing nothing, and `-readonly -roadmap` would
		// lose the warning.
		m.startRoadmap()
	}
	if o.GraphLR {
		m.graphOrient = orientLeftRight
	}
	if o.Revisit {
		// setRevisit, NOT toggleRevisit: the note-free half, so the read-only
		// warning below survives (`-readonly -revisit` lost it — the same
		// regression -roadmap's startRoadmap comment records). The same Init
		// hand-off as -filter: on a live store the verdict is a Cmd, and only
		// the fixture answers inside the constructor.
		m.startupFilter = tea.Batch(m.startupFilter, m.setRevisit(true))
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
	switch {
	case !m.b.Writable():
	case p.Live():
		m.note("loaded %d tasks in %dms", len(m.b.Tasks()), o.LoadMS)
	default:
		m.note("fixture · %d tasks", len(m.b.Tasks()))
	}
	// A clamped views.toml is actionable and rare, so it outranks the load
	// note above — but never the read-only warning, which is set exactly
	// once per session and restored by nothing (the Writable guard is that
	// warning's, not this one's).
	if len(o.ViewWarnings) > 0 && m.b.Writable() {
		m.fail("views.toml: %s", strings.Join(o.ViewWarnings, " · "))
	}
	return m
}

// Dump renders one frame at w x h — the headless verification surface — and
// returns it, optionally stripped of ANSI so the output is diffable. demo
// puts the model into a transient mid-gesture state first.
func (m *Model) Dump(w, h int, demo string, plain bool) (string, error) {
	m.w, m.h = w, h
	// -cols/-rows ARE the terminal here: geometry gated on a real size (the
	// roadmap's opening window) must not wait for a WindowSizeMsg that will
	// never come.
	m.sized = true
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
		// Panel open + focused, sliced to the bbq label: the inset board, the
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
		// filter's label lands in the chips, not silently on the task. The
		// typed line carries the inline tokens (t-69v9) plus one bad one, so
		// this single frame also proves the live echo AND the warning row.
		m.ti.SetValue("label:bbq")
		m.applyFilter("label:bbq")
		m.relayout()
		if c := m.enterAdd(); c != nil {
			_ = c
		}
		// Short enough for the 56-cell input window (cursor included): the
		// frame must show the typed TITLE too, not just the scrolled-to
		// tail. Quoted values are unit-tested; the frame's job is the echo.
		m.add.input.SetValue("盤面起票 value:4 due:+1d dep:t-jv3j check:再現 effort:高")

	case "adddraft":
		// The draft half of quick add (t-v4pp): the board narrowed to
		// is:draft — the fixture's one draft card with its dim marker — and
		// the modal opened UNDER that filter, so a single frame proves the
		// filter passthrough, the card marker and the inheritance: the chips
		// say "draft (no repo)" without the user typing a token, because a
		// plain add under a draft view would be born repo-attached and vanish
		// from the very view it was added into.
		m.ti.SetValue("is:draft")
		m.applyFilter("is:draft")
		m.relayout()
		if c := m.enterAdd(); c != nil {
			_ = c
		}
		m.add.input.SetValue("思いつきを控える")

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

	case "graphall":
		// The DEEPEST ego graph the fixture has, at radius all: six ranks, the
		// shape where the two orientations actually diverge. `graph` roots at
		// the default radius 2 and fits either way, so it proves the happy path
		// and nothing about the axis the frame has to negotiate.
		if err := m.demoState("graph"); err != nil {
			return err
		}
		m.graphRadius = graphAllRadius
		m.graphScroll = 0

	case "map":
		// The dependency map at its DEFAULT scope: done tasks dropped, so the
		// fixture's one 19-node tangle breaks into the three live clusters
		// that are actually in the way. Seeded on a blocked task, so the frame
		// also proves the selection gutter and the strip below it.
		m.openMap("t-jv3j")

	case "mapall":
		// The same board at scope=all: one 19-node cluster, depth 5, which is
		// the frame that proves the indent ladder and the "+N" blocker tag
		// (t-t38k has three blockers). Also the only demo where a panel is
		// taller than one column's share of the canvas, so it proves the pack
		// does not silently drop the overflow.
		m.mapScope = board.ClusterAll
		m.openMap("t-t38k")

	case "mapfiltered":
		// The map UNDER a board filter. The map deliberately shows what the
		// filter hides — an edge that vanishes because of a query is a lie
		// about the board — so this frame is the proof that such rows are
		// muted and COUNTED rather than dropped.
		// is:blocked is the filter that makes the point: the board narrows to
		// the tasks that are stuck, and the map still draws the ROOTS that are
		// doing the blocking — muted and counted, because a cluster missing
		// the task at the top of it explains nothing.
		m.ti.SetValue("is:blocked")
		m.applyFilter("is:blocked")
		m.relayout()
		m.openMap("t-ehk7")

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
		// active, and the input holding the keyboard: the state in which a
		// fixed-width input pushes the sort readout off the row (t-a54p), and
		// no other demo can produce it — the sort chip needs the table, the
		// slice chip needs a selection, and the input only pads the row while
		// it is focused mid-keystroke.
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

	case "editdeps":
		// The deps sub-editor on a task whose two deps resolve differently —
		// t-jv3j waits on an open task and a done one, so one frame proves
		// both state glyphs, the resolved titles and the remove/add keys.
		if !m.selectID("t-jv3j", false) {
			return fmt.Errorf("demo editdeps: t-jv3j is not on the fixture board")
		}
		m.enterEdit()
		if m.edit == nil {
			return fmt.Errorf("demo editdeps: the edit menu did not open")
		}
		m.edit.menuIdx = int(fieldDeps)
		m.openField(fieldDeps, m.b.Task("t-jv3j"))

	case "editrefs":
		// The refs sub-editor on the task whose two refs are the two forms
		// furrow documents — a file:line and a URL — so one frame proves the
		// rows, the cursor and the remove/add keys.
		if !m.selectID("t-9sa6", false) {
			return fmt.Errorf("demo editrefs: t-9sa6 is not on the fixture board")
		}
		m.enterEdit()
		if m.edit == nil {
			return fmt.Errorf("demo editrefs: the edit menu did not open")
		}
		m.edit.menuIdx = int(fieldRefs)
		m.openField(fieldRefs, m.b.Task("t-9sa6"))

	case "revisit":
		// The revisit lens with the peek on a flagged task: the ↻ chip in
		// the filter row, the board narrowed to what furrow revisit flags,
		// and the peek's reason line. t-jv3j carries the dep_done signal
		// (its dep t-t38k is done) on top of the fixture-wide staleness.
		// setRevisit(true), not a toggle: -revisit may already have turned
		// the lens on, and a toggle would cancel it.
		if c := m.setRevisit(true); c != nil {
			return fmt.Errorf("demo revisit: the fixture lens must answer synchronously")
		}
		if !m.selectID("t-jv3j", false) {
			return fmt.Errorf("demo revisit: t-jv3j is not on the fixture board")
		}
		m.peekOpen = true
		m.syncPeek()

	case "note":
		// The note input, focused and holding a typed CJK paragraph — the
		// state between `n` and ⏎ that no bare flag combination can reach.
		if !m.selectID("t-9sa6", false) {
			return fmt.Errorf("demo note: t-9sa6 is not on the fixture board")
		}
		if c := m.enterNote(); c == nil {
			return fmt.Errorf("demo note: the note input did not open")
		}
		m.edit.input.SetValue("重量実測まで完了。次は車載レイアウト案の2案目から。")

	case "refs":
		// The peek's refs section, both documented forms (file:line and URL)
		// in furrow's own order. The default -dump selection has no refs, so
		// no bare flag combination reaches this frame.
		if !m.selectID("t-9sa6", false) {
			return fmt.Errorf("demo refs: t-9sa6 is not on the fixture board")
		}
		m.peekOpen = true
		m.syncPeek()

	case "epicdeps":
		// The peek's epic-dep line, both resolutions at once: t-y4st's box
		// waits on an OPEN box (resolved to id+progress+title) and carries
		// a dep furrow already resolved away (outside open_deps —
		// satisfied). The default -dump selection is an unfiled task, so no
		// bare flag combination can reach this frame.
		if !m.selectID("t-y4st", false) {
			return fmt.Errorf("demo epicdeps: t-y4st is not on the fixture board")
		}
		m.peekOpen = true
		m.syncPeek()

	case "sliceepic":
		// The panel holding the keyboard on the EPIC axis — the state every
		// epic gesture starts from, and the only frame that shows the ▶/◆
		// lifecycle markers and the note advertising m/A. No bare flag
		// combination reaches it: the `slice` demo forces the label axis and
		// `filterchips` hands the keyboard to the filter input.
		m.toggleSlice()
		m.sliceField = sliceEpic
		m.noteSliceAxis()

	case "epic":
		// The overlay's menu on the one fully-populated box, cursor parked on
		// `active` — so the frame proves every row's value AND the activate
		// precondition ("slot held by e-fw2m"), which is what stops furrow's
		// exit 2 from being the user's first news of the one-active-per-repo
		// rule. `-table -demo epic` composes, which is the frame that covers the
		// overlay over the table view — a modal that owns the keyboard must be
		// visible in both, and it was not.
		if err := m.demoEpicPanel("e-c4mt"); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldActive)

	case "epiclist":
		// The deps sub-editor, all three resolutions in one frame: e-c4mt waits
		// on an OPEN box, on one the board holds CLOSED, and on an id no read
		// serves.
		if err := m.demoEpicPanel("e-c4mt"); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldDeps)
		if c := m.openEpicField(epicFieldDeps); c != nil {
			_ = c
		}

	case "epicreason":
		// The activate input. It is the confirm step AND the collection of
		// furrow's --reason, which is appended to the box's body as the
		// activation record — a stage that exists only between two keystrokes.
		if err := m.demoEpicPanel("e-c4mt"); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldActive)
		if c := m.openEpicField(epicFieldActive); c != nil {
			_ = c
		}
		m.epic.input.SetValue("ユーザー依頼: 冬支度を先に回す")

	case "epicconfirm":
		// The deactivate gate, reachable only on the ACTIVE box.
		if err := m.demoEpicPanel("e-fw2m"); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldActive)
		if c := m.openEpicField(epicFieldActive); c != nil {
			_ = c
		}

	case "epicshut":
		// The MENU on a closed box — the only frame where the `closed` row
		// reads its own state back. Without it the row could say "no — open"
		// on a box whose ⏎ reopens, and nothing would catch it.
		m.sliceEpicAll = true
		if err := m.demoEpicPanel("e-2b7h"); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldClosed)

	case "epicdone":
		// The close gate on the ACTIVE box, which is also the one with the
		// most work still under it. furrow closes such a box at exit 0, so
		// this frame is the only warning there is — and closing the active box
		// vacates its repo slot in the same write, which is the other half the
		// gate owes the user.
		if err := m.demoEpicPanel("e-fw2m"); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldClosed)
		if c := m.openEpicField(epicFieldClosed); c != nil {
			_ = c
		}

	case "epicreopen":
		// The same row on the CLOSED box, which is the other verb and the
		// other wording. Reaching it needs the widened scope, which is the
		// point: without it the box `reopen` targets is not on any list.
		m.sliceEpicAll = true
		if err := m.demoEpicPanel("e-2b7h"); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldClosed)
		if c := m.openEpicField(epicFieldClosed); c != nil {
			_ = c
		}

	case "sliceepicall":
		// The epic axis widened to the closed boxes. Driven through the panel's
		// own key handler rather than the field, so the frame also proves `z`
		// is BOUND here — the trap the epicnew demo documents.
		m.toggleSlice()
		m.sliceField = sliceEpic
		m.noteSliceAxis()
		if c := m.onSliceKey(tea.KeyPressMsg{Code: 'z', Text: "z"}); c != nil {
			_ = c
		}
		if !m.sliceEpicAll {
			return fmt.Errorf("demo sliceepicall: z did not widen the epic axis")
		}

	case "epicnew":
		// The new-box modal under a typed repo: filter, so the frame PROVES
		// the inheritance: the filter's repo lands in the chip, not silently
		// on the box. The filter, not a repo slice — `A` only answers on the
		// epic axis and the axis switch clears a repo-axis pick, so a typed
		// repo: is the one form that can still be in force when `A` fires.
		// Without a repo a new box cannot be activated at all, which is why
		// this one is worth a frame of its own.
		m.ti.SetValue("repo:tomo/kyushu-trip")
		m.applyFilter("repo:tomo/kyushu-trip")
		m.relayout()
		m.toggleSlice()
		m.sliceField = sliceEpic
		// Fed through the panel's own key handler, not enterEpicNew directly:
		// this frame is also the proof that `A` is BOUND — a staged call would
		// keep rendering after the binding was deleted.
		if c := m.onSliceKey(tea.KeyPressMsg{Code: 'A', Text: "A"}); c == nil {
			return fmt.Errorf("demo epicnew: A did not open the new-box modal")
		}
		m.epic.input.SetValue("薪ストーブ導入")

	case "boxes":
		// The overview at its default scope, driven through the board's own key
		// handler so the frame also proves `E` is BOUND — the trap the epicnew
		// demo documents.
		if c := m.onNormalKey(tea.KeyPressMsg{Code: 'E', Text: "E"}); c != nil {
			_ = c
		}
		if m.view != viewBoxes {
			return fmt.Errorf("demo boxes: E did not open the box overview")
		}

	case "boxesall":
		// The widened scope, cursor parked on the closed box — the row whose
		// dim styling and done marker have no other frame, and the proof that
		// a closed box keeps its repo group rather than collecting in one.
		m.openBoxes()
		if c := m.onBoxesKey(tea.KeyPressMsg{Code: 'z', Text: "z"}); c != nil {
			_ = c
		}
		l := m.buildBoxes()
		m.boxesLay = l
		if l.Row(boxKey("tomo/kyushu-trip", "e-2b7h")) == nil {
			return fmt.Errorf("demo boxesall: z did not widen the population")
		}
		m.boxesSel = boxKey("tomo/kyushu-trip", "e-2b7h")

	case "swim":
		// The swimlane as `W` opens it: every band folded to its per-lane
		// counts EXCEPT the one holding the board's cursor, which openSwim
		// opens so the view answers "where am I" on entry. Driven through the
		// board's own key handler so the frame also proves `W` is BOUND — the
		// trap the epicnew demo documents.
		if c := m.onNormalKey(tea.KeyPressMsg{Code: 'W', Text: "W"}); c != nil {
			_ = c
		}
		if m.view != viewSwim {
			return fmt.Errorf("demo swim: W did not open the swimlane")
		}

	case "swimopen":
		// One band UNFOLDED — the only state in which the view is a grid, and
		// the frame that proves the cells line up under the counts the header
		// line already printed.
		if err := m.demoState("swim"); err != nil {
			return err
		}
		l := m.buildSwim()
		m.swimLay = l
		if len(l.Bands) == 0 {
			return fmt.Errorf("demo swimopen: the fixture grouped into no bands")
		}
		// The band with the most tasks, so the frame shows ragged columns
		// rather than one row.
		best := 0
		for i, b := range l.Bands {
			if b.Total > l.Bands[best].Total {
				best = i
			}
		}
		m.swimOpen = map[string]bool{l.Bands[best].Key: true}
		m.swimSel = swimKey(l.Bands[best].Key, "")
		m.swimLay = nil

	case "swimrepo":
		// The repo axis. Its bands are the axis with the most lanes actually
		// spanned on the real board, and the one place a task carrying two
		// repos is drawn twice — which the header states as `placements`.
		if err := m.demoState("swim"); err != nil {
			return err
		}
		if c := m.onSwimKey(tea.KeyPressMsg{Code: tea.KeyTab}); c != nil {
			_ = c
		}
		if m.swimAxis != sliceRepo {
			return fmt.Errorf("demo swimrepo: tab did not reach the repo axis, got %s", m.swimAxis)
		}

	case "swimall":
		// Scope widened to the done lane: the one frame where the Done column
		// carries numbers, so `z`'s effect has a render site.
		if err := m.demoState("swim"); err != nil {
			return err
		}
		if c := m.onSwimKey(tea.KeyPressMsg{Code: 'z', Text: "z"}); c != nil {
			_ = c
		}
		if !m.swimAll {
			return fmt.Errorf("demo swimall: z did not widen the scope")
		}

	case "roadmapweek":
		// The week axis: a month compresses to ~4 cells, so this frame proves
		// the sparse labels and ◆s sharing cells they did not share at day
		// zoom. Driven through the real key handlers so it also proves `C`
		// and `z` are BOUND (the trap the epicnew demo documents); the day
		// axis itself needs no demo — it is `-dump -roadmap`.
		if c := m.onNormalKey(tea.KeyPressMsg{Code: 'C', Text: "C"}); c != nil {
			_ = c
		}
		if m.view != viewRoadmap {
			return fmt.Errorf("demo roadmapweek: C did not open the roadmap")
		}
		if c := m.onRoadKey(tea.KeyPressMsg{Code: 'z', Text: "z"}); c != nil {
			_ = c
		}
		if m.roadZoom != zoomWeek {
			return fmt.Errorf("demo roadmapweek: z did not zoom to week")
		}

	case "roadmapmonth":
		// The month axis — the labels' third shape, and the frame where the
		// fixture's every ◆ crowds into a handful of cells.
		if err := m.demoState("roadmapweek"); err != nil {
			return err
		}
		if c := m.onRoadKey(tea.KeyPressMsg{Code: 'z', Text: "z"}); c != nil {
			_ = c
		}
		if m.roadZoom != zoomMonth {
			return fmt.Errorf("demo roadmapmonth: z did not zoom to month")
		}

	case "views":
		// The saved-view tabs (t-es5v): three fixture views with CJK names,
		// tab 3 applied through the real key path — so the frame proves `3`
		// is BOUND (the epicnew trap), the lit tab, the unlit CJK ones and
		// the applied bundle (table view, due ▲) — and then one sort
		// keystroke of drift on top, so the SAME frame proves GH's
		// unsaved-changes dot against the saved bundle.
		m.views = demoViews()
		if c := m.onNormalKey(tea.KeyPressMsg{Code: '3', Text: "3"}); c != nil {
			_ = c
		}
		if m.view != viewTable || m.tableSort != sortDue || !m.tableSortAsc {
			return fmt.Errorf("demo views: 3 did not apply the saved table view")
		}
		if m.viewDirty() {
			return fmt.Errorf("demo views: a freshly applied view is already dirty")
		}
		m.cycleSort() // due asc → due desc: one keystroke of drift
		if !m.viewDirty() {
			return fmt.Errorf("demo views: the sort change did not dirty the view")
		}

	case "viewsroad":
		// A saved view that IS a full-screen view: tab 2 lands on the
		// roadmap, whose own title row must carry the strip (lit tab 2, no
		// dot) — the frame that proves the tabs survive leaving the board's
		// chrome, which is exactly where a hand-kept second strip would rot.
		m.views = demoViews()
		if c := m.onNormalKey(tea.KeyPressMsg{Code: '2', Text: "2"}); c != nil {
			_ = c
		}
		if m.view != viewRoadmap {
			return fmt.Errorf("demo viewsroad: 2 did not open the saved roadmap view")
		}
		if m.viewDirty() {
			return fmt.Errorf("demo viewsroad: a freshly applied view is already dirty")
		}

	case "viewsmany":
		// Nine tabs at their full name budget, active tab LAST: the roadmap
		// title row is the one in-spec surface where the strip must elide at
		// the 240 floor (its six-tab fullTabs prefix eats what the board's
		// Board|Table pair leaves), so this frame proves the +N markers and
		// the never-elided active tab — the state the second review found no
		// demo behind.
		m.views = make([]views.View, 9)
		for i := range m.views {
			m.views[i] = views.View{Name: fmt.Sprintf("保存済みビューの長い名前%d", i+1), Layout: "roadmap"}
		}
		if c := m.onNormalKey(tea.KeyPressMsg{Code: '9', Text: "9"}); c != nil {
			_ = c
		}
		if m.view != viewRoadmap {
			return fmt.Errorf("demo viewsmany: 9 did not open the saved roadmap view")
		}

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

// demoViews is the fixture view set the two demos inject — CJK names on
// purpose: the tab band measures its cells the way every other chrome does,
// and only a CJK name can prove it.
func demoViews() []views.View {
	return []views.View{
		{Name: "火の粉", Layout: "board", Q: "label:bbq"},
		{Name: "締切", Layout: "roadmap"},
		{Name: "表で総覧", Layout: "table", Sort: "due asc"},
	}
}

// demoEpicPanel reaches the epic overlay the way a user does — through the
// panel, on the epic axis, with the cursor on the box — so the frame behind the
// overlay is the real one and `esc` in the resulting state would land back in
// modeSlice rather than on a bare board.
func (m *Model) demoEpicPanel(id string) error {
	m.toggleSlice()
	m.sliceField = sliceEpic
	rows := m.sliceRows()
	found := false
	for i, r := range rows {
		if r.value == id {
			m.sliceIdx, found = i, true
		}
	}
	if !found {
		return fmt.Errorf("demo epic: %s is not a box on the fixture board", id)
	}
	m.enterEpic(id)
	if m.epic == nil {
		return fmt.Errorf("demo epic: the overlay did not open on %s", id)
	}
	return nil
}
