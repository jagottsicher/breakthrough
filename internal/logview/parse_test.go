package logview

import (
	"strings"
	"testing"
	"time"
)

func TestParseAllJSON(t *testing.T) {
	input := strings.Join([]string{
		`{"time":"2026-10-07T16:04:21Z","level":"error","msg":"database connection timeout","service":"app"}`,
		`{"time":"2026-10-07T16:04:23Z","level":"info","msg":"connection established","service":"app"}`,
	}, "\n")

	entries, format, err := ParseAll(strings.NewReader(input), "app.log", time.Time{})
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	if format != FormatJSON {
		t.Fatalf("format = %v, want FormatJSON", format)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Level != LevelError || entries[0].Message != "database connection timeout" || entries[0].Source != "app" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	if entries[0].File != "app.log" || entries[0].Line != 1 {
		t.Errorf("entries[0] File/Line = %q/%d, want app.log/1", entries[0].File, entries[0].Line)
	}
	wantRaw := `{"time":"2026-10-07T16:04:21Z","level":"error","msg":"database connection timeout","service":"app"}`
	if entries[0].Raw != wantRaw {
		t.Errorf("entries[0].Raw = %q, want the original JSON line", entries[0].Raw)
	}
	wantTime := time.Date(2026, 10, 7, 16, 4, 21, 0, time.UTC)
	if !entries[0].Time.Equal(wantTime) {
		t.Errorf("entries[0].Time = %v, want %v", entries[0].Time, wantTime)
	}
}

func TestParseAllGeneric(t *testing.T) {
	input := "2026-10-07 16:04:23 ERROR database connection timeout\n" +
		"2026-10-07 16:04:24 INFO  connection established\n"

	entries, format, err := ParseAll(strings.NewReader(input), "app.log", time.Time{})
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	if format != FormatGeneric {
		t.Fatalf("format = %v, want FormatGeneric", format)
	}
	if entries[0].Level != LevelError || entries[0].Message != "database connection timeout" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	if entries[1].Level != LevelInfo || entries[1].Message != "connection established" {
		t.Errorf("entries[1] = %+v", entries[1])
	}
}

// TestParseAllGenericCommaMilliseconds pins Python's own
// logging.Formatter default datefmt ("2026-10-07 16:04:23,123 INFO
// message") — a real gap this used to fall straight through to
// FormatPlain for (see reGenericTAB's own doc comment, detect.go):
// Level/Source always empty and the timestamp showing up as plain
// message text instead of being parsed at all.
func TestParseAllGenericCommaMilliseconds(t *testing.T) {
	input := "2026-10-07 16:04:23,123 ERROR database connection timeout\n"

	entries, format, err := ParseAll(strings.NewReader(input), "app.log", time.Time{})
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	if format != FormatGeneric {
		t.Fatalf("format = %v, want FormatGeneric", format)
	}
	if entries[0].Level != LevelError || entries[0].Message != "database connection timeout" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	wantTime := time.Date(2026, 10, 7, 16, 4, 23, 123000000, time.UTC)
	if !entries[0].Time.Equal(wantTime) {
		t.Errorf("entries[0].Time = %v, want %v", entries[0].Time, wantTime)
	}
}

// TestParseAllRFC3164WithISO8601Timestamp pins modern rsyslog's own
// default "high precision" file format (RSYSLOG_FileFormat) — an
// ISO8601 timestamp instead of RFC 3164's classic "Mon _2 HH:MM:SS",
// confirmed against the user's own real /var/log/syslog and
// /var/log/kern.log. Unlike the classic alternative, this timestamp
// already carries its own real year — no fallback year involved.
func TestParseAllRFC3164WithISO8601Timestamp(t *testing.T) {
	input := "2026-10-09T10:48:44.551263+02:00 kalimashaktide sudo: pam_ecryptfs: pam_sm_authenticate: /home/jens is already mounted\n"

	entries, format, err := ParseAll(strings.NewReader(input), "syslog", time.Time{})
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	if format != FormatSyslogRFC3164 {
		t.Fatalf("format = %v, want FormatSyslogRFC3164", format)
	}
	wantTime := time.Date(2026, 10, 9, 10, 48, 44, 551263000, time.FixedZone("", 2*60*60))
	if !entries[0].Time.Equal(wantTime) {
		t.Errorf("entries[0].Time = %v, want %v", entries[0].Time, wantTime)
	}
	if entries[0].Source != "sudo" || entries[0].Message != "pam_ecryptfs: pam_sm_authenticate: /home/jens is already mounted" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
}

// TestParseAllGenericWithNoLevelAtAll pins dpkg.log's own real shape —
// a perfectly good, parseable timestamp, but no level at all
// (status/install/trigproc are dpkg's own action words, none of them a
// recognized level) — a real, user-reported gap: this used to fall all
// the way back to FormatPlain for the sole reason that no level
// followed, losing the timestamp along with it.
func TestParseAllGenericWithNoLevelAtAll(t *testing.T) {
	input := "2026-09-30 20:05:33 status installed man-db:amd64 2.13.1-1\n"

	entries, format, err := ParseAll(strings.NewReader(input), "dpkg.log", time.Time{})
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	if format != FormatGeneric {
		t.Fatalf("format = %v, want FormatGeneric", format)
	}
	wantTime := time.Date(2026, 9, 30, 20, 5, 33, 0, time.UTC)
	if !entries[0].Time.Equal(wantTime) {
		t.Errorf("entries[0].Time = %v, want %v — the timestamp must still be recognized even with no level present", entries[0].Time, wantTime)
	}
	if entries[0].Level != LevelUnknown {
		t.Errorf("entries[0].Level = %v, want LevelUnknown (dpkg's own action words aren't real levels)", entries[0].Level)
	}
	if entries[0].Message != "status installed man-db:amd64 2.13.1-1" {
		t.Errorf("entries[0].Message = %q, want the whole remainder after the timestamp", entries[0].Message)
	}
}

func TestParseAllRFC3164UsesFallbackYear(t *testing.T) {
	input := "Oct  7 16:04:22 server kernel: eth0: link down\n"
	fallback := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	entries, format, err := ParseAll(strings.NewReader(input), "syslog", fallback)
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	if format != FormatSyslogRFC3164 {
		t.Fatalf("format = %v, want FormatSyslogRFC3164", format)
	}
	if entries[0].Time.Year() != 2026 {
		t.Errorf("entries[0].Time.Year() = %d, want 2026", entries[0].Time.Year())
	}
	if entries[0].Source != "kernel" || entries[0].Message != "eth0: link down" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
}

func TestParseAllPlainFallsBackToWholeLineAsMessage(t *testing.T) {
	input := "just some unstructured text\nanother line with no shape\n"
	fallback := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	entries, format, err := ParseAll(strings.NewReader(input), "raw.log", fallback)
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	if format != FormatPlain {
		t.Fatalf("format = %v, want FormatPlain", format)
	}
	if entries[0].Message != "just some unstructured text" {
		t.Errorf("entries[0].Message = %q", entries[0].Message)
	}
	if entries[0].Raw != entries[0].Message {
		t.Errorf("entries[0].Raw = %q, want identical to Message for FormatPlain", entries[0].Raw)
	}
	if !entries[0].Time.Equal(fallback) {
		t.Errorf("entries[0].Time = %v, want fallback %v", entries[0].Time, fallback)
	}
}

func TestParseLevel(t *testing.T) {
	tests := map[string]Level{
		"ERROR":   LevelError,
		"warn":    LevelWarn,
		"Warning": LevelWarn,
		"info":    LevelInfo,
		"DEBUG":   LevelDebug,
		"":        LevelUnknown,
		"bogus":   LevelUnknown,
	}
	for in, want := range tests {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
}
