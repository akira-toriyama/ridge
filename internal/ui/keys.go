package ui

import (
	"charm.land/bubbles/v2/key"
)

// keyMap is the whole keyboard surface. Every mouse gesture in this POC has an
// entry here — that is a hard rule, not a nicety: a terminal user may be on a
// mouse-less tmux, and drag-and-drop is the bonus, not the contract.
//
// Note WithKeys("space"), not WithKeys(" "): in bubbletea v2 key.Matches
// compares Key.String(), which renders the space bar as "space". " " compiles
// fine and silently never matches.
type keyMap struct {
	Up, Down, Left, Right key.Binding
	NextCol, PrevCol      key.Binding
	Top, Bottom           key.Binding

	Move       key.Binding
	Commit     key.Binding
	Cancel     key.Binding
	MoveTop    key.Binding
	MoveBottom key.Binding
	MoveFirst  key.Binding
	MoveLast   key.Binding
	QuickUp    key.Binding
	QuickDown  key.Binding
	LaneFwd    key.Binding
	LaneBack   key.Binding
	Peek       key.Binding
	Filter     key.Binding
	JumpBlock  key.Binding
	JumpBack   key.Binding
	Add        key.Binding
	Slice      key.Binding
	Sort       key.Binding
	Done       key.Binding
	Edit       key.Binding
	Note       key.Binding
	Reload     key.Binding
	Sync       key.Binding
	Tree       key.Binding
	View       key.Binding
	Mouse      key.Binding
	Check      key.Binding
	OnlyBlock  key.Binding
	Help       key.Binding
	Quit       key.Binding
	// ForceQuit is ctrl+c alone — the escape hatch inside modal text inputs,
	// where `q` must type. bubbletea v2's raw mode delivers ctrl+c as a
	// normal keystroke, so every modal key handler must match this itself.
	ForceQuit  key.Binding
	PeekScroll key.Binding

	Graph       key.Binding
	GraphRoot   key.Binding
	GraphRadius key.Binding
	// GraphOrient reuses `o`, the table's sort key, on the same licence
	// MapScope has for reusing `z`: `o` is ORDER — "how this view arranges what
	// it shows". In the table that is which column the rows are sorted by; in
	// the graph it is which screen axis the layers run along. The two views are
	// never on screen together, and each section's help string names its own
	// meaning, so no line of the overlay is false.
	GraphOrient key.Binding

	// The dep map's three keys. `T` is `t` (this task's dep tree) writ large —
	// the map IS every dep tree at once, drawn with the same indent-and-name
	// vocabulary — which is the uppercase/lowercase relation K/J/H/L already
	// use: the shifted key is the bigger version of the unshifted one.
	//
	// MapScope reuses `z`, the graph's radius key, on purpose: in both
	// full-screen views `z` cycles the one knob that decides how much of the
	// dependency structure is on screen. MapGraph carries ⏎ AND the graph's
	// own `S`/⇧space, so the gesture that opens a graph is the same letter
	// everywhere; its help text differs because from here it is not a re-root.
	Map      key.Binding
	MapScope key.Binding
	MapGraph key.Binding

	// Boxes opens the BOX OVERVIEW. Uppercase like the other two full-screen
	// views (S graph, T map), because those are the keys that replace the whole
	// screen and the lowercase letters are the board's own edits. `E` is the
	// initial of the entity furrow calls an epic, which is the word its CLI
	// uses even though this repo's UI calls the thing a box.
	//
	// Inside the view MapScope does its own job again: `z` decides how much of
	// the population is on screen, exactly as it decides how much of the
	// dependency structure is on screen in the other two.
	Boxes key.Binding
	// BoxSlice is ⏎ inside the overview, bound separately from Commit for the
	// reason MapGraph is bound separately from it: the help text is read as a
	// claim about what the key does, and "commit" is not what ⏎ does here.
	BoxSlice key.Binding

	// Roadmap opens the DUE TIMELINE — uppercase like the other full-screen
	// views. `C` (calendar), and neither of the two letters the view's own
	// words suggest: `R` is sync's, and `D` sits one missed shift from `d`,
	// the one lowercase twin in this row that WRITES (it closes the selected
	// task — every other view key's twin is a read). `c` is unbound, so a
	// missed shift here does nothing at all.
	Roadmap key.Binding
	// RoadZoom reuses `z` on the licence MapScope spells out: in every
	// full-screen view `z` is the one knob for how much is on screen — hops
	// in the graph, population in the map and the overview, calendar per
	// cell here.
	RoadZoom key.Binding

	// The SWIMLANE's four keys. `W` is the opener — uppercase like the other
	// four full-screen views (S graph, T map, E boxes, C roadmap), and chosen
	// by the same test `C` passed: `w` is unbound, so a missed shift does
	// nothing at all. The letters the view's own words suggest are all taken
	// or unsafe — `G` is Bottom, `L` carries a card a lane over, and `X`/`N`
	// each sit one missed shift from a key that WRITES (`x` toggles a
	// checklist item, `n` appends a note).
	//
	// SwimFold reuses `space` on the licence GraphOrient states for reusing
	// `o`: the board's Peek and this view are never on screen together (the
	// peek composites nothing under fullScreen()), and each section's help
	// string names its own meaning, so no line of the overlay is false.
	// SwimSlice is bound separately from Commit and BoxSlice for BoxSlice's
	// own stated reason — the help text is read as a claim about what the key
	// does, and neither "commit" nor "slice to this box" is what ⏎ does here.
	// SwimAxis is likewise not NextCol/PrevCol, whose help says "next column":
	// tab moves the GROUPING axis, and the columns are lanes.
	Swim      key.Binding
	SwimFold  key.Binding
	SwimSlice key.Binding
	SwimAxis  key.Binding

	// The saved-view tabs (viewtabs.go). ViewSave is the uppercase of `v` by
	// the K/J/H/L relation: `v` toggles the layout of the state you are in,
	// `V` names/saves the WHOLE state as a view. The digits are free on the
	// board — the graph's radius digits live in their own full-screen mode
	// (onGraphKey is routed first), so the two never collide.
	ViewTab  key.Binding
	ViewSave key.Binding

	// The slice panel's two epic-management keys (epicmode.go). EpicEdit is
	// `m`-only on purpose: keys.Move is ("enter","m") and the panel's ⏎ SLICES,
	// so reusing Move here would shadow the panel's own commit key. `e` was the
	// obvious letter and is deliberately left alone — it means $EDITOR, and
	// `furrow edit` takes an epic id, so that is the key epic body editing will
	// want. EpicNew is uppercase for the same reason R/K/J/H/L are: the
	// lowercase sibling (`a`) creates a TASK, and furrow has no `epic rm` to
	// undo a slip with.
	EpicEdit key.Binding
	EpicNew  key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Left:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "left")),
		Right:   key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "right")),
		NextCol: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next column")),
		PrevCol: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧tab", "prev column")),
		Top:     key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:  key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),

		// shift+space USED to be a third alias for move mode (GitHub Projects
		// documents it as one). It now opens the dependency graph, which is the
		// gesture the user asked for by name. Move mode keeps ⏎ and m — the two
		// bindings a lift is actually reached by — so nothing became
		// unreachable; one alias changed owner.
		Move:   key.NewBinding(key.WithKeys("enter", "m"), key.WithHelp("⏎/m", "move mode")),
		Commit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "commit")),
		Cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		// Move-to-the-extremes: uppercase = "all the way", the shifted sibling of
		// the lowercase hjkl single steps inside move mode. GitHub's documented
		// ctrl+arrows stay as silent aliases — macOS Terminal never delivers them
		// (Mission Control owns all four), and the repo rule is that a modified
		// gesture always has a bare-key alias. Help shows only the letters.
		MoveTop:    key.NewBinding(key.WithKeys("K", "ctrl+up"), key.WithHelp("K", "to top")),
		MoveBottom: key.NewBinding(key.WithKeys("J", "ctrl+down"), key.WithHelp("J", "to bottom")),
		MoveFirst:  key.NewBinding(key.WithKeys("H", "ctrl+left"), key.WithHelp("H", "first lane")),
		MoveLast:   key.NewBinding(key.WithKeys("L", "ctrl+right"), key.WithHelp("L", "last lane")),
		// Normal mode's uppercase row moves the CARD one step where lowercase
		// moves the cursor: K/J raise/lower in the lane, H/L carry it one lane
		// over — the taskell/kanban-tui convention. `[` `]` (the old lane pair)
		// are freed, not reassigned.
		QuickUp:   key.NewBinding(key.WithKeys("K"), key.WithHelp("K", "raise")),
		QuickDown: key.NewBinding(key.WithKeys("J"), key.WithHelp("J", "lower")),
		LaneBack:  key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "lane ←")),
		LaneFwd:   key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "lane →")),

		Peek:      key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "detail")),
		Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		JumpBlock: key.NewBinding(key.WithKeys(">"), key.WithHelp(">", "jump to blocker")),
		JumpBack:  key.NewBinding(key.WithKeys("<"), key.WithHelp("<", "jump back")),
		OnlyBlock: key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "only blocked")),
		Tree:      key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "dep tree")),
		View:      key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "board/table")),

		// `o` (order), not the `s` t-qve3 sketched: `s` is the slice panel, and
		// the panel deliberately reaches the table view too — two owners for
		// one key, and the panel got there first.
		Sort: key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "sort (table)")),
		Done: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "done")),
		Edit: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "$EDITOR")),
		// `n` next to `e`: the light body path (one appended paragraph,
		// `furrow note`'s contract) beside the heavy one ($EDITOR full open).
		Note:      key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "append note")),
		Check:     key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "toggle")),
		Add:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add item")),
		Slice:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "slice panel")),
		Reload:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
		Sync:      key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "sync (git)")),
		Mouse:     key.NewBinding(key.WithKeys("M"), key.WithHelp("M", "mouse on/off")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		ForceQuit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("^c", "quit")),
		// "scroll", not "scroll peek": the same binding scrolls the graph canvas,
		// and a help entry must stay true in every section that lists it.
		PeekScroll: key.NewBinding(key.WithKeys("ctrl+d", "ctrl+u"), key.WithHelp("^d/^u", "scroll")),

		// Note WithKeys("shift+space"), not " " and not "shift+ ": key.Matches
		// compares Key.String(), which renders the space bar as "space".
		// BOTH keys in the label, unlike MoveTop/MoveBottom where the hidden
		// alias is the dead one. Here it is the printed key that is dead:
		// shift+space cannot be encoded without the Kitty protocol, so on
		// macOS Terminal and most tmux `S` is the only way in — and the `?`
		// overlay, which renders Help() and can never show a WithKeys alias,
		// was advertising only the unreachable half.
		Graph:       key.NewBinding(key.WithKeys("shift+space", "S"), key.WithHelp("⇧space/S", "dep graph")),
		GraphRoot:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "re-root here")),
		GraphRadius: key.NewBinding(key.WithKeys("z", "1", "2", "3", "0"), key.WithHelp("z/1-3/0", "hop radius")),
		GraphOrient: key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "top-down / left-right")),

		Map:      key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "dep map")),
		Boxes:    key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "box overview")),
		BoxSlice: key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "slice to this box")),
		MapScope: key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "scope open/all")),
		// "graph here", not "dep graph": TestHelpAdvertisesThePortableGraphKey
		// reads the FIRST line carrying "dep graph" and requires it to name the
		// portable half of the gesture, and that line is normal mode's.
		MapGraph: key.NewBinding(key.WithKeys("enter", "S", "shift+space"), key.WithHelp("⏎/S", "graph here")),

		Roadmap:  key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "roadmap")),
		RoadZoom: key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "zoom day/week/month")),

		Swim:      key.NewBinding(key.WithKeys("W"), key.WithHelp("W", "swimlane")),
		SwimFold:  key.NewBinding(key.WithKeys("space"), key.WithHelp("space", "fold/unfold band")),
		SwimSlice: key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "slice to this band")),
		SwimAxis: key.NewBinding(key.WithKeys("tab", "shift+tab"),
			key.WithHelp("tab", "group by repo/label/box")),

		ViewTab: key.NewBinding(key.WithKeys("1", "2", "3", "4", "5", "6", "7", "8", "9"),
			key.WithHelp("1-9", "saved view")),
		ViewSave: key.NewBinding(key.WithKeys("V"), key.WithHelp("V", "save view")),

		EpicEdit: key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "manage box")),
		EpicNew:  key.NewBinding(key.WithKeys("A"), key.WithHelp("A", "new box")),
	}
}

// helpSection is one mode's slice of the key surface: the `?` overlay renders
// one titled block per section, so a key is listed under the mode that will
// actually answer it.
type helpSection struct {
	title  string // the glossary's mode name, verbatim
	groups [][]key.Binding
}

// HelpSections is the `?` overlay — and, since the footers went, the ONLY
// place the key surface is listed. That is the point: it is built from the
// same key.Bindings the Update path matches on, so it cannot advertise a key
// that does not work. The footers could, and did — the graph's offered `space
// detail` while the graph had no Peek case at all.
//
// The sections mirror the dispatch: onNormalKey, onMoveKey, onGraphKey. The
// modal inputs (filter / edit / add / slice) are absent by the same rule that
// keeps dead keys out — their keys live inside their overlays, and `?` cannot
// even be typed there.
// enterEdits says whether ⏎/m would open the EDIT overlay rather than lift the
// card — it diverts with the peek open or on a table row (model.go). The one
// canonical key list has to name the thing the next press actually does.
func (k keyMap) HelpSections(enterEdits bool) []helpSection {
	open := k.Move
	if enterEdits {
		open = key.NewBinding(key.WithKeys("enter", "m"), key.WithHelp("⏎/m", "edit fields"))
	}
	return []helpSection{
		{"normal mode", [][]key.Binding{
			{k.Up, k.Down, k.Left, k.Right, k.NextCol, k.PrevCol, k.Top, k.Bottom},
			{open, k.QuickUp, k.QuickDown, k.LaneBack, k.LaneFwd, k.Done, k.Edit, k.Note, k.Add},
			{k.Peek, k.Tree, k.PeekScroll, k.Filter, k.OnlyBlock, k.Slice, k.View, k.Sort, k.ViewTab, k.ViewSave},
			// Boxes was absent from this section for two releases: `E` worked
			// while the one canonical key list denied it existed. Every
			// full-screen view's opener belongs here — the section is "your
			// keys right now", and normal mode is where they are pressed.
			{k.Graph, k.Map, k.Boxes, k.Roadmap, k.Swim, k.JumpBlock, k.JumpBack, k.Reload, k.Sync, k.Mouse, k.Cancel, k.Help, k.Quit},
		}},
		{"move mode", [][]key.Binding{
			// The arrows come first because they are how the lifted card is
			// actually placed; onMoveKey has handled them all along, and the
			// section that omitted them left the extremes looking like the
			// only movement on offer.
			{k.Up, k.Down, k.Left, k.Right},
			{k.Commit, k.Cancel},
			{k.MoveTop, k.MoveBottom},
			{k.MoveFirst, k.MoveLast},
		}},
		// The graph's full surface, not just its two custom bindings: sectioning
		// turned this block into "your keys right now", so listing 2 of the ~10
		// keys — and no way out — was an assertion, not an omission.
		// The dep map's surface. Same rule as the graph's: every key onMapKey
		// acts on, not just the ones unique to it — a full-screen mode's
		// section is read as "your keys right now".
		{"dep map", [][]key.Binding{
			{k.Up, k.Down, k.Left, k.Right},
			{k.MapGraph, k.MapScope},
			{k.PeekScroll, k.Map, k.View, k.Cancel},
		}},
		// The box overview's surface. Same rule as the other two full-screen
		// sections: every key onBoxesKey acts on, because the section is read
		// as "your keys right now".
		{"box overview", [][]key.Binding{
			{k.Up, k.Down, k.Left, k.Right},
			{k.BoxSlice, k.EpicEdit, k.MapScope},
			{k.Top, k.Bottom, k.PeekScroll},
			{k.Boxes, k.View, k.Cancel},
		}},
		// The roadmap's surface. Same rule again: every key onRoadKey acts on.
		// The saved-view keys are listed here and in no other full-screen
		// section because the roadmap is the one full-screen view a saved
		// view can BE — its title row carries the tabs, so its keys must too.
		{"roadmap", [][]key.Binding{
			{k.Up, k.Down, k.Left, k.Right},
			{k.RoadZoom, k.Top, k.Bottom, k.PeekScroll},
			{k.ViewTab, k.ViewSave},
			{k.Roadmap, k.View, k.Cancel},
		}},
		// The swimlane's surface. Same rule as the other full-screen sections:
		// every key onSwimKey acts on, because the section is read as "your
		// keys right now".
		{"swimlane", [][]key.Binding{
			{k.Up, k.Down, k.Left, k.Right},
			{k.SwimFold, k.SwimSlice, k.SwimAxis, k.MapScope},
			{k.Top, k.Bottom, k.PeekScroll},
			{k.Swim, k.View, k.Cancel},
		}},
		{"graph", [][]key.Binding{
			// Sharpest omission of the three: re-rooting answers "is already
			// the root — move the selection first" while no listed key moved
			// it. onGraphKey has always handled the arrows.
			{k.Up, k.Down, k.Left, k.Right},
			{k.GraphRoot, k.GraphRadius, k.GraphOrient},
			{k.JumpBack, k.PeekScroll, k.Map},
			{k.View, k.Cancel},
		}},
	}
}

// The per-mode footers (move, edit, graph) lived here. They are gone: every
// mode already names its own exits where the eye is. Move and drag put
// "⏎ commit · esc restore" in the status line, the edit overlay carries its
// per-stage hints inside the box, and the graph's status line spells out
// "⏎ re-roots · z cycles radius · o flips the axis · esc returns".
