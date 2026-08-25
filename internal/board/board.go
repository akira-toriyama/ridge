// Package board is the pure core: furrow's task model (Task, Lane, Board,
// EpicInfo), the dependency Graph derived from it, and the Provider port the
// store adapters implement. No I/O, no rendering, no globals beyond the test
// clock — everything here is deterministic and unit-testable.
package board

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// nowFn is indirected so tests get deterministic timestamps.
var nowFn = time.Now

// priorityStep is furrow's sparse-priority spacing: reordering edits one
// integer field rather than renumbering a lane.
const priorityStep = 10

// ChecklistItem mirrors furrow's shard checklist entry.
type ChecklistItem struct {
	Text string
	Done bool
}

// Task mirrors the fields of a furrow task shard that ridge renders. Epics are
// NOT tasks: they are separate entities without a lane (EpicInfo), and a task
// carries only its membership id in Epic.
type Task struct {
	ID        string
	Title     string
	Status    string // the lane
	Priority  int    // sparse, 10-step; order WITHIN the lane
	Value     int    // 1..5
	Effort    int    // 1..5
	Labels    []string
	Repos     []string
	Epic      string // e- id of the box this task is filed under ("" = unfiled)
	Deps      []string
	Refs      []string
	Checklist []ChecklistItem
	Created   time.Time
	Updated   time.Time
	Closed    time.Time
	Reviewed  time.Time
	Due       time.Time // zero = no promise
	Body      string
}

// ShortRepo renders "akira-toriyama/vista" as "vista" for a narrow card.
func (t *Task) ShortRepo() string {
	if len(t.Repos) == 0 {
		return ""
	}
	r := t.Repos[0]
	if i := strings.LastIndex(r, "/"); i >= 0 {
		r = r[i+1:]
	}
	if len(t.Repos) > 1 {
		return fmt.Sprintf("%s+%d", r, len(t.Repos)-1)
	}
	return r
}

// CheckProgress counts the task's own checklist.
func (t *Task) CheckProgress() (done, total int) {
	for _, c := range t.Checklist {
		if c.Done {
			done++
		}
	}
	return done, len(t.Checklist)
}

// Lane is one column of the board.
type Lane struct {
	Name string
	Next bool // one of the lanes `furrow next` considers
	Done bool
	WIP  int // 0 = unset. RENDERED, never enforced — GitHub Projects parity.
}

// DisplayName is the lane as a HUMAN reads it — "In progress", not the
// `in-progress` slug. Presentation only: Name stays the slug everywhere the
// model, the filter grammar and `furrow set --status` are concerned.
func (l Lane) DisplayName() string {
	s := strings.ReplaceAll(l.Name, "-", " ")
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

// boardLanes is the lane vocabulary, in board order.
var boardLanes = []Lane{
	{Name: "inbox"},
	{Name: "backlog"},
	{Name: "ready", Next: true, WIP: 2},
	{Name: "in-progress", Next: true, WIP: 1},
	{Name: "done", Done: true},
	{Name: "icebox"},
}

// EpicInfo is one epic entity as `furrow epic ls --json` reports it. Epics
// have no lane, so they are board-level metadata rather than tasks: cards
// reference them by id (Task.Epic) and render the resolved title.
//
// The fields split three ways, and the split is the contract:
//
//   - Stored — Title, Goal, Labels, Repos, Meta, and the two PERMANENT-channel
//     declarations Standing/Pinned. `furrow epic set` writes exactly these.
//   - Active — at most ONE box per repo, and a box spanning several repos
//     consumes a slot in each. furrow's to enforce, and it REFUSES a second
//     box for a repo rather than stealing the slot, so ridge must never
//     present the flag as a toggle that always lands.
//   - Derived — Done, Total, Stuck, OpenDeps. furrow computes these and ridge
//     consumes them verbatim; recomputing any of them here would be the
//     front-end logic this repo exists to not have — and would silently break
//     the day the epic read gains a scope.
//
// There is no Closed field because the read is OPEN-ONLY (`epic ls` without
// --all): every box that reaches this struct is open. Serving closed boxes is
// t-sq02's, together with the `epic done`/`reopen` pair that needs them.
//
// Deps is the epic-to-epic edge furrow's `epic dep` records: "open this box
// after those close". It is INFORMATION, not enforcement — furrow itself
// warns and proceeds — so ridge renders it and gates nothing on it. OpenDeps
// is the subset still waiting (a dep on a closed epic is simply satisfied),
// and it is absent from the read entirely once nothing waits.
type EpicInfo struct {
	ID    string
	Title string
	Goal  string // the closing condition in one line; "" = none, and furrow does not lint that

	Active   bool
	Standing bool // permanent box: exempt from the finish-shaped nags
	Pinned   bool // its actionable tasks lead next/brief regardless of the active scope

	Labels []string
	Repos  []string
	Meta   map[string]string // free-form; furrow stores it and never interprets it

	Done     int
	Total    int
	Stuck    bool
	Deps     []string
	OpenDeps []string
}

// MetaKeys is the box's meta keys in sorted order. Map iteration is random, so
// every surface that renders or writes meta — the overlay's rows and the argv
// the adapter composes — has to agree on one order or the frame and the write
// both become nondeterministic.
func (e *EpicInfo) MetaKeys() []string {
	keys := make([]string, 0, len(e.Meta))
	for k := range e.Meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ActiveHolder names the box that already holds id's repo slots, "" when none
// does. It reads the served Active/Repos fields — it does NOT re-derive
// furrow's one-active-per-repo rule, which stays furrow's to enforce; the point
// is only to show the incumbent BEFORE the user tries, instead of letting
// furrow's refusal be the first news of it.
func (b *Board) ActiveHolder(id string) string {
	me := b.Epic(id)
	if me == nil {
		return ""
	}
	mine := make(map[string]bool, len(me.Repos))
	for _, r := range me.Repos {
		mine[r] = true
	}
	for _, e := range b.epics {
		if e.ID == id || !e.Active {
			continue
		}
		for _, r := range e.Repos {
			if mine[r] {
				return e.ID
			}
		}
	}
	return ""
}

// Board is the in-memory task set. Lane membership is Task.Status and lane
// ORDER is Task.Priority — there is no separate ordering table to fall out of
// sync with, exactly as in a real furrow store.
type Board struct {
	tasks    []*Task
	lanes    []Lane
	epics    []EpicInfo
	epicByID map[string]*EpicInfo
	writable bool
	schema   string // furrow's schema_state; "" for the fixture
}

// NewBoard builds a fixture-lane board that is always writable.
func NewBoard(tasks []*Task, epics ...EpicInfo) *Board {
	b := &Board{tasks: tasks, lanes: append([]Lane(nil), boardLanes...),
		epics: epics, writable: true}
	b.epicByID = make(map[string]*EpicInfo, len(epics))
	for i := range b.epics {
		b.epicByID[b.epics[i].ID] = &b.epics[i]
	}
	return b
}

// Epics returns the board's epic entities.
func (b *Board) Epics() []EpicInfo { return b.epics }

// NewStoreBoard assembles a snapshot of a real furrow store: its own lane
// vocabulary (not the fixture's), the epic entities, and the write pre-flight
// from `furrow board`.
func NewStoreBoard(lanes []Lane, tasks []*Task, epics []EpicInfo, writable bool, schema string) *Board {
	b := &Board{tasks: tasks, lanes: lanes, epics: epics, writable: writable, schema: schema}
	b.epicByID = make(map[string]*EpicInfo, len(epics))
	for i := range b.epics {
		b.epicByID[b.epics[i].ID] = &b.epics[i]
	}
	return b
}

// Epic resolves an e- id to its entity, nil when unknown (a stale membership
// renders as the raw id rather than vanishing).
func (b *Board) Epic(id string) *EpicInfo { return b.epicByID[id] }

// Writable reports the store's write pre-flight; a fixture board is always
// writable. False means furrow will refuse ordinary writes (schema gate).
func (b *Board) Writable() bool { return b.writable }

// SchemaState is furrow's own wording for the version gate ("current",
// "board-behind", ...), for the read-only warning.
func (b *Board) SchemaState() string { return b.schema }

// Neighbors reports the tasks around id in its own lane — the id AFTER it
// (the `--before` anchor) and the id BEFORE it (the `--after` anchor) — so a
// persist can reproduce an already-applied local placement in the store.
// Computed against the FULL lane, never the filtered view.
func (b *Board) Neighbors(id string) (beforeID, afterID string) {
	t := b.Task(id)
	if t == nil {
		return "", ""
	}
	lane := b.LaneTasks(t.Status)
	for i, x := range lane {
		if x.ID != id {
			continue
		}
		if i+1 < len(lane) {
			beforeID = lane[i+1].ID
		}
		if i > 0 {
			afterID = lane[i-1].ID
		}
		return beforeID, afterID
	}
	return "", ""
}

// Lanes returns the lane vocabulary in board order.
func (b *Board) Lanes() []Lane { return b.lanes }

// Lane resolves a lane by its slug, nil when unknown.
func (b *Board) Lane(name string) *Lane {
	for i := range b.lanes {
		if b.lanes[i].Name == name {
			return &b.lanes[i]
		}
	}
	return nil
}

// LaneIndex is the lane's position in board order, or -1.
func (b *Board) LaneIndex(name string) int {
	for i, l := range b.lanes {
		if l.Name == name {
			return i
		}
	}
	return -1
}

// Tasks returns every task on the board, unordered.
func (b *Board) Tasks() []*Task { return b.tasks }

// Task looks a task up by id, nil when absent.
func (b *Board) Task(id string) *Task {
	for _, t := range b.tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// LaneTasks returns a lane's tasks ordered by priority (id breaks ties, so the
// order is total and stable).
func (b *Board) LaneTasks(lane string) []*Task {
	var out []*Task
	for _, t := range b.tasks {
		if t.Status == lane {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// IndexIn is the task's position within its lane, or -1.
func (b *Board) IndexIn(lane, id string) int {
	for i, t := range b.LaneTasks(lane) {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// AdjustDropIndex converts an insertion index measured against a lane AS
// DISPLAYED — which still contains the moving card, because neither move mode
// nor a mouse drag reflows the board under the cursor — into the post-removal
// index MoveTo expects.
//
// This is the remove-then-insert boundary. Without it, dragging a card one slot
// DOWN inside its own lane is a no-op (the vacated slot absorbs the move), the
// classic off-by-one in every hand-rolled kanban.
func AdjustDropIndex(sameLane bool, fromIdx, idx int) int {
	if sameLane && idx > fromIdx {
		return idx - 1
	}
	return idx
}

// MoveTo places task id into lane at insertion index idx, where idx counts the
// destination lane WITHOUT the task (see AdjustDropIndex). It edits the sparse
// Priority field; only when the gap between neighbours is exhausted does it
// respace the whole lane, returning the ids it renumbered — furrow's contract.
func (b *Board) MoveTo(id, lane string, idx int) (renumbered []string, err error) {
	t := b.Task(id)
	if t == nil {
		return nil, fmt.Errorf("unknown task %q", id)
	}
	dst := b.Lane(lane)
	if dst == nil {
		return nil, fmt.Errorf("unknown lane %q", lane)
	}

	var peers []*Task
	for _, x := range b.LaneTasks(lane) {
		if x.ID != id {
			peers = append(peers, x)
		}
	}
	if idx < 0 {
		idx = 0
	}
	if idx > len(peers) {
		idx = len(peers)
	}

	wasDone := b.isDoneLane(t.Status)
	t.Status = lane
	t.Updated = nowFn().UTC().Truncate(time.Second)
	switch {
	case dst.Done && !wasDone:
		t.Closed = t.Updated
	case !dst.Done && wasDone:
		t.Closed = time.Time{}
	}

	if p, ok := sparsePriority(peers, idx); ok {
		t.Priority = p
		return nil, nil
	}
	return b.respace(lane, t, idx), nil
}

// sparsePriority finds a priority strictly between the neighbours at the
// insertion point, reporting false when the gap is exhausted.
func sparsePriority(peers []*Task, idx int) (int, bool) {
	switch {
	case len(peers) == 0:
		return priorityStep, true
	case idx == 0:
		if hi := peers[0].Priority; hi > priorityStep {
			return hi - priorityStep, true
		}
		return 0, false
	case idx == len(peers):
		return peers[len(peers)-1].Priority + priorityStep, true
	default:
		lo, hi := peers[idx-1].Priority, peers[idx].Priority
		if hi-lo >= 2 {
			return lo + (hi-lo)/2, true
		}
		return 0, false
	}
}

// respace rewrites the whole lane on the 10-step grid with t inserted at idx,
// returning the OTHER ids whose priority moved. Their Updated deliberately does
// not advance: a respace is positional bookkeeping, not progress.
func (b *Board) respace(lane string, t *Task, idx int) []string {
	var order []*Task
	for _, x := range b.LaneTasks(lane) {
		if x.ID != t.ID {
			order = append(order, x)
		}
	}
	if idx > len(order) {
		idx = len(order)
	}
	order = append(order[:idx:idx], append([]*Task{t}, order[idx:]...)...)

	var moved []string
	for i, x := range order {
		p := (i + 1) * priorityStep
		if x.Priority != p {
			if x.ID != t.ID {
				moved = append(moved, x.ID)
			}
			x.Priority = p
		}
	}
	return moved
}

func (b *Board) isDoneLane(name string) bool {
	l := b.Lane(name)
	return l != nil && l.Done
}

// DoneLane names the done lane, "" when the board has none.
func (b *Board) DoneLane() string {
	for _, l := range b.lanes {
		if l.Done {
			return l.Name
		}
	}
	return ""
}

// Close moves a task to the done lane, appending it at the end.
func (b *Board) Close(id string) error {
	d := b.DoneLane()
	if d == "" {
		return fmt.Errorf("board has no done lane")
	}
	if t := b.Task(id); t != nil && t.Status == d {
		return nil
	}
	_, err := b.MoveTo(id, d, len(b.LaneTasks(d)))
	return err
}

// ToggleCheck flips checklist item i and stamps Updated.
func (b *Board) ToggleCheck(id string, i int) error {
	t := b.Task(id)
	if t == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	if i < 0 || i >= len(t.Checklist) {
		return fmt.Errorf("task %s has no checklist item %d", id, i)
	}
	t.Checklist[i].Done = !t.Checklist[i].Done
	t.Updated = nowFn().UTC().Truncate(time.Second)
	return nil
}

// SetBody replaces a task's prose and stamps Updated, the way `furrow note`
// does (a bare file edit would leave Updated stale).
func (b *Board) SetBody(id, body string) error {
	t := b.Task(id)
	if t == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	t.Body = body
	t.Updated = nowFn().UTC().Truncate(time.Second)
	return nil
}

// dueOffset is furrow's signed-offset form: a sign, a count, and a
// minute/hour/day/week unit. The sign and a zero count are both legal
// (`-1d` back-dates, `+0d` means "now").
var dueOffset = regexp.MustCompile(`^[+-][0-9]+[mhdw]$`)

// parseDue mirrors furrow's `--due` grammar so the TUI can validate a keystroke
// without a round trip: a bare day (which furrow reads as the WHOLE day, i.e.
// end of that day LOCAL), a day+time read as LOCAL, an RFC3339 instant, or a
// signed offset from now. The grammar itself stays furrow's — ridge sends the
// raw string on to `furrow set --due` and this value only has to hold until the
// post-persist reconcile re-reads furrow's own truth. Keep it a mirror: a form
// this refuses is a form the UI cannot reach at all.
func parseDue(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if dueOffset.MatchString(s) {
		n, err := strconv.Atoi(s[:len(s)-1]) // Atoi keeps the sign
		if err == nil {
			unit := map[byte]time.Duration{
				'm': time.Minute,
				'h': time.Hour,
				'd': 24 * time.Hour,
				'w': 7 * 24 * time.Hour,
			}[s[len(s)-1]]
			return nowFn().UTC().Add(time.Duration(n) * unit).Truncate(time.Second), nil
		}
	}
	// A bare day is a promise for the whole day, so it lands at its last local
	// second — not at midnight, which would flag the task overdue all day.
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t.AddDate(0, 0, 1).Add(-time.Second).UTC(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04", s, time.Local); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("due %q is not a date: use YYYY-MM-DD (the whole day), "+
		"YYYY-MM-DDTHH:MM, an RFC3339 instant, or a signed offset like +1d/+2h", s)
}

// SetFields applies a metadata patch locally and stamps Updated — the
// optimistic half of Provider.PersistFields. It validates BEFORE mutating,
// so a refused gesture leaves the task untouched.
func (b *Board) SetFields(id string, p FieldPatch) error {
	t := b.Task(id)
	if t == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	for _, v := range []*int{p.Value, p.Effort} {
		if v != nil && (*v < 0 || *v > 5) {
			return fmt.Errorf("estimate %d: want 1..5, or 0 to clear", *v)
		}
	}
	if p.Title != nil && strings.TrimSpace(*p.Title) == "" {
		return fmt.Errorf("a title cannot be empty")
	}
	var due time.Time
	if p.Due != nil && *p.Due != "" {
		d, err := parseDue(*p.Due)
		if err != nil {
			return err
		}
		due = d
	}

	if p.Value != nil {
		t.Value = *p.Value
	}
	if p.Effort != nil {
		t.Effort = *p.Effort
	}
	for _, l := range p.AddLabels {
		if l != "" && !containsStr(t.Labels, l) {
			t.Labels = append(t.Labels, l)
		}
	}
	for _, l := range p.RmLabels {
		t.Labels = removeStr(t.Labels, l)
	}
	if p.Epic != nil {
		t.Epic = *p.Epic
	}
	if p.Due != nil {
		t.Due = due
	}
	if p.Title != nil {
		t.Title = strings.TrimSpace(*p.Title)
	}
	for _, r := range p.AddRepos {
		if r != "" && !containsStr(t.Repos, r) {
			t.Repos = append(t.Repos, r)
		}
	}
	for _, r := range p.RmRepos {
		t.Repos = removeStr(t.Repos, r)
	}
	// Append order preserved, add idempotent, rm exact-match — `furrow ref`'s
	// contract (refs are a sequence, not a sorted set).
	for _, r := range p.AddRefs {
		if r != "" && !containsStr(t.Refs, r) {
			t.Refs = append(t.Refs, r)
		}
	}
	for _, r := range p.RmRefs {
		t.Refs = removeStr(t.Refs, r)
	}
	t.Updated = nowFn().UTC().Truncate(time.Second)
	return nil
}

// AppendNote adds text as a new paragraph at the end of the body and stamps
// Updated — the optimistic half of Provider.PersistNote. It mirrors `furrow
// note` (v4.0.0 appendBody/normalizeNote, re-measured on dev 60074b8): the
// text loses its own trailing newlines, an empty body becomes the text alone,
// and any other body is padded up to AT LEAST one blank line before the text —
// existing trailing newlines are kept, never collapsed ("本文\n\n\n" + note is
// "本文\n\n\n追記\n"). An empty or whitespace-only text is furrow's "note text
// is empty" refusal (exit 2), so the same gesture is unreachable here.
func (b *Board) AppendNote(id, text string) error {
	t := b.Task(id)
	if t == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("note text is empty")
	}
	var s strings.Builder
	s.WriteString(t.Body)
	if t.Body != "" {
		if !strings.HasSuffix(t.Body, "\n") {
			s.WriteString("\n")
		}
		if !strings.HasSuffix(t.Body, "\n\n") {
			s.WriteString("\n")
		}
	}
	s.WriteString(text)
	s.WriteString("\n")
	t.Body = s.String()
	t.Updated = nowFn().UTC().Truncate(time.Second)
	return nil
}

// DepAdd makes id wait on dep and stamps Updated — the optimistic half of
// Provider.PersistDepAdd. It mirrors `furrow dep`'s contract the way parseDue
// mirrors --due: every dep must exist, adding is acyclic and idempotent. Keep
// it a mirror — a form this refuses is a form the UI cannot reach at all, and
// the acyclic walk here only has to hold until the persist's own verdict.
func (b *Board) DepAdd(id, dep string) error {
	t := b.Task(id)
	if t == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	if b.Task(dep) == nil {
		return fmt.Errorf("unknown dep %q — every dep must exist", dep)
	}
	if dep == id {
		return fmt.Errorf("%s cannot depend on itself", id)
	}
	if containsStr(t.Deps, dep) {
		return nil // idempotent, like furrow's own add
	}
	if b.depReaches(dep, id, map[string]bool{}) {
		return fmt.Errorf("%s → %s would close a cycle — deps are acyclic", id, dep)
	}
	t.Deps = append(t.Deps, dep)
	t.Updated = nowFn().UTC().Truncate(time.Second)
	return nil
}

// depReaches reports whether goal is reachable from id along dep edges.
func (b *Board) depReaches(id, goal string, seen map[string]bool) bool {
	if id == goal {
		return true
	}
	if seen[id] {
		return false
	}
	seen[id] = true
	t := b.Task(id)
	if t == nil {
		return false
	}
	for _, d := range t.Deps {
		if b.depReaches(d, goal, seen) {
			return true
		}
	}
	return false
}

// DepRm removes id's dependency on dep and stamps Updated.
func (b *Board) DepRm(id, dep string) error {
	t := b.Task(id)
	if t == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	if !containsStr(t.Deps, dep) {
		return fmt.Errorf("%s does not depend on %q", id, dep)
	}
	t.Deps = removeStr(t.Deps, dep)
	t.Updated = nowFn().UTC().Truncate(time.Second)
	return nil
}

// CheckAdd appends a checklist item and stamps Updated.
func (b *Board) CheckAdd(id, text string) error {
	t := b.Task(id)
	if t == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("a checklist item cannot be empty")
	}
	t.Checklist = append(t.Checklist, ChecklistItem{Text: text})
	t.Updated = nowFn().UTC().Truncate(time.Second)
	return nil
}

// CheckRm deletes checklist item i and stamps Updated.
func (b *Board) CheckRm(id string, i int) error {
	t := b.Task(id)
	if t == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	if i < 0 || i >= len(t.Checklist) {
		return fmt.Errorf("task %s has no checklist item %d", id, i)
	}
	t.Checklist = append(t.Checklist[:i], t.Checklist[i+1:]...)
	t.Updated = nowFn().UTC().Truncate(time.Second)
	return nil
}

// CheckReword replaces checklist item i's text and stamps Updated.
func (b *Board) CheckReword(id string, i int, text string) error {
	t := b.Task(id)
	if t == nil {
		return fmt.Errorf("unknown task %q", id)
	}
	if i < 0 || i >= len(t.Checklist) {
		return fmt.Errorf("task %s has no checklist item %d", id, i)
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("a checklist item cannot be empty")
	}
	t.Checklist[i].Text = text
	t.Updated = nowFn().UTC().Truncate(time.Second)
	return nil
}

func containsStr(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func removeStr(hay []string, needle string) []string {
	out := hay[:0]
	for _, h := range hay {
		if h != needle {
			out = append(out, h)
		}
	}
	return out
}

// Append adds an externally-created task to the snapshot (the mock store's
// add; the real store re-reads instead).
func (b *Board) Append(t *Task) { b.tasks = append(b.tasks, t) }
