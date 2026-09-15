package board

import (
	"strings"
	"testing"
)

// Unlaned is the set of tasks whose status names no lane — every lane counts
// as a lane, the done lane included, and the empty status is no lane. A
// count taken from it is what the UI reports, so it is pinned here as a
// count and a set, not merely "non-empty".
func TestUnlanedIsEveryTaskWhoseStatusNamesNoLane(t *testing.T) {
	b := NewBoard([]*Task{
		{ID: "r", Status: "ready", Priority: 10},
		{ID: "d", Status: "done", Priority: 10},
		{ID: "x", Status: "archived", Priority: 10},
		{ID: "y", Status: "someday", Priority: 20},
		{ID: "e", Status: "", Priority: 30},
	})
	var got []string
	for _, tk := range b.Unlaned() {
		got = append(got, tk.ID)
	}
	if s := strings.Join(got, ","); s != "x,y,e" {
		t.Errorf("Unlaned = %s, want x,y,e — the done lane is a lane, the empty status is not", s)
	}
	if got := len(b.Tasks()) - len(b.Unlaned()); got != 2 {
		t.Errorf("the lanes hold %d, want 2", got)
	}
}
