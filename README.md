# ridge

**The TUI front-end for furrow.** A keyboard-first kanban that reads and
writes [furrow](https://github.com/akira-toriyama/furrow) through its CLI/JSON
contract. The GUI sibling is [vista](https://github.com/akira-toriyama/vista).
The contract that ridge never imports furrow's Go packages, and why, is in
[CLAUDE.md](CLAUDE.md); terms are canonical in [glossary.md](glossary.md).

```sh
go run ./cmd/ridge            # start on the real furrow board (furrow v6.0.0+ on PATH — the contract job's pin in .github/workflows/build.yml is the floor)
go run ./cmd/ridge -mock      # start on the built-in fixture (no furrow needed)
go run ./cmd/ridge -dump      # emit one frame with no TTY (the fixture; -live for the real board)
go run ./cmd/ridge -benchload # measure the real board's load latency and exit (read-only)
```

## Status

Connected to the real furrow (t-s86r). Reads are three parallel execs —
`board --json` / `ls -r '' --json` / `epic ls -r '' --all --json` — plus the
body files (`-benchload` measures the latency on the real board). Writes go
through the **optimistic queue** (glossary: "persist queue"), which streams
`furrow set / done / check / retitle / repo / ref / dep / note / review /
edit --body` serially in the background — the list is canonical in the
Persist* methods of `internal/board/provider.go`. The exception is the
**store-first** writes (quick add and epic management — glossary:
"store-first").

## Views

The meaning of each view and the design reasons are canonical in the
glossary. This section is only what each view answers, and how to drive it.

### Board — kanban

Lanes are columns. The header carries counts, WIP and the value/effort sums.
Cards wrap their Japanese titles and carry `▸` actionable / `▤` epic chip /
`x1` blocked / `[0/7]` checklist / `⟳` repeats / label chips / repo.

**Move mode** is the central gesture. It follows GitHub Projects (`Enter`
lifts → arrows move → `Enter` commits / `Esc` cancels) and maps 1:1 onto
furrow's sparse-priority reordering. Cards can also be dragged with the mouse.

### Table — the flat view

`v` toggles it against the board (it is the `table` layout a saved view can
name). One row per task, the same population and filter as the board, sorted
by `o`'s cycle (updated / created / value / effort / due, each with a natural
direction) or by clicking a header cell — a second click flips it. `Enter`
opens the same field edit menu the peek has. It has no horizontal scroll
(under **Known gaps**).

### Graph — dependency graph

`S` (or `Shift+Space`) on a card. A layered graph rooted at that task:
**blockers in front, "what starts moving when this closes" behind**. `Enter`
re-roots on a node, so you walk the graph rather than read it. `o` switches
between top-down and left-right (top-down by default; the contract of the
two orientations is glossary: "orientation").

```
 ╭────────────────╮   ╭────────────────╮
 │ v t-ecfm       │   │ v t-g8bn       │
 ╰────────────────╯   ╰────────────────╯
          ╷                    ╷
          ╰────────────────────┤
                               ▼
       ┏━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
       ┃ x t-jv3j ◉ focus          ┃
       ┗━━━━━━━━━━━━━━━━━━━━━━━━━━━┛
                    ╷
       ├────────────┤
       ▼            ▼
 ╭──────────────╮  ╭──────────────╮
 │ x t-pk4f     │  │ x t-rmtc     │
 ╰──────────────╯  ╰──────────────╯
```

`o` turns the same graph left-right (`-dump -graphlr` renders it headless):

```
 ╭────────────────╮                        ╭──────────────╮
 │ v t-ecfm       │───╮                ╭──▶│ x t-pk4f     │
 ╰────────────────╯   │   ┏━━━━━━━━━┓  │   ╰──────────────╯
                      ├──▶┃ t-jv3j  ┃──┤
 ╭────────────────╮   │   ┗━━━━━━━━━┛  │   ╭──────────────╮
 │ v t-g8bn       │───╯                ╰──▶│ x t-rmtc     │
 ╰────────────────╯                        ╰──────────────╯
```

Ranks that do not fit are dropped and counted in the header (`z` narrows the
radius) — nothing is silently clipped at the right edge.

`ridge -graph` opens the real board in this view, rooted on the opening
cursor (the first card of the first lane with work, after `-filter` has
narrowed the board). With nothing under it — an empty board, a filter that
excludes every card — the board is drawn and the status line says so.
Headless: `-dump -graph`; the fixture's states are `-demo graph` /
`graphall`.

### Map — dependency map

`T` (the whole-board version of `t`, that task's dependency tree). **Every
dependency cluster on the board, laid out on one screen.** Where Graph answers
"what surrounds this task", Map has no root and answers "what on the board is
entangled with what". No lines are drawn: indentation is depth and `←` names
the blocker (glossary: "Map", "cluster", "scope").

`ridge -map` opens the real board in this view. Headless: `-dump -map`; the
fixture's states are `-demo map` / `mapall` / `mapfiltered`.

An excerpt of `-dump -demo map` (the real output is 240 columns in three
columns; this is narrowed to two):

```
 ── #1  4 nodes · depth 2 ────────────────────  ── #2  2 nodes · depth 1 ────────────────────
     t-ehk7 キャンプ献立プラン — 3泊分の夕食…       t-fn4k 装備の安全講習 — ナイフと火の…
 ▌   x t-jv3j 行程表 v2 — 阿蘇→高千穂を…  ←t-ehk7     x t-0esb 子どもの自由研究連動 —…  ←t-fn4k
       x t-pk4f 買い出し計画 — 道の駅ごとに… ←t-jv3j   1 unblocked · 1 blocked · t-fn4k holds up 1
       x t-rmtc 予約の総ざらい — 温泉・…      ←t-jv3j
   1 unblocked · 3 blocked · t-ehk7 holds up 3
```

- `z` switches scope (**open** by default; `all` for everything).
- `⏎` / `S` opens the Graph rooted at that row. `Esc` **returns to the Map**.
- An active filter never removes rows; they are dimmed and counted in the
  header.

### Peek — the detail pane

`Space` opens it. Resolved two-way dependency lists (`blocked by` / `blocks`
resolved to id, title and lane), the checklist, the body; a recurring task's
rule and its series start (`⟳ repeats FREQ=… (since <day>)` — the two facts
of `furrow show`'s `repeat:` line, the start as a day). `t` shows the
transitive tree. `Enter` opens the **field edit
menu** (glossary: "edit menu"): title / value / effort / labels / epic / due /
deps / repos / refs / checklist — the repeat rule is not among them
(`furrow set --repeat` is the CLI's).

### Boxes — the box overview

`E`. **Every epic on the board, grouped by repo.** Where Graph / Map answer
task dependencies, this answers "which box is each repo working out of right
now" (why it is not a graph: glossary, "box overview"). `⏎` slices the board
to that box (an `epic:<id>` slice term), `m` opens the box's overlay, `z`
includes closed boxes, `^u/^d` page.

`ridge -boxes` opens the real board in this view. Headless: `-dump -boxes`;
the fixture's states are `-demo boxes` / `boxesall`.

### Roadmap — the due timeline

`C`. **Open tasks with a due date, in due order, with `◆` placed on a time
axis.** The view that answers "what expires when" — the timeline form of
`furrow brief`'s due head and `-q is:overdue` (glossary: "Roadmap", "zoom").
`z` cycles day / week / month, `h`/`l` pan the window. Read-only.

`ridge -roadmap` opens the real board in this view. Headless: `-dump -roadmap`
(day) and `-demo roadmapweek` / `-demo roadmapmonth`.

### Swim — swimlanes (group by)

`W`. **A two-dimensional grid: the board's lanes across, the group-by values
down as "bands".** `furrow ls --tree` given a second axis; the default axis is
also **box**, matching `--tree` (`tab` cycles to repo / label). Bands start folded (a folded
frame is a histogram of the board) and `space` unfolds one. `⏎` slices the
board to that band, `z` switches scope open/all. Read-only (the band and rail
terms, and why there are no writes: glossary, "Swimlane", "band", "rail").

`ridge -swim` opens the real board in this view. Headless: `-dump -swim`; the
fixture's states are `-dump -demo swim` (default) / `swimopen` (a band
unfolded) / `swimrepo` (repo axis) / `swimall` (scope all).

### Sweep — archive / tidy / unarchive

`X`. **furrow's three maintenance passes, previewed and applied from one
screen.** Four sections, one cursor: the done tasks past the age guard
(`furrow archive` dry-run over the whole board), the open tasks with
satisfied dep edges and the shards with unknown keys (`furrow tidy`), and the
archive store (`furrow ls --archived`). Every candidate is furrow's; ridge
derives none. `⏎` on a row names the write in the header line — the exact
id list an archive sends, the count a tidy class prunes, the one task a
restore brings back — and a second `⏎` applies it; any other key cancels
(`ctrl+c` still quits).
`x` skips / includes an archive row: the write sends the **explicit ids on
screen**, never the id-less sweep, so what moves is what was previewed. `r`
re-reads the previews. Every write is store-first (glossary): the frame keeps
the pre-write preview until the write lands and both the board and the
previews are re-read.

`ridge -sweep` opens the real board in this view. Headless: `-dump -sweep`
(with `-live`, the real previews — the three reads run before the frame).
The fixture's states are `-dump -demo sweep` (at rest) / `sweepconfirm` (the
archive gate, one row skipped) / `sweeprestore` (the restore gate on an
archived row) / `sweepwait` (opened behind a queued write — the read is
deferred and the frame says so).

### Saved views — tabs + views.toml

The equivalent of GitHub Projects' view tabs (terms: glossary, "saved view",
"unsaved dot"). The file is `~/.config/ridge/views.toml` (`XDG_CONFIG_HOME`
honoured).

```toml
[[view]]
name = "this week's deadlines"
layout = "roadmap"    # board | table | roadmap (omitted = board)
q = "is:actionable"   # passed to furrow -q verbatim
sort = "due asc"      # updated|created|value|effort|due [asc|desc] (table only)
slice = "epic:e-xxxx" # repo|label|epic :value (same as the slice panel's selection)
```

- The tab strip sits right of Board|Table on the title line. `1`-`9` switch,
  `V` saves the current state into the active tab. `V` with no tabs creates
  "view N" (rename by editing views.toml). At most nine.
- When the state drifts from the active tab, the tab shows **●**. Pressing the
  digit again rewinds to the saved bundle. `1`-`9` / `V` also work inside the
  roadmap view.
- Fixture runs (`-mock` / `-dump` / `-readonly`) neither read nor write the
  real views.toml. Headless surfaces: `-demo views` / `-demo viewsroad` /
  `-demo viewsmany`.

## Keys

**`?` is the canon for every key** — it is built from the same `key.Binding`s
the handlers match (`HelpSections` in `internal/ui/keys.go`), so it can never
advertise a key that does not work. This table is only a foothold.

| Key | Action |
|---|---|
| `?` | **Key list** (everything is reachable from here) |
| `Space` | Peek |
| `s` | Slice panel (the left side nav: repo / label / epic; `tab` switches the axis, `⏎` slices, `z` includes closed boxes, `m` / `A` manage a box). A box title too long for the panel wraps to a second line, the row carries the repo where the title does not say it, and the bottom row names the row under the cursor in full |
| `S` | Dependency graph (rooted at one task; `o` for top-down / left-right) |
| `T` | Dependency map (every cluster at once) |
| `E` | Box overview (every epic by repo; `⏎` slices to that box, `z` includes closed) |
| `C` | Roadmap (due timeline; `z` for day/week/month, `h`/`l` pan) |
| `W` | Swimlanes (lanes × group by; `space` folds/unfolds a band, `tab` switches axis, `⏎` slices to the band) |
| `X` | Sweep (furrow archive / tidy / unarchive; `⏎` `⏎` applies the row's write, `x` skips an archive row) |
| `1`-`9` / `V` | Switch / save a saved view (views.toml is canonical; `V` with no tabs = new) |
| `Enter` | Move mode (`Enter` commits, `Esc` cancels). **With peek open, or in Table: the edit menu** |
| `f` | Revisit lens (only the open tasks `furrow revisit` flags; the reason in peek; `-revisit` headless) |
| `i` | Stamp the selected task reviewed (`furrow review <id>`; `updated` does not move) |
| `q` | Quit |

The bottom of the screen is one line, and it carries **what is not on the
screen** (the exit of the mode just entered, failures, load results) — plus,
inside a full-screen view, that view's own two-to-five keys, because none of
those views has a footer. It is never a key *reference*: the board's own
footer lists no keys at all, because a partial list was worse than none for
exactly as much as it made a reader think "that is all of them". While the slice panel holds
the keyboard the row names the box under the cursor in full, which is the same
rule: the panel's rows are scanned, and a title it had to cut is precisely what
is not on the screen.

## Design rules

- **Wide by assumption**: narrow terminals are out of scope (the target
  display and the column floor and target are in [CLAUDE.md](CLAUDE.md)).
- **Keyboard first**: everything the mouse can do has a keyboard equivalent.
  Mouse tracking disables the terminal's own text selection (the bypass key
  depends on the terminal: xterm/Ghostty/tmux = `Shift`, iTerm2 = `Option`),
  so `M` turns it off.
- **Optimistic TUI**: the screen updates before a write completes, and quit
  flushes the pending writes first. Only writes whose meaning lives on
  furrow's side are not applied ahead (glossary: "persist queue",
  "store-first").
- **Logic lives in furrow** (the rule of thumb is in [CLAUDE.md](CLAUDE.md)).

## Verification

Everything is checkable headless (no human has to look at a GUI or terminal):

```sh
go test ./...                      # every test (the contract tests run too when furrow is on PATH)
go run ./cmd/ridge -dump -plain -cols 240 -rows 60 # one frame as plain text
go run ./cmd/ridge -dump -peek               # with the peek open
go run ./cmd/ridge -dump -tree               # with the dependency tree open
go run ./cmd/ridge -demo drag -dump          # a transient state: one frame mid-drag
go run ./cmd/ridge -h                        # the list of every -demo state (canonical: ui.DemoNames)
go run ./cmd/ridge -readonly -dump           # a board made read-only by the schema gate
go run ./cmd/ridge -dump -demo unlaned       # a task whose status names no lane: the load note counts it, the lanes do not
go run ./cmd/ridge -graphlr -dump -demo graphall  # the dependency graph left-right (the same state as `o`)
go run ./cmd/ridge -dump -roadmap            # the due timeline (week/month axes: -demo roadmapweek / roadmapmonth)
go run ./cmd/ridge -dump -demo sweepconfirm  # the sweep with the archive gate open (sweep / sweeprestore / sweepwait are the others)
go run ./cmd/ridge -dump -demo done          # a task just closed with d: "closed <id> — unblocked N task(s)" counts what the close freed, not what depends on it
go run ./cmd/ridge -dump -demo repeatdone    # a recurring task just closed: its successor's card and the "repeat: next due <day> (<id>)" line (-demo repeat: the rule in the peek)
go run ./cmd/ridge -dump -live -plain -cols 320   # the REAL board (cwd, or FURROW_DIR) at rest — invariants and eyes, not golden: the frame carries the load time and today's dates
go run ./cmd/ridge -dump -live -sweep        # any view on the real board: -table / -roadmap / -graph / -map / -boxes / -swim / -sweep (-demo is the fixture's; refused with -live)
```

What `-dump` / `-demo` / `-live` / `-graphlr` / `-readonly` and the
opening-view flags mean, and why the latter are flags rather than `-demo`
states, is in the glossary's "Internals" section. The one-line description of
each `-demo` state lives in `internal/ui/demo.go`'s `demoState` (the comment
on each case).

### `-debuglog` — a structured log of the session's operations

```sh
go run ./cmd/ridge -debuglog session.jsonl        # the real board, every event recorded
go run ./cmd/ridge -mock -debuglog session.jsonl  # the fixture records too
```

Attach this file to any "I did X and the board ended up like Y" bug report
(the layers and hook points: glossary, "-debuglog"). **Keystrokes are
recorded one by one, verbatim** (including titles typed into modals and
filter text; only task bodies are absent) — check the contents before handing
it to anyone.

## Known gaps

- Swimlanes (`W`) are **not a saved-view layout**. The bundle is
  `{layout, q, sort, slice}` while this view's state also has an axis and a
  scope, so saving `layout = "swim"` alone would be a save that does not
  restore (the same reason map / boxes are excluded).
- The Table view has no horizontal scroll (a wide-by-assumption decision. If
  needed, wire the existing bubbles viewport v2's `SoftWrap=false` +
  `XOffset` — confirmed to need no new implementation. Trap: `SetXOffset` is a
  silent no-op while `SoftWrap=true`).

## Stack

```
charm.land/bubbletea/v2      runtime
charm.land/lipgloss/v2       styles, layout, compositor (Layer / Hit)
charm.land/bubbles/v2        help / key / textinput / viewport
github.com/charmbracelet/x/ansi  width-preserving truncation (a CJK must; the partner of the `len()` ban)
github.com/pelletier/go-toml/v2  views.toml I/O (saved views)
```

Note that since v2 the module paths moved from `github.com/charmbracelet/*`
to `charm.land/*` (v1 keeps the old paths).
