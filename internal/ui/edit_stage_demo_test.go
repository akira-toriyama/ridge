package ui

import (
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// stagePick and stageInput exist only between two keystrokes of a live
// overlay, so until t-36yr no frame could show them: a regression that blanked
// either stage would have shipped unseen. These pin what each demo frame must
// prove; footer_test's DemoNames sweep already guarantees both render and
// differ from the bare board.
func TestEditStageDemosProveTheirStages(t *testing.T) {
	for _, tc := range []struct {
		demo  string
		wants []string
	}{
		// The picker: its field header and the full key contract.
		{"editpick", []string{"edit t-9sa6", "set value", "press 1-5 · 0 clears · esc back"}},
		// The input: the sub-editor's name, the apply/back keys, and the tail
		// of the seeded CJK title (the cursor sits at the end, so the 48-cell
		// window shows the value's tail).
		{"editinput", []string{"edit t-9sa6", "retitle", "⏎ apply · esc back", "仮想化"}},
	} {
		m := New(memstore.New(), Options{})
		frame, err := m.Dump(240, 60, tc.demo, true)
		if err != nil {
			t.Fatalf("%s: %v", tc.demo, err)
		}
		for _, want := range tc.wants {
			if !strings.Contains(frame, want) {
				t.Errorf("-demo %s: %q is missing from the frame", tc.demo, want)
			}
		}
	}
}
