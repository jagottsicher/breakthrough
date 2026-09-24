package activitylog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestLogger(t *testing.T, level Level, categories map[Category]bool) (*Logger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "breakthrough.log")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return New(level, categories, w), path
}

func readLoggedLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	if len(data) == 0 {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestLoggerLogWritesOnceTheConfiguredLevelIsDetailedEnough(t *testing.T) {
	logger, path := newTestLogger(t, LevelActions, map[Category]bool{CategoryFileOps: true})

	logger.Log(LevelActions, CategoryFileOps, "copied a to b")

	lines := readLoggedLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1: %v", len(lines), lines)
	}
}

func TestLoggerLogDropsAnEntryMoreDetailedThanTheConfiguredLevel(t *testing.T) {
	logger, path := newTestLogger(t, LevelActions, map[Category]bool{CategoryFileOps: true})

	logger.Log(LevelDetailed, CategoryFileOps, "a sub-step")

	if lines := readLoggedLines(t, path); len(lines) != 0 {
		t.Errorf("got %d lines, want 0 (LevelDetailed is more detailed than the configured LevelActions): %v", len(lines), lines)
	}
}

func TestLoggerLogAlwaysDropsLevelOff(t *testing.T) {
	// LevelOff is a configuration state ("nothing"), never a real
	// entry's own level — logging *at* LevelOff must never write
	// anything, even with an otherwise very permissive configuration.
	logger, path := newTestLogger(t, LevelDebug, map[Category]bool{CategoryFileOps: true})

	logger.Log(LevelOff, CategoryFileOps, "should never be written")

	if lines := readLoggedLines(t, path); len(lines) != 0 {
		t.Errorf("got %d lines, want 0: %v", len(lines), lines)
	}
}

func TestLoggerLogDropsADisabledCategoryRegardlessOfLevel(t *testing.T) {
	logger, path := newTestLogger(t, LevelDebug, map[Category]bool{CategoryFileOps: false})

	logger.Log(LevelErrors, CategoryFileOps, "a failure")

	if lines := readLoggedLines(t, path); len(lines) != 0 {
		t.Errorf("got %d lines, want 0 (CategoryFileOps is disabled): %v", len(lines), lines)
	}
}

func TestLoggerErrorActionDetailDebugMapToTheirOwnLevel(t *testing.T) {
	logger, path := newTestLogger(t, LevelDebug, map[Category]bool{CategoryRsync: true})

	logger.Error(CategoryRsync, "e")
	logger.Action(CategoryRsync, "a")
	logger.Detail(CategoryRsync, "d")
	logger.Debug(CategoryRsync, "g")

	lines := readLoggedLines(t, path)
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4: %v", len(lines), lines)
	}
	wantLevels := []Level{LevelErrors, LevelActions, LevelDetailed, LevelDebug}
	for i, line := range lines {
		entry, ok := ParseLine(line)
		if !ok {
			t.Fatalf("line %d: ParseLine(%q) failed", i, line)
		}
		if entry.Level != wantLevels[i] {
			t.Errorf("line %d: Level = %v, want %v", i, entry.Level, wantLevels[i])
		}
	}
}

func TestLoggerLogUsesTheRealClockByDefault(t *testing.T) {
	logger, path := newTestLogger(t, LevelActions, map[Category]bool{CategoryFileOps: true})
	before := time.Now().Add(-time.Second)

	logger.Action(CategoryFileOps, "copied")

	lines := readLoggedLines(t, path)
	entry, ok := ParseLine(lines[0])
	if !ok {
		t.Fatal("ParseLine failed")
	}
	if entry.Time.Before(before) {
		t.Errorf("Time = %v, want it at or after %v", entry.Time, before)
	}
}

func TestLoggerOnANilLoggerIsANoOp(t *testing.T) {
	var logger *Logger
	// None of these should panic.
	logger.Log(LevelActions, CategoryFileOps, "x")
	logger.Error(CategoryFileOps, "x")
	logger.Action(CategoryFileOps, "x")
	logger.Detail(CategoryFileOps, "x")
	logger.Debug(CategoryFileOps, "x")
	if err := logger.Close(); err != nil {
		t.Errorf("Close on a nil Logger = %v, want nil", err)
	}
}

func TestLoggerWithANilWriterIsANoOp(t *testing.T) {
	logger := New(LevelDebug, map[Category]bool{CategoryFileOps: true}, nil)
	// Should not panic, and there's nothing to read back — no file was
	// ever given to write to.
	logger.Action(CategoryFileOps, "x")
}
