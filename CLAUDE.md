# ridge — repo conventions for Claude Code

Terms are canonical in [glossary.md](glossary.md) (add or rename a term in the
same PR as the code change). What this repository is: [README.md](README.md).

## Where this repo stands (read first)

- **ridge is a CLI/JSON client of furrow.** It does **not import** furrow's Go
  packages. That is not taste but the arrangement written down in
  [furrow's non-goals](https://github.com/akira-toriyama/furrow/blob/main/docs/non-goals.md);
  the GUI sibling [vista](https://github.com/akira-toriyama/vista) follows the
  same contract.
- **Logic has one home: furrow.** When torn over "if I write this in ridge,
  does vista need the same thing?", send a PR to furrow instead. The rule of
  thumb is furrow board `t-ehk7`: "would the logic be duplicated if someone
  built a TUI?". ridge is the second consumer that rule predicted, and
  **keeping business logic out of the front-end** is the reason this repo
  exists at all.
- **Task management lives in furrow** (`repos: akira-toriyama/ridge`). GitHub
  issues are not used.

## Go

- Confirm `go build ./...` / `go test ./...` pass before finishing.
  `GOTOOLCHAIN=local` (go.mod's version is canonical).
- House style follows the go-dev skill (thin main + `internal/`, typed exit
  codes, stdlib-only tests). Layout: `cmd/ridge` (a three-line main) /
  `internal/cli` (flags, exit codes) / `internal/board` (pure core + the
  Provider port) / `internal/store/{furrowstore,memstore}` (adapters) /
  `internal/ui` (the whole TUI) / `internal/views` (the saved views' on-disk
  vocabulary and views.toml I/O — the one file ridge writes).
  The filter is a furrow `-q` pass-through — ridge holds no query grammar
  (memstore's approximate evaluator is for -dump and tests only). Layer
  contracts are canonical in each package's head doc comment.
- Tests use the stdlib only (no testify).

## bubbletea v2 gotchas (known — do not rediscover)

Much changed from v1. Each of these was hit and confirmed:

- **`View()` returns a `tea.View` struct** (v1: string). `AltScreen` /
  `MouseMode` / `KeyboardEnhancements` are **fields re-declared every frame**,
  not `NewProgram` options.
- **Use `case tea.KeyPressMsg:`.** `tea.KeyMsg` is an interface matching both
  press and release, so in a type switch it must come **after** `KeyPressMsg`
  or it swallows the press.
- **`key.WithKeys("space")`**, not `" "`. **No compile error — it silently
  stops working.**
- **`Shift+Space` never reaches a plain terminal.** Space is sent **as text**,
  and text carries no modifiers. Only with `ReportAllKeysAsEscapeCodes` in
  `View.KeyboardEnhancements` (the Kitty protocol) can the two be told apart.
  Some terminals do not support it, so **every modifier gesture needs a
  bare-key alias** (`S` for the graph).
- **Mouse**: use `MouseModeCellMotion` (1002+1006). `AllMotion` (1003) is
  poorly supported under tmux/mosh and unnecessary for drag. **Remember the
  button yourself from the last `MouseClickMsg`** — some terminals report no
  button on motion events.
- **`bubbles/v2` has almost no mouse support** (only the viewport's wheel).
  list/table: none. Drag and hit testing are hand-rolled.
- **Use the `lipgloss/v2` compositor**: `Layer` with X/Y/**Z** and
  `Compositor.Hit(x,y)`. Ghosts, overlays and hit testing are native.
  **Do not use `bubblezone`** (its author warns it cannot coexist with the v2
  compositor).
- `lipgloss` v2 dropped `AdaptiveColor`. The `DefaultStyles` family takes
  `isDark bool`.

## CJK — the board is mostly Japanese

- **Always measure width with `lipgloss.Width` (display width). `len()` is
  forbidden.** Task titles are Japanese; those with dependencies have a median
  display width of 85 cells and a p90 of 141 (2026-09-03). One character eats
  two cells, so `len()` always breaks a frame.
- Never truncate by bytes either (use `ansi.Truncate` or similar).
- **When laying out bordered boxes side by side, always `-dump` at several
  widths and eyeball the column alignment.** Drift accumulates one cell at a
  time: invisible on a narrow screen, exposed on a wide one.

## Rendering and layout

- **Layout and hit testing are built from the same measurement**
  (`internal/ui/layout.go`). Card heights are measured by actually rendering,
  so "the drawing is right but the click lands elsewhere" is prevented
  structurally.
- **The card-height cache persists across frames and is discarded by
  `recompute()`** (measurer — the measured numbers, and what drifts when the
  discard is forgotten, are in the glossary).
- **Wide by assumption**: the target display is 3840×1620 (32:9). **240
  columns is the floor, 400 the target**; no fallback for narrow terminals is
  written.

## Verification (keep it checkable without a human at a GUI or terminal)

- `-dump` emits one frame with no TTY. `-plain` strips ANSI, so it diffs.
- **Every state that changes the frame must be producible headless in one
  frame** (`-demo` / `-readonly`). A state that cannot be produced is a hole in
  verification — a regression that erased the read-only warning once got
  through.
- **Mid-gesture states are frozen into one frame with `-demo`** (the names are
  canonical in `ui.DemoNames` — a copy here always goes stale: after PR #22
  unified three copies, this very line stayed at 8/10). "Is the drop marker
  showing?" is not left to human eyes.
- **The mouse can be driven by feeding synthetic SGR bytes to `tea.WithInput`**
  (`\x1b[<0;X;YM` press / `\x1b[<32;X;Ym` move / `\x1b[<0;X;Ym` release).
  End-to-end tests that run a real Program use this.
- After adding UI, **run `-dump` at several widths and check the frames by
  eye** (for the CJK column alignment above).

## Pre-merge gate (recurrence prevention for the 2026-08-10 session decay — t-xmry)

- **Run lint/test through `scripts/check.sh`** — a byte-identical local mirror
  of CI (the hub go-ci reusable). A bare `golangci-lint run` covers only the
  default set and let CI-only revive findings of the same class through twice
  (PR #8, #10). When the linter list changes, keep it byte-identical against
  the CI log.
- **The pre-push hook enforces check.sh.** Once per clone:
  `git config core.hooksPath scripts/hooks` (`git push --no-verify` only in an
  emergency).
- **An implementation PR needs one review pass independent of the context that
  implemented it before merge** (a separate agent/session told explicitly to
  "refute"; verification and review sit on the Opus side). The implementer's
  self-check and a green CI are no substitute — PR #5–#9 were merged on
  self-checks alone and forced a full independent re-review (t-74y3).
  Exceptions: docs-only changes that touch no logic, or a few-line mechanical
  fix.

## Commits

gitmoji-driven. Do not recite the format (sigil included) — open
[docs/commit-convention.md](docs/commit-convention.md) → `glyph.toml`
(the `[!]`-only format once copied into this document drifted).
Subject and body in English. One change = one PR (squash); docs are updated in
the same PR.
