package ui

// The grouping AXIS vocabulary: the three fields a task can be grouped or
// sliced by, spelled as furrow's -q field names. It is shared by the slice
// panel (slicemode.go, the interactive consumer) and the swimlane engine
// (swimlane.go, a pure one), and it lives in neither: the engine must not
// import the panel's file to name an axis, and the panel must not own a word
// the engine's layout is built on (t-fw3y). Nothing else belongs here — no
// key handling, no rendering, no query composition (that is board.QTerm).

type sliceField int

const (
	sliceRepo sliceField = iota
	sliceLabel
	sliceEpic
	sliceFieldCount
)

func (f sliceField) String() string {
	return [...]string{"repo", "label", "epic"}[f]
}
