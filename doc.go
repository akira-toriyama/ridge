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
// The POC's shortcut is still in place and is the first thing to fix: the
// Provider is backed by fixture.go, not by furrow. Nothing here reads or writes
// a real .furrow store yet.
//
// What it FAKES
//
//   - The data is a hardcoded in-memory copy of 24 real tasks (fixture.go).
//     The Provider interface is the seam where a real `furrow --json` client
//     would drop in; the mock never shells out and never touches a real
//     .furrow store. Mutations live and die with the process.
//   - `e` (edit body) launches $EDITOR on a temp file via tea.ExecProcess and
//     writes the result back into the in-memory board only.
//
// # What it is NOT
//
// This is a POC on a throwaway branch. furrow itself stays CLI-only and
// charm-free: this is a separate Go module precisely so that no charm
// dependency can reach furrow's core. Any real TUI would be an out-of-repo
// front-end (ridge) speaking furrow's CLI/JSON contract.
package main
