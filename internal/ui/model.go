// Package ui is the bubbletea Model and every renderer behind it. It owns
// the optimistic local apply and the strictly-serial persist queue, but no
// business logic — furrow semantics live in internal/board, and the store
// boundary is the board.Provider port. Nothing here execs or reads files.
package ui

import (
	"fmt"
	"github.com/akira-toriyama/ridge/internal/board"
	"strings"

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
	// every session — ridge writes no state of its own to disk.
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
	// way mapMoved does and for the same reason.
	roadZoom   roadZoom
	roadSel    string
	roadMoved  bool
	roadScroll int
	roadXOff   int
	roadLay    *roadLayout

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
		prov:        p,
		dbg:         dbg,
		th:          newTheme(true),
		ms:          newMeasurer(nil, nil),
		keys:        defaultKeys(),
		help:        help.New(),
		ti:          ti,
		vp:          vp,
		w:           240,
		h:           60,
		mouseOn:     true,
		tableSort:   sortCanonical, // sortKey's zero value is sortNone (fail-safe)
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
	return m.onNormalKey(msg)
}

// onGraphKey is the graph view's whole keyboard surface. Everything the board
// does to the BOARD is deliberately absent — the graph is a reading and walking
// tool, and a stray `d` closing a task you were only looking at would be a
// nasty surprise.
func (m *Model) onGraphKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quitOrFlush()

	case key.Matches(msg, m.keys.Help):
		m.fullHelp = !m.fullHelp

	case key.Matches(msg, m.keys.Cancel):
		if m.fullHelp {
			m.fullHelp = false
			return nil
		}
		m.closeGraph()

	case key.Matches(msg, m.keys.GraphRoot):
		m.rerootGraph()

	case key.Matches(msg, m.keys.JumpBack):
		m.graphBack()

	case key.Matches(msg, m.keys.GraphRadius):
		switch msg.String() {
		// No note: the graph header states the radius on every frame.
		case "1", "2", "3":
			m.graphRadius = int(msg.String()[0] - '0')
		case "0":
			m.graphRadius = graphAllRadius
		default:
			m.cycleGraphRadius()
		}
		m.graphScroll = 0

	case key.Matches(msg, m.keys.GraphOrient):
		// No note, same as the radius: the header names the direction on every
		// frame, in the two words that are half the redundancy contract.
		m.cycleGraphOrient()

	case key.Matches(msg, m.keys.Graph):
		// ⇧space on the node you are already on is a no-op re-root; treat it as
		// "root here", which is what the gesture means on the board.
		m.rerootGraph()

	case key.Matches(msg, m.keys.Map):
		// Zoom out: the ego graph's neighbourhood seen inside every cluster.
		// Seeded with the graph's own selection, not the board cursor, which
		// has not moved since the graph opened.
		m.openMap(m.graphSel)

	case key.Matches(msg, m.keys.PeekScroll):
		if msg.String() == "ctrl+d" {
			m.graphScroll += maxInt(1, m.graphCanvasH()/2)
		} else {
			m.graphScroll -= maxInt(1, m.graphCanvasH()/2)
		}
		m.graphScroll = maxInt(0, m.graphScroll)

	case key.Matches(msg, m.keys.View):
		m.closeGraph()

	case key.Matches(msg, m.keys.Up):
		m.graphMove(0, -1)
	case key.Matches(msg, m.keys.Down):
		m.graphMove(0, +1)
	case key.Matches(msg, m.keys.Left):
		m.graphMove(-1, 0)
	case key.Matches(msg, m.keys.Right):
		m.graphMove(+1, 0)
	}
	return nil
}

func (m *Model) onFilterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.ForceQuit):
		// `q` types into the filter; ctrl+c stays a way out (raw mode hands
		// it to us as an ordinary keystroke — nobody else will quit for us).
		return m.quitOrFlush()
	case key.Matches(msg, m.keys.Cancel):
		m.mode = modeNormal
		m.ti.Blur()
		m.ti.SetValue(m.qRaw) // discard the in-progress edit; the verdict is current
		return nil
	case key.Matches(msg, m.keys.Commit):
		m.mode = modeNormal
		m.ti.Blur()
		return m.applyFilter(m.ti.Value())
	}
	var c tea.Cmd
	m.ti, c = m.ti.Update(msg)
	return tea.Batch(c, m.applyFilter(m.ti.Value())) // live filtering as you type
}

func (m *Model) onNormalKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quitOrFlush()

	case key.Matches(msg, m.keys.Help):
		m.fullHelp = !m.fullHelp

	case key.Matches(msg, m.keys.Cancel):
		switch {
		// The help overlay sits at zHelp, above every other layer, so it is
		// what Esc must take off first. Without this case Esc reached past the
		// overlay the user was looking at and closed the peek UNDER it — the
		// board looked frozen and lost state at the same time. The graph's
		// Cancel has always had this branch; only the board's was missing.
		case m.fullHelp:
			m.fullHelp = false
		case m.treeOpen:
			m.treeOpen = false
		case m.peekOpen:
			m.peekOpen = false
		case m.qRaw != "":
			cmd := m.applyFilter("")
			m.ti.SetValue("")
			m.note("filter cleared")
			return cmd
		case m.sliceVal != "":
			// A slice-only filter must be escapable too — before this case,
			// the only way out was reopening the panel and re-selecting the
			// same row. Radio semantics: re-selecting clears.
			return m.selectSlice(m.sliceField, m.sliceVal)
		}

	case key.Matches(msg, m.keys.Filter):
		// Entering a modal input mid-drag would leave the drag armed and its
		// release would commit a move nobody is looking at any more.
		m.cancelDrag()
		// A modal owns the whole keyboard, so the help overlay must not ride
		// into it: `?` cannot be typed there to close it, and its "you are
		// here" would keep naming the mode that was just left.
		m.fullHelp = false
		m.mode = modeFilter
		m.ti.SetValue(m.qRaw)
		return m.ti.Focus()

	case key.Matches(msg, m.keys.View):
		// No note: the tab strip in the title row already says which view is
		// up, and the whole screen changed.
		// Capture the selection BEFORE switching: curTask() reads whichever
		// view is current, so asking after the switch returned table row 0 —
		// the row just assigned — and selectID then wrote that back over the
		// board cursor, lane included. setSort already does this correctly
		// ("a sort that teleports the cursor ... is a re-ordering of the user").
		t := m.curTask()
		if m.view == viewBoard {
			m.view, m.tableIdx = viewTable, 0
		} else {
			m.view = viewBoard
		}
		if t != nil {
			m.selectID(t.ID, false)
		}

	case key.Matches(msg, m.keys.Sort):
		if m.view != viewTable {
			m.note("sort belongs to the table — v switches views")
			return nil
		}
		m.cycleSort()

	case key.Matches(msg, m.keys.Mouse):
		m.mouseOn = !m.mouseOn
		if m.mouseOn {
			m.note("mouse tracking ON — drag cards; the terminal's own text selection needs the bypass modifier")
		} else {
			m.note("mouse tracking OFF — the terminal owns the mouse again; keyboard still does everything")
		}

	case key.Matches(msg, m.keys.Graph):
		m.openGraph()

	case key.Matches(msg, m.keys.Map):
		id := ""
		if t := m.curTask(); t != nil {
			id = t.ID
		}
		m.openMap(id)

	case key.Matches(msg, m.keys.Boxes):
		m.openBoxes()

	case key.Matches(msg, m.keys.Roadmap):
		m.openRoadmap()

	case key.Matches(msg, m.keys.Peek):
		m.peekOpen = !m.peekOpen
		if !m.peekOpen {
			// The tree renders only INSIDE the peek, so peek=false/tree=true is
			// invisible state: the next esc came off the ladder's treeOpen rung
			// and changed no pixel, and the next `t` appeared dead because it
			// was toggling the tree back OFF. Make the pair unrepresentable
			// rather than leave a state no frame can show.
			m.treeOpen = false
		}
		m.syncPeek()

	case key.Matches(msg, m.keys.PeekScroll):
		// Driven explicitly rather than by forwarding keys to the viewport:
		// viewport's default keymap owns up/down, which would make j/k scroll
		// the peek AND move the board cursor at the same time.
		//
		// The keys scroll whatever the user is looking at — the open peek
		// wins, then the table, then the focused column. They used to be
		// peek-only, which left them a SILENT dead key in the other two
		// states while the help advertised a plain "scroll" (t-84r1); a
		// gesture that cannot move must say so instead.
		down := msg.String() == "ctrl+d"
		dir := 1
		if !down {
			dir = -1
		}
		switch {
		case m.peekOpen:
			if down {
				m.vp.ScrollDown(maxInt(1, m.vp.Height()/2))
			} else {
				m.vp.ScrollUp(maxInt(1, m.vp.Height()/2))
			}
		case m.view == viewTable:
			// The table's window centers on the cursor, so half a page of
			// rows IS a cursor move — j/k's model, just bigger.
			rows := len(m.tableRows())
			step := maxInt(1, maxInt(1, m.tableVisRows())/2)
			before := m.tableIdx
			m.tableIdx = clamp(m.tableIdx+dir*step, 0, maxInt(0, rows-1))
			if m.tableIdx == before {
				m.note("already at the %s of the table", endName(dir))
			}
		default:
			// The focused board column, in the wheel's unit (cards) with the
			// wheel's guards, so the two scroll surfaces cannot disagree
			// about where the column ends. The cursor deliberately stays put
			// — like the wheel, a column scrolled away from its cursor is a
			// legitimate state (the next arrow re-pulls).
			lane := m.curLaneName()
			moved := 0
			if c := m.lay.Col(lane); c != nil {
				if len(c.Tasks) == 0 {
					m.note("%s is empty", lane)
					return nil
				}
				for i := maxInt(1, len(c.Cards)/2); i > 0; i-- {
					cc := m.lay.Col(lane)
					if down && cc.Hidden > 0 {
						m.scroll[lane] = cc.Scroll + 1
					} else if !down && cc.Scroll > 0 {
						m.scroll[lane] = cc.Scroll - 1
					} else {
						break
					}
					m.relayout()
					moved++
				}
			}
			if moved == 0 {
				m.note("%s is already at the %s", lane, endName(dir))
			}
		}

	case key.Matches(msg, m.keys.Tree):
		m.treeOpen = !m.treeOpen
		if m.treeOpen {
			m.peekOpen = true
		}
		m.syncPeek()

	case key.Matches(msg, m.keys.OnlyBlock):
		// TOKEN-wise, never a substring ReplaceAll: on "-is:blocked" that left a
		// bare "-" behind, which parses as a bare-word term and quietly changed
		// what the board showed while claiming the toggle was off.
		//
		// No note either way: `b` edits the filter query itself, so the filter
		// bar shows `is:blocked` appearing and disappearing.
		if q, had := dropToken(m.qRaw, "is:blocked"); had {
			cmd := m.applyFilter(q)
			m.ti.SetValue(m.qRaw)
			return cmd
		}
		cmd := m.applyFilter(strings.TrimSpace(m.qRaw + " is:blocked"))
		m.ti.SetValue(m.qRaw)
		return cmd

	case key.Matches(msg, m.keys.JumpBlock):
		m.jumpToBlocker()
	case key.Matches(msg, m.keys.JumpBack):
		m.jumpBack()

	case key.Matches(msg, m.keys.Reload):
		if m.inflight || len(m.pending) > 0 {
			// The reload would race the queue's own furrow process, land
			// behind the guard in onReloadDone and be dropped — leaving
			// "reloading…" on screen forever. The drain reconciles anyway.
			m.note("writes in flight — the board re-reads itself once they land")
			return nil
		}
		label := "reloaded"
		if !m.prov.Live() {
			label = "reloaded from the fixture — session edits discarded"
		}
		m.note("reloading…")
		return m.reloadCmd(label)

	case key.Matches(msg, m.keys.Sync):
		if !m.prov.Live() {
			m.note("the fixture has no store to sync")
			return nil
		}
		if m.inflight || len(m.pending) > 0 {
			m.note("writes in flight — sync once they land")
			return nil
		}
		m.note("syncing…")
		return m.syncCmd()

	case key.Matches(msg, m.keys.Add):
		m.fullHelp = false // same rule as Filter: a modal never inherits the overlay
		return m.enterAdd()

	case key.Matches(msg, m.keys.Slice):
		m.fullHelp = false
		m.toggleSlice()

	case key.Matches(msg, m.keys.Done):
		if t := m.curTask(); t != nil {
			id := t.ID
			// Board.Close moves the card out of its lane before enqueuePersist
			// can refuse, and that jump is what survives the window (t-8nyd's
			// shape, on the close path).
			if m.refuseWhileRollingBack("done " + id) {
				return nil
			}
			unblocked := len(m.g.OpenBlocks(id))
			if err := m.b.Close(id); err != nil {
				m.fail("%v", err)
				return nil
			}
			m.recompute()
			if unblocked > 0 {
				m.note("closed %s — unblocked %d task(s)", id, unblocked)
			} else {
				m.note("closed %s", id)
			}
			return m.enqueuePersist("done "+id, func() ([]string, error) {
				return nil, m.prov.PersistDone(id)
			})
		}

	case key.Matches(msg, m.keys.Edit):
		if t := m.curTask(); t != nil {
			return m.editCmd(t)
		}

	case key.Matches(msg, m.keys.Note):
		m.fullHelp = false // same rule as Filter: a modal never inherits the overlay
		return m.enterNote()

	case key.Matches(msg, m.keys.Move):
		// GitHub's Enter is cell EDITING; move mode is the board-only lift.
		// With the peek open (or on a table row) Enter edits the fields — the
		// board without a peek keeps Enter as the move-mode muscle memory.
		//
		// Close the overlay either way: the edit modal never inherits it, and
		// a lift needs the board visible — `?` works inside move mode for
		// whoever wants the listing back.
		m.fullHelp = false
		if m.view == viewTable || m.peekOpen {
			m.enterEdit()
		} else {
			m.enterMove()
		}

	case key.Matches(msg, m.keys.QuickUp):
		return m.quickReorder(-1)
	case key.Matches(msg, m.keys.QuickDown):
		return m.quickReorder(+1)

	case key.Matches(msg, m.keys.LaneBack):
		return m.cycleLane(-1)
	case key.Matches(msg, m.keys.LaneFwd):
		return m.cycleLane(+1)

	case key.Matches(msg, m.keys.Top):
		m.setPos(0)
	case key.Matches(msg, m.keys.Bottom):
		m.setPos(len(m.curTasks()) - 1)

	case key.Matches(msg, m.keys.Up):
		m.moveCursor(0, -1)
	case key.Matches(msg, m.keys.Down):
		m.moveCursor(0, +1)
	case key.Matches(msg, m.keys.Left):
		m.moveCursor(-1, 0)
	case key.Matches(msg, m.keys.Right):
		m.moveCursor(+1, 0)
	case key.Matches(msg, m.keys.NextCol):
		m.moveCursor(+1, 0)
	case key.Matches(msg, m.keys.PrevCol):
		m.moveCursor(-1, 0)
	}
	return nil
}

func (m *Model) countVisible() int {
	n := 0
	for _, l := range m.b.Lanes() {
		n += len(m.cols[l.Name])
	}
	return n
}

func (m *Model) setPos(i int) {
	if m.view == viewTable {
		m.tableIdx = clamp(i, 0, maxInt(0, len(m.tableRows())-1))
		m.syncPeek()
		return
	}
	m.curIdx[m.curLaneName()] = clamp(i, 0, maxInt(0, len(m.curTasks())-1))
	m.ensureVisible()
	m.syncPeek()
}

// moveCursor walks the grid. Moving between columns keeps the row position when
// it exists, which is what makes a kanban feel like a grid rather than a set of
// unrelated lists.
func (m *Model) moveCursor(dx, dy int) {
	if m.view == viewTable {
		if dy != 0 {
			m.tableIdx = clamp(m.tableIdx+dy, 0, maxInt(0, len(m.tableRows())-1))
			m.syncPeek()
		}
		return
	}
	if dy != 0 {
		m.setPos(m.curPos() + dy)
		return
	}
	if dx == 0 {
		return
	}
	want := m.curPos()
	i := m.curLane + dx
	if i < 0 || i >= len(m.b.Lanes()) {
		return
	}
	// One lane per keypress, landing on an EMPTY lane too (clamp snaps the
	// index to 0 there): you must be able to drop into one, and a lane you
	// cannot focus is a lane you cannot drop into. An earlier skip-empty
	// design scanned onward — that scan is deliberately gone.
	m.curLane = i
	m.curIdx[m.laneName(i)] = clamp(want, 0, len(m.cols[m.laneName(i)])-1)
	m.ensureVisible()
	m.syncPeek()
}

// selectID moves the cursor onto a task, pinning it past the filter when asked
// — a jump that lands nowhere is worse than no jump.
func (m *Model) selectID(id string, pin bool) bool {
	t := m.b.Task(id)
	if t == nil {
		return false
	}
	if pin {
		m.pinned[id] = true
		m.recompute()
	}
	for i, l := range m.b.Lanes() {
		for j, x := range m.cols[l.Name] {
			if x.ID == id {
				m.curLane, m.curIdx[l.Name] = i, j
				if m.view == viewTable {
					for k, r := range m.tableRows() {
						if r.ID == id {
							m.tableIdx = k
						}
					}
				}
				m.ensureVisible()
				m.syncPeek()
				return true
			}
		}
	}
	return false
}

// jumpToBlocker is the one interactive dep feature a static drawing cannot do.
// The real board's longest chain is 5 edges, so two presses reach any root
// blocker.
func (m *Model) jumpToBlocker() {
	t := m.curTask()
	if t == nil {
		return
	}
	blockers := m.g.BlockedBy(t.ID)
	if len(blockers) == 0 {
		m.note("%s is not blocked", t.ID)
		return
	}
	target := blockers[0]
	if !m.g.Known(target) {
		m.fail("%s depends on %s, which is not on this board", t.ID, target)
		return
	}
	m.jumpStack = append(m.jumpStack, t.ID)
	pinned := ""
	if !m.selectID(target, false) {
		m.selectID(target, true)
		pinned = " (pinned past the filter)"
	}
	m.note("→ %s (blocker %d/%d of %s)%s  ·  < to come back",
		target, 1, len(blockers), t.ID, pinned)
}

func (m *Model) jumpBack() {
	if len(m.jumpStack) == 0 {
		m.note("jump stack empty")
		return
	}
	id := m.jumpStack[len(m.jumpStack)-1]
	m.jumpStack = m.jumpStack[:len(m.jumpStack)-1]
	// Pin only when the target is genuinely hidden, the way jumpToBlocker does.
	// A pin is a permanent filter exemption plus a "+N pinned by jump" chip,
	// and pins are cleared only when the effective query empties — so pinning
	// unconditionally on an unfiltered board leaked an exemption that then
	// defied a filter typed later.
	if !m.selectID(id, false) {
		m.selectID(id, true)
	}
	m.note("← %s (%d left on the stack)", id, len(m.jumpStack))
}

// dropToken removes every occurrence of `tok` from a whitespace-separated query,
// reporting whether it was there.
func dropToken(raw, tok string) (string, bool) {
	var keep []string
	had := false
	for _, f := range strings.Fields(raw) {
		if f == tok {
			had = true
			continue
		}
		keep = append(keep, f)
	}
	return strings.Join(keep, " "), had
}

// cycleLane moves a task one lane over without entering move mode, appending it
// after the last card the filter SHOWS in the destination (boardInsertIndex's
// contract — not past cards the filter is hiding). A destination the filter
// EMPTIED puts the card on the lane's TOP: the slot a mouse drop into that
// same emptied column takes, kept identical so the two gestures cannot
// disagree about one visible state. It must go through commitMove: a direct
// MoveTo mutated the board before enqueuePersist could refuse it, so a
// rollback in flight rejected the write but not the gesture.
func (m *Model) cycleLane(d int) tea.Cmd {
	t := m.curTask()
	if t == nil {
		return nil
	}
	i := m.b.LaneIndex(t.Status) + d
	if i < 0 || i >= len(m.b.Lanes()) {
		m.note("no lane that way")
		return nil
	}
	dest := m.laneName(i)
	_, cmd, err := m.commitMove(t.ID, t.Status, dest, len(m.cols[dest]))
	if err != nil {
		m.fail("%v", err)
		return nil
	}
	// No note: the card is now in the other lane, with the cursor on it.
	return cmd
}

// quickReorder is shift+K / shift+J: nudge within the lane without the ceremony
// of move mode.
func (m *Model) quickReorder(d int) tea.Cmd {
	t := m.curTask()
	if t == nil {
		return nil
	}
	if m.view == viewTable && m.tableSort > sortCanonical {
		// GitHub's rule: a sorted table cannot be hand-reordered. The write
		// would land fine, but the sorted view wouldn't move — a nudge that
		// changes nothing on screen reads as a dead key.
		m.note("sorted by %s — reordering needs canonical order (o cycles back, or click lane)", m.tableSort)
		return nil
	}
	vis := m.cols[t.Status]
	from := -1
	for i, x := range vis {
		if x.ID == t.ID {
			from = i
		}
	}
	if from < 0 {
		return nil
	}
	to := from + d
	if to < 0 || to >= len(vis) {
		m.note("%s is already at the %s of %s", t.ID, endName(d), t.Status)
		return nil
	}
	moved, cmd, err := m.commitMove(t.ID, t.Status, t.Status, to+boolToInt(d > 0))
	if err != nil {
		m.fail("%v", err)
		return nil
	}
	if !moved {
		// This one stays: nothing happened, and nothing NOT happening is
		// exactly what the screen cannot show.
		m.note("%s did not move", t.ID)
		return nil
	}
	// The card visibly changed places, so there is nothing left to report.
	return cmd
}

func endName(d int) string {
	if d < 0 {
		return "top"
	}
	return "bottom"
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
