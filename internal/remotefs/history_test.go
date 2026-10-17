package remotefs

import (
	"reflect"
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

// TestRecordAttemptInheritsThePreviousIdentifierWhenNoneIsGiven pins
// the inherit half of RecordAttempt's own documented rule: reconnecting
// via a history row's own label — which never goes through the
// Identifier field at all, so conn always arrives with Identifier ==
// "" — must not blank out an Identifier a previous attempt already
// set.
func TestRecordAttemptInheritsThePreviousIdentifierWhenNoneIsGiven(t *testing.T) {
	withTestConfigHome(t)
	named := Connection{Host: "a.example.com", User: "jens", Identifier: "build-1"}
	if err := RecordAttempt(named, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	reconnect := Connection{Host: "a.example.com", User: "jens"} // no Identifier, as a plain reconnect builds it
	if err := RecordAttempt(reconnect, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 1 || entries[0].Identifier != "build-1" {
		t.Errorf("entries = %+v, want the Identifier \"build-1\" to survive a reconnect that didn't specify one", entries)
	}
}

// TestRecordAttemptANewNonEmptyIdentifierWins pins the other half: a
// freshly typed, non-empty Identifier always overwrites whatever the
// matched entry had before — renaming it, not merely setting it the
// first time.
func TestRecordAttemptANewNonEmptyIdentifierWins(t *testing.T) {
	withTestConfigHome(t)
	if err := RecordAttempt(Connection{Host: "a.example.com", User: "jens", Identifier: "build-1"}, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := RecordAttempt(Connection{Host: "a.example.com", User: "jens", Identifier: "build-2"}, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 1 || entries[0].Identifier != "build-2" {
		t.Errorf("entries = %+v, want the Identifier renamed to \"build-2\", not stuck on \"build-1\"", entries)
	}
}

// TestRecordAttemptInheritsThePreviousTagsWhenNoneAreGiven mirrors
// TestRecordAttemptInheritsThePreviousIdentifierWhenNoneIsGiven exactly,
// for Tags: a plain reconnect built from a history row's own label never
// goes through the Connect dialog's Tags field at all, so conn always
// arrives with Tags == nil — that must not blank out Tags a previous
// attempt already set.
func TestRecordAttemptInheritsThePreviousTagsWhenNoneAreGiven(t *testing.T) {
	withTestConfigHome(t)
	tagged := Connection{Host: "a.example.com", User: "jens", Tags: []string{"build", "staging"}}
	if err := RecordAttempt(tagged, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	reconnect := Connection{Host: "a.example.com", User: "jens"} // no Tags, as a plain reconnect builds it
	if err := RecordAttempt(reconnect, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 1 || !reflect.DeepEqual(entries[0].Tags, []string{"build", "staging"}) {
		t.Errorf("entries = %+v, want Tags [build staging] to survive a reconnect that didn't specify any", entries)
	}
}

// TestRecordAttemptANewNonEmptyTagsListWins mirrors
// TestRecordAttemptANewNonEmptyIdentifierWins exactly, for Tags: a
// freshly entered, non-empty Tags list always replaces whatever the
// matched entry had before — retagging, not merging.
func TestRecordAttemptANewNonEmptyTagsListWins(t *testing.T) {
	withTestConfigHome(t)
	if err := RecordAttempt(Connection{Host: "a.example.com", User: "jens", Tags: []string{"build"}}, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}
	if err := RecordAttempt(Connection{Host: "a.example.com", User: "jens", Tags: []string{"staging", "web"}}, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 1 || !reflect.DeepEqual(entries[0].Tags, []string{"staging", "web"}) {
		t.Errorf("entries = %+v, want Tags replaced with [staging web], not merged with [build]", entries)
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

// TestRecordAttemptKeepsDistinctShellFlagVariantsAsSeparateEntries
// pins the user's own explicit request for the "gs" SSH shell dropdown
// (see internal/ui's sshshell.go): the same Host/User, opened once
// with a shell flag on and once with it off, must both land in
// history as their own entry rather than one overwriting the other —
// unlike every other RecordAttempt case above, where re-recording the
// exact same Connection is deliberately a move-to-front, not a second
// entry.
func TestRecordAttemptKeepsDistinctShellFlagVariantsAsSeparateEntries(t *testing.T) {
	withTestConfigHome(t)
	plain := Connection{Host: "a.example.com", User: "jens"}
	withCompression := Connection{Host: "a.example.com", User: "jens", ShellCompression: true}

	if err := RecordAttempt(plain, false); err != nil {
		t.Fatalf("RecordAttempt(plain): %v", err)
	}
	if err := RecordAttempt(withCompression, false); err != nil {
		t.Fatalf("RecordAttempt(withCompression): %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2 — a flag-only difference must stay a separate entry, not overwrite the other", len(entries))
	}
	if !entries[0].ShellCompression || entries[1].ShellCompression {
		t.Errorf("entries = %+v, want the most recently recorded (ShellCompression=true) at the front, the plain one still behind it", entries)
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

// TestSaveConnectionOnABrandNewConnectionAddsItAsUntested pins
// SaveConnection's own deliberate override of RecordAttempt's "never
// tried, never added" rule: the user's own explicit request to be able
// to prepare several connections ahead of the servers behind them
// actually existing.
func TestSaveConnectionOnABrandNewConnectionAddsItAsUntested(t *testing.T) {
	withTestConfigHome(t)
	conn := Connection{Host: "future.example.com", User: "jens"}

	if err := SaveConnection(conn); err != nil {
		t.Fatalf("SaveConnection: %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 1 || !entries[0].Untested || entries[0].LastFailed {
		t.Errorf("entries = %+v, want one Untested, not-failed entry", entries)
	}
}

// TestSaveConnectionOnAnExistingEntryUpdatesInPlaceWithoutMovingIt is
// the case the advisor flagged: editing an already-tested entry's
// Identifier/Tags through "OK" must not jump it to the front of
// "most recently used", since nothing was actually used — and must not
// touch its real, previously-observed LastFailed/Untested status
// either, since nothing was actually tried this time.
func TestSaveConnectionOnAnExistingEntryUpdatesInPlaceWithoutMovingIt(t *testing.T) {
	withTestConfigHome(t)
	older := Connection{Host: "older.example.com", User: "jens"}
	target := Connection{Host: "target.example.com", User: "jens"}
	if err := RecordAttempt(target, false); err != nil {
		t.Fatalf("RecordAttempt(target): %v", err)
	}
	if err := RecordAttempt(older, false); err != nil {
		t.Fatalf("RecordAttempt(older): %v", err)
	}
	// History is now [older, target] (most-recently-recorded first).

	renamed := target
	renamed.Identifier = "renamed"
	if err := SaveConnection(renamed); err != nil {
		t.Fatalf("SaveConnection: %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 2 || entries[0].Host != "older.example.com" {
		t.Fatalf("entries = %+v, want [older, target] order preserved, not target jumped to the front", entries)
	}
	if entries[1].Identifier != "renamed" || entries[1].Untested {
		t.Errorf("entries[1] = %+v, want the renamed target, still not Untested (it really did connect before)", entries[1])
	}
}

// TestSaveConnectionOnAChangedIdentityCreatesASeparateUntestedEntry
// pins the user's own explicit description: changing Host/User/Port
// (the fields Connection.Equal actually keys on) makes it a different
// endpoint, so it gets its own new, Untested entry rather than
// updating the old one in place — exactly what falls out of matching
// by Equal with no special-case code needed.
func TestSaveConnectionOnAChangedIdentityCreatesASeparateUntestedEntry(t *testing.T) {
	withTestConfigHome(t)
	original := Connection{Host: "a.example.com", User: "jens"}
	if err := RecordAttempt(original, false); err != nil {
		t.Fatalf("RecordAttempt: %v", err)
	}

	changed := Connection{Host: "b.example.com", User: "jens"}
	if err := SaveConnection(changed); err != nil {
		t.Fatalf("SaveConnection: %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want both the old and the new entry kept", entries)
	}
	if entries[0].Host != "b.example.com" || !entries[0].Untested {
		t.Errorf("entries[0] = %+v, want the new, Untested entry at the front", entries[0])
	}
	if entries[1].Host != "a.example.com" || entries[1].Untested {
		t.Errorf("entries[1] = %+v, want the original, already-tested entry left exactly as it was", entries[1])
	}
}

// TestSaveConnectionOnABrandNewConnectionThatFailsBeforeIsStillAddedUntested
// is the flip side of RecordAttempt's own "a brand-new failed attempt
// is never added" rule: SaveConnection never dials at all, so there is
// no failure to speak of — it always adds a never-seen-before
// connection, unconditionally.
func TestSaveConnectionAlwaysAddsEvenConsecutively(t *testing.T) {
	withTestConfigHome(t)
	if err := SaveConnection(Connection{Host: "a.example.com"}); err != nil {
		t.Fatalf("SaveConnection(a): %v", err)
	}
	if err := SaveConnection(Connection{Host: "b.example.com"}); err != nil {
		t.Fatalf("SaveConnection(b): %v", err)
	}

	entries, err := LoadHistory()
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("entries = %+v, want both untested connections kept", entries)
	}
}
