package remotefs

import (
	"testing"
)

// withTestConfigHome points config.UserDir (and therefore
// connectionsFile) at a fresh, empty temp directory for the duration
// of one test — never the real developer's own
// ~/.config/breakthrough, the same isolation gitstatus_test.go's own
// requireGit/runGit helpers give real git commands.
func withTestConfigHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestLoadHistoryOnAFreshConfigDirReturnsNoEntriesNotAnError(t *testing.T) {
	withTestConfigHome(t)
	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("len(entries) = %d, want 0", len(entries))
	}
}

func TestRecordAttemptAddsANewEntryAtTheFront(t *testing.T) {
	withTestConfigHome(t)
	if err := RecordAttempt(Connection{Host: "a.example.com", User: "jens"}, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := RecordAttempt(Connection{Host: "b.example.com", User: "jens"}, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Host != "b.example.com" {
		t.Errorf("entries[0].Host = %q, want the most recently recorded one, %q", entries[0].Host, "b.example.com")
	}
}

func TestRecordAttemptOnAnExistingConnectionMovesItToTheFrontInsteadOfDuplicating(t *testing.T) {
	withTestConfigHome(t)
	conn := Connection{Host: "a.example.com", User: "jens"}
	if err := RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := RecordAttempt(Connection{Host: "b.example.com", User: "jens"}, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := RecordAttempt(conn, true); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2 (re-recording a.example.com must move it, not duplicate it)", len(entries))
	}
	if entries[0].Host != "a.example.com" || !entries[0].LastFailed {
		t.Errorf("entries[0] = %+v, want a.example.com recorded as failed, moved back to the front", entries[0])
	}
}

// TestRecordAttemptOnABrandNewConnectionThatFailsIsNotAdded pins the
// user's own explicit request: a connection nobody has ever reached
// before, that fails on its very first attempt, must not clutter the
// dropdown's history section at all — only entries that worked at
// least once belong there (see RecordAttempt's own doc comment).
func TestRecordAttemptOnABrandNewConnectionThatFailsIsNotAdded(t *testing.T) {
	withTestConfigHome(t)
	if err := RecordAttempt(Connection{Host: "never-worked.example.com", User: "jens"}, true); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %+v, want a brand-new failed attempt left out of history entirely", entries)
	}
}

// TestRecordAttemptOnAnExistingConnectionThatFailsIsKeptNotDropped is
// the flip side of TestRecordAttemptOnABrandNewConnectionThatFailsIsNotAdded:
// a connection that worked before and now fails is still worth
// keeping — "this used to work and just failed" is a real signal
// (LastFailed's own red coloring), not the same as never having
// worked at all.
func TestRecordAttemptOnAnExistingConnectionThatFailsIsKeptNotDropped(t *testing.T) {
	withTestConfigHome(t)
	conn := Connection{Host: "a.example.com", User: "jens"}
	if err := RecordAttempt(conn, false); err != nil {
		t.Fatalf("RecordAttempt (success): %v", err)
	}
	if err := RecordAttempt(conn, true); err != nil {
		t.Fatalf("RecordAttempt (failure): %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 1 || !entries[0].LastFailed {
		t.Errorf("entries = %+v, want the one entry kept and marked failed", entries)
	}
}

func TestRecordAttemptTrimsHistoryToTheMaxEntryCount(t *testing.T) {
	withTestConfigHome(t)
	for i := 0; i < historyMaxEntries+5; i++ {
		conn := Connection{Host: "host", User: "jens", Port: i + 1}
		if err := RecordAttempt(conn, false); err != nil {
			t.Fatalf("RecordAttempt: %v", err)
		}
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != historyMaxEntries {
		t.Errorf("len(entries) = %d, want %d", len(entries), historyMaxEntries)
	}
	// The most recently recorded connection used port
	// historyMaxEntries+5 — confirms trimming drops the *oldest*
	// entries, not the newest.
	if entries[0].Port != historyMaxEntries+5 {
		t.Errorf("entries[0].Port = %d, want %d (the most recent one)", entries[0].Port, historyMaxEntries+5)
	}
}

func TestRemoveFromHistoryDropsExactlyThatOneEntry(t *testing.T) {
	withTestConfigHome(t)
	a := Connection{Host: "a.example.com", User: "jens"}
	b := Connection{Host: "b.example.com", User: "jens"}
	if err := RecordAttempt(a, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := RecordAttempt(b, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	if err := RemoveFromHistory(a); err != nil {
		t.Fatalf("RemoveFromHistory: %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 1 || entries[0].Host != "b.example.com" {
		t.Errorf("entries = %+v, want only b.example.com left", entries)
	}
}

// TestRemoveFromHistoryOnAnEntryThatWasNeverThereIsANoOp matches every
// other read/write in this file's own "absence isn't an error"
// contract (see LoadHistory's own doc comment).
func TestRemoveFromHistoryOnAnEntryThatWasNeverThereIsANoOp(t *testing.T) {
	withTestConfigHome(t)
	if err := RecordAttempt(Connection{Host: "a.example.com"}, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	if err := RemoveFromHistory(Connection{Host: "never-connected.example.com"}); err != nil {
		t.Fatalf("RemoveFromHistory: %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("len(entries) = %d, want the unrelated entry left untouched", len(entries))
	}
}
