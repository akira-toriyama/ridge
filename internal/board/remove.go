package board

import (
	"fmt"
	"strings"
	"time"
)

// RemoveOptions mirrors `furrow rm`'s two flags. Apply=false is the dry run
// every delete gate reads before it asks; Force severs what still points at
// the target instead of letting furrow refuse.
type RemoveOptions struct {
	Force bool
	Apply bool
}

// RemoveReport is `furrow rm --json` / `furrow epic rm --json` as ridge reads
// it — the one report a preview (DryRun) and an apply both print. Tasks is
// `rm`'s targets as they were; Epic is `epic rm`'s box, nil on a task
// removal. Every field is furrow's verdict, copied.
type RemoveReport struct {
	DryRun     bool
	Force      bool
	Tasks      []RemovedTask
	Epic       *RemovedEpic
	References References
	Assets     AssetTransfer
}

// RemovedTask is one target as it was: what the gate names, and Repeat
// because removing a rule's only live occurrence ENDS its series — furrow
// discloses that on the preview and on the apply alike.
type RemovedTask struct {
	ID     string
	Title  string
	Repeat string
	Due    time.Time
}

// RemovedEpic is `epic rm`'s target as it was.
type RemovedEpic struct {
	ID    string
	Title string
}

// References is everything that still points at a target — what furrow
// refuses over (kind `referenced`, exit 2) and what --force severs: a dep
// edge dropped, a live [[id]] link de-linked to the bare id, a member
// unfiled, an epic dep dropped. furrow computes it; ridge reads it off the
// preview and never re-derives it.
type References struct {
	Deps     []RefEdge
	Links    []RefLink
	Members  []RefMember
	EpicDeps []RefEdge
}

// RefEdge is a dep edge naming a target: From waits on To.
type RefEdge struct{ From, To string }

// RefLink is a live [[To]] link in the body owned by Body — a task or box id.
type RefLink struct{ Body, To string }

// RefMember is a task filed under a target box.
type RefMember struct{ Task, Epic string }

// Empty reports that nothing points at the target: the plain delete lands.
func (r References) Empty() bool { return r.Count() == 0 }

// Count is every reference, the number a gate names.
func (r References) Count() int {
	return len(r.Deps) + len(r.Links) + len(r.Members) + len(r.EpicDeps)
}

// Summary is furrow's one-line roll-up of a reference set (app.References
// Summary), in its words so the gate and `furrow rm`'s refusal read alike;
// a body is named by its owner's id where furrow prints the file path.
func (r References) Summary() string {
	var parts []string
	if n := len(r.Deps); n > 0 {
		parts = append(parts, fmt.Sprintf("%d dep edge(s) from %s", n, strings.Join(uniqueFrom(r.Deps), ", ")))
	}
	if n := len(r.Links); n > 0 {
		seen := map[string]bool{}
		var bodies []string
		for _, l := range r.Links {
			if !seen[l.Body] {
				seen[l.Body] = true
				bodies = append(bodies, l.Body)
			}
		}
		parts = append(parts, fmt.Sprintf("%d [[link]](s) in %s", n, strings.Join(bodies, ", ")))
	}
	if n := len(r.Members); n > 0 {
		ids := make([]string, 0, n)
		for _, m := range r.Members {
			ids = append(ids, m.Task)
		}
		parts = append(parts, fmt.Sprintf("%d member(s): %s", n, strings.Join(ids, ", ")))
	}
	if n := len(r.EpicDeps); n > 0 {
		parts = append(parts, fmt.Sprintf("%d epic dep edge(s) from %s", n, strings.Join(uniqueFrom(r.EpicDeps), ", ")))
	}
	return strings.Join(parts, "; ")
}

func uniqueFrom(edges []RefEdge) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range edges {
		if !seen[e.From] {
			seen[e.From] = true
			out = append(out, e.From)
		}
	}
	return out
}

// AssetTransfer is what a removal does to the targets' attached assets:
// Deleted go with them, Kept stay because another body still shows them.
type AssetTransfer struct {
	Deleted []string
	Kept    []KeptAsset
}

// KeptAsset is one asset the store kept, and the ids still holding it.
type KeptAsset struct {
	Name   string
	HeldBy []string
}
