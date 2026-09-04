package board

import (
	"strings"
	"testing"
	"time"
)

// fixedZone pins THIS package's localZone (never time.Local — see its
// declaration) for the duration of a test; ui.localZone is separate. The whole point of the
// due grammar is that furrow reads dates in LOCAL time, and that is invisible
// on a machine (or a CI runner) whose zone happens to be UTC.
func fixedZone(t *testing.T, name string, offsetHours int) {
	t.Helper()
	prev := localZone
	zone := time.FixedZone(name, offsetHours*3600)
	localZone = func() *time.Location { return zone }
	t.Cleanup(func() { localZone = prev })
}

// fixedNow pins the board clock so an offset form has a computable answer.
func fixedNow(t *testing.T, at time.Time) {
	t.Helper()
	prev := nowFn
	nowFn = func() time.Time { return at }
	t.Cleanup(func() { nowFn = prev })
}

// The grammar is furrow's, measured against the real binary (2026-08-10):
// signed offsets in m/h/d/w — including 0 and negatives — a bare day that means
// the WHOLE day (end of it, local), a zone-less day+time read as local, and an
// RFC3339 instant. A form ridge refuses is a form the UI cannot reach at all,
// so the mirror has to be as wide as the original.
func TestParseDueMatchesFurrowsOffsetGrammar(t *testing.T) {
	fixedZone(t, "TEST", 9)
	now := time.Date(2026, 8, 10, 17, 1, 39, 500_000_000, localZone())
	fixedNow(t, now)

	tests := []struct {
		in   string
		want time.Time
	}{
		{"+1m", now.Add(time.Minute)},
		{"+1h", now.Add(time.Hour)},
		{"-1d", now.Add(-24 * time.Hour)},
		{"+0d", now},
		{"-0d", now},
		{"+2w", now.Add(14 * 24 * time.Hour)},
		{"+90m", now.Add(90 * time.Minute)},
		{"-12h", now.Add(-12 * time.Hour)},
		{" +1d ", now.Add(24 * time.Hour)}, // furrow trims, so must ridge
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseDue(tc.in)
			if err != nil {
				t.Fatalf("ParseDue(%q) refused a form furrow accepts: %v", tc.in, err)
			}
			if want := tc.want.Truncate(time.Second); !got.Equal(want) {
				t.Errorf("ParseDue(%q) = %s, want %s", tc.in, got.Format(time.RFC3339), want.Format(time.RFC3339))
			}
		})
	}
}

// A bare day is a promise for the whole day. furrow stores its LAST local
// second (measured: `--due 2026-09-01` → 2026-09-01T14:59:59Z on a UTC+9 box);
// midnight would render the task OVERDUE from the first minute of the day it
// was promised for.
func TestParseDueBareDayIsEndOfDayLocal(t *testing.T) {
	fixedZone(t, "TEST", 9)
	got, err := ParseDue("2026-09-01")
	if err != nil {
		t.Fatalf("parseDue: %v", err)
	}
	want := time.Date(2026, 9, 1, 23, 59, 59, 0, localZone())
	if !got.Equal(want) {
		t.Errorf("ParseDue(bare day) = %s, want %s (end of that day, local)",
			got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// YYYY-MM-DDTHH:MM carries no zone, and furrow reads it in the LOCAL one. A
// zone-less time.Parse would read it as UTC — nine hours off, the other way.
func TestParseDueDayTimeIsLocalNotUTC(t *testing.T) {
	fixedZone(t, "TEST", 9)
	got, err := ParseDue("2026-09-01T10:30")
	if err != nil {
		t.Fatalf("parseDue: %v", err)
	}
	want := time.Date(2026, 9, 1, 10, 30, 0, 0, localZone())
	if !got.Equal(want) {
		t.Errorf("ParseDue(day+time) = %s, want %s (local)",
			got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	// An RFC3339 instant carries its own zone and passes straight through.
	got, err = ParseDue("2026-09-01T10:30:00+09:00")
	if err != nil {
		t.Fatalf("ParseDue(RFC3339): %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("ParseDue(RFC3339) = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// The refusals are furrow's too — no unit ridge invents, no bare count, and an
// error that names the same four forms furrow's own message names.
func TestParseDueRefusesWhatFurrowRefuses(t *testing.T) {
	for _, in := range []string{"1d", "+d", "+1D", "+1s", "+1.5d", "2026-9-1", "tomorrow", "someday", ""} {
		if got, err := ParseDue(in); err == nil {
			t.Errorf("ParseDue(%q) = %s, want a refusal", in, got.Format(time.RFC3339))
		}
	}
	_, err := ParseDue("+1x")
	if err == nil {
		t.Fatal("ParseDue(+1x) must refuse")
	}
	for _, want := range []string{"YYYY-MM-DD", "YYYY-MM-DDTHH:MM", "RFC3339", "+1d"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q the way furrow's does: %v", want, err)
		}
	}
}
