// persist.go is the ring buffer's own optional durability: without it,
// every Message is gone the moment breakthrough exits — session.
// TabsPath's own reasoning applies here too (see its own doc comment):
// this is state worth restoring across a restart, not portable
// configuration or a document, so it belongs under the XDG State
// directory (session.StateDir), not $XDG_CONFIG_HOME or
// $XDG_DATA_HOME.
//
// A plain JSON array, Level/Category rendered as their own readable
// word (see Level.String/ParseLevel — Category is already just a
// string), not Message's own raw Go types — per the user's own
// explicit request for a file that reads sensibly on its own, without
// this package's own source open alongside it. The ring buffer's own
// hard cap at 999 (see Store's own doc comment) keeps this file small
// regardless of how long a session runs, so this needs no separate
// rotation or truncation of its own.
//
// Best-effort throughout, the same "a bad or missing file degrades to
// the safe default, silently" contract session.LoadTabs' own callers
// already accept for the saved tab layout: a Message is flüchtiger
// Sitzungszustand (see feature_ideas.txt's own reasoning for why
// deleting one needs no confirmation either), not file or system
// state, so losing this file, or failing to write it, is never worth
// interrupting startup or a real trigger's own completion for.
package notify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/jagottsicher/breakthrough/internal/session"
)

// persistedMessageFileName is Save/Load's own file, directly under
// session.StateDir() — see DefaultPath.
const persistedMessageFileName = "messages.json"

// DefaultPath is where NewWithPersistence's own real callers (see
// internal/ui's own NewRoot) keep the persisted message history —
// session.StateDir's own directory plus persistedMessageFileName. ""
// if session.StateDir itself can't determine one (see its own doc
// comment) — NewWithPersistence treats that exactly like New(): an
// ordinary in-memory-only Store, no error, no notice.
func DefaultPath() string {
	dir := session.StateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, persistedMessageFileName)
}

// persistedMessage is Message's own on-disk shape — see this file's
// own doc comment for why Level/Category are plain readable words
// here rather than Message's own raw Go types.
type persistedMessage struct {
	ID       uint64 `json:"id"`
	Time     string `json:"time"`
	Level    string `json:"level"`
	Category string `json:"category"`
	Text     string `json:"text"`
	Read     bool   `json:"read"`
}

// NewWithPersistence returns a Store pre-loaded from path (see this
// file's own doc comment on how a missing or malformed file degrades),
// that saves back to it after every real change (Push/SetRead/Delete)
// from then on. path == "" behaves exactly like New(): no load, no
// save, an ordinary in-memory-only Store.
func NewWithPersistence(path string) *Store {
	s := &Store{persistPath: path}
	s.load()
	return s
}

// load reads s.persistPath, if set, replacing whatever s already holds
// — only ever called once, from NewWithPersistence, before any real
// trigger has a chance to Push. A missing file (the common case: no
// prior session, or persistence turned off) or one that fails to parse
// leaves s empty, exactly as New() would have — see this file's own
// doc comment on why that's never treated as an error worth surfacing.
func (s *Store) load() {
	if s.persistPath == "" {
		return
	}
	data, err := os.ReadFile(s.persistPath)
	if err != nil {
		return
	}
	var in []persistedMessage
	if err := json.Unmarshal(data, &in); err != nil {
		return
	}
	// A hand-edited or otherwise oversized file only ever keeps its own
	// newest capacity entries — the same FIFO rule Push itself
	// enforces on every real arrival, applied once here too rather than
	// trusting the file to already respect it.
	if len(in) > capacity {
		in = in[len(in)-capacity:]
	}

	messages := make([]Message, 0, len(in))
	var maxID uint64
	for _, m := range in {
		t, err := time.Parse(time.RFC3339, m.Time)
		if err != nil {
			continue // one malformed entry is skipped, not fatal to the rest
		}
		messages = append(messages, Message{
			ID:       m.ID,
			Time:     t,
			Level:    ParseLevel(m.Level),
			Category: Category(m.Category),
			Text:     m.Text,
			Read:     m.Read,
		})
		if m.ID > maxID {
			maxID = m.ID
		}
	}

	s.mu.Lock()
	s.messages = messages
	s.nextID = maxID
	s.mu.Unlock()
}

// save writes every currently held Message to s.persistPath as
// human-readable JSON — a no-op when persistPath is "" (an ordinary
// New() Store, or NewWithPersistence("")), and best-effort otherwise
// (see this file's own doc comment on why a write failure is never
// surfaced). Snapshots under the lock, then writes after releasing
// it — the same "never hold the lock during real I/O" shape Messages()
// itself already follows, so a slow disk never blocks a concurrent
// Push/SetRead/Delete from a different goroutine.
func (s *Store) save() {
	if s.persistPath == "" {
		return
	}
	s.mu.Lock()
	messages := make([]Message, len(s.messages))
	copy(messages, s.messages)
	s.mu.Unlock()

	out := make([]persistedMessage, len(messages))
	for i, m := range messages {
		out[i] = persistedMessage{
			ID:       m.ID,
			Time:     m.Time.Format(time.RFC3339),
			Level:    m.Level.String(),
			Category: string(m.Category),
			Text:     m.Text,
			Read:     m.Read,
		}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.persistPath), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(s.persistPath, data, 0o644)
}
