package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// sliceOnEpicAxis opens the panel, puts it on the epic axis and lands the cursor
// on a box — the state every epic gesture starts from.
func sliceOnEpicAxis(t *testing.T, m *Model, id string) {
	t.Helper()
	m.toggleSlice()
	m.sliceField = sliceEpic
	for i, r := range m.sliceRows() {
		if r.value == id {
			m.sliceIdx = i
			return
		}
	}
	t.Fatalf("%s is not a row on the epic axis", id)
}

// Neither epic key may be a SILENT no-op on the axes where it cannot act:
// onSliceKey has no default case, so an axis-conditional binding would be a
// dead key — the failure ^d/^u was fixed for (t-84r1).
func TestEpicKeysExplainThemselvesOnTheOtherAxes(t *testing.T) {
	for _, axis := range []struct {
		name  string
		field sliceField
	}{{"repo", sliceRepo}, {"label", sliceLabel}} {
		for _, k := range []string{"m", "A"} {
			m := boardModel(t, 240, 50)
			m.toggleSlice()
			m.sliceField = axis.field
			m.status, m.statusErr = "", false
			press(m, k)
			if m.mode == modeEpic {
				t.Errorf("%s on the %s axis opened the epic overlay", k, axis.name)
			}
			if m.status == "" {
				t.Errorf("%s on the %s axis did nothing and said nothing", k, axis.name)
			}
			if !strings.Contains(m.status, "epic axis") {
				t.Errorf("%s on the %s axis said %q — it must name the axis that answers it",
					k, axis.name, m.status)
			}
		}
	}
}

// The panel's note is the ONLY place m/A can be advertised: the panel is modal,
// so `?` cannot be typed inside it and HelpSections omits the modals. Written
// once at open, the note went stale on the first tab.
func TestSliceNoteAdvertisesTheEpicKeysOnlyOnTheEpicAxis(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.toggleSlice() // opens on the repo axis
	if strings.Contains(m.status, "new box") {
		t.Errorf("the repo axis advertises a box key: %q", m.status)
	}
	for m.sliceField != sliceEpic {
		m.cycleSliceField(+1)
	}
	if !strings.Contains(m.status, "manages the box") || !strings.Contains(m.status, "new box") {
		t.Errorf("the epic axis note does not advertise m/A: %q", m.status)
	}
	m.cycleSliceField(+1)
	if strings.Contains(m.status, "manages the box") {
		t.Errorf("the note survived the axis switch: %q", m.status)
	}
}

// The overlay holds the box's ID, not the panel row index: furrow orders
// `epic ls` active-first, so the very write the overlay issues can move the row.
func TestEpicOverlayHoldsTheIDAcrossAReorderedPanel(t *testing.T) {
	m := boardModel(t, 240, 50)
	sliceOnEpicAxis(t, m, "e-c4mt")
	press(m, "m")
	if m.mode != modeEpic || m.epic == nil {
		t.Fatalf("m did not open the overlay (mode=%v)", m.mode)
	}
	if m.epic.id != "e-c4mt" {
		t.Fatalf("the overlay opened on %q, want e-c4mt", m.epic.id)
	}

	// Simulate the row moving: the cursor now points somewhere else entirely.
	m.sliceIdx = 0
	if got := m.epicBox(); got == nil || got.ID != "e-c4mt" {
		t.Errorf("the overlay followed the cursor instead of holding its id: %+v", got)
	}
}

// esc from the menu returns to the PANEL, which is still open and still where
// the cursor is. modeNormal would leave it rendered but unfocused.
func TestEpicOverlayEscReturnsToTheSlicePanel(t *testing.T) {
	m := boardModel(t, 240, 50)
	sliceOnEpicAxis(t, m, "e-c4mt")
	press(m, "m", "esc")
	if m.epic != nil {
		t.Error("esc left the overlay state behind")
	}
	if m.mode != modeSlice {
		t.Errorf("esc landed in mode %v, want modeSlice — the panel is still open", m.mode)
	}
	// The status row must describe the PANEL again. The overlay's own note says
	// "⏎ pick a field", which in the panel is false — there ⏎ slices — and it is
	// also the only place the panel's m/A keys are advertised.
	if strings.Contains(m.status, "pick a field") || !strings.Contains(m.status, "slice by epic") {
		t.Errorf("status = %q — esc left the overlay's note over the panel", m.status)
	}
}

// The activate row states its precondition BEFORE the keystroke. furrow refuses
// a second active box for a repo rather than swapping it in, so a UI that only
// showed "no" would make exit 2 the first news of the rule.
func TestEpicActiveCellNamesThePrecondition(t *testing.T) {
	m := boardModel(t, 240, 50)

	if got := m.epicActiveCell(m.b.Epic("e-fw2m")); got != "yes" {
		t.Errorf("the active box reads %q, want yes", got)
	}
	// e-c4mt shares e-fw2m's repo, so its slot is taken.
	if got := m.epicActiveCell(m.b.Epic("e-c4mt")); !strings.Contains(got, "e-fw2m") {
		t.Errorf("a slot-blocked box reads %q — it must name the incumbent", got)
	}
	// A box with no repos cannot be activated at all, and the reason differs.
	noRepo := board.EpicInfo{ID: "e-bare", Title: "repo なし"}
	if got := m.epicActiveCell(&noRepo); !strings.Contains(got, "repo") {
		t.Errorf("a repo-less box reads %q — it must say a repo is needed first", got)
	}
	// A free slot says just "no". This case must use a box that is really ON the
	// board: ActiveHolder returns "" early for an id it cannot resolve, so a
	// synthetic EpicInfo would pass here even with the repo-overlap check
	// deleted. e-p3dx's repo is the one no active box holds.
	free := m.b.Epic("e-p3dx")
	if free == nil || len(free.Repos) == 0 {
		t.Fatal("setup: e-p3dx must be an on-board box with a repo")
	}
	if got := m.epicActiveCell(free); got != "no" {
		t.Errorf("a box with a free slot reads %q, want no", got)
	}
	// And the overlap check itself: the holder is found through a SHARED repo,
	// not by being active anywhere.
	if got := m.b.ActiveHolder("e-p3dx"); got != "" {
		t.Errorf("ActiveHolder(e-p3dx) = %q, want none — its repo is not held", got)
	}
	if got := m.b.ActiveHolder("e-9wtv"); got != "e-fw2m" {
		t.Errorf("ActiveHolder(e-9wtv) = %q, want e-fw2m — they share tomo/kyushu-trip", got)
	}
	if got := m.b.ActiveHolder("e-nope"); got != "" {
		t.Errorf("ActiveHolder on an unknown id = %q, want none", got)
	}
}

// The list sub-editor: which way each row toggles, and the meta key=value parse.
// All four fields were reachable with zero coverage.
func TestEpicListSubEditorTogglesAndParses(t *testing.T) {
	newOverlay := func(t *testing.T, f epicField) (*Model, *scriptedProvider) {
		t.Helper()
		p := newScriptedProvider(scriptedEpicBoard)
		m := New(p, Options{})
		m.w, m.h = 240, 50
		m.recompute()
		sliceOnEpicAxis(t, m, "e-one")
		press(m, "m")
		m.epic.menuIdx = int(f)
		if c := m.openEpicField(f); c != nil {
			_ = c
		}
		return m, p
	}

	// A label the box does NOT carry toggles ON; one it carries toggles OFF.
	t.Run("labels", func(t *testing.T) {
		m, p := newOverlay(t, epicFieldLabels)
		rows := m.epicListRows(m.b.Epic("e-one"))
		if len(rows) == 0 {
			t.Skip("the scripted board has no label vocabulary")
		}
		m.epic.listIdx = 0
		cmd := m.epicListSelect(m.b.Epic("e-one"), rows)
		if cmd == nil {
			t.Fatal("selecting a label queued no write")
		}
		cmd()
		if len(p.calls) != 1 || !strings.HasPrefix(p.calls[0], "epicset ") {
			t.Errorf("calls = %v, want one epicset", p.calls)
		}
	})

	// A dep row points ONE way: selecting removes.
	t.Run("deps remove", func(t *testing.T) {
		m, p := newOverlay(t, epicFieldDeps)
		if err := p.EpicDepAdd("e-one", "e-two"); err != nil {
			t.Fatal(err)
		}
		p.calls = nil
		// The scripted provider records rather than applies, so seed the edge on
		// the board the overlay reads.
		m.b.Epic("e-one").Deps = []string{"e-two"}
		rows := m.epicListRows(m.b.Epic("e-one"))
		if len(rows) != 1 || rows[0] != "e-two" {
			t.Fatalf("rows = %v, want [e-two]", rows)
		}
		cmd := m.epicListSelect(m.b.Epic("e-one"), rows)
		if cmd == nil {
			t.Fatal("selecting a dep queued no write")
		}
		cmd() // the provider is only reached when the queue's Cmd runs
		if len(p.calls) != 1 || p.calls[0] != "epicdeprm e-one e-two" {
			t.Errorf("calls = %v, want the REMOVAL — a dep row cannot re-add", p.calls)
		}
	})

	// meta wants key=value, splits on the FIRST `=`, and refuses anything else.
	t.Run("meta parse", func(t *testing.T) {
		m, _ := newOverlay(t, epicFieldMeta)
		if c := m.startEpicInput(epicInputNewMeta, "", ""); c != nil {
			_ = c
		}
		m.epic.input.SetValue("キーだけ")
		if cmd := m.onEpicInputKey(keyMsg("enter")); cmd != nil {
			t.Error("a meta value with no = queued a write")
		}
		if !m.statusErr {
			t.Error("a malformed meta entry was refused silently")
		}

		m.status, m.statusErr = "", false
		if c := m.startEpicInput(epicInputNewMeta, "", ""); c != nil {
			_ = c
		}
		m.epic.input.SetValue("note=a=b c")
		if cmd := m.onEpicInputKey(keyMsg("enter")); cmd == nil {
			t.Fatal("a valid k=v queued no write")
		}
		if m.statusErr {
			t.Errorf("a valid k=v was refused: %q", m.status)
		}
	})
}

// The list cursor must not be left past the end of a list the store is about to
// shrink — the rows only change when the re-read lands, ~150ms later.
// Removing a row is STORE-FIRST: the overlay renders the old rows for the
// whole round trip, so the cursor must hold its row until the re-read
// actually shrinks the list — recompute owns the pull-back. The old
// gesture-time clamp got both halves wrong: it moved the cursor while both
// rows were still on screen, and it ran before refuseWhileWriting, so a
// refused gesture still re-aimed the next ⏎ at a different row (found by
// review).
func TestEpicListCursorPullsBackWhenTheReReadShrinksTheRows(t *testing.T) {
	m, _ := storeFirstModel(t)
	sliceOnEpicAxis(t, m, "e-one")
	press(m, "m")
	m.b.Epic("e-one").Deps = []string{"e-two", "e-three"}
	m.epic.menuIdx = int(epicFieldDeps)
	if c := m.openEpicField(epicFieldDeps); c != nil {
		_ = c
	}

	// Cursor on the LAST row, the one being removed.
	m.epic.listIdx = 1
	rows := m.epicListRows(m.b.Epic("e-one"))
	if c := m.epicListSelect(m.b.Epic("e-one"), rows); c == nil {
		t.Fatal("the removal queued no write")
	}
	if m.epic.listIdx != 1 {
		t.Errorf("listIdx = %d — the cursor moved while the overlay still renders both rows", m.epic.listIdx)
	}

	// The re-read lands: the edge is gone and recompute pulls the cursor in.
	m.b.Epic("e-one").Deps = []string{"e-two"}
	m.recompute()
	if m.epic.listIdx != 0 {
		t.Errorf("listIdx = %d after the re-read shrank the rows, want 0", m.epic.listIdx)
	}
}

// The arm the review actually caught: labels rows are vocabUnion(task vocab,
// box labels), so a box label no open task carries IS its own row — removing
// it shrinks the list exactly like a dep, and this arm had no clamp at all.
// The cursor sat past the end after the re-read: no ▌ on any row, ⏎/x
// silently dead until ↑/↓.
func TestEpicLabelRemovalPullsTheCursorBackOnReRead(t *testing.T) {
	m, _ := storeFirstModel(t)
	sliceOnEpicAxis(t, m, "e-one")
	press(m, "m")
	box := m.b.Epic("e-one")
	// TWO box-only labels: the scripted board has no task labels at all, so a
	// single row would leave the cursor at 0 and the clamp unexercised — the
	// first version of this test passed with the fix deleted (found by
	// review). Two rows put the cursor at 1 and the shrink leaves 1 row.
	box.Labels = append(box.Labels, "yyy-box-only", "zzz-box-only")
	m.epic.menuIdx = int(epicFieldLabels)
	if c := m.openEpicField(epicFieldLabels); c != nil {
		_ = c
	}
	rows := m.epicListRows(box)
	if len(rows) != 2 || rows[1] != "zzz-box-only" {
		t.Fatalf("rows = %v — want exactly the two box-only labels", rows)
	}
	m.epic.listIdx = 1
	if c := m.epicListSelect(box, rows); c == nil {
		t.Fatal("the removal queued no write")
	}

	// The re-read lands: the label is gone from the box, so its row is gone.
	box.Labels = box.Labels[:len(box.Labels)-1]
	m.recompute()
	if m.epic.listIdx != 0 {
		t.Errorf("listIdx = %d after the re-read shrank the rows to one, want 0", m.epic.listIdx)
	}
}

// The re-read can land while the overlay is parked in the `a` input; esc
// walks back into the list with the index untouched, so the clamp must not
// be gated on the list stage (found by review — the gated version regressed
// the deps arm the old gesture-time clamp happened to cover).
func TestEpicListCursorClampsEvenWhenTheReReadLandsMidInput(t *testing.T) {
	m, _ := storeFirstModel(t)
	sliceOnEpicAxis(t, m, "e-one")
	press(m, "m")
	m.b.Epic("e-one").Deps = []string{"e-two", "e-three"}
	m.epic.menuIdx = int(epicFieldDeps)
	if c := m.openEpicField(epicFieldDeps); c != nil {
		_ = c
	}
	m.epic.listIdx = 1
	rows := m.epicListRows(m.b.Epic("e-one"))
	if c := m.epicListSelect(m.b.Epic("e-one"), rows); c == nil {
		t.Fatal("the removal queued no write")
	}
	// `a` opens the add-input; the re-read lands while it is focused.
	if c := m.onEpicListKey(keyMsg("a")); c == nil {
		t.Fatal("a did not open the add-input")
	}
	m.b.Epic("e-one").Deps = []string{"e-two"}
	m.recompute()
	// esc restores the list; the cursor must already be back in range.
	if c := m.onEpicInputKey(keyMsg("esc")); c != nil {
		_ = c
	}
	if m.epic.stage != epicList {
		t.Fatalf("esc did not return to the list stage")
	}
	if m.epic.listIdx != 0 {
		t.Errorf("listIdx = %d after a mid-input re-read, want 0", m.epic.listIdx)
	}
}

// A refused gesture must leave the cursor exactly where it was — "nothing
// happened" has to be the whole truth of it.
func TestARefusedEpicListRemovalDoesNotMoveTheCursor(t *testing.T) {
	m, _ := storeFirstModel(t)
	sliceOnEpicAxis(t, m, "e-one")
	press(m, "m")
	m.b.Epic("e-one").Deps = []string{"e-two", "e-three"}
	m.epic.menuIdx = int(epicFieldDeps)
	if c := m.openEpicField(epicFieldDeps); c != nil {
		_ = c
	}
	m.epic.listIdx = 1
	m.storeFirstUnread = true
	rows := m.epicListRows(m.b.Epic("e-one"))
	if c := m.epicListSelect(m.b.Epic("e-one"), rows); c != nil {
		t.Fatal("a refused removal must queue nothing")
	}
	if m.epic.listIdx != 1 {
		t.Errorf("listIdx = %d — the refused gesture moved the cursor", m.epic.listIdx)
	}
}

// Every epic write is STORE-FIRST: the board must not change before furrow
// answers. An optimistic apply here would mean ridge deciding what an epic write
// means, which is the whole thing this family exists to avoid.
func TestEpicWritesApplyNothingLocally(t *testing.T) {
	prov := newScriptedProvider(scriptedEpicBoard)
	m := New(prov, Options{})
	m.w, m.h = 240, 50
	m.recompute()
	sliceOnEpicAxis(t, m, "e-one")
	press(m, "m")
	if m.epic == nil {
		t.Fatal("the overlay did not open")
	}

	// Rename it: type a new title and commit.
	m.epic.menuIdx = int(epicFieldTitle)
	if c := m.openEpicField(epicFieldTitle); c != nil {
		_ = c
	}
	m.epic.input.SetValue("改名した箱")
	cmd := m.onEpicInputKey(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("the rename queued no write")
	}
	if got := m.b.Epic("e-one").Title; got != "最初の箱" {
		t.Errorf("the board changed before furrow answered: title is %q", got)
	}
	if !strings.Contains(m.status, "waiting for furrow") {
		t.Errorf("status = %q — a store-first write must say what it waits for", m.status)
	}

	// A second gesture while that one is in flight is refused, not queued: the
	// overlay's rows still show the pre-write values, so the repeat would be
	// aimed at a board the user cannot see yet.
	before := len(m.pending)
	m.epic.menuIdx = int(epicFieldStanding)
	if c := m.openEpicField(epicFieldStanding); c != nil {
		_ = c
	}
	m.commitEpicConfirm()
	if len(m.pending) != before {
		t.Errorf("a second epic write queued while one was in flight (%d → %d)", before, len(m.pending))
	}
	if !m.statusErr {
		t.Errorf("the refusal was not reported as an error: %q", m.status)
	}
}

// Every write funnel owns the status row. The two list-stage writes used to
// re-note their stage AFTER queueing, which erased the one thing the row had to
// say — that a write is pending and the values on screen are not it yet.
func TestEveryEpicWriteLeavesThePendingWriteOnTheStatusRow(t *testing.T) {
	cases := []struct {
		name  string
		drive func(*Model)
	}{
		{"dep add", func(m *Model) {
			m.epic.menuIdx = int(epicFieldDeps)
			if c := m.openEpicField(epicFieldDeps); c != nil {
				_ = c
			}
			if c := m.startEpicInput(epicInputNewDep, "", ""); c != nil {
				_ = c
			}
			m.epic.input.SetValue("e-two")
			m.onEpicInputKey(keyMsg("enter"))
		}},
		{"meta add", func(m *Model) {
			m.epic.menuIdx = int(epicFieldMeta)
			if c := m.openEpicField(epicFieldMeta); c != nil {
				_ = c
			}
			if c := m.startEpicInput(epicInputNewMeta, "", ""); c != nil {
				_ = c
			}
			m.epic.input.SetValue("origin=テスト")
			m.onEpicInputKey(keyMsg("enter"))
		}},
		{"goal", func(m *Model) {
			m.epic.menuIdx = int(epicFieldGoal)
			if c := m.openEpicField(epicFieldGoal); c != nil {
				_ = c
			}
			m.epic.input.SetValue("新しいゴール")
			m.onEpicInputKey(keyMsg("enter"))
		}},
		{"standing", func(m *Model) {
			m.epic.menuIdx = int(epicFieldStanding)
			if c := m.openEpicField(epicFieldStanding); c != nil {
				_ = c
			}
			m.commitEpicConfirm()
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := storeFirstModel(t)
			sliceOnEpicAxis(t, m, "e-one")
			press(m, "m")
			tc.drive(m)
			if len(m.pending) == 0 {
				t.Fatal("the gesture queued no write")
			}
			if !strings.Contains(m.status, "waiting for furrow") {
				t.Errorf("status = %q — a store-first write must leave the pending "+
					"write on the row, not the stage's key hints", m.status)
			}
		})
	}
}

// The window between the write LANDING and its re-read arriving: the queue is
// empty, but the overlay's rows are still the pre-write ones, so a toggle here
// recomputes from a stale value and a dep removal names an edge furrow already
// dropped.
func TestEpicGestureWaitsForTheReReadAfterAWriteLands(t *testing.T) {
	m, p := storeFirstModel(t)
	sliceOnEpicAxis(t, m, "e-one")
	press(m, "m")

	goal := "着地するゴール"
	cmd := m.epicPatch("goal", board.EpicPatch{Goal: &goal})
	if cmd == nil {
		t.Fatal("the first write did not queue")
	}
	m.onPersistDone(cmd().(persistDoneMsg))
	if m.storeFirstInflight() {
		t.Fatal("setup: the queue should be empty once the write landed")
	}
	if !m.storeFirstUnread {
		t.Fatal("a landed store-first write was not marked unread")
	}

	before := len(p.calls)
	m.status, m.statusErr = "", false
	if c := m.epicPatch("goal", board.EpicPatch{Goal: &goal}); c != nil {
		t.Error("a second gesture queued before the re-read landed")
	}
	if len(p.calls) != before {
		t.Errorf("the store saw another call: %v", p.calls[before:])
	}
	if !m.statusErr || !strings.Contains(m.status, "re-read") {
		t.Errorf("status = %q — the refusal must say what it is waiting for", m.status)
	}

	// Once the re-read lands, gestures work again.
	m.onReloadDone(reloadDoneMsg{})
	if m.storeFirstUnread {
		t.Error("the applied re-read did not clear the window")
	}
	if c := m.epicPatch("goal", board.EpicPatch{Goal: &goal}); c == nil {
		t.Error("the overlay stayed locked after the re-read landed")
	}
}

// Which provider method each gesture reaches, and with what. Nothing pinned this
// before: every assertion went through the board or the status row, so an
// overlay that called EpicSet where it meant EpicActivate — or activated the
// wrong box — was green. scriptedProvider records exactly this.
func TestEachEpicGestureReachesItsOwnProviderCall(t *testing.T) {
	cases := []struct {
		name  string
		want  string
		drive func(*Model)
	}{
		{"retitle", "epicset e-one", func(m *Model) {
			m.epic.menuIdx = int(epicFieldTitle)
			if c := m.openEpicField(epicFieldTitle); c != nil {
				_ = c
			}
			m.epic.input.SetValue("改名した箱")
			m.onEpicInputKey(keyMsg("enter"))
		}},
		{"activate carries the reason", "epicactivate e-one reason=ユーザー依頼", func(m *Model) {
			m.epic.menuIdx = int(epicFieldActive)
			if c := m.openEpicField(epicFieldActive); c != nil {
				_ = c
			}
			m.epic.input.SetValue("ユーザー依頼")
			m.onEpicInputKey(keyMsg("enter"))
		}},
		{"pinned", "epicset e-one", func(m *Model) {
			m.epic.menuIdx = int(epicFieldPinned)
			if c := m.openEpicField(epicFieldPinned); c != nil {
				_ = c
			}
			m.commitEpicConfirm()
		}},
		{"dep add", "epicdep e-one e-two", func(m *Model) {
			m.epic.menuIdx = int(epicFieldDeps)
			if c := m.openEpicField(epicFieldDeps); c != nil {
				_ = c
			}
			if c := m.startEpicInput(epicInputNewDep, "", ""); c != nil {
				_ = c
			}
			m.epic.input.SetValue("e-two")
			m.onEpicInputKey(keyMsg("enter"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, p := storeFirstModel(t)
			sliceOnEpicAxis(t, m, "e-one")
			press(m, "m")
			before := *m.b.Epic("e-one")
			tc.drive(m)

			if len(m.pending) != 1 {
				t.Fatalf("pending = %d, want exactly one queued write", len(m.pending))
			}
			if _, err := m.pending[0].run(); err != nil {
				t.Fatalf("the queued write failed: %v", err)
			}
			if len(p.calls) != 1 || p.calls[0] != tc.want {
				t.Errorf("provider calls = %v, want [%s]", p.calls, tc.want)
			}
			// Store-first for EVERY gesture, not just the retitle: the board must
			// be untouched until the re-read.
			if got := *m.b.Epic("e-one"); got.Title != before.Title || got.Goal != before.Goal ||
				got.Active != before.Active || got.Pinned != before.Pinned ||
				got.Standing != before.Standing || len(got.Deps) != len(before.Deps) ||
				len(got.Labels) != len(before.Labels) || len(got.Repos) != len(before.Repos) {
				t.Errorf("the board changed before furrow answered:\n got %+v\nwant %+v", got, before)
			}
		})
	}
}

// An empty patch would be exit 2 from furrow; it must never reach the queue.
func TestEpicEmptyPatchNeverQueues(t *testing.T) {
	m := boardModel(t, 240, 50)
	sliceOnEpicAxis(t, m, "e-c4mt")
	press(m, "m")
	if cmd := m.epicPatch("noop", board.EpicPatch{}); cmd != nil {
		t.Error("an empty patch queued a write")
	}
	if !m.statusErr {
		t.Error("an empty patch was refused silently")
	}
}

// `epic set --title ""` is exit 2 and has no clearing meaning, unlike --goal "".
func TestEpicTitleRefusesEmptyWhileGoalClears(t *testing.T) {
	m := boardModel(t, 240, 50)
	sliceOnEpicAxis(t, m, "e-c4mt")
	press(m, "m")

	m.epic.menuIdx = int(epicFieldTitle)
	if c := m.openEpicField(epicFieldTitle); c != nil {
		_ = c
	}
	m.epic.input.SetValue("")
	if cmd := m.onEpicInputKey(keyMsg("enter")); cmd != nil {
		t.Error("an empty title queued a write")
	}
	if !m.statusErr {
		t.Error("an empty title was refused silently")
	}

	m.status, m.statusErr = "", false
	m.epic.menuIdx = int(epicFieldGoal)
	if c := m.openEpicField(epicFieldGoal); c != nil {
		_ = c
	}
	m.epic.input.SetValue("")
	if cmd := m.onEpicInputKey(keyMsg("enter")); cmd == nil {
		t.Error(`an empty goal must queue the clear — --goal "" is furrow's clearing form`)
	}
}

// The new-box modal inherits the FILTER's repo — it matters more here than in
// quick add, because a box naming no repo cannot be activated at all. Driven
// through onSliceKey, not enterEpicNew directly: `A` only answers on the epic
// axis, where any repo-axis pick has already been cleared by the axis switch,
// so the typed repo: term is the one route a repo can still arrive by. The
// old slice-derived inheritance was exactly the branch this path can never
// reach — and its test called enterEpicNew() by hand, passing for the wrong
// reason (found by review).
func TestEpicNewInheritsTheFilterRepo(t *testing.T) {
	m := New(memstore.New(), Options{})
	m.w, m.h = 240, 50
	m.recompute()
	m.ti.SetValue("repo:tomo/kyushu-trip")
	m.applyFilter("repo:tomo/kyushu-trip")
	m.toggleSlice()
	m.sliceField = sliceEpic
	if cmd := m.onSliceKey(keyMsg("A")); cmd == nil {
		t.Fatal("A on the epic axis did not focus the new-box input")
	}
	if m.epic == nil || !m.epic.creating {
		t.Fatal("A did not open the new-box modal")
	}
	m.epic.input.SetValue("薪ストーブ導入")
	cmd := m.onEpicNewKey(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("the new box queued no write")
	}
	msg, ok := cmd().(persistDoneMsg)
	if !ok {
		t.Fatal("the new box did not produce a persist result")
	}
	if msg.err != nil {
		t.Fatalf("the fixture refused the box: %v", msg.err)
	}
	m.onPersistDone(msg)

	var got *board.EpicInfo
	for _, e := range m.b.Epics() {
		if e.Title == "薪ストーブ導入" {
			box := e
			got = &box
		}
	}
	if got == nil {
		t.Fatal("the created box is not on the board after the write landed")
	}
	if len(got.Repos) != 1 || got.Repos[0] != "tomo/kyushu-trip" {
		t.Errorf("repos = %v, want the filter's repo inherited", got.Repos)
	}
	if got.Active {
		t.Error("a new box must never be active")
	}
}

// A repo-axis pick must NOT leak into a box created after tabbing to the epic
// axis: cycleSliceField cleared it, and the modal's chip said "no repo" — a
// write carrying the dead pick would disagree with the frame the user read.
func TestEpicNewDoesNotResurrectAClearedRepoSlice(t *testing.T) {
	m := New(memstore.New(), Options{})
	m.w, m.h = 240, 50
	m.recompute()
	m.toggleSlice()
	m.sliceField = sliceRepo
	if c := m.selectSlice(sliceRepo, "tomo/kyushu-trip"); c != nil {
		_ = c
	}
	// tab · tab — the real axis walk repo → label → epic, clearing the pick.
	_ = m.cycleSliceField(+1)
	_ = m.cycleSliceField(+1)
	if m.sliceField != sliceEpic {
		t.Fatalf("two tabs from repo landed on %v, want the epic axis", m.sliceField)
	}
	if cmd := m.onSliceKey(keyMsg("A")); cmd == nil {
		t.Fatal("A on the epic axis did not focus the new-box input")
	}
	m.epic.input.SetValue("素の箱")
	cmd := m.onEpicNewKey(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("the new box queued no write")
	}
	msg, ok := cmd().(persistDoneMsg)
	if !ok || msg.err != nil {
		t.Fatalf("the fixture refused the box: %v", msg.err)
	}
	m.onPersistDone(msg)
	for _, e := range m.b.Epics() {
		if e.Title == "素の箱" {
			if len(e.Repos) != 0 {
				t.Errorf("repos = %v, want none — the cleared pick must stay dead", e.Repos)
			}
			return
		}
	}
	t.Fatal("the created box is not on the board after the write landed")
}

// A refused epic add must hand the typed title back by reopening the modal —
// the write is store-first, so nothing on the board even hints the title
// existed. The quick add has kept this contract since t-74y3; the epic add
// closed the modal before queueing and ate the title (found by review).
func TestARefusedEpicAddReopensTheModalWithTheTitle(t *testing.T) {
	m, p := storeFirstModel(t)
	p.epicErr, p.epicFailAt = errors.New("board is read-only"), 1
	sliceOnEpicAxis(t, m, "e-one")
	if cmd := m.onSliceKey(keyMsg("A")); cmd == nil {
		t.Fatal("A on the epic axis did not focus the new-box input")
	}
	m.epic.input.SetValue("薪ストーブ導入")
	m.epic.newRepo = "tomo/kyushu-trip" // as a repo: filter would have seeded it
	cmd := m.onEpicNewKey(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("the new box queued no write")
	}
	if m.epic != nil {
		t.Fatal("the modal closes while the write is in flight — store-first shows nothing early")
	}
	if c := m.onPersistDone(cmd().(persistDoneMsg)); c != nil {
		_ = c
	}

	if m.mode != modeEpic || m.epic == nil || !m.epic.creating {
		t.Fatal("the refusal must reopen the new-box modal")
	}
	if got := m.epic.input.Value(); got != "薪ストーブ導入" {
		t.Fatalf("reopened title = %q, want the typed text back", got)
	}
	if m.epic.newRepo != "tomo/kyushu-trip" {
		t.Errorf("reopened newRepo = %q — the inherited repo was silently lost", m.epic.newRepo)
	}
	if !m.statusErr {
		t.Error("the refusal must surface as an error")
	}
}

// The reopened modal's own ⏎ must survive the rollback window: the guard has
// to fire BEFORE the modal closes, or the queue's refusal lands with m.epic
// already nil and the title is eaten a second time — exactly on the
// reopened-after-refusal retry (found by review).
func TestEpicAddRetryInsideTheRollbackWindowKeepsTheModal(t *testing.T) {
	m, _ := storeFirstModel(t)
	sliceOnEpicAxis(t, m, "e-one")
	if cmd := m.onSliceKey(keyMsg("A")); cmd == nil {
		t.Fatal("A on the epic axis did not focus the new-box input")
	}
	m.epic.input.SetValue("薪ストーブ導入")
	m.rollingBack = true
	if cmd := m.onEpicNewKey(keyMsg("enter")); cmd != nil {
		t.Fatal("⏎ inside the rollback window must queue nothing")
	}
	if m.mode != modeEpic || m.epic == nil || !m.epic.creating {
		t.Fatal("the refused ⏎ closed the modal")
	}
	if got := m.epic.input.Value(); got != "薪ストーブ導入" {
		t.Fatalf("title = %q — the typed text did not survive the refusal", got)
	}
	if len(m.pending) != 0 {
		t.Fatal("nothing may queue inside the window")
	}
	// The window closes; the same ⏎ now goes through.
	m.rollingBack = false
	if cmd := m.onEpicNewKey(keyMsg("enter")); cmd == nil {
		t.Fatal("⏎ after the window closed did not queue the add")
	}
}

// A refusal landing after the user moved on must not steal the newer mode —
// the reopen arrives ~100ms after the ⏎, and by then another overlay may own
// the keyboard (the quick add pins the same rule).
func TestARefusedEpicAddDoesNotStealANewerMode(t *testing.T) {
	m, p := storeFirstModel(t)
	p.epicErr, p.epicFailAt = errors.New("board is read-only"), 1
	sliceOnEpicAxis(t, m, "e-one")
	if cmd := m.onSliceKey(keyMsg("A")); cmd == nil {
		t.Fatal("A on the epic axis did not focus the new-box input")
	}
	m.epic.input.SetValue("薪ストーブ導入")
	cmd := m.onEpicNewKey(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("the new box queued no write")
	}
	// Before the refusal lands, the user reopens the box overlay on a row.
	press(m, "m")
	if m.epic == nil || m.epic.creating {
		t.Fatal("m did not open the box overlay")
	}
	if c := m.onPersistDone(cmd().(persistDoneMsg)); c != nil {
		_ = c
	}
	if m.epic == nil || m.epic.creating || m.mode != modeEpic {
		t.Error("the reopen stole the overlay the user had moved into")
	}
}

// A reload (or another machine's sync) can drop the box the overlay is holding.
// The next keystroke must close the overlay and say so, not panic.
func TestEpicOverlaySurvivesTheBoxLeavingTheBoard(t *testing.T) {
	prov := newScriptedProvider(scriptedEpicBoard)
	m := New(prov, Options{})
	m.w, m.h = 240, 50
	m.recompute()
	sliceOnEpicAxis(t, m, "e-one")
	press(m, "m")
	if m.epic == nil {
		t.Fatal("the overlay did not open")
	}

	// The store's truth loses the box, and a reconcile lands.
	prov.truth = func() *board.Board {
		return board.NewBoard(
			[]*board.Task{{ID: "t-a", Title: "a", Status: "ready", Priority: 10}},
			board.EpicInfo{ID: "e-two", Title: "二番目の箱", Repos: []string{"lab/lab"}, Active: true},
		)
	}
	if err := prov.Reload(); err != nil {
		t.Fatal(err)
	}
	m.reload()

	press(m, "j") // any key at all
	if m.epic != nil {
		t.Error("the overlay stayed open on a box that is no longer served")
	}
	if !m.statusErr || !strings.Contains(m.status, "e-one") {
		t.Errorf("status = %q — it must name the box that vanished", m.status)
	}
}

// Inside the rollback window every write path refuses, and the refusal must
// survive: the gesture's own "waiting for furrow" note would otherwise erase the
// one message telling the user their board is showing state the store rejected.
func TestEpicWriteRefusedInsideTheRollbackWindowKeepsTheReason(t *testing.T) {
	m, _ := storeFirstModel(t)
	sliceOnEpicAxis(t, m, "e-one")
	press(m, "m")
	if m.epic == nil {
		t.Fatal("the overlay did not open")
	}
	m.rollingBack = true

	goal := "巻き戻し中に書こうとするゴール"
	if cmd := m.epicPatch("goal", board.EpicPatch{Goal: &goal}); cmd != nil {
		t.Error("a write queued inside the rollback window")
	}
	if len(m.pending) != 0 {
		t.Errorf("pending = %d, want 0 — the write must not be queued", len(m.pending))
	}
	if !m.statusErr {
		t.Errorf("status = %q — the refusal must stay an error row", m.status)
	}
	if !strings.Contains(m.status, "rolling back") {
		t.Errorf("status = %q — the rollback reason was overwritten by the gesture's own note", m.status)
	}
}

// The frames a regression could blank. Each string below is the reason its
// -demo exists, so an overlay that renders an empty box still fails here.
func TestEpicDemoFramesCarryWhatTheyExistFor(t *testing.T) {
	cases := []struct {
		demo string
		want []string
	}{
		// The panel: the ▶/◆ lifecycle markers and the keys only its note can
		// advertise (`?` cannot be typed inside a modal).
		{"sliceepic", []string{glyphEpicActive, glyphEpicPinned, "manages the box", "new box"}},
		// The menu: the derived line furrow owns, and the activate precondition.
		{"epic", []string{"box e-c4mt", "0/1 done", "no — slot held by e-fw2m",
			"standing", "pinned", "meta", "origin,season"}},
		// The deps list: BOTH resolutions in one frame — a still-open box
		// resolved to id+progress+title, and a dep furrow already resolved away.
		{"epiclist", []string{"open this box after those close", "e-fw2m", "(6/18)",
			"e-2b7h", "(satisfied)"}},
		// The reason input: it is the confirm step AND the audit trail.
		{"epicreason", []string{"activate — who asked, and why", "ユーザー依頼", "⏎ apply"}},
		// The deactivate gate, reachable only on the active box.
		{"epicconfirm", []string{"box e-fw2m", "deactivate this box", "⏎ confirms"}},
		// The new-box modal PROVES the inherited repo lands in a chip rather
		// than silently on the box. "tomo/kyushu-trip" alone would NOT prove it:
		// the cards behind the overlay carry that repo too, so the marker has to
		// be the chip's own text.
		{"epicnew", []string{"new box", "薪ストーブ導入", "repo tomo/kyushu-trip", "⏎ creates"}},
	}
	for _, tc := range cases {
		t.Run(tc.demo, func(t *testing.T) {
			out := strings.Join(dumpFrame(t, 240, 40, tc.demo), "\n")
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("-demo %s lost %q", tc.demo, want)
				}
			}
		})
	}
}

// scriptedEpicBoard is a two-box board for the store-first assertions: the
// scripted provider records calls and never applies them, which is exactly the
// "furrow has not answered yet" state.
func scriptedEpicBoard() *board.Board {
	return board.NewBoard(
		[]*board.Task{{ID: "t-a", Title: "a", Status: "ready", Priority: 10, Repos: []string{"lab/lab"}}},
		board.EpicInfo{ID: "e-one", Title: "最初の箱", Repos: []string{"lab/lab"}, Total: 1},
		board.EpicInfo{ID: "e-two", Title: "二番目の箱", Repos: []string{"lab/lab"}, Active: true},
	)
}
