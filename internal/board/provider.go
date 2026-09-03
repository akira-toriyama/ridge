package board

import (
	"fmt"
	"strings"
)

// Provider is the seam between the UI and the task store — the port; the
// adapters live in internal/store (furrowstore for the real CLI/JSON store,
// memstore for the fixture).
//
// The division of labour: the MODEL owns the optimistic local apply — it
// mutates the Board() snapshot through the same furrow-semantics helpers
// (MoveTo, Close, ToggleCheck, SetBody) on the UI thread — and a Persist*
// method only records that already-applied change in the backing store.
// Persists run OFF the UI thread, inside a tea.Cmd, so a provider must never
// mutate a Board it has handed out: Reload builds a fresh one and swaps.
type Provider interface {
	// Board returns the current snapshot. The model mutates it optimistically
	// on the UI thread; the provider treats handed-out boards as frozen.
	Board() *Board

	// Reload re-reads the backing store into a fresh Board. For the mock the
	// backing store is the fixture, so a reload discards session edits —
	// except a quick add, which the fixture keeps so it does not lie about
	// the add having happened.
	Reload() error

	// Sync runs the store's git sync (commit, pull --rebase, push). A
	// provider without a store returns an error.
	Sync() error

	// Query evaluates a furrow -q expression against the store and returns
	// the matching task ids ("" matches everything). The grammar's one
	// definition lives furrow-side (t-ehk7); ridge passes the string through
	// and intersects the ids with its own Board() snapshot. All-or-nothing:
	// a query furrow refuses returns an error and NO ids.
	Query(q string) ([]string, error)

	// Live reports whether persists land in an external store whose truth
	// can drift from the in-memory board. The model reconciles (re-reads)
	// after its persist queue drains only when this is true — reconciling the
	// mock would just revert the session's own edits.
	Live() bool

	// PersistMove records id's already-applied placement: the lane plus at
	// most one anchor. beforeID wins when both are set; both empty means the
	// task is the lane's only card. renumbered reports the neighbours the
	// store renumbered when the sparse-priority gap was exhausted.
	PersistMove(id, lane, beforeID, afterID string) (renumbered []string, err error)

	// PersistDone records id's already-applied close.
	PersistDone(id string) error

	// PersistCheck records checklist item i's already-applied state. done is
	// the state AFTER the local toggle, so the write is idempotent.
	PersistCheck(id string, i int, done bool) error

	// PersistBody records id's already-applied body replacement. body is
	// non-empty after trimming: furrow refuses an empty replacement (a body
	// is never cleared, exit 2), and Board.SetBody mirrors that refusal
	// before anything queues.
	PersistBody(id, body string) error

	// PersistFields records id's already-applied metadata edit. Everything
	// set-shaped in the patch lands in ONE `furrow set` write; Title, the
	// repo edits and the ref edits are their own commands (retitle / repo /
	// ref), so a mixed patch may cost up to four writes — the UI edits one
	// field per gesture, so in practice it is one.
	PersistFields(id string, p FieldPatch) error

	// PersistNote records an already-applied note append: one paragraph added
	// to the body with Updated stamped, `furrow note`'s contract. The local
	// half is Board.AppendNote, which already refused an empty text.
	PersistNote(id, text string) error

	// PersistCheckAdd records an already-appended checklist item.
	PersistCheckAdd(id, text string) error

	// PersistCheckRm records checklist item i's already-applied deletion.
	PersistCheckRm(id string, i int) error

	// PersistCheckReword records checklist item i's already-applied rewording.
	PersistCheckReword(id string, i int, text string) error

	// PersistDepAdd records id's already-applied dependency on dep. The
	// acyclic/exists rules are furrow's; the local apply (Board.DepAdd) only
	// mirrors them, so a refusal here still rolls the board back.
	PersistDepAdd(id, dep string) error

	// PersistDepRm records id's already-applied dependency removal.
	PersistDepRm(id, dep string) error

	// PersistReview records id's already-applied review stamp — `furrow
	// review <id>`, which sets `reviewed` and touches nothing else (a review
	// changes no content, so Updated stays; the local half is Board.Review).
	PersistReview(id string) error

	// Revisit is `furrow revisit -q <q>`: the open tasks worth a fresh
	// judgment, each with furrow's reasons. The signals (no_repo, value/effort
	// unset, stale, dep_done) and the staleness window are furrow's — ridge
	// never derives one of its own, exactly as Query never parses. q ANDs
	// the same -q language onto the flagged set ("" = every flagged task);
	// like Query it is all-or-nothing, so a refused q returns an error and
	// no rows.
	Revisit(q string) ([]Revisit, error)

	// Add creates a task in the store and returns its id. Unlike the
	// Persist* family this is NOT the record of an applied edit: the store
	// owns id assignment, so the model waits for the id and re-reads instead
	// of applying optimistically (a single add measures ~57ms). Both
	// adapters run o.Validate() themselves — the UI refuses first for the
	// modal's sake, but a future caller must not be able to slip a comma'd
	// ref past the CSV flag layer by skipping the modal.
	Add(title string, o AddOptions) (id string, err error)

	// --- epic writes: store-first, NOT the Persist* contract ---------------
	//
	// The Persist* family records a change the model already applied to the
	// board on the UI thread. The epic family deliberately does not join it:
	// what an epic write MEANS is furrow's, not ridge's — activate refuses a
	// second box for a repo rather than stealing the slot, add invents the id,
	// and Done/Total/Stuck/OpenDeps are all derived furrow-side. Mirroring any
	// of that locally is the front-end logic this repo exists to not have.
	//
	// So these writes apply NOTHING locally: they ride the same strictly-serial
	// queue (ordering and the quit-flush still hold), and the board converges
	// on the re-read that follows the drain. A refusal therefore needs no
	// rollback — there is no optimistic half to undo — but the queue must still
	// re-read when an earlier store-first write in the same drain LANDED, or
	// that write stays invisible until the next reload (persist.go).

	// EpicAdd creates a box and returns its id. Never active on creation:
	// opening one is a separate deliberate act (`epic activate`).
	EpicAdd(title string, o EpicAddOptions) (id string, err error)

	// EpicSet writes one metadata edit; everything in the patch lands in ONE
	// `furrow epic set`. An empty patch is refused rather than sent: furrow
	// answers exit 2 ("needs at least one change") and a no-op that reports a
	// store error is worse than one that reports nothing.
	EpicSet(id string, p EpicPatch) error

	// EpicActivate makes the box active for every repo it names. reason is
	// furrow's --reason: it is appended to the box's own body as an activation
	// record, which is how a switch stays visible to the next session. "" omits
	// the flag.
	EpicActivate(id, reason string) error

	// EpicDeactivate clears the active flag without closing the box, and
	// returns furrow's suggestion of where to go back to — computed fresh from
	// the activation log, never a stored pointer. The zero value means furrow
	// had no record to decide it, which is a legitimate answer, not an error.
	EpicDeactivate(id string) (EpicPrevious, error)

	// EpicDone closes the box, and vacates the active slot with it when the box
	// held one — furrow does both in the one write, so this answers the same
	// "where to return" suggestion EpicDeactivate does. It is not a one-way
	// door: the read serves closed boxes, so the box stays on the board for
	// EpicReopen to name.
	//
	// furrow does NOT refuse a box with open members. Closing one is a
	// judgement, not an error, so the caller owes the user the progress before
	// the keystroke rather than trusting a refusal that will not come.
	EpicDone(id string) (EpicPrevious, error)

	// EpicReopen clears the closing stamp. The box comes back OPEN and
	// INACTIVE: furrow refuses to chain reopening to activating, and ridge must
	// not paper over that with a second write of its own.
	EpicReopen(id string) error

	// EpicDepAdd makes id wait on dep ("open this box after that one closes").
	// Acyclic and idempotent furrow-side.
	EpicDepAdd(id, dep string) error

	// EpicDepRm removes the edge. The dep need not resolve on this board — a
	// closed or archived box leaves a removable edge behind, which is exactly
	// the removal that matters.
	EpicDepRm(id, dep string) error
}

// Revisit is one `furrow revisit --json` row: a task and why it surfaced.
// Reasons keep furrow's order (`furrow vocab revisit-codes` for the codes).
type Revisit struct {
	ID      string
	Reasons []RevisitReason
}

// RevisitReason is one revisit signal as furrow reports it: a code from the
// closed vocabulary and its human detail.
type RevisitReason struct {
	Code   string
	Detail string
}

// EpicAddOptions is a new box's inherited context — the same
// filtered-metadata inheritance rule quick add follows. Repos matters most: a
// box naming no repo cannot be activated at all (furrow refuses it, because a
// repo-less box would bypass the one-active-per-repo rule).
type EpicAddOptions struct {
	Goal   string
	Labels []string
	Repos  []string
}

// EpicPatch is one `furrow epic set` write. nil/empty means "untouched"; a
// pointed-to zero clears (--goal "" is furrow's clear, and --standing=false /
// --pinned=false are its explicit negatives — an omitted flag never touches the
// stored value).
//
// Title has no clearing form on purpose: `epic set --title ""` is exit 2
// ("epic title must not be empty"), so an empty rename is refused upstream.
type EpicPatch struct {
	Title     *string
	Goal      *string
	AddLabels []string
	RmLabels  []string
	AddRepos  []string
	RmRepos   []string
	SetMeta   map[string]string
	RmMeta    []string
	Standing  *bool
	Pinned    *bool
}

// Empty reports whether the patch would send `furrow epic set` with no change
// flag, which furrow refuses (exit 2). The check lives here rather than in the
// adapter so the UI can refuse the gesture before it queues a write.
//
// It counts Standing/Pinned even though furrow's own refusal message does not
// list them: a standing-only set IS accepted, so deriving this from that
// message would refuse a write furrow takes.
func (p EpicPatch) Empty() bool {
	return p.Title == nil && p.Goal == nil && p.Standing == nil && p.Pinned == nil &&
		len(p.AddLabels) == 0 && len(p.RmLabels) == 0 &&
		len(p.AddRepos) == 0 && len(p.RmRepos) == 0 &&
		len(p.SetMeta) == 0 && len(p.RmMeta) == 0
}

// EpicPrevious is `epic deactivate`'s "where to return" suggestion: the open,
// inactive box with the newest activation record. furrow computes it and never
// acts on it — activating remains the human's call — so ridge shows it and
// nothing more. The zero value means furrow named nobody.
type EpicPrevious struct {
	ID    string
	Title string
}

// AddOptions is quick add's inherited context — the GitHub Projects rule
// that a filtered view's metadata applies to the item it creates — plus the
// detail fields the modal's inline tokens carry (t-69v9), furrow add's
// --value/--effort/--due/--dep/--check/--ref. The zero value of every detail
// field means "omitted": an add has no clearing form, so 0/"" never reaches
// the flag. Bulk creation (--stdin) is deliberately NOT here — pasting many
// titles is the CLI's job, not a modal's.
type AddOptions struct {
	Lane  string // "" = the store's default lane
	Label string // one inherited label
	Epic  string // e- id
	Repo  string // owner/repo or unique short name; "" = the board's auto-attach

	Value  int      // 1..5; 0 = unset
	Effort int      // 1..5; 0 = unset
	Due    string   // furrow date form incl. the +1d offset; "" = no promise
	Deps   []string // t- ids; existence/acyclicity stay furrow's rules
	Checks []string // unchecked checklist items, text verbatim
	Refs   []string // free text; `,` and `"` refused (the t-pwrp CSV caveat)

	// Draft creates the task with NO repo attached — furrow's `add --draft`,
	// which also suppresses the board's auto-attach. It conflicts with Repo
	// (furrow refuses `--draft` with `-r`), and Validate mirrors that refusal.
	Draft bool
}

// Validate refuses an AddOptions the flag layer would mangle or furrow would
// refuse — BEFORE the modal closes, so the typed line survives as a
// still-open modal instead of a refusal round trip. Due goes through the same
// ParseDue mirror SetFields uses and refs through the same CSV caveat; the
// estimate check is deliberately STRICTER than furrow, which clamps instead
// of refusing (measured on dev 60074b8: `--value 9` exits 0 and stores 5) —
// a silent clamp would stamp an estimate the user did not type.
func (o AddOptions) Validate() error {
	if o.Draft && o.Repo != "" {
		return fmt.Errorf("draft conflicts with repo %q — a draft attaches no repo", o.Repo)
	}
	for _, v := range []int{o.Value, o.Effort} {
		if v < 0 || v > 5 {
			return fmt.Errorf("estimate %d: want 1..5", v)
		}
	}
	if o.Due != "" {
		if _, err := ParseDue(o.Due); err != nil {
			return err
		}
	}
	for _, d := range o.Deps {
		if d == "" {
			return fmt.Errorf("dep: needs a task id")
		}
		// --dep is the same pflag CSV field as --ref: a comma'd id would
		// SPLIT silently and a bare `"` is pflag's own exit-2 parse error.
		if strings.ContainsAny(d, `,"`) {
			return fmt.Errorf("dep %q: `,` and `\"` cannot ride furrow's CSV flag parsing", d)
		}
	}
	for _, c := range o.Checks {
		if strings.TrimSpace(c) == "" {
			return fmt.Errorf("check: needs text")
		}
	}
	for _, r := range o.Refs {
		if err := validateRef(r); err != nil {
			return err
		}
	}
	return nil
}

// FieldPatch is one already-applied metadata edit. nil means "untouched";
// the zero value of a pointed-to field means "clear" (furrow --clear-value /
// --clear-effort / --clear-due / -e "").
type FieldPatch struct {
	Value     *int // 1..5; 0 clears
	Effort    *int // 1..5; 0 clears
	AddLabels []string
	RmLabels  []string
	Epic      *string // e- id; "" unfiles
	Due       *string // furrow date forms incl. the +1d snooze; "" clears
	Title     *string
	AddRepos  []string // full owner/repo
	RmRepos   []string
	// Refs are a SEQUENCE, not a sorted set like labels: furrow appends adds
	// at the end and keeps the order given. Add is idempotent, Rm is
	// exact-match and a no-op on an absent ref (measured on dev 60074b8).
	// The form is free text (file:line or URL) with one flag-layer caveat:
	// furrow's --add/--rm are pflag CSV StringSlices, so SetFields refuses
	// adds carrying `,` or `"` until furrow takes them verbatim (t-pwrp).
	AddRefs []string
	RmRefs  []string
}
