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
		Sort:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "sort (table)")),
		Done:      key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "done")),
		Edit:      key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "$EDITOR")),
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
		Graph:       key.NewBinding(key.WithKeys("shift+space", "S"), key.WithHelp("⇧space", "dep graph")),
		GraphRoot:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "re-root here")),
		GraphRadius: key.NewBinding(key.WithKeys("z", "1", "2", "3", "0"), key.WithHelp("z/1-3/0", "hop radius")),
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
func (k keyMap) HelpSections() []helpSection {
	return []helpSection{
		{"normal mode", [][]key.Binding{
			{k.Up, k.Down, k.Left, k.Right, k.NextCol, k.PrevCol, k.Top, k.Bottom},
			{k.Move, k.QuickUp, k.QuickDown, k.LaneBack, k.LaneFwd, k.Done, k.Edit, k.Add},
			{k.Peek, k.Tree, k.PeekScroll, k.Filter, k.OnlyBlock, k.Slice, k.View, k.Sort},
			{k.Graph, k.JumpBlock, k.JumpBack, k.Reload, k.Sync, k.Mouse, k.Cancel, k.Help, k.Quit},
		}},
		{"move mode", [][]key.Binding{
			{k.Commit, k.Cancel},
			{k.MoveTop, k.MoveBottom},
			{k.MoveFirst, k.MoveLast},
		}},
		// The graph's full surface, not just its two custom bindings: sectioning
		// turned this block into "your keys right now", so listing 2 of the ~10
		// keys — and no way out — was an assertion, not an omission.
		{"graph", [][]key.Binding{
			{k.GraphRoot, k.GraphRadius},
			{k.JumpBack, k.PeekScroll},
			{k.View, k.Cancel},
		}},
	}
}

// The per-mode footers (move, edit, graph) lived here. They are gone: every
// mode already names its own exits where the eye is. Move and drag put
// "⏎ commit · esc restore" in the status line, the edit overlay carries its
// per-stage hints inside the box, and the graph's status line spells out
// "⏎ re-roots · z cycles radius · esc returns".
