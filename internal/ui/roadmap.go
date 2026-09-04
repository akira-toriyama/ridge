package ui

import (
	"slices"
	"strings"
	"time"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The ROADMAP's layout engine — pure, deterministic, and with no knowledge of
// lipgloss, the theme or the terminal. roadmapview.go draws what this file
// decides.
//
// The shape is ONE ROW PER OPEN PROMISE: every open task that carries a due,
// sorted by that due, its ◆ placed on a shared time axis. It is deliberately
// not a bar chart: furrow records no start date, so a task IS a single
// instant, and drawing a bar would invent a duration nobody promised. (GH's
// roadmap grows bars exactly when a start field exists; this one may too, the
// day furrow grows one.)
//
// Time is CALENDAR-LOCAL throughout. A due is stored as a UTC instant, but
// the promise it records is a local day — board.ParseDue puts a bare day "at
// its last local second" — so every cell boundary here is a local calendar
// boundary: day, Monday-aligned week, month. Arithmetic on the instant
// (Truncate, hours/24) would shear the axis by one cell exactly at the
// timezone offset, the same bug class the peek's "due a day early" comment
// records.

// roadZoom is how much calendar one cell holds.
type roadZoom int

const (
	zoomDay roadZoom = iota
	zoomWeek
	zoomMonth
)

func (z roadZoom) String() string {
	switch z {
	case zoomWeek:
		return "week"
	case zoomMonth:
		return "month"
	}
	return "day"
}

// roadRow is one dated task's placed line — the unit the cursor moves over.
type roadRow struct {
	ID  string
	Y   int // row index, top to bottom in due order
	X   int // cell of the due on the full (unwindowed) axis
	Due time.Time
}

// roadLayout is one frame's worth of roadmap geometry. There is no W: the
// axis is WINDOWED by the view (roadXOff), never packed to a width — a due
// five months out sits at its true X whether or not it is on screen.
type roadLayout struct {
	Zoom   roadZoom
	Rows   []roadRow
	rowAt  map[string]int
	Cells  int // full axis length, in cells
	TodayX int
	start  int // unit index of cell 0 (dayNum/weekNum/monthNum per Zoom)
}

// dayNum is the local calendar day, counted in whole days from the epoch —
// derived from the LOCAL y/m/d and nothing else, so two instants on the same
// local day always share it and a DST-shortened day still counts as one.
func dayNum(t time.Time) int {
	y, m, d := t.In(localZone()).Date()
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// weekNum is dayNum's Monday-aligned week (1970-01-05, the epoch's first
// Monday, starts week 0's successor run; floorDiv keeps the alignment exact
// on the Thursday–Sunday before it).
func weekNum(t time.Time) int { return floorDiv(dayNum(t)-4, 7) }

// monthNum counts calendar months from year zero.
func monthNum(t time.Time) int {
	y, m, _ := t.In(localZone()).Date()
	return y*12 + int(m) - 1
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

// unitOf places an instant on the zoom's axis.
func unitOf(z roadZoom, t time.Time) int {
	switch z {
	case zoomWeek:
		return weekNum(t)
	case zoomMonth:
		return monthNum(t)
	}
	return dayNum(t)
}

// unitStart is the local midnight a unit begins at — unitOf's inverse, which
// is what the axis labels are made of.
func unitStart(z roadZoom, u int) time.Time {
	switch z {
	case zoomWeek:
		return dayStart(u*7 + 4)
	case zoomMonth:
		return time.Date(u/12, time.Month(u%12+1), 1, 0, 0, 0, 0, localZone())
	}
	return dayStart(u)
}

// dayStart is dayNum's inverse: that day's local midnight.
func dayStart(dn int) time.Time {
	u := time.Unix(int64(dn)*86400, 0).UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, localZone())
}

// packRoad lays the dated population onto the axis. The caller owns the
// POPULATION rule (open tasks that carry a due — roadPopulation); this
// function owns the ORDER — due ascending, id as the tiebreak, so the order
// is total and stable — and the geometry.
//
// The axis runs from one unit before the earliest of (first due, today) to
// one unit after the latest of (last due, today): today is always ON the
// axis even when every promise sits to one side of it, and the pad keeps the
// endpoints readable rather than flush against the frame.
func packRoad(tasks []*board.Task, z roadZoom, now time.Time) *roadLayout {
	l := &roadLayout{Zoom: z, rowAt: map[string]int{}}
	rows := append([]*board.Task(nil), tasks...)
	slices.SortStableFunc(rows, func(a, b *board.Task) int {
		if c := a.Due.Compare(b.Due); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})

	today := unitOf(z, now)
	lo, hi := today, today
	for _, t := range rows {
		u := unitOf(z, t.Due)
		lo, hi = minInt(lo, u), maxInt(hi, u)
	}
	l.start = lo - 1
	l.Cells = hi - l.start + 2
	l.TodayX = today - l.start

	for i, t := range rows {
		l.rowAt[t.ID] = i
		l.Rows = append(l.Rows, roadRow{ID: t.ID, Y: i, X: unitOf(z, t.Due) - l.start, Due: t.Due})
	}
	return l
}

// Row is the placed row for a task id, nil when it is not on the axis.
func (l *roadLayout) Row(id string) *roadRow {
	i, ok := l.rowAt[id]
	if !ok {
		return nil
	}
	return &l.Rows[i]
}

// Empty reports a board with no dated open task at all. The view says so in
// words rather than drawing a bare axis and leaving the reader to wonder.
func (l *roadLayout) Empty() bool { return len(l.Rows) == 0 }

// step walks the cursor vertically. The rows are one flat due-ordered list,
// so unlike the packed views there is no column axis to cross — the
// horizontal keys PAN the window instead (onRoadKey).
func (l *roadLayout) step(from string, dy int) string {
	cur := l.Row(from)
	if cur == nil {
		if len(l.Rows) == 0 {
			return from
		}
		return l.Rows[0].ID
	}
	return l.Rows[clamp(cur.Y+dy, 0, len(l.Rows)-1)].ID
}

// roadTick is one axis label, X relative to the window the renderer asked
// about.
type roadTick struct {
	X    int
	Text string
}

// roadTicks composes the two axis label rows for the window [off, off+n) of
// an axis whose cell 0 is unit `start`. What each row says differs per zoom,
// because what fits differs:
//
//	day:   coarse "2006-01" at month starts · fine "02" at Mondays
//	week:  coarse "2006" at year starts     · fine "01".."12" at month starts
//	month: coarse "2006" at year starts     · fine "01"/"04"/"07"/"10" at quarters
//
// A label is dropped rather than overlapped or cut when the one before it is
// still in the way or the window's right edge would halve it — a truncated
// "2026-0…" names a period that does not exist.
func roadTicks(z roadZoom, start, off, n int) (coarse, fine []roadTick) {
	coarseEnd, fineEnd := 0, 0
	add := func(row *[]roadTick, end *int, x int, text string) {
		// Labels are ASCII digits and dashes by construction (the Format
		// strings below), so byte length IS display width — this file stays
		// lipgloss-free like depmap.go.
		if x < *end || x+len(text) > n {
			return
		}
		*row = append(*row, roadTick{X: x, Text: text})
		*end = x + len(text) + 1
	}
	for x := 0; x < n; x++ {
		t := unitStart(z, start+off+x)
		switch z {
		case zoomDay:
			if t.Day() == 1 {
				add(&coarse, &coarseEnd, x, t.Format("2006-01"))
			}
			if t.Weekday() == time.Monday {
				add(&fine, &fineEnd, x, t.Format("02"))
			}
		case zoomWeek:
			prev := t.AddDate(0, 0, -7)
			if t.Year() != prev.Year() {
				add(&coarse, &coarseEnd, x, t.Format("2006"))
			}
			if t.Month() != prev.Month() {
				add(&fine, &fineEnd, x, t.Format("01"))
			}
		case zoomMonth:
			if t.Month() == time.January {
				add(&coarse, &coarseEnd, x, t.Format("2006"))
			}
			if (int(t.Month())-1)%3 == 0 {
				add(&fine, &fineEnd, x, t.Format("01"))
			}
		}
	}
	// The left edge must still name the period it is INSIDE when the first
	// real label sits far in (or never comes): a panned frame otherwise opens
	// on unlabeled cells. Prepended AFTER the real labels are placed so that
	// a period start near the edge wins over the context (placed first, the
	// context's collision guard drops the real label instead).
	form := "2006"
	if z == zoomDay {
		form = "2006-01"
	}
	ctx := unitStart(z, start+off).Format(form)
	if (len(coarse) == 0 || coarse[0].X >= len(ctx)+1) && len(ctx) <= n {
		coarse = append([]roadTick{{X: 0, Text: ctx}}, coarse...)
	}
	return coarse, fine
}
