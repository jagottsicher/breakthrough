package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rivo/tview"
)

// TestDebugDirUsesXDGStateHome pins DebugDir's own resolution — the
// XDG State directory, matching this project's own existing
// $XDG_CONFIG_HOME convention for settings (see internal/config), not
// guessed at inline here.
func TestDebugDirUsesXDGStateHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	want := filepath.Join(dir, "breakthrough")
	if got := DebugDir(); got != want {
		t.Errorf("DebugDir() = %q, want %q", got, want)
	}
}

// TestCrashLogPathUsesXDGStateHome pins crashLogPath's own resolution —
// DebugDir's own directory plus "crash.log".
func TestCrashLogPathUsesXDGStateHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	want := filepath.Join(dir, "breakthrough", "crash.log")
	if got := crashLogPath(); got != want {
		t.Errorf("crashLogPath() = %q, want %q", got, want)
	}
}

// TestLogCrashWritesRecord pins logCrash's own record format — a
// timestamp, the name identifying which background task it was, the
// recovered panic value, and the stack trace — called directly and
// synchronously here (not through safeGo/a real goroutine), so this
// doesn't depend on QueueUpdateDraw ever being drained, the same
// reason this package's own other async tests avoid that (see e.g.
// TestComputeHashesUpdatesPropertiesText's own doc comment).
func TestLogCrashWritesRecord(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	logCrash("test task", "boom", []byte("fake stack trace"))

	data, err := os.ReadFile(crashLogPath())
	if err != nil {
		t.Fatalf("reading crash log: %v", err)
	}
	got := string(data)
	for _, want := range []string{"test task", "boom", "fake stack trace"} {
		if !strings.Contains(got, want) {
			t.Errorf("crash log is missing %q, got:\n%s", want, got)
		}
	}
}

// TestLogCrashAppendsAcrossMultipleCalls pins that a second crash
// doesn't overwrite the first one's own record — a real crash log is
// only useful if it accumulates history, not just the most recent
// entry.
func TestLogCrashAppendsAcrossMultipleCalls(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	logCrash("first task", "boom one", []byte("stack one"))
	logCrash("second task", "boom two", []byte("stack two"))

	data, err := os.ReadFile(crashLogPath())
	if err != nil {
		t.Fatalf("reading crash log: %v", err)
	}
	got := string(data)
	for _, want := range []string{"first task", "boom one", "second task", "boom two"} {
		if !strings.Contains(got, want) {
			t.Errorf("crash log is missing %q, got:\n%s", want, got)
		}
	}
}

// TestSafeGoRecoversPanicWithoutCrashingProcess is the real, end-to-end
// point of this whole file: a panic inside a goroutine safeGo starts
// must never take the rest of the process down with it — the exact,
// hand-verified failure mode a real crash report exposed (see safeGo's
// own doc comment for the full reasoning and how it was confirmed).
// This test process itself surviving to report a result at all, for a
// fn that panics unconditionally, already proves the core guarantee;
// polling for the crash log record additionally confirms the recovered
// panic was actually captured, not just silently dropped.
func TestSafeGoRecoversPanicWithoutCrashingProcess(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.safeGo("test task", nil, func() {
		panic("deliberate test panic")
	})

	// safeGo's own recover handler writes the crash log synchronously,
	// before ever touching r.app.QueueUpdateDraw (which nothing here
	// drains — see TestLogCrashWritesRecord's own doc comment on why
	// that's the established convention, not an oversight) — so once
	// the record shows up, the panic has already been fully recovered
	// and logged, regardless of whether onPanic/showError themselves
	// ever get to run in this test.
	deadline := time.Now().Add(2 * time.Second)
	var data []byte
	for time.Now().Before(deadline) {
		data, err = os.ReadFile(crashLogPath())
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("crash log was never written within the deadline: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "test task") || !strings.Contains(got, "deliberate test panic") {
		t.Errorf("crash log is missing the expected task name/panic value, got:\n%s", got)
	}
}

// TestReportPanicWritesTheCrashLog pins the one thing that made a real,
// regularly-reproducible crash impossible to act on: a panic on the main
// goroutine left no evidence at all, because nothing recovered it and
// nothing wrote it down (see ReportPanic's own doc comment).
func TestReportPanicWritesTheCrashLog(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	path := ReportPanic("main", "something went wrong")

	if path == "" {
		t.Fatal("ReportPanic reported no log path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the crash log: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "something went wrong") {
		t.Errorf("crash log doesn't name the panic:\n%s", got)
	}
	if !strings.Contains(got, `panic in "main"`) {
		t.Errorf("crash log doesn't say where it came from:\n%s", got)
	}
	if !strings.Contains(got, "goroutine ") {
		t.Errorf("crash log has no stack trace — the part that makes it useful:\n%s", got)
	}
}

// TestReportPanicSurvivesAnUnwritableStateDir pins that the reporter
// can't itself become the crash: it runs from inside a panic-recovery
// path, where a second failure would replace a diagnosable report with
// nothing at all.
func TestReportPanicSurvivesAnUnwritableStateDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/proc/nonexistent-and-unwritable")

	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("ReportPanic panicked instead of degrading: %v", rec)
		}
	}()
	ReportPanic("main", "boom")
}
