// Package furrowstore is the real store adapter: the ONLY place ridge execs
// the furrow binary. It speaks furrow's CLI/JSON contract — never furrow's Go
// packages (furrow's non-goals doc) — and assembles board.Board snapshots for
// the port.
package furrowstore

import (
	"github.com/akira-toriyama/ridge/internal/board"

	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// Store is the real store client: every read and write shells out to
// the furrow binary and speaks its CLI/JSON contract (ridge never imports
// furrow's Go packages — that boundary is furrow's non-goals doc).
//
// It keeps the latest snapshot behind a mutex and swaps it wholesale on
// Reload; boards it has handed out are never mutated again, which is what
// makes the model's optimistic edits on the UI thread race-free.
type Store struct {
	c *furrowClient

	mu sync.Mutex
	b  *board.Board
}

// New probes the store and performs the initial load, so a
// missing binary or an unreadable board fails at startup rather than at the
// first keypress.
func New(perf func(op string, d time.Duration)) (*Store, error) {
	c := newFurrowClient()
	c.perf = perf
	p := &Store{c: c}
	if err := p.Reload(); err != nil {
		return nil, err
	}
	return p, nil
}

// Board returns the current snapshot (board.Provider).
func (p *Store) Board() *board.Board {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.b
}

// Live is true: persists land in an external store (board.Provider).
func (p *Store) Live() bool { return true }

// Reload re-reads the store into a fresh board and swaps it in
// (board.Provider).
func (p *Store) Reload() error {
	b, err := p.load()
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.b = b
	p.mu.Unlock()
	return nil
}

// Sync is furrow's thin git wrapper: commit the board, pull --rebase, push.
// Network-bound, so it gets its own generous deadline; the caller reloads
// afterwards to pick up whatever the pull brought in.
func (p *Store) Sync() error {
	_, err := p.c.runTimeout("sync", 120*time.Second, "sync")
	return err
}

// --- reads -----------------------------------------------------------------

// boardJSON is `furrow board --json`, the lane vocabulary and store pre-flight.
type boardJSON struct {
	Lanes       []string `json:"lanes"`
	NextLanes   []string `json:"next_lanes"`
	DoneLane    string   `json:"done_lane"`
	Writable    bool     `json:"writable"`
	SchemaState string   `json:"schema_state"`
	Store       string   `json:"store"`
}

// taskJSON is one `furrow ls --json` row. Timestamps are RFC3339 or null.
// Body is a STORE-RELATIVE PATH ("bodies/<id>.md"), never content — furrow
// ships the pointer and the file is ours to read (the same contract `edit`
// serves interactively).
type taskJSON struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	Priority  int      `json:"priority"`
	Value     int      `json:"value"`
	Effort    int      `json:"effort"`
	Labels    []string `json:"labels"`
	Repos     []string `json:"repos"`
	Deps      []string `json:"deps"`
	Refs      []string `json:"refs"`
	Epic      string   `json:"epic"`
	Body      string   `json:"body"`
	Checklist []struct {
		Text string `json:"text"`
		Done bool   `json:"done"`
	} `json:"checklist"`
	Created  *time.Time `json:"created"`
	Updated  *time.Time `json:"updated"`
	Closed   *time.Time `json:"closed"`
	Reviewed *time.Time `json:"reviewed"`
	Due      *time.Time `json:"due"`
}

// epicJSON is one `furrow epic ls --json` row.
type epicJSON struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Deps  []string `json:"deps"`
	// open_deps is furrow-derived, like progress and stuck: the deps still
	// waiting, with deps on closed epics already resolved away (omitted
	// entirely when none remain). ridge never recomputes it.
	OpenDeps []string `json:"open_deps"`
	Progress struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	} `json:"progress"`
	Stuck bool `json:"stuck"`
}

// wipDefaults renders the WIP budget on the lanes that have one — Projects #5
// parity, display-only. furrow itself has no WIP concept, so this lives here.
var wipDefaults = map[string]int{"ready": 2, "in-progress": 1}

// load runs the three reads concurrently — lane vocabulary, every task
// (drafts included: that needs the empty -r), every epic — and assembles a
// Board. Measured on the real 914-task store: 63-77ms wall warm, 181ms cold,
// bodies included.
func (p *Store) load() (*board.Board, error) {
	var (
		cfg   boardJSON
		rows  []taskJSON
		boxes []epicJSON
		errs  [3]error
	)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		out, err := p.c.run("board", "board", "--json")
		if err == nil {
			err = json.Unmarshal(out, &cfg)
		}
		errs[0] = err
	}()
	go func() {
		defer wg.Done()
		out, err := p.c.run("ls", "ls", "-r", "", "--json")
		if err == nil {
			err = json.Unmarshal(out, &rows)
		}
		errs[1] = err
	}()
	go func() {
		defer wg.Done()
		out, err := p.c.run("epic-ls", "epic", "ls", "-r", "", "--json")
		if err == nil {
			err = json.Unmarshal(out, &boxes)
		}
		errs[2] = err
	}()
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	inNext := toSet(cfg.NextLanes)
	lanes := make([]board.Lane, 0, len(cfg.Lanes))
	for _, name := range cfg.Lanes {
		lanes = append(lanes, board.Lane{
			Name: name,
			Next: inNext[name],
			Done: name == cfg.DoneLane,
			WIP:  wipDefaults[name],
		})
	}

	tasks := make([]*board.Task, 0, len(rows))
	for _, r := range rows {
		t := &board.Task{
			ID:       r.ID,
			Title:    r.Title,
			Status:   r.Status,
			Priority: r.Priority,
			Value:    r.Value,
			Effort:   r.Effort,
			Labels:   r.Labels,
			Repos:    r.Repos,
			Deps:     r.Deps,
			Refs:     r.Refs,
			Epic:     r.Epic,
			Created:  fromPtr(r.Created),
			Updated:  fromPtr(r.Updated),
			Closed:   fromPtr(r.Closed),
			Reviewed: fromPtr(r.Reviewed),
			Due:      fromPtr(r.Due),
		}
		for _, c := range r.Checklist {
			t.Checklist = append(t.Checklist, board.ChecklistItem{Text: c.Text, Done: c.Done})
		}
		tasks = append(tasks, t)
	}
	if err := readBodies(cfg.Store, rows, tasks); err != nil {
		return nil, err
	}

	epics := make([]board.EpicInfo, 0, len(boxes))
	for _, e := range boxes {
		epics = append(epics, board.EpicInfo{
			ID: e.ID, Title: e.Title,
			Done: e.Progress.Done, Total: e.Progress.Total,
			Stuck: e.Stuck, Deps: e.Deps, OpenDeps: e.OpenDeps,
		})
	}

	return board.NewStoreBoard(lanes, tasks, epics, cfg.Writable, cfg.SchemaState), nil
}

// readBodies loads every task's prose eagerly. furrow's JSON carries only the
// store-relative body path, and the peek/editor want content without a
// per-open stall — measured on the real 906-task store this is a handful of
// milliseconds, folded into the load that already runs off the UI thread.
// A missing file is a loud error, not an empty peek: furrow's own reads never
// narrow silently, and neither should ours.
func readBodies(store string, rows []taskJSON, tasks []*board.Task) error {
	type job struct {
		path string
		dst  *board.Task
	}
	jobs := make(chan job)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				raw, err := os.ReadFile(j.path)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					continue
				}
				j.dst.Body = string(raw)
			}
		}()
	}
	for i, r := range rows {
		if r.Body == "" {
			continue
		}
		jobs <- job{path: filepath.Join(store, r.Body), dst: tasks[i]}
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errCh:
		return fmt.Errorf("reading a task body: %w", err)
	default:
		return nil
	}
}

func toSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func fromPtr(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// --- persists --------------------------------------------------------------

// setEnvelope is one element of `furrow set --json`'s per-id envelope array;
// only the respace report matters here. Each renumbered entry is
// {id, from, to} — the neighbour and its old/new priority.
type setEnvelope struct {
	Renumbered []struct {
		ID string `json:"id"`
	} `json:"renumbered"`
}

// PersistMove records an already-applied placement via `furrow set`
// (board.Provider).
func (p *Store) PersistMove(id, lane, beforeID, afterID string) ([]string, error) {
	args := []string{"set", id, "-s", lane}
	switch {
	case beforeID != "":
		args = append(args, "--before", beforeID)
	case afterID != "":
		args = append(args, "--after", afterID)
	}
	args = append(args, "--json")
	out, err := p.c.run("set", args...)
	if err != nil {
		return nil, err
	}
	var envs []setEnvelope
	if err := json.Unmarshal(out, &envs); err != nil {
		return nil, fmt.Errorf("furrow set: undecodable envelope: %v", err)
	}
	var renumbered []string
	for _, e := range envs {
		for _, r := range e.Renumbered {
			renumbered = append(renumbered, r.ID)
		}
	}
	return renumbered, nil
}

// PersistDone records an already-applied close via `furrow done`
// (board.Provider).
func (p *Store) PersistDone(id string) error {
	_, err := p.c.run("done", "done", id)
	return err
}

// PersistCheck records an already-applied checklist toggle via `furrow
// check` (board.Provider).
func (p *Store) PersistCheck(id string, i int, done bool) error {
	args := []string{"check", id, strconv.Itoa(i)}
	if !done {
		args = append(args, "--off")
	}
	_, err := p.c.run("check", args...)
	return err
}

// PersistBody asks furrow for the body path (`edit` prints it when there is no
// TTY) and replaces the file.
//
// `furrow edit --body -` would be better — it replaces the body AND stamps the
// entity's `updated` in one command, where a direct write leaves `updated`
// stale. It is implemented (t-8q8c, 2026-08-10) but NOT RELEASED: the newest
// furrow release is v4.0.0 (2026-08-09), and the contract job — which pins a
// release precisely so ridge cannot drift ahead of one — answers
// `unknown flag: --body`. Switching now would work only against a
// source-built furrow and break every released one, so it waits for the
// release that carries it.
func (p *Store) PersistBody(id, body string) error {
	out, err := p.c.run("edit", "edit", id, "--json")
	if err != nil {
		return err
	}
	var resp struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(out, &resp); err != nil || resp.Path == "" {
		return fmt.Errorf("furrow edit: no body path in %q", string(out))
	}
	// 0600 matches the mode furrow itself creates body files with.
	return os.WriteFile(resp.Path, []byte(body), 0o600)
}

var _ board.Provider = (*Store)(nil)

// Query evaluates q via `furrow ls -q` (board.Provider). The whole grammar
// lives furrow-side; ridge passes the string through untouched, so the filter
// bar and `furrow ls` can never disagree about what a query means.
func (p *Store) Query(q string) ([]string, error) {
	out, err := p.c.run("ls-q", "ls", "-r", "", "-q", q, "--json")
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("furrow ls -q: undecodable rows: %v", err)
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// PersistFields records an already-applied metadata edit (board.Provider).
// The set-shaped fields compose into ONE `furrow set` write; Title and repo
// edits are their own commands, so a mixed patch costs up to three writes.
func (p *Store) PersistFields(id string, patch board.FieldPatch) error {
	args := []string{"set", id}
	switch {
	case patch.Value == nil:
	case *patch.Value == 0:
		args = append(args, "--clear-value")
	default:
		args = append(args, "--value", strconv.Itoa(*patch.Value))
	}
	switch {
	case patch.Effort == nil:
	case *patch.Effort == 0:
		args = append(args, "--clear-effort")
	default:
		args = append(args, "--effort", strconv.Itoa(*patch.Effort))
	}
	for _, l := range patch.AddLabels {
		args = append(args, "--add-label", l)
	}
	for _, l := range patch.RmLabels {
		args = append(args, "--rm-label", l)
	}
	if patch.Epic != nil {
		args = append(args, "-e", *patch.Epic)
	}
	switch {
	case patch.Due == nil:
	case *patch.Due == "":
		args = append(args, "--clear-due")
	default:
		args = append(args, "--due", *patch.Due)
	}
	if len(args) > 2 {
		if _, err := p.c.run("set-fields", args...); err != nil {
			return err
		}
	}
	if patch.Title != nil {
		// `--` before the title: it is user free text, and furrow's parser
		// reads a leading `-` as a shorthand flag. Without it a retitle to
		// "-foo" exits 2 AFTER the board optimistically showed the rename,
		// so the user watched it land and then get yanked by the rollback.
		if _, err := p.c.run("retitle", "retitle", id, "--", *patch.Title); err != nil {
			return err
		}
	}
	if len(patch.AddRepos) > 0 || len(patch.RmRepos) > 0 {
		args := []string{"repo", id}
		for _, r := range patch.AddRepos {
			args = append(args, "--add", r)
		}
		for _, r := range patch.RmRepos {
			args = append(args, "--rm", r)
		}
		if _, err := p.c.run("repo", args...); err != nil {
			return err
		}
	}
	return nil
}

// PersistCheckAdd records an already-appended checklist item via `furrow
// check --add` (board.Provider).
func (p *Store) PersistCheckAdd(id, text string) error {
	_, err := p.c.run("check-add", "check", id, "--add", text)
	return err
}

// PersistCheckRm records an already-applied checklist deletion via `furrow
// check <i> --rm` (board.Provider).
func (p *Store) PersistCheckRm(id string, i int) error {
	_, err := p.c.run("check-rm", "check", id, strconv.Itoa(i), "--rm")
	return err
}

// PersistCheckReword records an already-applied checklist rewording via
// `furrow check <i> --reword` (board.Provider).
func (p *Store) PersistCheckReword(id string, i int, text string) error {
	_, err := p.c.run("check-reword", "check", id, strconv.Itoa(i), "--reword", text)
	return err
}

// addRow is the one field Add needs back from `furrow add --json` (a single
// add answers with ONE object; only --stdin bulk answers with an array).
type addRow struct {
	ID string `json:"id"`
}

// Add creates a task via `furrow add` (board.Provider), mapping the
// inherited context onto -s/-l/-e/-r.
func (p *Store) Add(title string, o board.AddOptions) (string, error) {
	args := []string{"add", "--json"}
	if o.Lane != "" {
		args = append(args, "-s", o.Lane)
	}
	if o.Label != "" {
		args = append(args, "-l", o.Label)
	}
	if o.Epic != "" {
		args = append(args, "-e", o.Epic)
	}
	if o.Repo != "" {
		args = append(args, "-r", o.Repo)
	}
	// The title goes LAST, behind `--`: it is user free text and a leading
	// dash would otherwise be parsed as a flag and refused.
	args = append(args, "--", title)
	out, err := p.c.run("add", args...)
	if err != nil {
		return "", err
	}
	var row addRow
	if err := json.Unmarshal(out, &row); err != nil || row.ID == "" {
		return "", fmt.Errorf("furrow add: undecodable reply: %v", err)
	}
	return row.ID, nil
}
