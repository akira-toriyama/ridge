package memstore

import (
	"strings"
	"testing"
)

// The three persists a box id rides accept the fixture's boxes and refuse
// what is neither task nor box; the fixture keeps one box with a record and
// a review, the body stage's and the reviewed row's only fixture sites.
func TestPersistBodyNoteAndReviewTakeABoxID(t *testing.T) {
	p := New()
	for _, id := range []string{"e-fw2m", "t-jv3j"} {
		if err := p.PersistBody(id, "x"); err != nil {
			t.Errorf("PersistBody(%s): %v", id, err)
		}
		if err := p.PersistNote(id, "x"); err != nil {
			t.Errorf("PersistNote(%s): %v", id, err)
		}
		if err := p.PersistReview(id); err != nil {
			t.Errorf("PersistReview(%s): %v", id, err)
		}
	}
	if err := p.PersistNote("e-nope", "x"); err == nil {
		t.Error("an id that is neither task nor box must be refused")
	}
	b := p.Board()
	with := 0
	for _, e := range b.EpicsAll() {
		if strings.TrimSpace(e.Body) != "" {
			with++
			if !strings.Contains(e.Body, "activated") {
				t.Errorf("%s's record must carry an activation line, the reason the body is read at all: %q", e.ID, e.Body)
			}
		}
	}
	if with == 0 {
		t.Error("the fixture must keep one box with a record — the body stage's only fixture site")
	}
	if e := b.Epic("e-fw2m"); e == nil || e.Reviewed.IsZero() || e.Updated.IsZero() {
		t.Errorf("e-fw2m carries the fixture's one box review and an updated stamp: %+v", e)
	}
}
