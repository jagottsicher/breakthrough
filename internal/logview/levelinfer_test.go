package logview

import (
	"strings"
	"testing"
	"time"
)

// TestInferLevelFalsePositiveCorpus pins every shape a bare substring
// search for "error"/"warn" would have wrongly recovered a Level from
// — real-world text, not contrived: "errors=remount-ro" is an actual
// ext4 mount option line's own tail (common in kern.log), "0 errors"/
// "no errors found" are common tool-output phrasing, "/var/log/
// error.log"/"error_log" are paths and config keys, and "Terror" pins
// the word-boundary requirement itself. Every case must come back
// LevelUnknown — inferLevel finding a Level in any of these would
// silently poison "next error"/"next warning" navigation on exactly
// the real logs this feature exists for.
func TestInferLevelFalsePositiveCorpus(t *testing.T) {
	cases := []string{
		"EXT4-fs (sda1): re-mounted. Opts: errors=remount-ro",
		"0 errors",
		"no errors found",
		"reading /var/log/error.log",
		"error_log is not configured",
		"Terror",
		"this is fine, nothing to see",
	}
	for _, msg := range cases {
		if lvl, inferred, extracted := inferLevel(msg); lvl != LevelUnknown {
			t.Errorf("inferLevel(%q) = (%v, %v, %q), want (LevelUnknown, false, \"\")", msg, lvl, inferred, extracted)
		}
	}
}

// TestInferLevelDoesNotOverrideADeclaredLevel pins the central
// ParseAll fallback's own gate (parse.go): a JSON line whose own
// "level" field already said "info" must never be re-guessed from its
// message text even when that text contains "error" — model.go's own
// Entry.LevelInferred doc comment states this is never allowed to
// happen, and it's exactly what the fallback's own
// `if e.Level == LevelUnknown` gate (not inferLevel itself) is
// responsible for.
func TestInferLevelDoesNotOverrideADeclaredLevel(t *testing.T) {
	input := `{"time":"2026-11-06T10:00:00Z","level":"info","msg":"retrying after error, will succeed"}` + "\n"
	entries, _, err := ParseAll(strings.NewReader(input), "app.log", time.Time{})
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	if entries[0].Level != LevelInfo {
		t.Errorf("entries[0].Level = %v, want LevelInfo (the declared field, not a guess from the message text)", entries[0].Level)
	}
	if entries[0].LevelInferred {
		t.Error("entries[0].LevelInferred = true, want false — the level came from a declared JSON field")
	}
}

func TestInferLevelLeadingToken(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    Level
	}{
		{"plain colon", "Error: 404 Not Found", LevelError},
		{"bracket wrapped", "[WARN] disk usage high", LevelWarn},
		{"paren wrapped", "(INFO) starting up", LevelInfo},
		{"dash separator", "FATAL - out of memory", LevelFatal},
	}
	for _, tc := range tests {
		lvl, inferred, _ := inferLevel(tc.message)
		if lvl != tc.want {
			t.Errorf("%s: inferLevel(%q) level = %v, want %v", tc.name, tc.message, lvl, tc.want)
		}
		if !inferred {
			t.Errorf("%s: inferLevel(%q) inferred = false, want true (scanned from free text)", tc.name, tc.message)
		}
	}
}

// TestInferLevelLogbackStyle pins the second real, user-confirmed
// shape (19e, 2026-11-06, production /var/log/syslog) — a Java/
// Logback line nested inside a syslog message, level word after a
// numeric prefix and bracketed thread name rather than at the very
// start of the message.
func TestInferLevelLogbackStyle(t *testing.T) {
	message := "209860721 [scheduling-1] DEBUG n.j.s.c.DefaultLockingTaskExecutor - Locked 'task', lock will be held"
	lvl, inferred, extracted := inferLevel(message)
	if lvl != LevelDebug {
		t.Errorf("level = %v, want LevelDebug", lvl)
	}
	if !inferred {
		t.Error("inferred = false, want true")
	}
	if extracted != "" {
		t.Errorf("extractedMessage = %q, want empty — the leading/logback anchors never rewrite Message", extracted)
	}
}

// TestInferLevelNestedGenericLine pins the first real shape (19e,
// 2026-11-06): AWS's amazon-ssm-agent writes its own complete
// "timestamp LEVEL component message" line straight into the syslog
// message RFC 3164 already stripped a tag/timestamp off of — a
// genuinely nested, fully-shaped FormatGeneric line, not just a
// leading token. inferLevel recovers it by reusing parseGenericLine
// directly, not a new regex. extractedMessage must replace Entry's own
// Message (per the user's own explicit request, 2026-11-06); Raw
// still keeps the original full line untouched — pinned at the
// ParseAll level in TestParseAllRFC3164RecoversNestedGenericLevel,
// since that is what actually owns Raw.
func TestInferLevelNestedGenericLine(t *testing.T) {
	message := "2026-11-01 00:12:37.9973 WARN EC2RoleProvider Failed to connect to Systems Manager"
	lvl, inferred, extracted := inferLevel(message)
	if lvl != LevelWarn {
		t.Errorf("level = %v, want LevelWarn", lvl)
	}
	if inferred {
		t.Error("inferred = true, want false — a fully nested, parsed line is a declared field, not a scanned guess")
	}
	if extracted != "EC2RoleProvider Failed to connect to Systems Manager" {
		t.Errorf("extractedMessage = %q", extracted)
	}
}

// TestParseAllRFC3164RecoversNestedGenericLevel pins the same nested
// shape through the real ParseAll entry point, including Raw staying
// the full original syslog line while Message is replaced.
func TestParseAllRFC3164RecoversNestedGenericLevel(t *testing.T) {
	input := "Oct  7 16:04:22 server amazon-ssm-agent[123]: 2026-11-01 00:12:37.9973 WARN EC2RoleProvider Failed to connect\n"
	entries, format, err := ParseAll(strings.NewReader(input), "syslog", time.Time{})
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	if format != FormatSyslogRFC3164 {
		t.Fatalf("format = %v, want FormatSyslogRFC3164", format)
	}
	if entries[0].Level != LevelWarn {
		t.Errorf("entries[0].Level = %v, want LevelWarn", entries[0].Level)
	}
	if entries[0].LevelInferred {
		t.Error("entries[0].LevelInferred = true, want false")
	}
	if entries[0].Message != "EC2RoleProvider Failed to connect" {
		t.Errorf("entries[0].Message = %q", entries[0].Message)
	}
	if entries[0].Raw != input[:len(input)-1] {
		t.Errorf("entries[0].Raw = %q, want the full original line unchanged", entries[0].Raw)
	}
}

func TestInferLevelLogfmt(t *testing.T) {
	tests := []struct {
		message string
		want    Level
	}{
		{"connection refused level=error retrying", LevelError},
		{`msg="x" lvl=warn component=db`, LevelWarn},
		{"severity=info starting up", LevelInfo},
	}
	for _, tc := range tests {
		lvl, inferred, _ := inferLevel(tc.message)
		if lvl != tc.want {
			t.Errorf("inferLevel(%q) level = %v, want %v", tc.message, lvl, tc.want)
		}
		if inferred {
			t.Errorf("inferLevel(%q) inferred = true, want false — logfmt is a declared field", tc.message)
		}
	}
}

func TestParseAllCLFMapsStatusToLevel(t *testing.T) {
	tests := []struct {
		name         string
		status       string
		wantLevel    Level
		wantInferred bool
	}{
		{"2xx stays unknown", "200", LevelUnknown, false},
		{"4xx is warn, inferred", "404", LevelWarn, true},
		{"5xx is error, not inferred", "500", LevelError, false},
	}
	for _, tc := range tests {
		input := `localhost - - [03/Oct/2026:14:15:46 +0200] "GET / HTTP/1.1" ` + tc.status + " 183\n"
		entries, format, err := ParseAll(strings.NewReader(input), "access_log", time.Time{})
		if err != nil {
			t.Fatalf("%s: ParseAll: %v", tc.name, err)
		}
		if format != FormatCLF {
			t.Fatalf("%s: format = %v, want FormatCLF", tc.name, format)
		}
		if entries[0].Level != tc.wantLevel {
			t.Errorf("%s: Level = %v, want %v", tc.name, entries[0].Level, tc.wantLevel)
		}
		if entries[0].LevelInferred != tc.wantInferred {
			t.Errorf("%s: LevelInferred = %v, want %v", tc.name, entries[0].LevelInferred, tc.wantInferred)
		}
	}
}

func TestParseAllAptHistoryErrorFieldIsLevelError(t *testing.T) {
	input := "Start-Date: 2026-02-18  18:39:28\n" +
		"Commandline: apt install thunderbird\n" +
		"Error: Sub-process /usr/bin/dpkg returned an error code\n" +
		"End-Date: 2026-02-18  18:39:39\n"
	entries, format, err := ParseAll(strings.NewReader(input), "history.log", time.Time{})
	if err != nil {
		t.Fatalf("ParseAll: %v", err)
	}
	if format != FormatAptHistory {
		t.Fatalf("format = %v, want FormatAptHistory", format)
	}
	if entries[2].Level != LevelError {
		t.Errorf("entries[2] (Error: line) Level = %v, want LevelError", entries[2].Level)
	}
	if entries[2].LevelInferred {
		t.Error("entries[2].LevelInferred = true, want false — apt's own declared field")
	}
	if entries[0].Level != LevelUnknown || entries[1].Level != LevelUnknown {
		t.Errorf("entries[0]/[1].Level = %v/%v, want LevelUnknown — only the Error: field itself gets one", entries[0].Level, entries[1].Level)
	}
}
