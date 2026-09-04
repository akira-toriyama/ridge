package ui

import (
	"github.com/akira-toriyama/ridge/internal/board"
	"image/color"
	"time"

	lg "charm.land/lipgloss/v2"
)

// Glyphs. Every one of these is asserted single-width by
// TestGlyphsAreSingleWidth: a Wide or Fullwidth glyph would silently shear
// every card's right border on a CJK board, and this fixture IS a CJK board.
//
// East-Asian-AMBIGUOUS glyphs are deliberately allowed. lipgloss measures them
// as 1, several of the ones below (▤ ↕ ▲ ▼ ●) are ambiguous, and so is the
// rounded/thick box-drawing frame the entire UI is built from — so
// "ambiguous renders narrow" is an assumption this codebase already makes
// everywhere and could not drop without redrawing every border.
const (
	glyphActionable = "▸" // furrow next would hand you this one
	glyphBlocked    = "x" // unsatisfied deps
	glyphEpic       = "▤" // a container: a box, not work
	glyphDone       = "v"
	glyphRevisit    = "↻" // furrow revisit flagged this task: worth a fresh look
	glyphOpen       = "o"
	glyphUnknown    = "?" // a dep pointing at an id not on the board
	glyphWIPOver    = "!" // over the (unenforced) WIP limit
	glyphDrop       = "╌" // dashed, so it cannot be read as the header rule
	glyphDropL      = "▸" // the caret brackets make it an insertion POINT
	glyphDropR      = "◂"
	glyphLift       = "↕"
	glyphLaneDot    = "●"
	glyphSortAsc    = "▲" // the table's active sort direction, in its header
	glyphSortDesc   = "▼"
	// glyphEpicActive is `furrow brief`'s own marker for the box a repo is
	// currently working out of — the same character, so the two surfaces read as
	// one vocabulary. glyphEpicPinned likewise for the PERMANENT channel.
	glyphEpicActive = "▶"
	glyphEpicPinned = "◆"

	// The roadmap's two timeline glyphs. glyphDue shares glyphEpicPinned's
	// character on the arrowheads' licence: a diamond on a DATE AXIS is the
	// milestone convention (GH draws its markers so), on an epic row it is
	// the pin, and the two never label the same surface. glyphToday is
	// deliberately NOT the │ the pane separator uses: colour is exactly what
	// -plain strips, so the two verticals must differ by shape.
	glyphDue   = "◆"
	glyphToday = "┊"

	// The two graph arrowheads. Every edge in one picture points the same way —
	// down when the graph runs top-down, right when it runs left-right, always
	// in the direction unblocking flows. Position and arrowhead carry the same
	// fact, which is deliberate redundancy: a reader must never have to
	// remember which way the arrows go.
	//
	// Both triangles are already in the vocabulary for something else
	// (glyphSortDesc, glyphEpicActive). That is not a collision: a directional
	// triangle reads as direction on every surface, and the graph has pointed
	// with glyphSortDesc's character since it was written.
	glyphArrowDown  = '▼'
	glyphArrowRight = '▶'
)

// graphArrow is the arrowhead that terminates an edge in the given orientation.
func graphArrow(o graphOrient) rune {
	if o == orientLeftRight {
		return glyphArrowRight
	}
	return glyphArrowDown
}

// theme is the whole palette. lipgloss v2 removed AdaptiveColor, so the light /
// dark choice is made once, from tea.BackgroundColorMsg, and baked in here.
type theme struct {
	dark bool

	base    lg.Style
	muted   lg.Style
	dim     lg.Style
	accent  lg.Style // move mode / drag
	ok      lg.Style // actionable
	warn    lg.Style
	danger  lg.Style
	invert  lg.Style
	title   lg.Style
	crumb   lg.Style
	rule    lg.Style
	chip    lg.Style
	chipAlt lg.Style

	tabOn     lg.Style
	tabOff    lg.Style
	colHdr    lg.Style
	colHdrOn  lg.Style
	colCount  lg.Style
	colBG     lg.Style
	colBGOn   lg.Style
	chipHues  []lg.Style
	laneHue   map[string]lg.Style
	card      lg.Style
	cardSel   lg.Style
	cardLift  lg.Style
	cardGhost lg.Style
	cardDone  lg.Style
	dropInd   lg.Style
	peek      lg.Style
	peekHdr   lg.Style
	status    lg.Style
	errText   lg.Style

	// The graph view. Node borders carry state REDUNDANTLY with colour —
	// double = the root, thick = the selection — because in -plain, on a
	// 16-colour TTY and for a colourblind reader a hue is not a signal.
	// The one exception is the state the graph opens in: root and selection
	// are the same node, the thick selection ring wins, and only colour
	// separates graphNodeFocusSel from graphNodeSel. That is deliberate —
	// when the two coincide there is nothing to tell apart, and the double
	// ring appears on the first move away.
	edge              lg.Style
	graphNode         lg.Style
	graphNodeSel      lg.Style
	graphNodeFocus    lg.Style
	graphNodeFocusSel lg.Style
	graphNodeDone     lg.Style
	graphNodeUnknown  lg.Style
}

func newTheme(dark bool) *theme {
	c := func(d, l string) color.Color {
		if dark {
			return lg.Color(d)
		}
		return lg.Color(l)
	}
	var (
		fg     = c("#d7d9e0", "#1c1e26")
		muted  = c("#8b8fa3", "#63677a")
		dim    = c("#5b5f72", "#9aa0b4")
		line   = c("#3a3d4d", "#c9ccd8")
		accent = c("#f2a90f", "#b26f00")
		ok     = c("#57d38c", "#12783f")
		danger = c("#f2707a", "#c02c38")
		sel    = c("#7aa2f7", "#2f5fd0")
		panel  = c("#22242e", "#eef0f6")
	)
	t := &theme{dark: dark}
	t.base = lg.NewStyle().Foreground(fg)
	t.muted = lg.NewStyle().Foreground(muted)
	t.dim = lg.NewStyle().Foreground(dim)
	t.accent = lg.NewStyle().Foreground(accent)
	t.ok = lg.NewStyle().Foreground(ok)
	t.warn = lg.NewStyle().Foreground(accent)
	t.danger = lg.NewStyle().Foreground(danger)
	t.invert = lg.NewStyle().Foreground(c("#11131a", "#ffffff")).Background(sel).Bold(true)
	t.title = lg.NewStyle().Foreground(fg).Bold(true)
	t.crumb = lg.NewStyle().Foreground(muted)
	t.rule = lg.NewStyle().Foreground(line)
	t.chip = lg.NewStyle().Foreground(c("#c0c4d6", "#3a3d4d")).Background(c("#2e3140", "#dfe2ec"))
	t.chipAlt = lg.NewStyle().Foreground(sel)

	t.tabOn = lg.NewStyle().Foreground(fg).Bold(true).Underline(true)
	t.tabOff = lg.NewStyle().Foreground(dim)

	t.colHdr = lg.NewStyle().Foreground(muted).Bold(true)
	t.colHdrOn = lg.NewStyle().Foreground(fg).Bold(true)
	// A count pill, not a right-aligned number.
	t.colCount = lg.NewStyle().Foreground(muted).Background(c("#2a2d3a", "#e3e6ef"))
	// The column container: one step off the terminal background, deliberately
	// NOT the `panel` colour the selected card uses, so the two never merge.
	t.colBG = lg.NewStyle().Background(c("#1a1c24", "#f2f4f9"))
	t.colBGOn = lg.NewStyle().Background(c("#1e2029", "#eaeef7"))

	// Per-label hues. One flat grey chip for every label made them all
	// visually identical, which reads as a tag LIST; GitHub's coloured
	// labels are one of the two or three most recognisable things on a card.
	// Hashed by name, so a label keeps its colour on every card and every run.
	for _, h := range [][2]string{
		{"#7aa2f7", "#2f5fd0"}, {"#57d38c", "#12783f"}, {"#f2a90f", "#8a5b00"},
		{"#f2707a", "#c02c38"}, {"#b48ef2", "#6b3fb8"}, {"#4fd1c5", "#0f7a72"},
		{"#e08fc0", "#a83b7d"}, {"#9aa7c7", "#4a5570"},
	} {
		t.chipHues = append(t.chipHues, lg.NewStyle().Foreground(c(h[0], h[1])).Bold(true))
	}
	// Per-lane identity: GitHub derives a coloured dot from the single-select
	// option, which is what lets you find "Done" without reading.
	t.laneHue = map[string]lg.Style{
		"inbox":       lg.NewStyle().Foreground(c("#8b8fa3", "#63677a")),
		"backlog":     lg.NewStyle().Foreground(c("#7aa2f7", "#2f5fd0")),
		"ready":       lg.NewStyle().Foreground(c("#57d38c", "#12783f")),
		"in-progress": lg.NewStyle().Foreground(c("#f2a90f", "#8a5b00")),
		"done":        lg.NewStyle().Foreground(c("#b48ef2", "#6b3fb8")),
		"icebox":      lg.NewStyle().Foreground(c("#5b5f72", "#9aa0b4")),
	}

	cardBase := lg.NewStyle().Border(lg.RoundedBorder()).Padding(0, 1)
	t.card = cardBase.BorderForeground(line).Foreground(fg)
	// A THICK border, not just a blue one: in -plain, on a 16-colour TTY and for
	// a colourblind reader, a hue-only focus ring is byte-identical to its
	// neighbours. Still one cell wide, so every measured height holds.
	t.cardSel = cardBase.Border(lg.ThickBorder()).BorderForeground(sel).Foreground(fg).Background(panel)
	t.cardLift = cardBase.Border(lg.DoubleBorder()).BorderForeground(accent).Foreground(accent)
	t.cardGhost = cardBase.BorderForeground(accent).Foreground(accent).Background(panel)
	t.cardDone = cardBase.BorderForeground(line).Foreground(dim)
	t.dropInd = lg.NewStyle().Foreground(accent).Bold(true)

	t.peek = lg.NewStyle().Border(lg.RoundedBorder()).BorderForeground(sel).
		Background(panel).Foreground(fg).Padding(0, 1)
	t.peekHdr = lg.NewStyle().Foreground(sel).Bold(true)
	t.status = lg.NewStyle().Foreground(muted)
	t.errText = lg.NewStyle().Foreground(danger)

	// Edges are drawn one step brighter than a card border: they are the
	// SUBJECT of the graph view, not its chrome.
	t.edge = lg.NewStyle().Foreground(c("#7c8098", "#6a6f85"))
	graphNodeBase := lg.NewStyle().Border(lg.RoundedBorder()).Padding(0, 1)
	t.graphNode = graphNodeBase.BorderForeground(line).Foreground(fg)
	t.graphNodeSel = graphNodeBase.Border(lg.ThickBorder()).BorderForeground(sel).
		Foreground(fg).Background(panel)
	t.graphNodeFocus = graphNodeBase.Border(lg.DoubleBorder()).BorderForeground(accent).
		Foreground(fg)
	t.graphNodeFocusSel = graphNodeBase.Border(lg.ThickBorder()).BorderForeground(accent).
		Foreground(fg).Background(panel)
	t.graphNodeDone = graphNodeBase.BorderForeground(line).Foreground(dim)
	t.graphNodeUnknown = graphNodeBase.BorderForeground(danger).Foreground(danger)
	return t
}

// chipFor is a label's stable colour: FNV-1a over the name, into a fixed
// palette. Stable per name means `ui` is the same colour on every card and
// across sessions, which is the only thing that makes a colour a LABEL rather
// than decoration.
func (t *theme) chipFor(name string) lg.Style {
	if len(t.chipHues) == 0 {
		return t.chip
	}
	return t.chipHues[chipIndex(name, len(t.chipHues))]
}

// chipIndex maps a label name onto a palette slot: FNV-1a over the name's
// BYTES, modulo n (which every caller must keep positive).
//
// Bytes — not runes, not display cells. The repo rule "measure text with
// lipgloss.Width, never len()" governs LAYOUT and has no jurisdiction over a
// hash: applied here it hashes only a prefix of every multi-byte name, and
// ranging runes feeds code points where the contract says bytes. Both
// recolour CJK labels without failing anything but
// TestChipIndexHashesBytesNotRunesOrCells, which is why n is a parameter:
// that test pins its own, so the palette can grow without going red.
func chipIndex(name string, n int) int {
	h := uint32(2166136261)
	for _, b := range []byte(name) {
		h ^= uint32(b)
		h *= 16777619
	}
	return int(h % uint32(n)) //nolint:gosec // n is a palette length: tiny and positive, nowhere near MaxUint32
}

// laneDot is the column's colour identity, falling back to the muted default
// for a lane the palette does not know.
func (t *theme) laneDot(l board.Lane) lg.Style {
	if s, ok := t.laneHue[l.Name]; ok {
		return s
	}
	return t.dim
}

// nowFn and localZone are indirected so tests get deterministic timestamps and
// a chosen zone. Tests override THESE, never time.Local: time.Now reads
// time.Local from the runtime's timer goroutine, so a test writing it races
// with any timer still alive from an earlier test (measured under -race).
// Both are plain vars read on the UI thread only; a test must not pin them
// while a real tea.Program is running.
var (
	nowFn     = time.Now
	localZone = func() *time.Location { return time.Local }
)
