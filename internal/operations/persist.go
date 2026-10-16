// persist.go is Tracker's own optional durability for finished
// Operations only — see feature_ideas.txt's own "3. Aufgaben- und
// Transferzentrale": "Nur abgeschlossene, relevante Zusammenfassungen
// persistieren; laufende Jobs nach Programmende nicht vortäuschen."
// (Only persist completed, relevant summaries; never pretend a running
// job survived program end.)
//
// That second half needs no special-case code at all: Start/SetState
// never write to disk, only Finish does, and Finish is the one call
// that only ever happens for an Operation that genuinely finished. A
// job killed mid-run by the process exiting never reaches Finish, so
// it's simply absent from the file afterward — exactly the "don't
// pretend" the spec asks for, by construction rather than by checking
// for it.
//
// The same session.StateDir() location — session-restorable state,
// not portable configuration — and the same plain, human-readable JSON
// array with Outcome rendered as its own word (see Outcome.String/
// ParseOutcome) notify's own persist.go already establishes for
// Message, reused rather than re-derived. Best-effort throughout, the
// same reason: a missing or malformed file degrades silently to no
// history at all, and a failed write is never worth interrupting a
// job's own completion for — this is a convenience for the Transfers
// screen, not file or system state.
package operations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/jagottsicher/breakthrough/internal/session"
)

// persistedOperationFileName is Save/Load's own file, directly under
// session.StateDir() — see DefaultPath.
const persistedOperationFileName = "operations.json"

// DefaultPath is where NewTrackerWithPersistence's own real caller
// (internal/ui's own NewRoot) keeps the persisted operation history —
// mirrors notify.DefaultPath exactly, including "" (an ordinary,
// unpersisted Tracker, no error, no notice) whenever session.StateDir
// itself can't determine one.
func DefaultPath() string {
	dir := session.StateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, persistedOperationFileName)
}

// persistedOperation is a finished Operation's own on-disk shape —
// never a running/paused one (see this file's own doc comment) — State
// itself isn't stored at all: every persisted entry is StateDone by
// definition, restored as such on load rather than round-tripped.
type persistedOperation struct {
	ID        uint64   `json:"id"`
	Type      string   `json:"type"`
	Source    string   `json:"source"`
	Target    string   `json:"target"`
	StartTime string   `json:"startTime"`
	EndTime   string   `json:"endTime"`
	Outcome   string   `json:"outcome"`
	Succeeded int      `json:"succeeded"`
	Failed    int      `json:"failed"`
	Skipped   int      `json:"skipped"`
	Errors    []string `json:"errors,omitempty"`
}

// NewTrackerWithPersistence returns a Tracker pre-loaded from path (see
// this file's own doc comment on how a missing or malformed file
// degrades), that saves every currently-finished Operation back to it
// after each real Finish from then on. path == "" behaves exactly like
// NewTracker(): no load, no save, an ordinary in-memory-only Tracker.
func NewTrackerWithPersistence(path string) *Tracker {
	t := &Tracker{persistPath: path}
	t.load()
	return t
}

// load reads t.persistPath, if set, replacing whatever t already holds
// — only ever called once, from NewTrackerWithPersistence, before any
// real Start/Finish has a chance to run. A missing file (the common
// case: no prior session, or persistence turned off) or one that fails
// to parse leaves t empty, exactly as NewTracker() would have.
func (t *Tracker) load() {
	if t.persistPath == "" {
		return
	}
	data, err := os.ReadFile(t.persistPath)
	if err != nil {
		return
	}
	var in []persistedOperation
	if err := json.Unmarshal(data, &in); err != nil {
		return
	}
	// A hand-edited or otherwise oversized file only ever keeps its own
	// newest capacity entries — the same FIFO rule Start itself enforces
	// on every real arrival, applied once here too rather than trusting
	// the file to already respect it.
	if len(in) > capacity {
		in = in[len(in)-capacity:]
	}

	ops := make([]Operation, 0, len(in))
	var maxID uint64
	for _, p := range in {
		start, err := time.Parse(time.RFC3339, p.StartTime)
		if err != nil {
			continue // one malformed entry is skipped, not fatal to the rest
		}
		end, err := time.Parse(time.RFC3339, p.EndTime)
		if err != nil {
			continue
		}
		ops = append(ops, Operation{
			ID:        p.ID,
			Type:      p.Type,
			Source:    p.Source,
			Target:    p.Target,
			StartTime: start,
			EndTime:   end,
			State:     StateDone,
			Outcome:   ParseOutcome(p.Outcome),
			Succeeded: p.Succeeded,
			Failed:    p.Failed,
			Skipped:   p.Skipped,
			Errors:    p.Errors,
		})
		if p.ID > maxID {
			maxID = p.ID
		}
	}

	t.mu.Lock()
	t.ops = ops
	t.nextID = maxID
	t.mu.Unlock()
}

// save writes every currently-finished (StateDone) Operation t holds to
// t.persistPath as human-readable JSON — a no-op when persistPath is ""
// (an ordinary NewTracker(), or NewTrackerWithPersistence("")), and
// best-effort otherwise. Still-running/paused Operations are filtered
// out here, not merely left unpersisted by never calling this for
// them — Finish is the only caller, but a future caller mistakenly
// invoking this at the wrong time would still get the right file rather
// than quietly leaking a live job's own in-flight state to disk.
func (t *Tracker) save() {
	if t.persistPath == "" {
		return
	}
	t.mu.Lock()
	var done []Operation
	for _, op := range t.ops {
		if op.State == StateDone {
			done = append(done, op)
		}
	}
	t.mu.Unlock()

	out := make([]persistedOperation, len(done))
	for i, op := range done {
		out[i] = persistedOperation{
			ID:        op.ID,
			Type:      op.Type,
			Source:    op.Source,
			Target:    op.Target,
			StartTime: op.StartTime.Format(time.RFC3339),
			EndTime:   op.EndTime.Format(time.RFC3339),
			Outcome:   op.Outcome.String(),
			Succeeded: op.Succeeded,
			Failed:    op.Failed,
			Skipped:   op.Skipped,
			Errors:    op.Errors,
		}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(t.persistPath), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(t.persistPath, data, 0o644)
}
