package notify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewWithPersistenceEmptyPathBehavesLikeNew(t *testing.T) {
	s := NewWithPersistence("")
	s.Push(LevelSuccess, CategoryRsync, "hello")

	if len(s.Messages()) != 1 {
		t.Fatalf("got %d messages, want 1 (Push itself must still work)", len(s.Messages()))
	}
	// No file should exist anywhere real — nothing to check a path
	// against, since "" never resolves to one; this just documents
	// that Push doesn't panic or otherwise misbehave with persistence
	// off.
}

func TestSaveThenNewWithPersistenceRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.json")
	s := NewWithPersistence(path)
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	withFixedTime(t, at)

	s.Push(LevelSuccess, CategoryRsync, "rsync finished")
	s.Push(LevelError, CategoryPaste, "paste failed")
	second := s.Messages()[1]
	s.SetRead(second.ID, true)

	reloaded := NewWithPersistence(path)
	got := reloaded.Messages()
	if len(got) != 2 {
		t.Fatalf("got %d messages after reload, want 2", len(got))
	}
	if got[0].Text != "rsync finished" || got[0].Level != LevelSuccess || got[0].Category != CategoryRsync {
		t.Errorf("got %+v, want the first pushed message back unchanged", got[0])
	}
	if !got[0].Time.Equal(at) {
		t.Errorf("got time %v, want %v", got[0].Time, at)
	}
	if !got[1].Read {
		t.Error("Read status was not persisted across reload")
	}
}

// TestNewWithPersistenceContinuesIDsPastWhatWasLoaded pins that a
// freshly reloaded Store's own nextID picks up from the highest ID it
// just loaded, rather than restarting at 0 and immediately colliding
// with an ID a still-live reference to an old Message might use.
func TestNewWithPersistenceContinuesIDsPastWhatWasLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.json")
	s := NewWithPersistence(path)
	s.Push(LevelInfo, CategoryFirewall, "first")
	last := s.Push(LevelInfo, CategoryFirewall, "second")

	reloaded := NewWithPersistence(path)
	next := reloaded.Push(LevelInfo, CategoryFirewall, "third")

	if next.ID <= last.ID {
		t.Errorf("got ID %d after reload, want something greater than %d", next.ID, last.ID)
	}
}

func TestNewWithPersistenceMissingFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist", "messages.json")
	s := NewWithPersistence(path)

	if len(s.Messages()) != 0 {
		t.Errorf("got %d messages, want 0 for a file that was never written", len(s.Messages()))
	}
}

func TestNewWithPersistenceMalformedFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.json")
	if err := os.WriteFile(path, []byte("{ not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewWithPersistence(path)

	if len(s.Messages()) != 0 {
		t.Errorf("got %d messages, want 0 for a malformed file", len(s.Messages()))
	}
}

// TestSaveWritesHumanReadableJSON pins the user's own explicit request:
// Level/Category render as their own real word, not Message's own raw
// underlying int/string type — a file that reads sensibly on its own.
func TestSaveWritesHumanReadableJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.json")
	s := NewWithPersistence(path)
	s.Push(LevelError, CategoryRsync, "rsync backup failed")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(data)
	for _, want := range []string{`"level": "error"`, `"category": "rsync"`, `"text": "rsync backup failed"`} {
		if !strings.Contains(text, want) {
			t.Errorf("file content = %q, want it to contain %q", text, want)
		}
	}

	var raw []map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("the file itself must be valid JSON: %v", err)
	}
}

// TestLoadTrimsAnOversizedFileToCapacity guards against a hand-edited
// or otherwise corrupted file holding more than capacity entries —
// Load keeps only the newest, the same FIFO rule Push itself already
// enforces on every real arrival.
func TestLoadTrimsAnOversizedFileToCapacity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.json")
	entries := make([]persistedMessage, capacity+10)
	for i := range entries {
		entries[i] = persistedMessage{
			ID:    uint64(i + 1),
			Time:  time.Now().Format(time.RFC3339),
			Level: "info",
			Text:  "msg",
		}
	}
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewWithPersistence(path)

	got := s.Messages()
	if len(got) != capacity {
		t.Fatalf("got %d messages, want %d (trimmed to capacity)", len(got), capacity)
	}
	if got[0].ID != 11 {
		t.Errorf("got oldest surviving ID %d, want 11 (the first 10 dropped)", got[0].ID)
	}
}

func TestSetReadAndDeletePersistToo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messages.json")
	s := NewWithPersistence(path)
	first := s.Push(LevelSuccess, CategoryRsync, "keep")
	second := s.Push(LevelSuccess, CategoryRsync, "delete me")
	s.SetRead(first.ID, true)
	s.Delete(second.ID)

	reloaded := NewWithPersistence(path)
	got := reloaded.Messages()
	if len(got) != 1 {
		t.Fatalf("got %d messages after reload, want 1 (the deleted one must stay gone)", len(got))
	}
	if !got[0].Read {
		t.Error("Read status set before Delete was not persisted")
	}
}

func TestDefaultPathEndsInMessagesJSON(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	path := DefaultPath()
	if path == "" {
		t.Fatal("DefaultPath() = \"\", want a real path with XDG_STATE_HOME set")
	}
	if filepath.Base(path) != "messages.json" {
		t.Errorf("DefaultPath() = %q, want it to end in messages.json", path)
	}
}
