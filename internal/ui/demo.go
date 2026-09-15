package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/views"
)

// The -demo harness: the transient states -dump can freeze into one frame,
// and the selectors that pick each state's subject by shape. A verification
// fixture, not product code — it reaches into some thirty private fields and
// bypasses the persist queue where a frame needs it (sweeprestore archives
// synchronously, sweepwait plants a fake op), the one file allowed those
// liberties. DemoNames stays in api.go with the flag surface it feeds; the
// one-line description of every state is the comment on its case below.

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
		label, err := m.demoLabel("slice")
		if err != nil {
			return err
		}
		m.toggleSlice()
		m.sliceField = sliceLabel
		rows := m.sliceRows()
		for i, r := range rows {
			if r.value == label {
				m.sliceIdx = i
			}
		}
		if c := m.selectSlice(sliceLabel, label); c != nil {
			_ = c
		}

	case "add":
		// A filtered board, so the modal PROVES the context inheritance: the
		// filter's label lands in the chips, not silently on the task. The
		// typed line carries the inline tokens (t-69v9) plus one bad one, so
		// this single frame also proves the live echo AND the warning row.
		label, err := m.demoLabel("add")
		if err != nil {
			return err
		}
		dep, err := m.demoAnyTask("add")
		if err != nil {
			return err
		}
		m.ti.SetValue("label:" + label)
		m.applyFilter("label:" + label)
		m.relayout()
		if c := m.enterAdd(); c != nil {
			_ = c
		}
		// Short enough for the 56-cell input window (cursor included): the
		// frame must show the typed TITLE too, not just the scrolled-to
		// tail. Quoted values are unit-tested; the frame's job is the echo.
		m.add.input.SetValue(fmt.Sprintf("盤面起票 value:4 due:+1d dep:%s check:再現 effort:高", dep.ID))

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
		// (demoEditTask; t-9sa6 on the fixture)
		// and advance straight into the checklist sub-editor — the stage with
		// a cursor, which is the one a still frame can say something about.
		// The menu rows themselves are NOT exercised by this demo; they are
		// covered by unit tests instead.
		subj, err := m.demoEditTask("edit")
		if err != nil {
			return err
		}
		if !m.selectID(subj.ID, false) {
			return fmt.Errorf("demo edit: %s is on the board but not in view", subj.ID)
		}
		m.enterEdit()
		if m.edit == nil {
			return fmt.Errorf("demo edit: the edit menu did not open")
		}
		m.edit.menuIdx = int(fieldChecklist)
		m.openField(fieldChecklist, subj)
		m.edit.listIdx = 1

	case "editpick":
		// The 1..5 picker (value / effort). With editinput below, one of the
		// two SUB-EDITOR stages -dump could not reach (t-36yr): both exist
		// only between two keystrokes of a live overlay, so a regression that
		// blanked them could ship unseen. stageMenu is still -dump-less on
		// purpose — due_test frames it directly, and a menu is not a
		// mid-keystroke state.
		subj, err := m.demoEditTask("editpick")
		if err != nil {
			return err
		}
		if !m.selectID(subj.ID, false) {
			return fmt.Errorf("demo editpick: %s is on the board but not in view", subj.ID)
		}
		m.enterEdit()
		if m.edit == nil {
			return fmt.Errorf("demo editpick: the edit menu did not open")
		}
		m.edit.menuIdx = int(fieldValue)
		if c := m.openField(fieldValue, subj); c != nil {
			_ = c
		}

	case "editinput":
		// The retitle input, focused and pre-seeded with the task's CJK
		// title: one frame proves the prompt, the seeded value (its tail —
		// the cursor sits at the end) and the apply/back keys.
		subj, err := m.demoEditTask("editinput")
		if err != nil {
			return err
		}
		if !m.selectID(subj.ID, false) {
			return fmt.Errorf("demo editinput: %s is on the board but not in view", subj.ID)
		}
		m.enterEdit()
		if m.edit == nil {
			return fmt.Errorf("demo editinput: the edit menu did not open")
		}
		m.edit.menuIdx = int(fieldTitle)
		if c := m.openField(fieldTitle, subj); c != nil {
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
		// that are actually in the way. Seeded on a blocked task
		// (demoMixedDepsTask), so the frame also proves the selection gutter
		// and the strip below it.
		seed, err := m.demoMixedDepsTask("map")
		if err != nil {
			return err
		}
		m.openMap(seed.ID)

	case "mapall":
		// The same board at scope=all: one 19-node cluster, depth 5, which is
		// the frame that proves the indent ladder and the "+N" blocker tag
		// (demoMostDepsTask; the fixture's t-t38k has three blockers). Also
		// the only demo where a panel is taller than one column's share of
		// the canvas, so it proves the pack does not silently drop the
		// overflow.
		seed, err := m.demoMostDepsTask("mapall")
		if err != nil {
			return err
		}
		m.mapScope = board.ClusterAll
		m.openMap(seed.ID)

	case "mapfiltered":
		// The map UNDER a board filter. The map deliberately shows what the
		// filter hides — an edge that vanishes because of a query is a lie
		// about the board — so this frame is the proof that such rows are
		// muted and COUNTED rather than dropped.
		// is:blocked is the filter that makes the point: the board narrows to
		// the tasks that are stuck, and the map still draws the ROOTS that are
		// doing the blocking — muted and counted, because a cluster missing
		// the task at the top of it explains nothing. The seed is such a root
		// (demoRootTask; t-ehk7 on the fixture).
		root, err := m.demoRootTask("mapfiltered")
		if err != nil {
			return err
		}
		m.ti.SetValue("is:blocked")
		m.applyFilter("is:blocked")
		m.relayout()
		m.openMap(root.ID)

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
		active, err := m.demoActiveBox("filterchips")
		if err != nil {
			return err
		}
		m.toggleSlice()
		m.sliceField = sliceEpic
		for i, r := range m.sliceRows() {
			if r.value == active.ID {
				m.sliceIdx = i
			}
		}
		if c := m.selectSlice(sliceEpic, active.ID); c != nil {
			_ = c
		}
		m.mode = modeFilter
		m.ti.SetValue("lane:backlog is:blocked")
		m.ti.Focus()
		_ = m.applyFilter(m.ti.Value())

	case "editdeps":
		// The deps sub-editor on a task whose two deps resolve differently —
		// demoMixedDepsTask: t-jv3j on the fixture, which waits on an open
		// task and a done one — so one frame proves
		// both state glyphs, the resolved titles and the remove/add keys.
		subj, err := m.demoMixedDepsTask("editdeps")
		if err != nil {
			return err
		}
		if !m.selectID(subj.ID, false) {
			return fmt.Errorf("demo editdeps: %s is on the board but not in view", subj.ID)
		}
		m.enterEdit()
		if m.edit == nil {
			return fmt.Errorf("demo editdeps: the edit menu did not open")
		}
		m.edit.menuIdx = int(fieldDeps)
		m.openField(fieldDeps, subj)

	case "editrefs":
		// The refs sub-editor on the task whose two refs are the two forms
		// furrow documents — a file:line and a URL (demoRefsTask; t-9sa6 on
		// the fixture) — so one frame proves the
		// rows, the cursor and the remove/add keys.
		subj, err := m.demoRefsTask("editrefs")
		if err != nil {
			return err
		}
		if !m.selectID(subj.ID, false) {
			return fmt.Errorf("demo editrefs: %s is on the board but not in view", subj.ID)
		}
		m.enterEdit()
		if m.edit == nil {
			return fmt.Errorf("demo editrefs: the edit menu did not open")
		}
		m.edit.menuIdx = int(fieldRefs)
		m.openField(fieldRefs, subj)

	case "revisit":
		// The revisit lens with the peek on a flagged task: the ↻ chip in
		// the filter row, the board narrowed to what furrow revisit flags,
		// and the peek's reason line. demoMixedDepsTask's row carries the
		// dep_done signal (a done dep) on top of the fixture-wide staleness.
		// setRevisit(true), not a toggle: -revisit may already have turned
		// the lens on, and a toggle would cancel it.
		subj, err := m.demoMixedDepsTask("revisit")
		if err != nil {
			return err
		}
		if c := m.setRevisit(true); c != nil {
			return fmt.Errorf("demo revisit: the fixture lens must answer synchronously")
		}
		if !m.selectID(subj.ID, false) {
			return fmt.Errorf("demo revisit: the lens did not flag %s, or it is not in view", subj.ID)
		}
		m.peekOpen = true
		m.syncPeek()

	case "note":
		// The note input, focused and holding a typed CJK paragraph — the
		// state between `n` and ⏎ that no bare flag combination can reach.
		subj, err := m.demoEditTask("note")
		if err != nil {
			return err
		}
		if !m.selectID(subj.ID, false) {
			return fmt.Errorf("demo note: %s is on the board but not in view", subj.ID)
		}
		if c := m.enterNote(); c == nil {
			return fmt.Errorf("demo note: the note input did not open")
		}
		m.edit.input.SetValue("重量実測まで完了。次は車載レイアウト案の2案目から。")

	case "refs":
		// The peek's refs section, both documented forms (file:line and URL)
		// in furrow's own order. The default -dump selection has no refs, so
		// no bare flag combination reaches this frame.
		subj, err := m.demoRefsTask("refs")
		if err != nil {
			return err
		}
		if !m.selectID(subj.ID, false) {
			return fmt.Errorf("demo refs: %s is on the board but not in view", subj.ID)
		}
		m.peekOpen = true
		m.syncPeek()

	case "epicdeps":
		// The peek's epic-dep line, both resolutions at once: the row's box
		// (demoEpicDepsTask; t-y4st's e-c4mt on the fixture)
		// waits on an OPEN box (resolved to id+progress+title) and carries
		// a dep furrow already resolved away (outside open_deps —
		// satisfied). The default -dump selection is an unfiled task, so no
		// bare flag combination can reach this frame.
		subj, err := m.demoEpicDepsTask("epicdeps")
		if err != nil {
			return err
		}
		if !m.selectID(subj.ID, false) {
			return fmt.Errorf("demo epicdeps: %s is on the board but not in view", subj.ID)
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
		// The overlay's menu on the one fully-populated box (demoRichBox;
		// e-c4mt on the fixture), cursor parked on `active` — so the frame
		// proves every row's value AND the activate precondition ("slot held
		// by e-fw2m"), which is what stops furrow's
		// exit 2 from being the user's first news of the one-active-per-repo
		// rule. `-table -demo epic` composes, which is the frame that covers the
		// overlay over the table view — a modal that owns the keyboard must be
		// visible in both, and it was not.
		box, err := m.demoRichBox("epic")
		if err != nil {
			return err
		}
		if err := m.demoEpicPanel("epic", box.ID); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldActive)

	case "epiclist":
		// The deps sub-editor, all three resolutions in one frame: the
		// fixture's e-c4mt waits on an OPEN box, on one the board holds
		// CLOSED, and on an id no read serves. demoRichBox asks only for the
		// open one — a dangling epic dep is a lint ERROR in furrow, so a real
		// board rarely has the other two — and demo_test pins that the
		// fixture's box is the one with all three.
		box, err := m.demoRichBox("epiclist")
		if err != nil {
			return err
		}
		if err := m.demoEpicPanel("epiclist", box.ID); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldDeps)
		if c := m.openEpicField(epicFieldDeps, m.b.Epic(m.epic.id)); c != nil {
			_ = c
		}

	case "epicreason":
		// The activate input. It is the confirm step AND the collection of
		// furrow's --reason, which is appended to the box's body as the
		// activation record — a stage that exists only between two keystrokes.
		box, err := m.demoRichBox("epicreason")
		if err != nil {
			return err
		}
		if err := m.demoEpicPanel("epicreason", box.ID); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldActive)
		if c := m.openEpicField(epicFieldActive, m.b.Epic(m.epic.id)); c != nil {
			_ = c
		}
		m.epic.input.SetValue("ユーザー依頼: 冬支度を先に回す")

	case "epicconfirm":
		// The deactivate gate, reachable only on the ACTIVE box.
		active, err := m.demoActiveBox("epicconfirm")
		if err != nil {
			return err
		}
		if err := m.demoEpicPanel("epicconfirm", active.ID); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldActive)
		if c := m.openEpicField(epicFieldActive, m.b.Epic(m.epic.id)); c != nil {
			_ = c
		}

	case "epicshut":
		// The MENU on a closed box — the only frame where the `closed` row
		// reads its own state back. Without it the row could say "no — open"
		// on a box whose ⏎ reopens, and nothing would catch it.
		closed, err := m.demoClosedBox("epicshut")
		if err != nil {
			return err
		}
		m.sliceEpicAll = true
		if err := m.demoEpicPanel("epicshut", closed.ID); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldClosed)

	case "epicdone":
		// The close gate on the ACTIVE box, which is also the one with the
		// most work still under it. furrow closes such a box at exit 0, so
		// this frame is the only warning there is — and closing the active box
		// vacates its repo slot in the same write, which is the other half the
		// gate owes the user.
		active, err := m.demoActiveBox("epicdone")
		if err != nil {
			return err
		}
		if err := m.demoEpicPanel("epicdone", active.ID); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldClosed)
		if c := m.openEpicField(epicFieldClosed, m.b.Epic(m.epic.id)); c != nil {
			_ = c
		}

	case "epicreopen":
		// The same row on the CLOSED box, which is the other verb and the
		// other wording. Reaching it needs the widened scope, which is the
		// point: without it the box `reopen` targets is not on any list.
		closed, err := m.demoClosedBox("epicreopen")
		if err != nil {
			return err
		}
		m.sliceEpicAll = true
		if err := m.demoEpicPanel("epicreopen", closed.ID); err != nil {
			return err
		}
		m.epic.menuIdx = int(epicFieldClosed)
		if c := m.openEpicField(epicFieldClosed, m.b.Epic(m.epic.id)); c != nil {
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

	case "sliceepicclosed":
		// The widened axis with the cursor ON the closed box. The one frame
		// that can show the exception the closed row's styling carries: the
		// cursor outranks the dim, so the row a reader is standing on is never
		// the recessed one. Driven through the key handler, like sliceepicall.
		m.toggleSlice()
		m.sliceField = sliceEpic
		m.noteSliceAxis()
		if c := m.onSliceKey(tea.KeyPressMsg{Code: 'z', Text: "z"}); c != nil {
			_ = c
		}
		if c := m.onSliceKey(tea.KeyPressMsg{Code: 'G', Text: "G"}); c != nil {
			_ = c
		}
		rows := m.sliceRows()
		if m.sliceIdx != len(rows)-1 || !rows[m.sliceIdx].closed {
			return fmt.Errorf("demo sliceepicclosed: the cursor is on %d of %d, and it is not a closed box", m.sliceIdx, len(rows))
		}

	case "epicnew":
		// The new-box modal under a typed repo: filter, so the frame PROVES
		// the inheritance: the filter's repo lands in the chip, not silently
		// on the box. The filter, not a repo slice — `A` only answers on the
		// epic axis and the axis switch clears a repo-axis pick, so a typed
		// repo: is the one form that can still be in force when `A` fires.
		// Without a repo a new box cannot be activated at all, which is why
		// this one is worth a frame of its own.
		repo, err := m.demoRepo("epicnew")
		if err != nil {
			return err
		}
		m.ti.SetValue("repo:" + repo)
		m.applyFilter("repo:" + repo)
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
		closed, err := m.demoClosedBox("boxesall")
		if err != nil {
			return err
		}
		m.openBoxes()
		if c := m.onBoxesKey(tea.KeyPressMsg{Code: 'z', Text: "z"}); c != nil {
			_ = c
		}
		l := m.buildBoxes()
		m.boxesLay = l
		// The row's group is its first repo, or the no-repo group packBoxes
		// files a repo-less box under.
		repo := boxNoRepo
		if len(closed.Repos) > 0 {
			repo = closed.Repos[0]
		}
		key := boxKey(repo, closed.ID)
		if l.Row(key) == nil {
			return fmt.Errorf("demo boxesall: z did not widen the population")
		}
		m.boxesSel = key

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
		label, err := m.demoLabel("views")
		if err != nil {
			return err
		}
		m.views = demoViews(label)
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
		label, err := m.demoLabel("viewsroad")
		if err != nil {
			return err
		}
		m.views = demoViews(label)
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

	case "sweep":
		// The sweep at rest, driven through the board's own key handler so the
		// frame also proves `X` is BOUND. The fixture's nine done tasks are all
		// past the age guard and several open tasks carry edges to them, so
		// the archive and done-deps sections both have rows; unknown-keys and
		// the archive store are empty — the two "nothing" lines have no other
		// frame.
		if c := m.onNormalKey(tea.KeyPressMsg{Code: 'X', Text: "X"}); c != nil {
			_ = c
		}
		if m.view != viewSweep {
			return fmt.Errorf("demo sweep: X did not open the sweep")
		}

	case "sweepconfirm":
		// The archive gate with one row skipped: the header line names the
		// exact id list and count the second ⏎ sends, and the skipped row's
		// marker is dim — the frame the destructive write is judged from.
		if c := m.openSweep(); c != nil {
			_ = c
		}
		rows := sweepRows(m.sweep)
		if c := m.onSweepKey(tea.KeyPressMsg{Code: 'x', Text: "x"}); c != nil {
			_ = c
		}
		m.sweepSel = sweepStep(rows, m.sweepSel, +1)
		if c := m.onSweepKey(tea.KeyPressMsg{Code: tea.KeyEnter}); c != nil {
			_ = c
		}
		if m.sweepGate == nil {
			return fmt.Errorf("demo sweepconfirm: ⏎ did not arm the archive gate")
		}

	case "sweeprestore":
		// After an archive landed: the archive store has rows, the cursor is
		// on one, and the restore gate is open — the other direction of the
		// round trip, on the same screen.
		if c := m.openSweep(); c != nil {
			_ = c
		}
		ids := sweepArchiveSet(m.sweep, nil)
		if len(ids) < 2 {
			return fmt.Errorf("demo sweeprestore: the fixture has %d archivable tasks, want 2+", len(ids))
		}
		if err := m.prov.Archive(ids[:2]); err != nil {
			return fmt.Errorf("demo sweeprestore: %v", err)
		}
		m.reload()
		if c := m.loadSweep(); c != nil {
			_ = c
		}
		m.sweepSel = sweepKey(sweepArchived, ids[0])
		if c := m.onSweepKey(tea.KeyPressMsg{Code: tea.KeyEnter}); c != nil {
			_ = c
		}
		if m.sweepGate == nil {
			return fmt.Errorf("demo sweeprestore: ⏎ did not arm the restore gate")
		}

	case "sweepwait":
		// The sweep opened while a write is still queued: the preview read is
		// deferred to the drain (it would race the queue's furrow process), and
		// the header must say so — four empty sections here would claim there
		// is nothing to sweep. The op is a stand-in; nothing runs it.
		subj, err := m.demoAnyTask("sweepwait")
		if err != nil {
			return err
		}
		m.pending = append(m.pending, persistOp{label: "move " + subj.ID, run: func() ([]string, error) { return nil, nil }})
		m.inflight = true
		if c := m.openSweep(); c != nil {
			_ = c
		}
		if m.sweep != nil || !m.sweepLoading {
			return fmt.Errorf("demo sweepwait: the read was not deferred (sweep=%v loading=%v)", m.sweep != nil, m.sweepLoading)
		}

	case "unlaned":
		// A task whose status names no lane: the load note counts it and
		// names the gap, the lanes and the title-bar count do not hold it.
		// Built onto the fixture here rather than in it — one task added to
		// the fixture breaks 21 tests (t-38fm).
		tasks := append(append([]*board.Task(nil), m.b.Tasks()...),
			&board.Task{ID: "t-unlaned", Title: "lane removed from furrow's config", Status: "archived", Priority: 10})
		m.b = board.NewStoreBoard(m.b.Lanes(), tasks, m.b.EpicsAll(), m.b.Writable(), m.b.SchemaState())
		m.recompute()
		m.noteLoad(false, 0)

	case "fail":
		// A refused write. The ⚠ styling has its own colour and its own row,
		// and nothing else in the demo set renders an error at all.
		// onPersistDone sets lastPersist BEFORE it branches on the error, so a
		// real refusal always carries the latency readout too. Leaving it
		// empty rendered a frame the app cannot actually be in.
		subj, err := m.demoAnyTask("fail")
		if err != nil {
			return err
		}
		m.lastPersist = "move " + subj.ID + " 96ms"
		m.fail("%s: the store refused the write — the board is rolling back", subj.ID)
		m.rollingBack = true

	default:
		return fmt.Errorf("unknown -demo %q (want %s)", kind, strings.Join(DemoNames, "|"))
	}
	m.relayout()
	return nil
}

// demoViews is the view set the two demos inject — CJK names on purpose:
// the tab band measures its cells the way every other chrome does, and only
// a CJK name can prove it. The first tab's query is the board's commonest
// label (demoLabel), so the set holds no fixture vocabulary.
func demoViews(label string) []views.View {
	return []views.View{
		{Name: "火の粉", Layout: "board", Q: "label:" + label},
		{Name: "締切", Layout: "roadmap"},
		{Name: "表で総覧", Layout: "table", Sort: "due asc"},
	}
}

// demoEpicPanel reaches the epic overlay the way a user does — through the
// panel, on the epic axis, with the cursor on the box — so the frame behind the
// overlay is the real one and `esc` in the resulting state would land back in
// modeSlice rather than on a bare board.
func (m *Model) demoEpicPanel(demo, id string) error {
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
		return fmt.Errorf("demo %s: %s is not a box on this board", demo, id)
	}
	m.enterEpic(id)
	if m.epic == nil {
		return fmt.Errorf("demo %s: the overlay did not open on %s", demo, id)
	}
	return nil
}

// The demos pick their subject by the shape the frame needs, never by id.
// About twenty of them once named t-9sa6 / t-jv3j / e-c4mt outright and died
// with "is not on the fixture board" over any other data, so no demo state
// could be produced on a synthetic or real-shaped board (t-360e). Each
// predicate is the frame's own precondition, spelled out in the error when
// the board has no such row; the first match in board order wins, so the
// fixture keeps drawing the frames it always drew — demo_test pins the rows
// the predicates land on there, and runs every demo over a board that holds
// none of the fixture's ids.

// demoTask is the first task in board order that pred accepts; the error
// names the shape the demo needs.
func (m *Model) demoTask(demo, need string, pred func(*board.Task) bool) (*board.Task, error) {
	for _, t := range m.b.Tasks() {
		if pred(t) {
			return t, nil
		}
	}
	return nil, fmt.Errorf("demo %s: no task on this board %s", demo, need)
}

// demoBox is demoTask over every box, closed ones included.
func (m *Model) demoBox(demo, need string, pred func(board.EpicInfo) bool) (board.EpicInfo, error) {
	for _, e := range m.b.EpicsAll() {
		if pred(e) {
			return e, nil
		}
	}
	return board.EpicInfo{}, fmt.Errorf("demo %s: no box on this board %s", demo, need)
}

// demoAnyTask is the board's first task, for the demos that only need an id
// to print (a queued op's label, a refused write's message, a typed dep:).
func (m *Model) demoAnyTask(demo string) (*board.Task, error) {
	if ts := m.b.Tasks(); len(ts) > 0 {
		return ts[0], nil
	}
	return nil, fmt.Errorf("demo %s: the board is empty", demo)
}

// demoEditTask is the subject of the edit-overlay demos: a checklist of two
// or more items (the checklist stage parks its cursor on the second) and a
// label, so every menu row has a value to show.
func (m *Model) demoEditTask(demo string) (*board.Task, error) {
	return m.demoTask(demo, "has both a checklist of two or more items and a label", func(t *board.Task) bool {
		return len(t.Checklist) >= 2 && len(t.Labels) > 0
	})
}

// demoRefsTask carries two or more refs — on the fixture, furrow's two
// documented forms, a file:line and a URL.
func (m *Model) demoRefsTask(demo string) (*board.Task, error) {
	return m.demoTask(demo, "carries two or more refs", func(t *board.Task) bool {
		return len(t.Refs) >= 2
	})
}

// demoMixedDepsTask is an open task waiting on one open task and one done
// one: both dep glyphs in a single frame, the dep_done revisit signal, and
// an open blocker for the map to seed on.
func (m *Model) demoMixedDepsTask(demo string) (*board.Task, error) {
	return m.demoTask(demo, "is open and waits on both an open task and a done one", func(t *board.Task) bool {
		if m.g.IsDone(t.ID) {
			return false
		}
		var open, done bool
		for _, d := range t.Deps {
			switch {
			case m.g.IsDone(d):
				done = true
			case m.g.Known(d):
				open = true
			}
		}
		return open && done
	})
}

// demoMostDepsTask is the task with the most deps, done or not — the deepest
// row scope=all can show, and the "+N" blocker tag's site. Ties keep the
// first in board order.
func (m *Model) demoMostDepsTask(demo string) (*board.Task, error) {
	var best *board.Task
	for _, t := range m.b.Tasks() {
		if len(t.Deps) > 0 && (best == nil || len(t.Deps) > len(best.Deps)) {
			best = t
		}
	}
	if best == nil {
		return nil, fmt.Errorf("demo %s: no task on this board has a dep", demo)
	}
	return best, nil
}

// demoRootTask is the open root doing the most blocking: no deps of its own,
// and more open dependants than any other such task (ties: board order).
// Under is:blocked it is a row the filter HIDES, which is the frame's point.
func (m *Model) demoRootTask(demo string) (*board.Task, error) {
	var best *board.Task
	var bestN int
	for _, t := range m.b.Tasks() {
		if m.g.IsDone(t.ID) || len(t.Deps) > 0 {
			continue
		}
		if n := len(m.g.OpenBlocks(t.ID)); n > bestN {
			best, bestN = t, n
		}
	}
	if best == nil {
		return nil, fmt.Errorf("demo %s: no open task on this board blocks another without waiting on anything itself", demo)
	}
	return best, nil
}

// demoEpicDepsTask is filed under a box that both waits on an open box and
// carries a dep furrow already resolved away, so the peek's epic-dep line
// shows both resolutions at once.
func (m *Model) demoEpicDepsTask(demo string) (*board.Task, error) {
	return m.demoTask(demo, "is filed under a box that waits on an open box and also carries a dep already resolved away", func(t *board.Task) bool {
		e := m.b.Epic(t.Epic)
		return e != nil && len(e.OpenDeps) > 0 && len(e.Deps) > len(e.OpenDeps)
	})
}

// demoRichBox is the populated inactive box the epic overlay's menu is worth
// a frame on: a goal, a wait on an open box, and a repo slot some active box
// already holds, so the `active` row has a precondition to state.
func (m *Model) demoRichBox(demo string) (board.EpicInfo, error) {
	return m.demoBox(demo, "is an open, inactive box with a goal, a wait on an open box, and a repo slot the active box holds", func(e board.EpicInfo) bool {
		return !e.Active && e.Closed.IsZero() && e.Goal != "" && len(e.OpenDeps) > 0 && m.b.ActiveHolder(e.ID) != ""
	})
}

// demoActiveBox is the board's active box — the only one deactivate and the
// close gate's "vacates its repo slot" clause are reachable on.
func (m *Model) demoActiveBox(demo string) (board.EpicInfo, error) {
	return m.demoBox(demo, "is active", func(e board.EpicInfo) bool { return e.Active })
}

// demoClosedBox is the closed box the overlay has the most to show on — the
// first one carrying a goal, so the menu's goal row is not blank — and the
// first closed box at all when none does (a board whose only finished boxes
// are reserved ones still has a reopen frame).
func (m *Model) demoClosedBox(demo string) (board.EpicInfo, error) {
	closed := func(e board.EpicInfo) bool { return !e.Closed.IsZero() }
	for _, e := range m.b.EpicsAll() {
		if closed(e) && e.Goal != "" {
			return e, nil
		}
	}
	return m.demoBox(demo, "is closed", closed)
}

// demoLabel is the label the most tasks carry (ties: the first in label
// order) — the context the slice and the add modal inherit.
func (m *Model) demoLabel(demo string) (string, error) {
	return demoMostCommon(demo, "carries a label", m.b.Tasks(), func(t *board.Task) []string { return t.Labels })
}

// demoRepo is the repo the most tasks carry (ties: the first in repo order),
// so a new box filed under it can actually be activated.
func (m *Model) demoRepo(demo string) (string, error) {
	return demoMostCommon(demo, "carries a repo", m.b.Tasks(), func(t *board.Task) []string { return t.Repos })
}

func demoMostCommon(demo, need string, tasks []*board.Task, of func(*board.Task) []string) (string, error) {
	count := map[string]int{}
	for _, t := range tasks {
		for _, v := range of(t) {
			count[v]++
		}
	}
	best := ""
	for v, n := range count {
		if best == "" || n > count[best] || n == count[best] && v < best {
			best = v
		}
	}
	if best == "" {
		return "", fmt.Errorf("demo %s: no task on this board %s", demo, need)
	}
	return best, nil
}
