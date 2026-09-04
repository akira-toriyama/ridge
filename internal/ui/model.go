// Package ui is the bubbletea Model and every renderer behind it. It owns
// the optimistic local apply and the strictly-serial persist queue, but no
// business logic — furrow semantics live in internal/board, and the store
// boundary is the board.Provider port. Nothing here execs or touches the
// filesystem: the one write that is not a Provider call — views.toml — rides
// the injected Options.SaveViews closure, nil in fixture sessions.
package ui

import (
	"fmt"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/views"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

type mode int

const (
	modeNormal mode = iota
	modeMove        // a card is lifted; arrows place it
	modeFilter      // the filter input has the keyboard
	modeEdit        // the field-edit overlay has the keyboard (editmode.go)
	modeAdd         // the quick-add modal has the keyboard (addmode.go)
	modeSlice       // the slice panel has the keyboard (slicemode.go)
	modeEpic        // the epic overlay has the keyboard (epicmode.go)
)

func (md mode) String() string {
	switch md {
	case modeNormal:
		return "normal"
	case modeMove:
		return "move"
	case modeFilter:
		return "filter"
	case modeEdit:
		return "edit"
	case modeAdd:
		return "add"
	case modeSlice:
		return "slice"
	case modeEpic:
		return "epic"
	}
	return "unknown"
}

type viewKind int

const (
	viewBoard viewKind = iota
	viewTable
	// viewGraph is the dependency graph — a FULL-SCREEN view, not an overlay.
	// The board's geometry is irrelevant inside it, so it gets the whole
	// terminal instead of a cramped panel floating over the columns.
	viewGraph
	// viewMap is the dependency MAP — also full-screen. The graph is rooted on
	// one task; this one is rooted on nothing and shows every cluster at once.
	viewMap
	// viewBoxes is the BOX OVERVIEW — full-screen, and the only view whose
	// rows are epics rather than tasks. The other three answer questions about
	// work; this one answers "what is each repo working out of".
	viewBoxes
	// viewRoadmap is the ROADMAP — full-screen, the open tasks that carry a
	// due placed on one time axis. The others answer "what and in which
	// order"; this one answers "by when".
	viewRoadmap
	// viewSwim is the SWIMLANE — full-screen, the board's lanes across and a
	// grouping axis down. The board answers "what is in each lane"; this one
	// answers "which of my boxes / repos / labels is work sitting in", which
	// is `furrow ls --tree` given a second dimension.
	viewSwim
)

func (v viewKind) String() string {
	switch v {
	case viewBoard:
		return "board"
	case viewTable:
		return "table"
	case viewGraph:
		return "graph"
	case viewMap:
		return "map"
	case viewBoxes:
		return "boxes"
	case viewRoadmap:
		return "roadmap"
	case viewSwim:
		return "swimlane"
	}
	return "unknown"
}

// Model is the whole application state.
type Model struct {
	prov board.Provider
	b    *board.Board
	g    *board.Graph
	th   *theme
	ms   *measurer // card-height cache, kept across frames (see layout.go)
	keys keyMap
	help help.Model
	ti   textinput.Model
	vp   viewport.Model

	w, h     int
	mode     mode
	view     viewKind
	fullHelp bool
	peekOpen bool
	treeOpen bool
	mouseOn  bool

	qRaw     string          // the active -q expression ("" = no filter)
	qMatched map[string]bool // the store's verdict; nil = no verdict yet
	qErr     string          // furrow's refusal for the current text, "" when clean
	qSeq     int             // debounce + staleness fence (filter.go)
	// The revisit lens (filter.go): while on, the verdict comes from `furrow
	// revisit -q` instead of `ls -q`, so the board shows only the flagged
	// tasks and revisitWhy carries furrow's reasons for the peek. Not part
	// of a saved view — it is a question about the board, not a view of it.
	revisitOn  bool
	revisitWhy map[string][]board.RevisitReason

	// The saved-view tabs (viewtabs.go). viewIdx is the active tab, -1 until
	// the first switch or save — the session's own flag-shaped state is not a
	// tab. saveViews is the ONE write path out of this package that is not a
	// board.Provider call: an injected closure over views.toml, nil in
	// fixture sessions, so the package still never touches the filesystem
	// itself.
	views     []views.View
	viewIdx   int
	saveViews func([]views.View) error

	startupFilter tea.Cmd // pending verdict for Options.Filter, fired by Init

	edit *editState // non-nil exactly while mode == modeEdit
	add  *addState  // non-nil exactly while mode == modeAdd
	epic *epicState // non-nil exactly while mode == modeEpic

	selectAfterReload string // id to select once the next re-read lands

	sliceOpen  bool       // the slice panel is visible (board inset left)
	sliceField sliceField // the panel's axis
	sliceVal   string     // selected value; "" = not slicing
	sliceIdx   int        // the panel's cursor row
	sliceOff   int        // the panel's scroll offset (sliceViewport)
	// sliceEpicAll widens the epic axis to the CLOSED boxes as well. Off by
	// default: the real board carries 36 closed boxes against 117 open ones,
	// and a 26-cell panel is a picker, not an archive. It is a view setting,
	// not a slice term — the `-q epic:` the panel emits is unaffected.
	sliceEpicAll bool

	// The box overview's state (boxboard.go). boxesSel is a boxKey rather than
	// an epic id because a box naming two repos is placed under both, and an
	// id alone cannot say which of the two rows the cursor is on. boxesLay is
	// the pack the last frame drew — the key handlers walk it rather than
	// repacking, exactly as the dep map does.
	boxesAll    bool
	boxesSel    string
	boxesScroll int
	boxesLay    *boxLayout

	pinned map[string]bool // ids forced visible despite the filter (jump targets)
	cols   map[string][]*board.Task

	laneOff   int
	curLane   int
	curIdx    map[string]int
	scroll    map[string]int
	jumpStack []string
	tableIdx  int
	// The table's sort axis and direction (table.go). The zero value is
	// sortCanonical — the board's own lane-then-priority order.
	tableSort    sortKey
	tableSortAsc bool

	// keyboard move mode. dropIdx is measured against the destination column
	// AS DISPLAYED, so it needs AdjustDropIndex on commit — same convention as
	// the mouse drag, one arithmetic path for both.
	moveID   string
	moveFrom string
	dropLane string
	dropIdx  int
	// The cursor as it was when the card was lifted, so esc can restore the
	// SELECTION and not just the board.
	moveCurLane int
	moveCurIdx  map[string]int

	drag dragState

	// A filter verdict that landed while a keyboard move was aiming: applied
	// on the move's exit so the drop slot cannot be rewritten mid-gesture.
	heldVerdict *filterResultMsg

	// The persist queue (persist.go): optimistic edits already applied to m.b,
	// waiting to be recorded in the store, strictly in order.
	pending     []persistOp
	inflight    bool
	quitting    bool   // quit requested while writes were in flight; leave after the drain
	lastPersist string // "move t-x 92ms" — the title bar's passive latency readout
	// A persist FAILED and the rollback re-read has not landed yet: the board
	// is showing state the store refused. Index- and neighbour-addressed
	// writes computed against it would hit the wrong rows in the store, so
	// every write path refuses gestures until the re-read arrives (t-74y3:
	// the wrong-checklist-item write, and the rollback that any keystroke
	// could preempt).
	rollingBack bool
	// A write has LANDED in a live store and the board has not re-read since.
	// Two things are stale until it does: a store-first write (persistOp.noLocal)
	// has no local half at all, so its effect is invisible; and furrow's derived
	// values (epic progress, close stamps, respaced priorities) lag even for an
	// optimistic one. The refusal path that rolls nothing back therefore has to
	// re-read anyway.
	//
	// Cleared when a re-read actually APPLIES, never at drain end: the drain's
	// own reconcile can be dropped by onReloadDone's in-flight guard (a keypress
	// landing behind it), and clearing early would let the next refusal skip the
	// re-read that the dropped one still owed.
	//
	// Set only through markUnread and cleared only through clearUnread, with
	// storeFirstUnread; a failed ROLLBACK re-read leaves both standing (see
	// onReloadDone).
	unreadLanded bool
	// The same window, narrowed to the STORE-FIRST writes: the overlay that
	// issued one is still showing pre-write values until the re-read lands, so it
	// refuses another gesture. Separate from unreadLanded because that one is set
	// by ordinary optimistic writes too, and refusing an epic gesture with "a box
	// write is in flight" after a card move would name the wrong write.
	storeFirstUnread bool
	// A $EDITOR result that arrived inside the rollback window. Every other
	// refused write is a gesture the user can repeat; this one's payload is
	// hand-typed text whose temp file is already deleted, so it is held and
	// applied when the window closes (t-74y3).
	heldBody *editorDoneMsg

	// The dependency graph view. graphFocus is what the picture is rooted on,
	// graphSel is the node the cursor is on (they start equal and diverge as
	// you walk), and graphStack retraces the re-roots.
	graphFocus  string
	graphSel    string
	graphRadius int
	// graphOrient is which screen axis the layers run along. It is view state
	// with no counterpart on the board, and like graphRadius it is re-made
	// every session — the one display state ridge persists at all is the
	// explicitly saved views.toml (viewtabs.go), and the graph is not part
	// of a saved view.
	graphOrient graphOrient
	// graphScroll is a screen-LINE offset into the composed frame, in both
	// orientations. Which axis those lines run down changes; the unit does not.
	graphScroll int
	graphStack  []string
	graphLay    *egoLayout
	// graphFrom is the view `esc` returns to. The graph is reachable from the
	// board AND from the dep map, and dumping a reader who came from the map
	// back onto the board loses the overview they were reading.
	graphFrom viewKind

	// The dependency map view (depmap.go). mapSel is the row the cursor is on;
	// mapScope decides whether done tasks take part.
	mapScope board.ClusterScope
	mapSel   string
	// mapMoved reports that the USER walked the cursor while in the map.
	// Closing the map carries the cursor back to the board, and without this
	// the fallback row that clampMapSel picked — for any task in no cluster,
	// which is most of the board — was carried back as if it were a choice,
	// silently relocating the board cursor on a read-only round trip.
	mapMoved  bool
	mapScroll int
	mapLay    *mapLayout

	// The roadmap view (roadmap.go). roadSel is the row the cursor is on;
	// roadZoom is the axis unit; roadXOff is the window's pan over the axis,
	// in cells; roadMoved carries the walk back to the board on close, the
	// way mapMoved does and for the same reason. roadAnchored reports that
	// the opening window has been PLACED — render only places it on a frame
	// whose size is real (m.sized), because the interactive program draws
	// one frame before the terminal reports a size, and a window placed
	// against the constructor's default width put today off screen on every
	// other terminal (found by review, twice: first against the flag path,
	// then against the pre-size frame).
	roadZoom     roadZoom
	roadSel      string
	roadMoved    bool
	roadAnchored bool
	roadScroll   int
	roadXOff     int
	roadLay      *roadLayout

	// The swimlane view (swimlane.go). swimAxis is the grouping axis and is
	// deliberately SEPARATE from sliceField: that one carries the active
	// filter, and re-grouping a read-only view must not rewrite the query.
	// swimOpen is the unfolded set — the OPEN side is stored because bands
	// are folded by default, so a 57-band board carries an empty map rather
	// than 57 entries. swimLane is the cursor's desired COLUMN, carried
	// alongside the key because a band header spans every column and so
	// cannot say which one a vertical walk was descending. Like the graph's
	// radius and the roadmap's zoom, none of it survives the session.
	swimAxis   sliceField
	swimAll    bool
	swimOpen   map[string]bool
	swimSel    string
	swimLane   int
	swimMoved  bool
	swimScroll int
	swimLay    *swimLayout

	// sized reports that the terminal has told us who it is: a WindowSizeMsg
	// landed, or -dump set the size by hand. Until then w/h are newModel's
	// defaults, and geometry that must not survive them (the roadmap's
	// opening window) waits for it.
	sized bool

	lay       *layout
	status    string
	statusErr bool

	// The -debuglog recorder (debuglog.go). nil = off; every emit site calls
	// through anyway, because the nil *DebugLog is the disabled recorder.
	dbg *DebugLog
}

// newModel takes the recorder up front, not via a setter: the constructor
// itself emits status (the read-only warning below), and a recorder attached
// after the fact missed it — the one status set exactly once per session, so
// a -readonly -debuglog file could not explain its own status line (found by
// review).
func newModel(p board.Provider, dbg *DebugLog) *Model {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.SetWidth(48)
	vp := viewport.New()
	vp.MouseWheelEnabled = true

	m := &Model{
		prov:      p,
		dbg:       dbg,
		th:        newTheme(true),
		ms:        newMeasurer(nil, nil),
		keys:      defaultKeys(),
		help:      help.New(),
		ti:        ti,
		vp:        vp,
		w:         240,
		h:         60,
		mouseOn:   true,
		tableSort: sortCanonical, // sortKey's zero value is sortNone (fail-safe)
		// The swimlane opens grouped by BOX, not by sliceField's zero value:
		// the parity audit filed this view as `furrow ls --tree`'s analogue,
		// and that command groups by epic. tab reaches the other two.
		swimAxis:    sliceEpic,
		swimOpen:    map[string]bool{},
		viewIdx:     -1, // no saved view is active until one is chosen
		pinned:      map[string]bool{},
		curIdx:      map[string]int{},
		scroll:      map[string]int{},
		graphRadius: 2,
	}
	m.reload()
	// Start on the first lane that actually has work.
	for i, l := range m.b.Lanes() {
		if len(m.cols[l.Name]) > 0 {
			m.curLane = i
			break
		}
	}
	m.recompute()
	if !m.b.Writable() {
		m.fail("board is read-only (%s) — writes will fail until `furrow upgrade`", m.b.SchemaState())
	}
	return m
}

// Init requests the terminal background so the palette can pick light or dark —
// lipgloss v2 removed AdaptiveColor, so this is now the idiomatic route.
func (m *Model) Init() tea.Cmd {
	if m.startupFilter != nil {
		return tea.Batch(tea.RequestBackgroundColor, m.startupFilter)
	}
	return tea.RequestBackgroundColor
}

// reload swaps in the provider's current board, keeping the selection on the
// same task when it still exists — an async reconcile must not teleport the
// cursor.
func (m *Model) reload() {
	var cur string
	if m.b != nil {
		if t := m.curTask(); t != nil {
			cur = t.ID
		}
	}
	m.b = m.prov.Board()
	m.g = board.NewGraph(m.b)
	m.recompute()
	if cur != "" {
		m.selectID(cur, false)
	}
}

// recompute rebuilds the derived graph and the filtered columns, then clamps
// every cursor — and cancels a drag whose card left the lane it was grabbed
// in (dropDragIfCardLeftLane), the least obvious of its jobs. Called after
// any mutation: with 34 tasks it is free, and it removes a whole class of
// stale-index bugs.
func (m *Model) recompute() {
	m.g = board.NewGraph(m.b)
	m.ms.rebind(m.g, m.th)
	m.cols = map[string][]*board.Task{}
	for _, l := range m.b.Lanes() {
		var keep []*board.Task
		for _, t := range m.b.LaneTasks(l.Name) {
			if m.taskVisible(t) {
				keep = append(keep, t)
			}
		}
		m.cols[l.Name] = keep
	}
	for name, idx := range m.curIdx {
		m.curIdx[name] = clamp(idx, 0, maxInt(0, len(m.cols[name])-1))
	}
	m.curLane = clamp(m.curLane, 0, len(m.b.Lanes())-1)
	m.tableIdx = clamp(m.tableIdx, 0, maxInt(0, len(m.tableRows())-1))
	// The slice cursor rides the vocab, which a reload can shrink — an
	// unclamped cursor pushes the panel window past the end and it renders
	// zero rows under a "↑ N more" line.
	m.sliceIdx = clamp(m.sliceIdx, 0, maxInt(0, len(m.sliceRows())-1))
	// Same shrink, epic overlay's list cursor: a removed label/repo leaves the
	// rows when no task carries it, and a removed dep/meta row IS the row.
	// Clamped here — where the re-read lands — and nowhere else: clamping at
	// the gesture moved the cursor even when the write was refused, and the
	// labels/repos arms had no clamp at all, so removing their last row left
	// ⏎/x silently dead on a cursor past the end. Not gated on the list
	// stage: the re-read can land while the overlay is parked in the `a`
	// input, whose esc walks back into the list with the index untouched
	// (found by review) — e.field still names the list the index is for, and
	// epicListRows is empty on the non-list fields, where openEpicField
	// re-zeroes the index anyway.
	if m.epic != nil && !m.epic.creating {
		if box := m.b.Epic(m.epic.id); box != nil {
			m.epic.listIdx = clamp(m.epic.listIdx, 0, maxInt(0, len(m.epicListRows(box))-1))
		}
	}
	// The swimlane's pack is rebuilt from the board, so a re-read invalidates
	// it. Dropped here rather than clamped: the key handlers walk m.swimLay
	// when it is non-nil, and a pack built over the PREVIOUS board would hand
	// them rows the store no longer has. clampSwimSel then moves the cursor on
	// the next frame if its row went away.
	m.swimLay = nil
	m.dropDragIfCardLeftLane()
	m.ensureVisible()
	m.syncPeek()
}

func (m *Model) laneName(i int) string { return m.b.Lanes()[i].Name }

func (m *Model) curLaneName() string { return m.laneName(m.curLane) }

func (m *Model) curTasks() []*board.Task { return m.cols[m.curLaneName()] }

func (m *Model) curPos() int {
	return clamp(m.curIdx[m.curLaneName()], 0, maxInt(0, len(m.curTasks())-1))
}

// curTask is the selected task, nil in an empty column.
func (m *Model) curTask() *board.Task {
	if m.view == viewTable {
		rows := m.tableRows()
		if m.tableIdx < len(rows) {
			return rows[m.tableIdx]
		}
		return nil
	}
	ts := m.curTasks()
	if len(ts) == 0 {
		return nil
	}
	return ts[m.curPos()]
}

// ensureVisible scrolls the focused column and pans the lane strip so the
// cursor is on screen.
//
// It is called from the paths that MOVE THE SELECTION, never blanket-called
// from Update. That distinction is load-bearing: run it on every event and it
// re-asserts "the cursor must be visible" immediately after the mouse wheel
// scrolled a column, so the column snaps back and the wheel appears dead. A
// column scrolled away from its selection is a legitimate state — GitHub's
// columns do it too.
func (m *Model) ensureVisible() {
	vis, colW := boardCols(maxInt(m.w, 1), len(m.b.Lanes()))
	if m.curLane < m.laneOff {
		m.laneOff = m.curLane
	}
	if m.curLane >= m.laneOff+vis {
		m.laneOff = m.curLane - vis + 1
	}
	m.laneOff = clamp(m.laneOff, 0, maxInt(0, len(m.b.Lanes())-vis))

	name := m.curLaneName()
	m.scroll[name] = scrollToShow(m.cols[name], m.curPos(), m.scroll[name],
		boardTop, maxInt(m.h, 1)-footerH, colW, m.ms)
}

// Update is the whole event loop.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The debug layers hook HERE, the single funnel, and nowhere deeper:
	// input is recorded before dispatch, mode/view as a diff after it.
	m.dbgInput(msg)
	preMode, preView := m.mode, m.view

	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.sized = true
		m.help.SetWidth(msg.Width)
		// The filter input is deliberately NOT sized here: its width depends
		// on the chips sharing its row, so chromeLayers derives it per frame.
		// Sizing it in two places is how the fixed w-30 remainder shipped —
		// and then starved the sort readout the moment a slice term joined it.
		m.ensureVisible()
		m.syncPeek()

	case tea.BackgroundColorMsg:
		m.th = newTheme(msg.IsDark())
		// The measurer caches card heights per theme; leaving it bound to the
		// old theme splits render geometry from hit-test geometry.
		m.ms.rebind(m.g, m.th)

	case editorDoneMsg:
		switch {
		case msg.err != nil:
			m.fail("editor: %v", msg.err)
		case m.rollingBack:
			// The one write whose payload is hand-typed and already gone
			// from disk (the temp file is removed on editor exit): refusing
			// it would destroy the text, so it waits for the window to
			// close instead (t-74y3).
			held := msg
			m.heldBody = &held
			m.note("%s body held — the store refused the last write; it applies after the rollback", msg.id)
		default:
			if c := m.applyEditorBody(msg); c != nil {
				cmds = append(cmds, c)
			}
		}

	case persistDoneMsg:
		if c := m.onPersistDone(msg); c != nil {
			cmds = append(cmds, c)
		}

	case reloadDoneMsg:
		if c := m.onReloadDone(msg); c != nil {
			cmds = append(cmds, c)
		}

	case filterTickMsg:
		if c := m.onFilterTick(msg); c != nil {
			cmds = append(cmds, c)
		}

	case filterResultMsg:
		m.onFilterResult(msg)

	case tea.KeyPressMsg:
		if c := m.onKey(msg); c != nil {
			cmds = append(cmds, c)
		}

	case tea.MouseClickMsg:
		if c := m.onMouseDown(msg); c != nil {
			cmds = append(cmds, c)
		}
	case tea.MouseMotionMsg:
		if c := m.onMouseMove(msg); c != nil {
			cmds = append(cmds, c)
		}
	case tea.MouseReleaseMsg:
		if c := m.onMouseUp(msg); c != nil {
			cmds = append(cmds, c)
		}
	case tea.MouseWheelMsg:
		m.onWheel(msg)

	case dragScrollMsg:
		if c := m.onDragScroll(msg); c != nil {
			cmds = append(cmds, c)
		}
	}

	m.dbgTransitions(preMode, preView)
	m.relayout()
	return m, tea.Batch(cmds...)
}

// relayout measures the frame. It runs at the end of every Update so a mouse
// event arriving before the next render still hit-tests against current
// geometry.
func (m *Model) relayout() {
	// A terminal is at least one cell. -dump takes its size from the command
	// line, a resize can report 0, and every downstream strings.Repeat would
	// panic on a negative.
	m.w, m.h = maxInt(m.w, 1), maxInt(m.h, 1)
	m.lay = buildLayout(m.w, m.h, m.sliceInset(), m.b.Lanes(), m.cols, m.laneOff, m.scroll, m.ms)
	m.laneOff = m.lay.LaneOff
}

// note/fail are the status funnel, and the debug status layer rides it: the
// board's refusals that never reach the persist queue (a double-press while a
// store-first write is in flight, a local validation) surface ONLY here, and
// without them a log of "I pressed it and nothing happened" shows an
// input/key followed by silence (found in review).
func (m *Model) note(f string, a ...any) {
	m.status, m.statusErr = fmt.Sprintf(f, a...), false
	m.dbg.event("status", "note", map[string]any{"text": m.status})
}

func (m *Model) fail(f string, a ...any) {
	m.status, m.statusErr = fmt.Sprintf(f, a...), true
	m.dbg.event("status", "fail", map[string]any{"text": m.status})
}

func (m *Model) onKey(msg tea.KeyPressMsg) tea.Cmd {
	// A modal text input owns Esc, full stop. Checking cancelDrag() first let a
	// still-armed drag eat the Esc meant to dismiss the filter, leaving the
	// input modal with no way out but Enter.
	if m.mode == modeFilter {
		return m.onFilterKey(msg)
	}
	// The edit overlay is modal the same way the filter input is: it owns the
	// whole keyboard, Esc walks its stages back out.
	if m.mode == modeEdit {
		return m.onEditKey(msg)
	}
	if m.mode == modeAdd {
		return m.onAddKey(msg)
	}
	if m.mode == modeSlice {
		return m.onSliceKey(msg)
	}
	// The epic overlay is modal like the others, and it is reached FROM the
	// slice panel, so it must be routed before it.
	if m.mode == modeEpic {
		return m.onEpicKey(msg)
	}
	// Esc while a mouse button is down cancels the drag before anything else
	// gets to interpret it — and leaves the drag armed so the release that
	// follows is swallowed rather than treated as a drop.
	if key.Matches(msg, m.keys.Cancel) && m.cancelDrag() {
		return nil
	}
	if m.mode == modeMove {
		return m.onMoveKey(msg)
	}
	// The graph is a full-screen MODE, not an overlay: it owns the arrows,
	// enter and esc while it is up, so it is routed before the board keys
	// rather than being a case inside them.
	if m.view == viewGraph {
		return m.onGraphKey(msg)
	}
	// The dep map is a full-screen mode by the same rule.
	if m.view == viewMap {
		return m.onMapKey(msg)
	}
	// The box overview, likewise.
	if m.view == viewBoxes {
		return m.onBoxesKey(msg)
	}
	// The roadmap, likewise.
	if m.view == viewRoadmap {
		return m.onRoadKey(msg)
	}
	// The swimlane, likewise.
	if m.view == viewSwim {
		return m.onSwimKey(msg)
	}
	return m.onNormalKey(msg)
}
