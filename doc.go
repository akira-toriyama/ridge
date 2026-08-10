// Command ridge is furrow's TUI front-end: a GitHub-Projects-shaped kanban over
// furrow's task model, built on bubbletea/v2 + lipgloss/v2's native layer
// compositor.
//
// It started as a proof-of-concept on furrow's poc/tui-bubbletea-v2 branch,
// which answered three questions: a GH-Projects visual grammar IS achievable in
// a terminal, mouse drag-and-drop IS possible (~200 lines, because lipgloss v2
// ships z-ordered layers and hit-testing), and dependencies CAN be made legible
// — including a real layered graph, which no Go library draws to text, so both
// the layout and the renderer here are ours.
//
// # Data
//
// The default provider is the real furrow store: reads are three concurrent
// `furrow ... --json` execs plus the body files (furrow's JSON carries body
// PATHS, not content), writes are optimistic — applied to the in-memory board
// on the UI thread first, then recorded through a strictly-serial persist
// queue (persist.go) with a store re-read as the rollback. ridge never imports
// furrow's Go packages; the CLI/JSON contract is the whole boundary (furrow's
// non-goals doc).
//
// The fixture provider (-mock) serves a hardcoded in-memory copy of 24 real
// tasks (fixture.go). Tests, -dump and -demo always use it: their frames are
// deterministic and diffable, which is the house verification style. The
// contract tests (provider_furrow_test.go) cover the real client against a
// throwaway store, and skip where no furrow binary is on PATH.
package main
