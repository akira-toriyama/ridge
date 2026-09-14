// Command ridge is furrow's TUI front-end: a GitHub-Projects-shaped kanban
// over furrow's task model, built on bubbletea/v2 + lipgloss/v2's native
// layer compositor. Both the layered dependency graph's layout and its
// renderer are ours: no Go library draws one to text.
//
// # Data
//
// The default provider is the real furrow store (internal/store/furrowstore):
// reads are three concurrent `furrow ... --json` execs plus the body files
// (furrow's JSON carries body PATHS, not content), writes are optimistic —
// applied to the in-memory board on the UI thread first, then recorded
// through a strictly-serial persist queue with a store re-read as the
// rollback. ridge never imports furrow's Go packages; the CLI/JSON contract
// is the whole boundary (furrow's non-goals doc).
//
// The fixture provider (-mock, internal/store/memstore) serves a hardcoded
// in-memory board with CJK-heavy titles and epic entities. Tests, -dump and
// -demo always use it: their frames are deterministic and diffable, which is
// the house verification style. The contract tests (furrowstore) cover the
// real client against a throwaway store, and skip where no furrow binary is
// on PATH.
package main

import (
	"os"

	"github.com/akira-toriyama/ridge/internal/cli"
)

func main() { os.Exit(int(cli.Execute())) }
