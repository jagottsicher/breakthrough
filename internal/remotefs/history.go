package remotefs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// historyMaxEntries caps the persisted connection history — long
// enough to hold "everywhere I connected recently" without the
// dropdown (see internal/ui's connectionmenu.go) growing into a
// second, unbounded panel of its own.
const historyMaxEntries = 20

// HistoryEntry is one remembered endpoint — everything Connection
// itself already excludes (see its own doc comment) plus how the last
// attempt to reach it went, which is what lets the connection
// dropdown color a previously-failed entry red before the user ever
// retries it.
//
// Untested marks an entry SaveConnection created or last touched
// without ever actually dialing it (the Connect dialog's own "OK"
// button, see that function's own doc comment) — shown in the
// dropdown as plain, uncolored text rather than LastFailed's red or a
// real success's green, since neither of those has actually been
// observed yet. Only ever cleared (implicitly, by never being set
// again) once a real RecordAttempt — a genuine Dial, succeeding or
// failing — replaces this entry with one that has a real outcome.
type HistoryEntry struct {
	Connection
	LastUsed   time.Time
	LastFailed bool
	Untested   bool `json:",omitempty"`
}

// connectionsFile is where history.go persists HistoryEntry list —
// alongside the main config file, but its own dedicated JSON document
// rather than another key in it: a list of records doesn't fit the
// flat "key = value" format the main config file is deliberately kept
// to (see internal/config's own package doc on that format's limits).
func connectionsFile() string {
	dir := config.UserDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "connections.json")
}

// LoadHistory reads the persisted connection history, most-recently-
// used first. A missing file (nothing ever connected yet, or no user
// config directory at all — see config.UserDir) is not an error: it
// reads back as an empty history, the same "absence just means
// nothing's there yet" contract config.ParseFile itself already
// follows for the main config file.
func LoadHistory() ([]HistoryEntry, error) {
	path := connectionsFile()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []HistoryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// SaveHistory writes entries back out, most-recently-used first — the
// exact order LoadHistory itself returns them in and the connection
// dropdown displays them in, so callers never need to sort on the way
// in or out.
func SaveHistory(entries []HistoryEntry) error {
	path := connectionsFile()
	if path == "" {
		return nil // no user config directory at all — nowhere safe to write
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// RecordAttempt loads the current history, moves conn to the front
// (or inserts it) with today's outcome, trims it back down to
// historyMaxEntries, and saves it — the single read-modify-write a
// connection attempt performs, so the dropdown's own "most recent on
// top, failed ones in red" behavior stays correct without every caller
// re-implementing this sequence by hand.
//
// A failing attempt on a connection that was never in history before
// is not added at all, per the user's own explicit request: an entry
// that has never once worked is only ever going to show up red, which
// is clutter, not a useful "reconnect to this" shortcut — the whole
// point of the dropdown's history section. A failing attempt on a
// connection that *did* work before is the opposite case, and still
// updates it as usual: "this used to work and just failed" is exactly
// the signal LastFailed/the dropdown's red coloring exists to surface,
// and losing that by dropping the entry instead would hide a real
// regression a sysadmin actively wants to notice.
//
// Identifier inherit rule: conn arrives built fresh by its caller (see
// connectdialog.go/sshshell.go) with no way to already know what the
// matched history entry's own Identifier was — Equal deliberately
// ignores Identifier (see Connection's own doc comment), so a renamed
// or freshly reconnected entry would otherwise lose it on every single
// reconnect. A non-empty conn.Identifier always wins (the user just
// typed one in the Connect dialog); an empty one inherits whatever the
// matched entry already had, so reconnecting via a history row's own
// label — which never goes through the Identifier field at all —
// can't silently blank it out.
//
// Tags inherit exactly the same way, for exactly the same reason:
// Equal ignores Tags too (see Connection's own doc comment), so a
// non-empty conn.Tags (the Connect dialog's own field, already run
// through ParseTags) replaces the matched entry's Tags outright, and
// an empty conn.Tags inherits whatever it already had — a plain
// reconnect through a history row's own label can never go through the
// Tags field at all, so it must not silently clear it either.
func RecordAttempt(conn Connection, failed bool) error {
	return upsertHistory(conn, historyOutcome{tested: true, failed: failed})
}

// SaveConnection persists conn without ever dialing it — the Connect
// dialog's own "OK" button (internal/ui's connectdialog.go), as
// opposed to "Connect" which actually attempts a connection via
// RecordAttempt. Lets a user prepare several named/tagged connections
// ahead of time, before the servers behind them even exist yet,
// without being forced through a real (and necessarily failing) dial
// first — the user's own explicit request, which deliberately
// overrides RecordAttempt's "never tried, never added" rule above:
// that rule exists to keep clutter out of history after a real,
// unprompted failure, not to forbid a connection the user knowingly
// saved without testing.
//
// A brand-new entry (no existing match) is inserted at the front,
// marked Untested — neither known to work (green) nor known to have
// failed (red), shown instead in the dropdown's own plain, uncolored
// "white" per the user's own explicit request, until a real Connect
// decides which way it goes. An existing entry (same Host/User/Port —
// see Connection's own Equal) is updated in place, at its own current
// position, preserving whatever LastUsed/LastFailed/Untested it
// already had: editing an entry's Identifier/Tags through this path
// never actually tries the connection, so it must not silently
// overwrite a real, previously-observed outcome, and must not jump the
// entry to the front of "most recently used" either, since nothing
// was actually used.
func SaveConnection(conn Connection) error {
	return upsertHistory(conn, historyOutcome{tested: false})
}

// historyOutcome tells upsertHistory what actually happened — a real
// Dial attempt (RecordAttempt) or merely saving edited fields with no
// attempt at all (SaveConnection) — see each one's own doc comment.
type historyOutcome struct {
	tested bool
	failed bool
}

// upsertHistory is RecordAttempt and SaveConnection's shared
// read-modify-write: load, find any existing entry matching conn (see
// Connection.Equal), inherit Identifier/Tags the same way either
// caller needs it, then either move the match to the front with a
// fresh outcome (a real attempt always does, matching the dropdown's
// own "most recently used first" order), update it in place without
// moving it (a save with no attempt), or insert a brand-new entry at
// the front — trim to historyMaxEntries and save.
func upsertHistory(conn Connection, outcome historyOutcome) error {
	entries, err := LoadHistory()
	if err != nil {
		return err
	}

	matchIndex := -1
	for i, e := range entries {
		if e.Equal(conn) {
			matchIndex = i
			break
		}
	}
	existed := matchIndex >= 0

	if outcome.tested && outcome.failed && !existed {
		return nil
	}

	if existed {
		if conn.Identifier == "" {
			conn.Identifier = entries[matchIndex].Identifier
		}
		if len(conn.Tags) == 0 {
			conn.Tags = entries[matchIndex].Tags
		}
	}

	var updated []HistoryEntry
	switch {
	case outcome.tested:
		kept := make([]HistoryEntry, 0, len(entries))
		for i, e := range entries {
			if i != matchIndex {
				kept = append(kept, e)
			}
		}
		entry := HistoryEntry{Connection: conn, LastUsed: time.Now(), LastFailed: outcome.failed}
		updated = append([]HistoryEntry{entry}, kept...)
	case existed:
		updated = append([]HistoryEntry(nil), entries...)
		prev := updated[matchIndex]
		updated[matchIndex] = HistoryEntry{Connection: conn, LastUsed: prev.LastUsed, LastFailed: prev.LastFailed, Untested: prev.Untested}
	default:
		entry := HistoryEntry{Connection: conn, LastUsed: time.Now(), LastFailed: false, Untested: true}
		updated = append([]HistoryEntry{entry}, entries...)
	}

	if len(updated) > historyMaxEntries {
		updated = updated[:historyMaxEntries]
	}
	return SaveHistory(updated)
}

// RemoveFromHistory drops conn out of the persisted history entirely —
// the connection dropdown's own "✕" per entry (see internal/ui's
// connectionmenu.go), for dropping a stale or unwanted entry without
// ever having to connect to it again first. A no-op, not an error, if
// conn isn't in the history at all — the same "absence isn't an
// error" contract every other read/write here already follows.
func RemoveFromHistory(conn Connection) error {
	entries, err := LoadHistory()
	if err != nil {
		return err
	}
	kept := entries[:0]
	for _, e := range entries {
		if !e.Equal(conn) {
			kept = append(kept, e)
		}
	}
	return SaveHistory(kept)
}
