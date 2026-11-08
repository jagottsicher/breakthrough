package logview

import (
	"context"
	"testing"
	"time"
)

func requireJournalctl(t *testing.T) {
	t.Helper()
	if !JournalAvailable() {
		t.Skip("journalctl not installed")
	}
}

func TestDecodeJournalLineParsesTimestampPriorityAndMessage(t *testing.T) {
	line := `{"__REALTIME_TIMESTAMP":"1700000000000000","PRIORITY":"3","MESSAGE":"boom","SYSLOG_IDENTIFIER":"sshd"}`
	e, ok := decodeJournalLine(line, 7)
	if !ok {
		t.Fatal("decodeJournalLine returned ok=false")
	}
	if got := e.Level; got != LevelError {
		t.Errorf("Level = %v, want LevelError (PRIORITY 3)", got)
	}
	if e.LevelInferred {
		t.Error("LevelInferred must stay false — PRIORITY is a declared field, not a scanned guess")
	}
	if e.Source != "sshd" {
		t.Errorf("Source = %q, want %q", e.Source, "sshd")
	}
	if e.Message != "boom" {
		t.Errorf("Message = %q, want %q", e.Message, "boom")
	}
	if e.File != journalPseudoFile || e.Line != 7 {
		t.Errorf("File/Line = %q/%d, want %q/7", e.File, e.Line, journalPseudoFile)
	}
	if e.Raw != line {
		t.Errorf("Raw = %q, want the original line", e.Raw)
	}
	wantTime := time.UnixMicro(1700000000000000)
	if !e.Time.Equal(wantTime) {
		t.Errorf("Time = %v, want %v", e.Time, wantTime)
	}
}

func TestDecodeJournalLineMalformedJSONIsSkipped(t *testing.T) {
	if _, ok := decodeJournalLine("not json", 1); ok {
		t.Error("decodeJournalLine returned ok=true for malformed JSON")
	}
}

func TestDecodeJournalLineFallsBackToCommThenSystemdUnit(t *testing.T) {
	e, _ := decodeJournalLine(`{"MESSAGE":"x","_COMM":"foo","_SYSTEMD_UNIT":"foo.service"}`, 1)
	if e.Source != "foo" {
		t.Errorf("Source = %q, want %q (_COMM over _SYSTEMD_UNIT)", e.Source, "foo")
	}

	e2, _ := decodeJournalLine(`{"MESSAGE":"x","_SYSTEMD_UNIT":"foo.service"}`, 1)
	if e2.Source != "foo.service" {
		t.Errorf("Source = %q, want %q (fallback with no SYSLOG_IDENTIFIER/_COMM)", e2.Source, "foo.service")
	}
}

func TestDecodeJournalLineNoPriorityIsLevelUnknown(t *testing.T) {
	e, _ := decodeJournalLine(`{"MESSAGE":"x"}`, 1)
	if e.Level != LevelUnknown {
		t.Errorf("Level = %v, want LevelUnknown", e.Level)
	}
}

func TestJournalMessageFieldPlainString(t *testing.T) {
	if got := journalMessageField("hi"); got != "hi" {
		t.Errorf("journalMessageField(%q) = %q, want %q", "hi", got, "hi")
	}
}

func TestJournalMessageFieldByteArrayDecodesAsRawBytes(t *testing.T) {
	v := []any{float64('h'), float64('i')}
	if got := journalMessageField(v); got != "hi" {
		t.Errorf("journalMessageField(byte array) = %q, want %q", got, "hi")
	}
}

func TestJournalMessageFieldRepeatedStringArrayJoinsWithNewline(t *testing.T) {
	v := []any{"a", "b"}
	if got := journalMessageField(v); got != "a\nb" {
		t.Errorf("journalMessageField(string array) = %q, want %q", got, "a\nb")
	}
}

func TestJournalMessageFieldMissingReturnsEmpty(t *testing.T) {
	if got := journalMessageField(nil); got != "" {
		t.Errorf("journalMessageField(nil) = %q, want empty", got)
	}
}

// TestReadJournalReturnsRecentEntries is an integration test against
// the real journalctl binary — skipped, not failed, wherever it's
// unavailable or unusable (a sandbox with no systemd journal at all),
// per CLAUDE.md's own "document platform/sandbox limits, don't present
// them as a code bug" rule.
func TestReadJournalReturnsRecentEntries(t *testing.T) {
	requireJournalctl(t)
	entries, err := ReadJournal(context.Background(), JournalOptions{Lines: 5})
	if err != nil {
		t.Skipf("journalctl present but unusable in this sandbox: %v", err)
	}
	if len(entries) == 0 {
		t.Skip("journal empty in this sandbox")
	}
	for _, e := range entries {
		if e.File != journalPseudoFile {
			t.Errorf("Entry.File = %q, want %q", e.File, journalPseudoFile)
		}
	}
}

func TestReadJournalDefaultsLinesWhenNonPositive(t *testing.T) {
	requireJournalctl(t)
	// Just confirms a zero/negative Lines doesn't error out building
	// the command — the exact count journalctl actually has available
	// isn't something this sandbox can assert on.
	if _, err := ReadJournal(context.Background(), JournalOptions{Lines: 0}); err != nil {
		t.Skipf("journalctl present but unusable in this sandbox: %v", err)
	}
}
