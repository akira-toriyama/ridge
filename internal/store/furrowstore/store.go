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

func (p *Store) Board() *board.Board {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.b
}

func (p *Store) Live() bool { return true }

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
	Terminal    []string `json:"terminal"`
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
	ID       string `json:"id"`
	Title    string `json:"title"`
	Goal     string `json:"goal"`
	Progress struct {
		Done  int `json:"done"`
		Total int `json:"total"`
	} `json:"progress"`
	Stuck  bool `json:"stuck"`
	Active bool `json:"active"`
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

	inNext, inTerm := toSet(cfg.NextLanes), toSet(cfg.Terminal)
	lanes := make([]board.Lane, 0, len(cfg.Lanes))
	for _, name := range cfg.Lanes {
		lanes = append(lanes, board.Lane{
			Name: name,
			Next: inNext[name],
			Done: name == cfg.DoneLane,
			Term: inTerm[name],
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
			ID: e.ID, Title: e.Title, Goal: e.Goal,
			Done: e.Progress.Done, Total: e.Progress.Total,
			Stuck: e.Stuck, Active: e.Active,
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

func (p *Store) PersistDone(id string) error {
	_, err := p.c.run("done", "done", id)
	return err
}

func (p *Store) PersistCheck(id string, i int, done bool) error {
	args := []string{"check", id, strconv.Itoa(i)}
	if !done {
		args = append(args, "--off")
	}
	_, err := p.c.run("check", args...)
	return err
}

// PersistBody asks furrow for the body path (`edit` prints it when there is no
// TTY) and replaces the file. Direct body edits are inside furrow's contract —
// bodies are the hand-editable half of the store — but they do NOT advance the
// shard's `updated`; the fix for that gap is filed upstream (t-8q8c), and until
// it lands the staleness drift is accepted (t-s86r's recorded decision).
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
