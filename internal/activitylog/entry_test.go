package activitylog

import (
	"testing"
	"time"
)

func TestEntryFormatAndParseLineRoundTrip(t *testing.T) {
	want := Entry{
		Time:     time.Date(2026, 9, 23, 10, 15, 30, 0, time.FixedZone("CEST", 2*60*60)),
		Level:    LevelActions,
		Category: CategoryFileOps,
		Message:  "copied /home/jens/a.txt -> /home/jens/backup/a.txt",
	}

	got, ok := ParseLine(want.Format())
	if !ok {
		t.Fatalf("ParseLine(%q) = ok=false, want a real Entry", want.Format())
	}
	if !got.Time.Equal(want.Time) {
		t.Errorf("Time = %v, want %v", got.Time, want.Time)
	}
	if got.Level != want.Level || got.Category != want.Category || got.Message != want.Message {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseLineRejectsAnEmptyLine(t *testing.T) {
	if _, ok := ParseLine(""); ok {
		t.Error("ParseLine(\"\") should report ok=false")
	}
	if _, ok := ParseLine("\n"); ok {
		t.Error("ParseLine(\"\\n\") should report ok=false")
	}
}

func TestParseLineRejectsTooFewFields(t *testing.T) {
	if _, ok := ParseLine("2026-09-23T10:15:30+02:00 actions"); ok {
		t.Error("a line with no message should report ok=false")
	}
}

func TestParseLineRejectsAnInvalidTimestamp(t *testing.T) {
	if _, ok := ParseLine("not-a-time actions fileops copied a to b"); ok {
		t.Error("a malformed timestamp should report ok=false")
	}
}

func TestEntryFormatFlattensAnEmbeddedNewlineInTheMessage(t *testing.T) {
	e := Entry{Time: time.Now(), Level: LevelDebug, Category: CategoryShell, Message: sanitizeMessage("line one\nline two")}
	got, ok := ParseLine(e.Format())
	if !ok {
		t.Fatal("ParseLine should still succeed once the message is sanitized")
	}
	if got.Message != "line one line two" {
		t.Errorf("Message = %q, want the embedded newline flattened to a space", got.Message)
	}
}
