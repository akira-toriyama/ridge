package board

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// fixedZone pins the zone (never time.Local — see the clock's declaration)
// for the duration of a test. The whole point of the due grammar is that
// furrow reads dates in the board's calendar, and that is invisible on a
// machine (or a CI runner) whose zone happens to be UTC.
func fixedZone(t *testing.T, name string, offsetHours int) {
	t.Helper()
	zone := time.FixedZone(name, offsetHours*3600)
	t.Cleanup(SetClock(nil, func() *time.Location { return zone }))
}

// fixedNow pins the clock so an offset form has a computable answer.
func fixedNow(t *testing.T, at time.Time) {
	t.Helper()
	t.Cleanup(SetClock(func() time.Time { return at }, nil))
}

// The grammar is furrow's, measured against the real binary (2026-08-10):
// signed offsets in m/h/d/w — including 0 and negatives — a bare day that means
// the WHOLE day (end of it in the board's calendar), a zone-less day+time
// read in that calendar, and an RFC3339 instant. A form ridge refuses is a
// form the UI cannot reach at all, so the mirror has to be as wide as the
// original.
func TestParseDueMatchesFurrowsOffsetGrammar(t *testing.T) {
	fixedZone(t, "TEST", 9)
	now := time.Date(2026, 8, 10, 17, 1, 39, 500_000_000, Zone())
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

// A bare day is a promise for the whole day. furrow stores its LAST second in
// the board's calendar (measured: `--due 2026-09-01` → 2026-09-01T14:59:59Z
// in a UTC+9 calendar); midnight would render the task OVERDUE from the first
// minute of the day it was promised for.
func TestParseDueBareDayIsEndOfDayInTheBoardsCalendar(t *testing.T) {
	fixedZone(t, "TEST", 9)
	got, err := ParseDue("2026-09-01")
	if err != nil {
		t.Fatalf("parseDue: %v", err)
	}
	want := time.Date(2026, 9, 1, 23, 59, 59, 0, Zone())
	if !got.Equal(want) {
		t.Errorf("ParseDue(bare day) = %s, want %s (end of that day in the board's calendar)",
			got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// YYYY-MM-DDTHH:MM carries no zone, and furrow reads it in the board's
// calendar. A zone-less time.Parse would read it as UTC — nine hours off, the
// other way.
func TestParseDueDayTimeIsInTheBoardsCalendarNotUTC(t *testing.T) {
	fixedZone(t, "TEST", 9)
	got, err := ParseDue("2026-09-01T10:30")
	if err != nil {
		t.Fatalf("parseDue: %v", err)
	}
	want := time.Date(2026, 9, 1, 10, 30, 0, 0, Zone())
	if !got.Equal(want) {
		t.Errorf("ParseDue(day+time) = %s, want %s (board calendar)",
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

// An offset whose nanoseconds overflow int64 must not silently wrap into the
// PAST: `+999999d` once landed in 1841, so a promise for the far future was
// stored as overdue and every "is it late?" surface read it backwards. furrow
// refuses the same forms (internal/app/query_date.go), so refusing keeps the
// mirror rather than narrowing it.
func TestParseDueRefusesOffsetsThatOverflowInsteadOfWrappingIntoThePast(t *testing.T) {
	fixedZone(t, "TEST", 9)
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, Zone())
	fixedNow(t, now)

	// Boundary-valued, computed from the same constants the guard uses so the
	// test cannot drift from it: exactly the widest offset per unit is
	// accepted, one more is refused. A round-numbers-only test passed a guard
	// that was off by one in both directions and still landed 1734.
	units := map[byte]time.Duration{'m': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour}
	for u, d := range units {
		widest := int64(math.MaxInt64) / int64(d)
		for _, sign := range []string{"+", "-"} {
			ok := fmt.Sprintf("%s%d%c", sign, widest, u)
			if _, err := ParseDue(ok); err != nil {
				t.Errorf("ParseDue(%q) refused the widest offset that fits: %v", ok, err)
			}
			over := fmt.Sprintf("%s%d%c", sign, widest+1, u)
			if got, err := ParseDue(over); err == nil {
				t.Errorf("ParseDue(%q) = %s, want a refusal: one past the widest offset overflows",
					over, got.Format(time.RFC3339))
			}
		}
	}

	for _, in := range []string{"+999999d", "-999999d", "+999999w", "+9999999999h", "+99999999999999999999m"} {
		t.Run(in, func(t *testing.T) {
			got, err := ParseDue(in)
			if err == nil {
				t.Fatalf("ParseDue(%q) = %s, want a refusal: the nanosecond product overflows",
					in, got.Format(time.RFC3339))
			}
		})
	}

	// A few spellings well inside the range must keep working — the guard is
	// an overflow check, not a new length limit.
	for _, in := range []string{"+106750d", "-106750d", "+15250w"} {
		t.Run(in, func(t *testing.T) {
			got, err := ParseDue(in)
			if err != nil {
				t.Fatalf("ParseDue(%q) refused a form that does not overflow: %v", in, err)
			}
			if in[0] == '+' && !got.After(now) {
				t.Errorf("ParseDue(%q) = %s, want an instant after %s",
					in, got.Format(time.RFC3339), now.Format(time.RFC3339))
			}
		})
	}
}

// furrow's --due accepts five absolute layouts (internal/app/due.go
// dueLayouts): RFC3339, then four zoneless wall-clock forms read in the
// operator's zone. ridge accepted two of the four, so "2026-09-13 10:30" typed
// into the edit overlay was refused here and accepted by furrow -- the mirror
// the doc comment claims has to be as wide as the original.
func TestParseDueAcceptsEveryWallClockLayoutFurrowDoes(t *testing.T) {
	fixedZone(t, "TEST", 9)
	want := time.Date(2026, 9, 13, 10, 30, 0, 0, Zone()).UTC()
	wantSec := want.Add(45 * time.Second)
	for in, exp := range map[string]time.Time{
		"2026-09-13T10:30":    want,
		"2026-09-13T10:30:45": wantSec,
		"2026-09-13 10:30":    want,
		"2026-09-13 10:30:45": wantSec,
	} {
		got, err := ParseDue(in)
		if err != nil {
			t.Errorf("ParseDue(%q) refused a layout furrow accepts: %v", in, err)
			continue
		}
		if !got.Equal(exp) {
			t.Errorf("ParseDue(%q) = %s, want %s", in, got.Format(time.RFC3339), exp.Format(time.RFC3339))
		}
	}
}

// Zone is the calendar the store declared (SetZone), and the process zone
// only until one is declared — furrow's own fallback; a bare day binds at its
// last second in the calendar in force, whatever the terminal's TZ (t-kt2h).
// A test's SetClock pin outranks the declaration and its restore lifts only
// the pin: a pin a later store construction could overwrite is not a pin.
func TestZoneIsTheDeclaredCalendarAndAPinOutranksIt(t *testing.T) {
	t.Cleanup(SetClock(nil, func() *time.Location { return nil }))
	t.Cleanup(func() { SetZone(nil) })
	SetZone(nil)
	if Zone() != time.Local {
		t.Fatalf("undeclared: Zone() = %v, want the process zone", Zone())
	}
	auckland := time.FixedZone("NZST", 12*3600)
	SetZone(auckland)
	if Zone() != auckland {
		t.Fatalf("declared: Zone() = %v, want %v", Zone(), auckland)
	}
	got, err := ParseDue("2026-10-02")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 2, 11, 59, 59, 0, time.UTC); !got.Equal(want) {
		t.Errorf("a bare day binds in the declared calendar: got %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	restore := SetClock(nil, func() *time.Location { return time.UTC })
	if Zone() != time.UTC {
		t.Errorf("a pin must outrank the declaration, got %v", Zone())
	}
	restore()
	if Zone() != auckland {
		t.Errorf("restore must lift the pin and leave the declaration, got %v", Zone())
	}
	SetZone(nil)
	if Zone() != time.Local {
		t.Errorf("SetZone(nil) must return to the process zone, got %v", Zone())
	}
}

// The bare day is a wall-clock 23:59:59, as furrow builds it: on a day whose
// midnight does not exist (America/Santiago springs forward AT 00:00 on
// 2026-09-06) the arithmetic form — midnight + 24h − 1s — lands an hour
// early. furrow binds `--due 2026-09-06` there at 2026-09-07T02:59:59Z and
// the 24-hour day before at 2026-09-06T03:59:59Z (measured on dev 0f7559d).
func TestParseDueBareDayIsAWallClockLastSecondOnADSTDay(t *testing.T) {
	santiago, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Skipf("no tzdata for America/Santiago: %v", err)
	}
	t.Cleanup(SetClock(nil, func() *time.Location { return santiago }))
	for day, want := range map[string]time.Time{
		"2026-09-06": time.Date(2026, 9, 7, 2, 59, 59, 0, time.UTC),
		"2026-09-05": time.Date(2026, 9, 6, 3, 59, 59, 0, time.UTC),
	} {
		got, err := ParseDue(day)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Equal(want) {
			t.Errorf("ParseDue(%s) in Santiago = %s, want %s (furrow's binding)", day, got.Format(time.RFC3339), want.Format(time.RFC3339))
		}
	}
}
