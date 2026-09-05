package furrowstore

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The sweep half of the adapter (board.Provider): furrow's archive / tidy /
// unarchive, read as previews and applied by explicit id or class. Every
// shape below was measured on furrow v5.1.0 (82b181b) against a scratch
// store; the contract tests pin them against the release build.yml pins.

// archiveJSON is `furrow archive --json`: dry_run says whether anything
// moved, tasks are ls rows. older_than_days and repos ride only on the sweep
// form, never on the id form — the preview is the sweep, the write is by id.
type archiveJSON struct {
	DryRun        bool       `json:"dry_run"`
	OlderThanDays int        `json:"older_than_days"`
	Tasks         []taskJSON `json:"tasks"`
}

// tidyJSON is `furrow tidy --json`. Each class is absent from the object when
// empty, so both are pointers-by-omission (nil slices).
type tidyJSON struct {
	Applied  bool `json:"applied"`
	Changed  bool `json:"changed"`
	DoneDeps []struct {
		ID   string   `json:"id"`
		Deps []string `json:"deps"`
	} `json:"done_deps"`
	UnknownKeys []struct {
		ID   string   `json:"id"`
		File string   `json:"file"`
		Keys []string `json:"keys"`
	} `json:"unknown_keys"`
}

func sweepTaskOf(r taskJSON) board.SweepTask {
	return board.SweepTask{ID: r.ID, Title: r.Title, Repos: r.Repos, Closed: fromPtr(r.Closed)}
}

// SweepPreview runs the three reads in parallel, the way load runs its three
// (board.Provider). All-or-nothing: one refused read fails the preview, so a
// frame never shows two fresh sections beside one stale one.
func (p *Store) SweepPreview() (board.Sweep, error) {
	var (
		arch archiveJSON
		tidy tidyJSON
		rows []taskJSON
		errs [3]error
	)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		// The empty -r is load's: the board ridge shows is the whole board,
		// so the sweep it previews is the whole board's. No --yes: dry run.
		out, err := p.c.run("archive-preview", "archive", "-r", "", "--json")
		if err == nil {
			err = json.Unmarshal(out, &arch)
		}
		errs[0] = err
	}()
	go func() {
		defer wg.Done()
		out, err := p.c.run("tidy-preview", "tidy", "--json")
		if err == nil {
			err = json.Unmarshal(out, &tidy)
		}
		errs[1] = err
	}()
	go func() {
		defer wg.Done()
		out, err := p.c.run("ls-archived", "ls", "-r", "", "--archived", "--json")
		if err == nil {
			err = json.Unmarshal(out, &rows)
		}
		errs[2] = err
	}()
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return board.Sweep{}, err
		}
	}
	if !arch.DryRun {
		// A preview that MOVED something is a contract break worth refusing
		// loudly: the id-less form without --yes is documented as dry-run.
		return board.Sweep{}, fmt.Errorf("furrow archive preview reported dry_run=false — refusing to trust the read")
	}
	s := board.Sweep{OlderThanDays: arch.OlderThanDays}
	for _, r := range arch.Tasks {
		s.Archivable = append(s.Archivable, sweepTaskOf(r))
	}
	for _, d := range tidy.DoneDeps {
		s.DoneDeps = append(s.DoneDeps, board.TidyDoneDep{ID: d.ID, Deps: d.Deps})
	}
	for _, u := range tidy.UnknownKeys {
		s.UnknownKeys = append(s.UnknownKeys, board.TidyUnknownKey{ID: u.ID, File: u.File, Keys: u.Keys})
	}
	for _, r := range rows {
		s.Archived = append(s.Archived, sweepTaskOf(r))
	}
	return s, nil
}

// Archive retires exactly ids via `furrow archive <ids> --yes`
// (board.Provider). The ids are positionals; `--` fences them so an id can
// never be read as a flag, and the empty list is refused here because without
// ids the same command is the aged sweep.
func (p *Store) Archive(ids []string) error {
	if err := board.ValidateSweepIDs("archive", ids); err != nil {
		return err
	}
	args := append([]string{"archive", "--yes", "--json", "--"}, ids...)
	out, err := p.c.run("archive", args...)
	if err != nil {
		return err
	}
	var reply archiveJSON
	if err := json.Unmarshal(out, &reply); err != nil {
		return fmt.Errorf("furrow archive: undecodable reply: %v", err)
	}
	if reply.DryRun || len(reply.Tasks) != len(ids) {
		return fmt.Errorf("furrow archive: moved %d of %d (dry_run=%s)", len(reply.Tasks), len(ids), strconv.FormatBool(reply.DryRun))
	}
	return nil
}

// Unarchive restores exactly ids via `furrow unarchive <ids>`
// (board.Provider). All-or-nothing furrow-side: a miss is exit 1 with the
// misses in details, an id already on the hot board exit 2.
func (p *Store) Unarchive(ids []string) error {
	if err := board.ValidateSweepIDs("unarchive", ids); err != nil {
		return err
	}
	args := append([]string{"unarchive", "--json", "--"}, ids...)
	out, err := p.c.run("unarchive", args...)
	if err != nil {
		return err
	}
	var envs []struct {
		Unarchived bool `json:"unarchived"`
		After      struct {
			ID string `json:"id"`
		} `json:"after"`
	}
	if err := json.Unmarshal(out, &envs); err != nil {
		return fmt.Errorf("furrow unarchive: undecodable reply: %v", err)
	}
	if len(envs) != len(ids) {
		return fmt.Errorf("furrow unarchive: restored %d of %d", len(envs), len(ids))
	}
	for _, e := range envs {
		if !e.Unarchived {
			return fmt.Errorf("furrow unarchive: %s reports unarchived=false", e.After.ID)
		}
	}
	return nil
}

// Tidy prunes one class via `furrow tidy <class> --yes` (board.Provider).
func (p *Store) Tidy(class board.TidyClass) error {
	flag := class.Flag()
	if flag == "" {
		return fmt.Errorf("tidy: unknown class %d", int(class))
	}
	out, err := p.c.run("tidy", "tidy", flag, "--yes", "--json")
	if err != nil {
		return err
	}
	var reply tidyJSON
	if err := json.Unmarshal(out, &reply); err != nil {
		return fmt.Errorf("furrow tidy: undecodable reply: %v", err)
	}
	// An EMPTY class answers applied=false, changed=false at exit 0 (measured:
	// `tidy --unknown-keys --yes` on a clean store) — nothing to prune is not a
	// refusal. Only "there was something and it was not applied" is one.
	if !reply.Applied && reply.Changed {
		return fmt.Errorf("furrow tidy %s reported applied=false with changes pending", class)
	}
	return nil
}
