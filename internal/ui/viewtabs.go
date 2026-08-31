package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/akira-toriyama/ridge/internal/views"
)

// The saved-view tabs (t-es5v): GitHub Projects' view tabs over the bundle
// ridge already treats as display state — {layout, q, sort, slice}. The
// bundles live in views.toml (internal/views), loaded once at startup and
// written ONLY by the explicit save key: switching a tab is the filter bar,
// the slice panel, the sort key and the view toggle pressed at once, built
// on the same primitives so it cannot disagree with any of them, and it
// never writes anywhere.
//
// The tabs are keyboard-only (1-9 / V) on purpose: the Board|Table strip
// they extend has no click surface either, and the mouse rule only runs the
// other way (every mouse gesture needs a key twin, not every key a click).

// viewTabW is the display-cell budget for one tab NAME (the digit prefix
// rides outside it). Measured, not len(): the names are CJK.
const viewTabW = 14

// glyphViewDirty is GH's unsaved-changes dot on the active tab: the session
// state has drifted from the saved bundle, and V would capture it.
const glyphViewDirty = "●"

// layoutOf maps a viewKind to its saved-layout spelling. The full-screen
// side-trips (graph, map, boxes) have no spelling on purpose — a graph is
// rooted on a task and the other two are population toggles, so none of
// them is a state a NAMED view could reproduce from cold. ok=false marks
// them, and viewDirty treats a side-trip as "layout unchanged" rather than
// as an edit of the view.
func layoutOf(v viewKind) (string, bool) {
	switch v {
	case viewBoard:
		return "board", true
	case viewTable:
		return "table", true
	case viewRoadmap:
		return "roadmap", true
	}
	return "", false
}

func viewKindOf(layout string) viewKind {
	switch layout {
	case "table":
		return viewTable
	case "roadmap":
		return viewRoadmap
	}
	return viewBoard
}

// sliceFieldOf is sliceField.String's inverse over views.SliceFields.
func sliceFieldOf(s string) (sliceField, bool) {
	for f := sliceField(0); f < sliceFieldCount; f++ {
		if f.String() == s {
			return f, true
		}
	}
	return 0, false
}

// parseSort maps the saved "<key>[ asc| desc]" spelling onto the table's
// enum, defaulting a missing direction to the key's natural one — exactly
// what the `o` cycle does on entry. The vocabulary is views.SortKeys;
// TestViewVocabulariesStayMapped pins the two against drift.
func parseSort(s string) (k sortKey, asc, ok bool) {
	key, dir, ok := views.SplitSort(s)
	if !ok {
		return sortCanonical, false, s == ""
	}
	for c := sortCanonical + 1; c < sortKeyEnd; c++ {
		if c.String() == key {
			k = c
		}
	}
	if k == sortNone {
		return sortCanonical, false, false
	}
	switch dir {
	case "asc":
		asc = true
	case "desc":
		asc = false
	default:
		asc = k.naturalAsc()
	}
	return k, asc, true
}

// formatSort is parseSort's inverse in the CANONICAL spelling: direction
// always explicit, so two bundles can be compared as strings without knowing
// any key's natural direction.
func formatSort(k sortKey, asc bool) string {
	if k <= sortCanonical || k >= sortKeyEnd {
		return ""
	}
	if asc {
		return k.String() + " asc"
	}
	return k.String() + " desc"
}

// normalizeView rewrites a saved view into the comparison form currentBundle
// produces: name dropped, layout concrete, sort in the canonical spelling,
// anything unspeakable emptied. views.Load already clamps files, but the
// demo/test injection path does not go through it.
func normalizeView(v views.View) views.View {
	out := views.View{Q: strings.TrimSpace(v.Q)}
	out.Layout = "board"
	if v.Layout == "table" || v.Layout == "roadmap" {
		out.Layout = v.Layout
	}
	if k, asc, ok := parseSort(v.Sort); ok {
		out.Sort = formatSort(k, asc)
	}
	if f, val, found := strings.Cut(v.Slice, ":"); found && val != "" && !strings.Contains(val, `"`) {
		if _, ok := sliceFieldOf(f); ok {
			out.Slice = v.Slice
		}
	}
	return out
}

// currentBundle is the session's display state in views.View spelling, name
// left empty — the thing viewDirty compares and V saves.
func (m *Model) currentBundle() views.View {
	v := views.View{Q: m.qRaw, Sort: formatSort(m.tableSort, m.tableSortAsc)}
	switch lay, ok := layoutOf(m.view); {
	case ok:
		v.Layout = lay
	case m.viewIdx >= 0 && m.viewIdx < len(m.views):
		// A side-trip view is up: the layout dimension reads as unchanged.
		v.Layout = normalizeView(m.views[m.viewIdx]).Layout
	default:
		v.Layout = "board"
	}
	if m.sliceVal != "" {
		v.Slice = m.sliceField.String() + ":" + m.sliceVal
	}
	return v
}

// viewDirty reports the active tab's unsaved-changes state.
func (m *Model) viewDirty() bool {
	if m.viewIdx < 0 || m.viewIdx >= len(m.views) {
		return false
	}
	return m.currentBundle() != normalizeView(m.views[m.viewIdx])
}

// switchView applies saved view i — display state only, no store write, so
// it is safe in every window a filter keystroke is safe in (rollback
// included: verdicts are gated downstream, exactly as they are for typing).
func (m *Model) switchView(i int) tea.Cmd {
	if i < 0 || i >= len(m.views) {
		if len(m.views) == 0 {
			m.note("no saved views — [[view]] tables in ~/.config/ridge/views.toml define the tabs (V saves the first)")
		} else {
			m.note("no view %d — %d view(s) in views.toml", i+1, len(m.views))
		}
		return nil
	}
	// The board re-shapes wholesale under the pointer; a surviving drag would
	// drop into a lane the pointer never visited (toggleSlice's rule).
	m.cancelDrag()
	v := normalizeView(m.views[i])
	prev := m.curTask()
	m.viewIdx = i

	// The slice is ASSIGNED, not selectSlice'd: selection there is a radio
	// toggle, and re-applying the already-active view must stay a no-op
	// rather than un-slice it.
	if v.Slice != "" {
		f, val, _ := strings.Cut(v.Slice, ":")
		if sf, ok := sliceFieldOf(f); ok {
			m.sliceField, m.sliceVal = sf, val
		}
	} else {
		m.sliceVal = ""
	}
	m.sliceIdx, m.sliceOff = 0, 0

	m.qRaw = v.Q
	m.ti.SetValue(v.Q)
	// A view change is a new view: pins were jump/add artifacts of the old
	// one (selectSlice's rule, and applyFilter's when everything empties).
	m.pinned = map[string]bool{}

	if k, asc, ok := parseSort(v.Sort); ok {
		m.tableSort, m.tableSortAsc = k, asc
	}

	// Layout last: the roadmap seeds its cursor from the board selection and
	// builds from the filtered population, so the filter fields must already
	// be in place. On a live store the verdict lands async and the view
	// re-packs when it does — the same contract as typing `/` inside it.
	switch target := viewKindOf(v.Layout); {
	case target == viewRoadmap:
		if s := m.startRoadmap(); s != "" {
			// The seed-fallback sentence (why the cursor moved) still applies.
			m.note("%s", s)
		}
	case m.view != target:
		if m.view == viewRoadmap {
			m.roadMoved = false // leaving by tab is not the walk-carrying esc
		}
		m.view = target
		if target == viewTable {
			m.tableIdx = 0
		}
	}

	cmd := m.refire(prev, false)
	if prev != nil {
		m.selectID(prev.ID, false)
	}
	return cmd
}

// viewTabDigit is the 1-9 keys' index, -1 for anything else.
func viewTabDigit(msg tea.KeyPressMsg) int {
	s := msg.String()
	if len(s) != 1 || s[0] < '1' || s[0] > '9' {
		return -1
	}
	return int(s[0] - '1')
}

// saveView is the V key: the current bundle into the active tab, or a new
// tab when none is active. The write is synchronous — views.toml is one
// small local file, not a furrow exec, so it does not ride the persist
// queue and cannot be rolled back by it.
func (m *Model) saveView() {
	if m.saveViews == nil {
		m.fail("this session has no views.toml — saved views ride the real store, not the fixture")
		return
	}
	b := m.currentBundle()
	created := false
	if m.viewIdx >= 0 && m.viewIdx < len(m.views) {
		b.Name = m.views[m.viewIdx].Name
	} else {
		// GH's "New view" too creates a placeholder name; the file is the
		// rename surface.
		b.Name = fmt.Sprintf("view %d", len(m.views)+1)
		created = true
	}
	// Build the candidate list first and keep the model untouched until the
	// write lands: a refused save must leave no tab the file does not have.
	next := append([]views.View(nil), m.views...)
	if created {
		next = append(next, b)
	} else {
		next[m.viewIdx] = b
	}
	if err := m.saveViews(next); err != nil {
		m.fail("views.toml: %v", err)
		return
	}
	m.views = next
	if created {
		m.viewIdx = len(m.views) - 1
		m.note("saved as %q — name it in ~/.config/ridge/views.toml", b.Name)
		return
	}
	m.note("view %q saved", b.Name)
}

// viewTabStrip is the saved-view half of the tab band, "" when none are
// loaded — chrome must not advertise a surface that has no keys behind it.
// Each tab leads with the digit that reaches it, the active tab is lit, and
// the dirty dot rides only the active one (an inactive tab cannot drift).
func (m *Model) viewTabStrip() string {
	if len(m.views) == 0 {
		return ""
	}
	th := m.th
	parts := make([]string, 0, len(m.views))
	for i, v := range m.views {
		label := ansi.Truncate(v.Name, viewTabW, "…")
		if i < 9 {
			label = fmt.Sprintf("%d %s", i+1, label)
		}
		switch {
		case i == m.viewIdx && m.viewDirty():
			parts = append(parts, th.tabOn.Render(label)+th.accent.Render(glyphViewDirty))
		case i == m.viewIdx:
			parts = append(parts, th.tabOn.Render(label))
		default:
			parts = append(parts, th.tabOff.Render(label))
		}
	}
	return th.crumb.Render("  ·  ") + strings.Join(parts, th.dim.Render(" │ "))
}
