package furrowstore

import (
	"strings"
	"testing"
)

// decodeAddReply keeps its two failures apart: undecodable bytes name the
// JSON error, while decodable JSON without an id names the reply itself.
// Folded into one message, the second case printed the nil error as its
// cause — "undecodable reply: <nil>" (found by review).
func TestDecodeAddReplySeparatesShapeFromSyntax(t *testing.T) {
	if _, err := decodeAddReply("furrow epic add", []byte("not json")); err == nil ||
		!strings.Contains(err.Error(), "undecodable") {
		t.Errorf("syntax failure = %v, want an undecodable-reply error", err)
	}

	// Decodes cleanly into an empty addRow. Defensive, not a live furrow
	// shape: measured on dev (60074b8), `epic add --json` answers a bare row
	// with a top-level id — the envelope here stands in for any id-less
	// object a future furrow might answer.
	_, err := decodeAddReply("furrow epic add", []byte(`{"before":null,"after":{},"changed":[]}`))
	if err == nil || strings.Contains(err.Error(), "<nil>") {
		t.Errorf("shape failure = %v — the nil cause leaked into the message", err)
	}
	if err == nil || !strings.Contains(err.Error(), "names no id") {
		t.Errorf("shape failure = %v, want it to quote the id-less reply", err)
	}

	if id, err := decodeAddReply("furrow add", []byte(`{"id":"t-x1"}`)); err != nil || id != "t-x1" {
		t.Errorf("good reply = %q, %v", id, err)
	}
}

// trimReply must cut in runes: a reply quotes CJK titles, and a byte cut
// splits a character (the repo-wide no-byte-truncation rule).
func TestTrimReplyCutsRunesNotBytes(t *testing.T) {
	got := trimReply([]byte("箱の名前は日本語である"), 4)
	if got != "箱の名前…" {
		t.Errorf("trimReply = %q, want the first four runes and an ellipsis", got)
	}
	if got := trimReply([]byte("one\ntwo"), 120); got != "one …" {
		t.Errorf("multi-line reply = %q, want the first line marked elided", got)
	}
	if got := trimReply([]byte("one\r\ntwo"), 120); got != "one …" {
		t.Errorf("CRLF reply = %q — a stray CR must not reach the terminal", got)
	}
}
