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
type HistoryEntry struct {
	Connection
	LastUsed   time.Time
	LastFailed bool
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
func RecordAttempt(conn Connection, failed bool) error {
	entries, err := LoadHistory()
	if err != nil {
		return err
	}

	var existed bool
	kept := entries[:0]
	for _, e := range entries {
		if e.Equal(conn) {
			existed = true
			continue
		}
		kept = append(kept, e)
	}

	if failed && !existed {
		return nil
	}

	updated := append([]HistoryEntry{{Connection: conn, LastUsed: time.Now(), LastFailed: failed}}, kept...)
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
