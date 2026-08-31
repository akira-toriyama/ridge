package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/store/memstore"
	"github.com/akira-toriyama/ridge/internal/views"
)

func pressKey(m *Model, r rune) tea.Cmd {
	return m.onKey(tea.KeyPressMsg{Code: r, Text: string(r)})
}

// TestViewVocabulariesStayMapped pins the ui enums to the views package's
// on-disk vocabulary in both directions: a value views.Load accepts that the
// ui cannot express (or the reverse) is exactly the drift this test exists
// to catch.
func TestViewVocabulariesStayMapped(t *testing.T) {
	for _, lay := range views.Layouts {
		kind := viewKindOf(lay)
		back, ok := layoutOf(kind)
		if !ok || back != lay {
			t.Errorf("layout %q: viewKindOf→layoutOf = (%q, %v)", lay, back, ok)
		}
	}
	for _, key := range views.SortKeys {
		k, asc, ok := parseSort(key)
		if !ok || k <= sortCanonical {
			t.Fatalf("sort key %q: parseSort = (%v, %v, %v)", key, k, asc, ok)
		}
		if asc != k.naturalAsc() {
			t.Errorf("sort key %q: bare spelling got asc=%v, want the natural %v", key, asc, k.naturalAsc())
		}
		spelled := formatSort(k, asc)
		k2, asc2, ok2 := parseSort(spelled)
		if !ok2 || k2 != k || asc2 != asc {
			t.Errorf("sort key %q: canonical %q did not round-trip", key, spelled)
		}
	}
	for _, f := range views.SliceFields {
		sf, ok := sliceFieldOf(f)
		if !ok || sf.String() != f {
			t.Errorf("slice field %q: sliceFieldOf = (%v, %v)", f, sf, ok)
		}
	}
}

func TestSwitchViewAppliesTheWholeBundle(t *testing.T) {
	m := New(memstore.New(), Options{Views: []views.View{
		{Name: "表", Layout: "table", Q: "label:bbq", Sort: "due", Slice: "label:bbq"},
	}})
	if c := pressKey(m, '1'); c != nil {
		c()
	}
	if m.view != viewTable {
		t.Fatalf("layout: got %v, want table", m.view)
	}
	if m.qRaw != "label:bbq" || m.ti.Value() != "label:bbq" {
		t.Errorf("q: got %q (input %q), want label:bbq in both", m.qRaw, m.ti.Value())
	}
	if m.tableSort != sortDue || !m.tableSortAsc {
		t.Errorf("sort: got %v asc=%v, want due's natural asc", m.tableSort, m.tableSortAsc)
	}
	if m.sliceField != sliceLabel || m.sliceVal != "bbq" {
		t.Errorf("slice: got %v:%q, want label:bbq", m.sliceField, m.sliceVal)
	}
	if m.viewIdx != 0 {
		t.Errorf("viewIdx: got %d, want 0", m.viewIdx)
	}
	if m.viewDirty() {
		t.Error("a freshly applied view is already dirty")
	}

	// Re-pressing the active tab's digit re-APPLIES (it is assignment, not
	// the panel's radio toggle): the slice must survive, not clear.
	if c := pressKey(m, '1'); c != nil {
		c()
	}
	if m.sliceVal != "bbq" {
		t.Errorf("re-applying the active view cleared the slice: %q", m.sliceVal)
	}
}

func TestSwitchViewReachesAndLeavesTheRoadmap(t *testing.T) {
	m := New(memstore.New(), Options{Views: []views.View{
		{Name: "締切", Layout: "roadmap"},
		{Name: "盤", Layout: "board", Q: "label:bbq"},
	}})
	if c := pressKey(m, '1'); c != nil {
		c()
	}
	if m.view != viewRoadmap {
		t.Fatalf("got %v, want the roadmap", m.view)
	}
	// The switch keys are bound INSIDE the roadmap too (its title row shows
	// the strip), so tab 2 must lead straight back out.
	if c := pressKey(m, '2'); c != nil {
		c()
	}
	if m.view != viewBoard || m.qRaw != "label:bbq" || m.viewIdx != 1 {
		t.Fatalf("leaving by tab: view=%v q=%q idx=%d, want board/label:bbq/1", m.view, m.qRaw, m.viewIdx)
	}
}

func TestSwitchViewOutOfRangeSaysWhy(t *testing.T) {
	m := New(memstore.New(), Options{})
	if c := pressKey(m, '4'); c != nil {
		c()
	}
	if !strings.Contains(m.status, "no saved views") {
		t.Errorf("no views: status %q does not say why the key did nothing", m.status)
	}

	m = New(memstore.New(), Options{Views: demoViews()})
	if c := pressKey(m, '9'); c != nil {
		c()
	}
	if !strings.Contains(m.status, "no view 9") {
		t.Errorf("out of range: status %q does not say why", m.status)
	}
}

// TestViewDirtyTracksEveryDimension drifts each of the four saved dimensions
// in turn and re-applies the tab between them: the dot must rise on every
// one and fall on every re-apply.
func TestViewDirtyTracksEveryDimension(t *testing.T) {
	m := New(memstore.New(), Options{Views: []views.View{
		{Name: "基準", Layout: "table", Q: "label:bbq", Sort: "due", Slice: "label:bbq"},
	}})
	reapply := func() {
		if c := pressKey(m, '1'); c != nil {
			c()
		}
		if m.viewDirty() {
			t.Fatal("re-applying the tab did not clear the drift")
		}
	}
	reapply()

	if c := m.applyFilter("label:bbq is:blocked"); c != nil {
		c()
	}
	if !m.viewDirty() {
		t.Error("a q edit did not dirty the view")
	}
	reapply()

	if c := pressKey(m, 'v'); c != nil { // table → board
		c()
	}
	if !m.viewDirty() {
		t.Error("a layout toggle did not dirty the view")
	}
	reapply()

	m.cycleSort() // due asc → due desc
	if !m.viewDirty() {
		t.Error("a sort flip did not dirty the view")
	}
	reapply()

	if c := m.selectSlice(sliceLabel, "bbq"); c != nil { // radio: re-select clears
		c()
	}
	if !m.viewDirty() {
		t.Error("clearing the slice did not dirty the view")
	}
	reapply()
}

func TestSaveViewUpdatesTheActiveTab(t *testing.T) {
	var got [][]views.View
	m := New(memstore.New(), Options{
		Views:     []views.View{{Name: "表", Layout: "table", Sort: "due"}},
		SaveViews: func(vs []views.View) error { got = append(got, vs); return nil },
	})
	if c := pressKey(m, '1'); c != nil {
		c()
	}
	m.cycleSort() // drift: due asc → due desc
	if !m.viewDirty() {
		t.Fatal("setup: expected drift before the save")
	}
	if c := pressKey(m, 'V'); c != nil {
		c()
	}
	if len(got) != 1 {
		t.Fatalf("save calls: got %d, want 1", len(got))
	}
	want := views.View{Name: "表", Layout: "table", Sort: "due desc"}
	if got[0][0] != want {
		t.Errorf("saved bundle: got %+v, want %+v", got[0][0], want)
	}
	if m.viewDirty() {
		t.Error("the view is still dirty after its own save")
	}
	if !strings.Contains(m.status, "saved") {
		t.Errorf("status %q does not confirm the save", m.status)
	}
}

func TestSaveViewCreatesTheFirstTab(t *testing.T) {
	var got [][]views.View
	m := New(memstore.New(), Options{
		SaveViews: func(vs []views.View) error { got = append(got, vs); return nil },
	})
	if c := m.applyFilter("label:bbq"); c != nil {
		c()
	}
	if c := pressKey(m, 'V'); c != nil {
		c()
	}
	if len(got) != 1 || len(got[0]) != 1 {
		t.Fatalf("save calls: got %v, want one call with one view", got)
	}
	if got[0][0].Name != "view 1" || got[0][0].Q != "label:bbq" || got[0][0].Layout != "board" {
		t.Errorf("created view: got %+v", got[0][0])
	}
	if m.viewIdx != 0 || len(m.views) != 1 {
		t.Errorf("model after create: idx=%d len=%d, want 0/1", m.viewIdx, len(m.views))
	}
	if !strings.Contains(m.status, `"view 1"`) || !strings.Contains(m.status, "views.toml") {
		t.Errorf("status %q does not name the new tab and where to rename it", m.status)
	}
}

func TestSaveViewRefusesWithoutAStore(t *testing.T) {
	m := New(memstore.New(), Options{}) // fixture: SaveViews nil
	if c := pressKey(m, 'V'); c != nil {
		c()
	}
	if !m.statusErr || !strings.Contains(m.status, "views.toml") {
		t.Errorf("V on the fixture: status (%q, err=%v) does not refuse loudly", m.status, m.statusErr)
	}
}

// TestSaveViewFailureLeavesNoPhantomTab: a refused write must leave no tab
// the file does not have.
func TestSaveViewFailureLeavesNoPhantomTab(t *testing.T) {
	m := New(memstore.New(), Options{
		SaveViews: func([]views.View) error { return errors.New("disk full") },
	})
	if c := pressKey(m, 'V'); c != nil {
		c()
	}
	if len(m.views) != 0 || m.viewIdx != -1 {
		t.Errorf("failed save mutated the model: %d view(s), idx %d", len(m.views), m.viewIdx)
	}
	if !m.statusErr || !strings.Contains(m.status, "disk full") {
		t.Errorf("status (%q, err=%v) does not surface the write failure", m.status, m.statusErr)
	}
}

func TestSaveViewInsideTheRoadmap(t *testing.T) {
	var got [][]views.View
	m := New(memstore.New(), Options{
		SaveViews: func(vs []views.View) error { got = append(got, vs); return nil },
	})
	m.openRoadmap()
	if c := pressKey(m, 'V'); c != nil {
		c()
	}
	if len(got) != 1 || got[0][0].Layout != "roadmap" {
		t.Fatalf("V inside the roadmap: got %v, want one roadmap-layout view", got)
	}
}

func TestViewTabStripShowsDigitsNamesAndTheDirtyDot(t *testing.T) {
	m := New(memstore.New(), Options{Views: demoViews()})
	strip := ansiStrip(m.viewTabStrip(200))
	for _, want := range []string{"1 火の粉", "2 締切", "3 表で総覧"} {
		if !strings.Contains(strip, want) {
			t.Errorf("strip %q is missing %q", strip, want)
		}
	}
	if strings.Contains(strip, glyphViewDirty) {
		t.Errorf("strip %q carries a dirty dot with no active tab", strip)
	}
	if c := pressKey(m, '3'); c != nil {
		c()
	}
	m.cycleSort()
	if strip := ansiStrip(m.viewTabStrip(200)); !strings.Contains(strip, "表で総覧"+glyphViewDirty) {
		t.Errorf("strip %q does not dot the drifted active tab", strip)
	}

	if s := New(memstore.New(), Options{}).viewTabStrip(200); s != "" {
		t.Errorf("strip with no views: got %q, want \"\" (no chrome without keys behind it)", s)
	}
}

// TestViewTabStripNeverElidesTheActiveTab: joinEnds truncates a row's LEFT
// end, which is exactly where an unbudgeted strip put the lit tab and its
// dot — the two signals with no other surface (found by review). The strip
// must spend its budget outward from the active tab and mark what it drops.
func TestViewTabStripNeverElidesTheActiveTab(t *testing.T) {
	nine := make([]views.View, 9)
	for i := range nine {
		nine[i] = views.View{Name: fmt.Sprintf("保存済みビューの長い名前%d", i+1)}
	}
	m := New(memstore.New(), Options{Views: nine})
	if c := pressKey(m, '9'); c != nil {
		c()
	}
	if c := m.applyFilter("label:bbq"); c != nil { // drift: the dot must ride along
		c()
	}
	for _, budget := range []int{40, 60, 90, 130, 400} {
		strip := ansiStrip(m.viewTabStrip(budget))
		if w := lg.Width(m.viewTabStrip(budget)); w > budget {
			t.Errorf("budget %d: strip measures %d cells", budget, w)
		}
		if !strings.Contains(strip, "9 ") || !strings.Contains(strip, glyphViewDirty) {
			t.Errorf("budget %d: active tab 9 or its dot elided: %q", budget, strip)
		}
		if budget <= 130 && !strings.Contains(strip, "+") {
			t.Errorf("budget %d: dropped tabs are unmarked: %q", budget, strip)
		}
	}
	// The whole title row composes without overflow at the design floor.
	frame, err := m.Dump(240, 8, "", true)
	if err != nil {
		t.Fatal(err)
	}
	row := strings.SplitN(frame, "\n", 2)[0]
	if !strings.Contains(row, "9 ") || !strings.Contains(row, glyphViewDirty) {
		t.Errorf("240-col title row lost the active tab or dot:\n%s", row)
	}
	if !strings.Contains(row, "? help") {
		t.Errorf("240-col title row lost its right side:\n%s", row)
	}
}

// TestSwitchViewCarriesTheRoadmapWalk: the two exits from the roadmap must
// agree about the cursor. esc (closeRoadmap) carries the walked-to row; a
// tab switch used to restore the pre-roadmap board cursor instead — and a
// roadmap→roadmap re-press snapped the walk back (found by review; the
// keys.View comment records the same curTask trap).
func TestSwitchViewCarriesTheRoadmapWalk(t *testing.T) {
	m := New(memstore.New(), Options{Views: []views.View{
		{Name: "締切", Layout: "roadmap"},
		{Name: "表", Layout: "table"},
	}})
	if c := pressKey(m, '1'); c != nil {
		c()
	}
	if m.view != viewRoadmap || m.roadLay == nil || len(m.roadLay.Rows) < 2 {
		t.Fatalf("setup: roadmap did not open with rows (lay=%v)", m.roadLay)
	}
	m.roadMove(+1) // walk one row: roadSel now differs from the board cursor
	walked := m.roadSel
	if walked == "" {
		t.Fatal("setup: the walk did not land on a row")
	}

	// roadmap → roadmap (re-press): the walk must survive the rebuild.
	if c := pressKey(m, '1'); c != nil {
		c()
	}
	if m.roadSel != walked {
		t.Errorf("re-pressing the roadmap tab snapped the walk back: got %s, want %s", m.roadSel, walked)
	}

	// roadmap → table: the cursor the user SEES is the walked row.
	if c := pressKey(m, '2'); c != nil {
		c()
	}
	if got := m.curTask(); got == nil || got.ID != walked {
		id := "<nil>"
		if got != nil {
			id = got.ID
		}
		t.Errorf("leaving by tab restored an invisible cursor: got %s, want %s", id, walked)
	}
}

// TestSwitchViewCarriesTheFilterHiddenWalk is the second review's blocking
// find: the roadmap MUTES filter-hidden rows rather than dropping them, so
// roadSel is routinely a task the board cols do not contain — and the first
// fix round-tripped the seed through the board cursor, which loses exactly
// those rows. The seed must travel explicitly.
func TestSwitchViewCarriesTheFilterHiddenWalk(t *testing.T) {
	m := New(memstore.New(), Options{Views: []views.View{
		{Name: "絞", Layout: "roadmap", Q: "label:bbq"},
		{Name: "表", Layout: "table", Q: "label:bbq"},
	}})
	if c := pressKey(m, '1'); c != nil {
		c()
	}
	if m.view != viewRoadmap || m.roadLay == nil || len(m.roadLay.Rows) < 2 {
		t.Fatalf("setup: roadmap did not open with rows")
	}
	// Walk until the cursor stands on a row the filter hides (muted, still
	// walkable) — the fixture's dated set guarantees one under label:bbq.
	hidden := ""
	for range m.roadLay.Rows {
		if m.taskHidden(m.roadSel) {
			hidden = m.roadSel
			break
		}
		m.roadMove(+1)
	}
	if hidden == "" {
		t.Fatal("setup: no filter-hidden dated row on the fixture — the test lost its subject")
	}

	// roadmap → roadmap: the muted row survives the rebuild.
	if c := pressKey(m, '1'); c != nil {
		c()
	}
	if m.roadSel != hidden {
		t.Errorf("re-press snapped the filter-hidden walk back: got %s, want %s", m.roadSel, hidden)
	}

	// roadmap → table under the same filter: prev cannot be shown, and the
	// tab deliberately does NOT pin it past its own filter (a saved view
	// shows exactly its named population — closeRoadmap's esc pin is the
	// other exit's contract, and they diverge on purpose).
	if c := pressKey(m, '2'); c != nil {
		c()
	}
	if len(m.pinned) != 0 {
		t.Errorf("a tab switch smuggled %d pin(s) into the saved view", len(m.pinned))
	}
}

// TestHelpFooterAdvertisesViewKeysOnlyWhereTheyWork: the overlay composites
// in every full-screen view, and 1-9/V are dead in the graph/map/boxes —
// the t-84r1 class this repo pins. Scoped to the FOOTER line on purpose:
// the sectioned key rows are read under the mode that answers them
// (HelpSections' doctrine) and render everywhere unconditionally.
func TestHelpFooterAdvertisesViewKeysOnlyWhereTheyWork(t *testing.T) {
	const marker = "saved views (1-9/V)"
	m := New(memstore.New(), Options{})
	m.fullHelp = true
	m.w, m.h = 240, 50

	frame := func() string { return ansiStrip(m.View().Content) }
	if !strings.Contains(frame(), marker) {
		t.Errorf("board help lacks %q", marker)
	}
	m.openMap("")
	if strings.Contains(frame(), marker) {
		t.Errorf("dep-map help advertises %q where the keys are dead", marker)
	}
	m.view = viewBoard
	m.openRoadmap()
	if !strings.Contains(frame(), marker) {
		t.Errorf("roadmap help lacks %q — the keys DO work there", marker)
	}
}

// TestSaveViewRefusesATenthTab: the digits are the only way to reach a tab,
// so a tenth would be born unreachable with the session parked on it.
func TestSaveViewRefusesATenthTab(t *testing.T) {
	nine := make([]views.View, 9)
	for i := range nine {
		nine[i] = views.View{Name: fmt.Sprintf("v%d", i+1)}
	}
	calls := 0
	m := New(memstore.New(), Options{
		Views:     nine,
		SaveViews: func([]views.View) error { calls++; return nil },
	})
	if c := pressKey(m, 'V'); c != nil {
		c()
	}
	if calls != 0 || len(m.views) != 9 || m.viewIdx != -1 {
		t.Errorf("tenth-tab save went through: calls=%d len=%d idx=%d", calls, len(m.views), m.viewIdx)
	}
	if !m.statusErr || !strings.Contains(m.status, "digit budget") {
		t.Errorf("status (%q, err=%v) does not refuse with the reason", m.status, m.statusErr)
	}
}

// TestFixtureEmptyViewsNoteDoesNotAdvertiseV: the two messages one keystroke
// apart must not contradict — a session that refuses V must not sell it.
func TestFixtureEmptyViewsNoteDoesNotAdvertiseV(t *testing.T) {
	m := New(memstore.New(), Options{}) // SaveViews nil
	if c := pressKey(m, '1'); c != nil {
		c()
	}
	if strings.Contains(m.status, "V saves") {
		t.Errorf("fixture note %q advertises the V it will refuse", m.status)
	}
	m = New(memstore.New(), Options{SaveViews: func([]views.View) error { return nil }})
	if c := pressKey(m, '1'); c != nil {
		c()
	}
	if !strings.Contains(m.status, "V saves") {
		t.Errorf("real-session note %q should teach V", m.status)
	}
}

// TestNewSurfacesViewWarnings: a clamped views.toml must say so — but never
// over the read-only warning, which is set once and restored by nothing.
func TestNewSurfacesViewWarnings(t *testing.T) {
	m := New(memstore.New(), Options{ViewWarnings: []string{`view 1 (x): unknown layout "graph"`}})
	if !m.statusErr || !strings.Contains(m.status, "views.toml") {
		t.Errorf("status (%q, err=%v) does not surface the clamp warning", m.status, m.statusErr)
	}

	m = New(memstore.NewGated("board-behind"), Options{ViewWarnings: []string{"view 1 (x): bad"}})
	if !strings.Contains(m.status, "read-only") {
		t.Errorf("status %q lost the read-only warning to a views warning", m.status)
	}
}
