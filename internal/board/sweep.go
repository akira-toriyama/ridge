package board

import (
	"fmt"
	"time"
)

// The sweep: furrow's three maintenance passes as ridge shows and applies
// them — `archive` (retire aged done tasks), `tidy` (prune dead bookkeeping)
// and `unarchive` (bring a retired task back). Every judgement is furrow's:
// which done task is old enough, which dep edge is satisfied, which shard key
// is unknown. ridge reads the previews, lets the user narrow the archive set
// by id, and applies — it derives no candidate of its own, so the frame can
// never name a task furrow would not move.
//
// The writes are store-first (Provider): nothing is applied locally, the
// board and the previews converge on the re-read after the write lands.

// Sweep is one preview read: what the three passes would do right now.
type Sweep struct {
	// OlderThanDays is the age guard the archive sweep applied — furrow's
	// config `archive.older_than_days`, echoed back so the frame can say it.
	OlderThanDays int
	// Archivable is `furrow archive --json` (dry run) over the whole board:
	// the done tasks closed more than OlderThanDays ago, in furrow's order.
	Archivable []SweepTask
	// DoneDeps is tidy's first class: OPEN tasks carrying dep edges to
	// done-lane tasks — satisfied edges that gate nothing. One row per task,
	// with the satisfied deps.
	DoneDeps []TidyDoneDep
	// UnknownKeys is tidy's second class: shards carrying keys this furrow
	// does not know (what `furrow upgrade` parked for a human).
	UnknownKeys []TidyUnknownKey
	// Archived is the archive store (`furrow ls --archived`), the population
	// Unarchive can name. Retired tasks are not on the Board at all.
	Archived []SweepTask
}

// SweepTask is a task as the archive passes report it — the fields the
// sweep's rows show. It is not a *Task: an archived task is off the board,
// and a candidate's row must not tempt the renderer into the board's card
// machinery.
type SweepTask struct {
	ID     string
	Title  string
	Repos  []string
	Closed time.Time
}

// TidyDoneDep is one open task whose Deps include done-lane tasks.
type TidyDoneDep struct {
	ID   string
	Deps []string // the satisfied edges only, furrow's order
}

// TidyUnknownKey is one record furrow parked unknown keys on.
type TidyUnknownKey struct {
	ID   string // the task/epic id, "" for meta.json
	File string // store-relative shard path
	Keys []string
}

// TidyClass selects which of tidy's two classes a write applies. furrow makes
// the selector mandatory beside --yes (each class is a policy call), and so
// does Tidy: there is no "prune everything" write.
type TidyClass int

// The two classes furrow names; the zero value is "no class" so an unset
// selector cannot silently pick one.
const (
	TidyDoneDeps TidyClass = iota + 1
	TidyUnknownKeys
)

// Flag is the furrow selector the class rides as.
func (c TidyClass) Flag() string {
	switch c {
	case TidyDoneDeps:
		return "--done-deps"
	case TidyUnknownKeys:
		return "--unknown-keys"
	}
	return ""
}

func (c TidyClass) String() string {
	switch c {
	case TidyDoneDeps:
		return "done-deps"
	case TidyUnknownKeys:
		return "unknown-keys"
	}
	return "unknown"
}

// ValidateSweepIDs refuses the id list every adapter's Archive/Unarchive must
// refuse BEFORE exec: an EMPTY list. `furrow archive --yes` with no id is the
// aged SWEEP over the board scope — the one write this surface must never
// issue by accident — and `furrow unarchive` with no id is a usage error. The
// UI always sends the explicit ids the user previewed, so what moves is what
// was on screen, not what aged in between.
func ValidateSweepIDs(verb string, ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("%s: no task named — an empty id list is refused before it reaches furrow", verb)
	}
	for _, id := range ids {
		if id == "" {
			return fmt.Errorf("%s: an empty id in the list", verb)
		}
	}
	return nil
}
