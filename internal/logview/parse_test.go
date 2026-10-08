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
