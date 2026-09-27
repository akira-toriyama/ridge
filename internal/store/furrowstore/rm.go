package furrowstore

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/akira-toriyama/ridge/internal/board"
)

// rmJSON is `furrow rm --json` and `furrow epic rm --json`: one report each,
// preview and apply alike. tasks are `ls` rows as the targets were, epic is
// the box, references is furrow's reference shape (all in v6.0.0, CI's pin).
// assets is furrow #345 (2026-09-15, after the tag): the pinned release's
// report carries no such key (measured on a v6.0.0 build, 2026-09-27) and
// decodes to the empty transfer, so the gate's asset line and the landing
// note's asset words appear only against a newer furrow; the shape is pinned
// by a canned reply (rm_unit_test.go) and, where the binary has it, by the
// attach contract test.
type rmJSON struct {
	DryRun     bool       `json:"dry_run"`
	Force      bool       `json:"force"`
	Tasks      []taskJSON `json:"tasks"`
	Epic       *epicJSON  `json:"epic"`
	References struct {
		Deps  []refEdgeJSON `json:"deps"`
		Links []struct {
			Body string `json:"body"`
			To   string `json:"to"`
		} `json:"links"`
		Members []struct {
			Task string `json:"task"`
			Epic string `json:"epic"`
		} `json:"members"`
		EpicDeps []refEdgeJSON `json:"epic_deps"`
	} `json:"references"`
	Assets struct {
		Deleted []string `json:"deleted"`
		Kept    []struct {
			Name   string   `json:"name"`
			HeldBy []string `json:"held_by"`
		} `json:"kept"`
	} `json:"assets"`
}

type refEdgeJSON struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (r rmJSON) toReport() board.RemoveReport {
	rep := board.RemoveReport{DryRun: r.DryRun, Force: r.Force}
	for _, t := range r.Tasks {
		rep.Tasks = append(rep.Tasks, board.RemovedTask{ID: t.ID, Title: t.Title, Repeat: t.Repeat, Due: fromPtr(t.Due)})
	}
	if r.Epic != nil {
		rep.Epic = &board.RemovedEpic{ID: r.Epic.ID, Title: r.Epic.Title, Active: r.Epic.Active}
	}
	for _, d := range r.References.Deps {
		rep.References.Deps = append(rep.References.Deps, board.RefEdge{From: d.From, To: d.To})
	}
	for _, l := range r.References.Links {
		rep.References.Links = append(rep.References.Links, board.RefLink{Body: l.Body, To: l.To})
	}
	for _, m := range r.References.Members {
		rep.References.Members = append(rep.References.Members, board.RefMember{Task: m.Task, Epic: m.Epic})
	}
	for _, d := range r.References.EpicDeps {
		rep.References.EpicDeps = append(rep.References.EpicDeps, board.RefEdge{From: d.From, To: d.To})
	}
	rep.Assets.Deleted = append([]string(nil), r.Assets.Deleted...)
	for _, k := range r.Assets.Kept {
		rep.Assets.Kept = append(rep.Assets.Kept, board.KeptAsset{Name: k.Name, HeldBy: append([]string(nil), k.HeldBy...)})
	}
	return rep
}

// rmFlags is the flag half of both commands: --json always, --force and
// --yes as asked. No --yes is the dry run furrow documents.
func rmFlags(o board.RemoveOptions) []string {
	args := []string{"--json"}
	if o.Force {
		args = append(args, "--force")
	}
	if o.Apply {
		args = append(args, "--yes")
	}
	return args
}

// rmArgs is `rm`'s argv: the flags, then the ids fenced by `--` so an id can
// never be read as a flag. Duplicates collapse here, in order, because they
// collapse furrow-side too (app.RemoveTasks) and the report names each
// target once — the count check below holds only over the unique list.
func rmArgs(ids []string, o board.RemoveOptions) (args []string, unique []string) {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	args = append([]string{"rm"}, rmFlags(o)...)
	args = append(append(args, "--"), unique...)
	return args, unique
}

// epicRmArgs is `epic rm`'s argv, fenced the same way.
func epicRmArgs(id string, o board.RemoveOptions) []string {
	args := append([]string{"epic", "rm"}, rmFlags(o)...)
	return append(args, "--", id)
}

// checkRmReply holds the reply to the call: a preview that says it applied,
// or an apply that says it did not, is a contract break worth refusing over —
// the gate would otherwise show one and the board the other.
func checkRmReply(verb string, reply rmJSON, o board.RemoveOptions) error {
	if reply.DryRun == o.Apply {
		return fmt.Errorf("furrow %s: dry_run=%s on a call with --yes=%s — refusing to trust the reply",
			verb, strconv.FormatBool(reply.DryRun), strconv.FormatBool(o.Apply))
	}
	return nil
}

// Remove is `furrow rm <ids>` (board.Provider). The argv is rmArgs's; the
// empty list is refused before exec (ValidateSweepIDs). A refusal — a miss
// (exit 1), a reference still standing (exit 2, kind referenced) — comes back
// as furrow's own envelope, message and kind.
func (p *Store) Remove(ids []string, o board.RemoveOptions) (board.RemoveReport, error) {
	if err := board.ValidateSweepIDs("rm", ids); err != nil {
		return board.RemoveReport{}, err
	}
	args, unique := rmArgs(ids, o)
	out, err := p.c.run("rm", args...)
	if err != nil {
		return board.RemoveReport{}, err
	}
	var reply rmJSON
	if err := json.Unmarshal(out, &reply); err != nil {
		return board.RemoveReport{}, fmt.Errorf("furrow rm: undecodable reply: %v", err)
	}
	if err := checkRmReply("rm", reply, o); err != nil {
		return board.RemoveReport{}, err
	}
	if len(reply.Tasks) != len(unique) {
		return board.RemoveReport{}, fmt.Errorf("furrow rm: the report names %d of %d targets", len(reply.Tasks), len(unique))
	}
	return reply.toReport(), nil
}

// EpicRemove is `furrow epic rm <id>` (board.Provider): the same contract
// over one box.
func (p *Store) EpicRemove(id string, o board.RemoveOptions) (board.RemoveReport, error) {
	if err := board.ValidateSweepIDs("epic rm", []string{id}); err != nil {
		return board.RemoveReport{}, err
	}
	out, err := p.c.run("epic-rm", epicRmArgs(id, o)...)
	if err != nil {
		return board.RemoveReport{}, err
	}
	var reply rmJSON
	if err := json.Unmarshal(out, &reply); err != nil {
		return board.RemoveReport{}, fmt.Errorf("furrow epic rm: undecodable reply: %v", err)
	}
	if err := checkRmReply("epic rm", reply, o); err != nil {
		return board.RemoveReport{}, err
	}
	if reply.Epic == nil || reply.Epic.ID != id {
		return board.RemoveReport{}, fmt.Errorf("furrow epic rm: the report does not name %s", id)
	}
	return reply.toReport(), nil
}
