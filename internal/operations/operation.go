package operations

import (
	"strings"
	"time"
)

// State is whether an Operation is still live — mirrors the one real
// mid-flight transition Paste already has (pause/resume, see
// internal/ui/pastepause.go) alongside the ordinary running/done split
// every job type has.
type State int

const (
	StateRunning State = iota
	StatePaused
	StateDone
)

// String renders s as a lowercase word — for log lines and a future
// screen's own display, the same "readable word, not a raw int" shape
// notify.Level.String()/activitylog.Level.String() already use.
func (s State) String() string {
	switch s {
	case StatePaused:
		return "paused"
	case StateDone:
		return "done"
	default:
		return "running"
	}
}

// Outcome is how a finished Operation ended — OutcomeNone for one
// that's still Running/Paused. Four real endings, not a plain
// success/failure bool: finishPasteJob (internal/ui/pasteconflict.go)
// can end with items both succeeded and failed in the very same job,
// a real case a three-value enum would have to silently pick a side on
// — OutcomePartial is exactly that case, kept distinct rather than
// folded into either neighbor.
type Outcome int

const (
	OutcomeNone Outcome = iota
	OutcomeSucceeded
	OutcomePartial
	OutcomeFailed
	OutcomeCancelled
)

// String renders o as a lowercase word, the same reason State.String()
// does.
func (o Outcome) String() string {
	switch o {
	case OutcomeSucceeded:
		return "succeeded"
	case OutcomePartial:
		return "partial"
	case OutcomeFailed:
		return "failed"
	case OutcomeCancelled:
		return "cancelled"
	default:
		return "none"
	}
}

// ParseOutcome reads back Outcome.String()'s own output, case-
// insensitively — defaulting to OutcomeNone for anything unrecognized
// (a hand-edited file, a future value an older build doesn't know yet)
// rather than failing outright, the same "a bad value degrades to the
// safe default" principle notify.ParseLevel already follows. The one
// real caller is persist.go's own load, reading back what Finish's own
// save already wrote via String() — never a raw int, so a persisted
// file reads sensibly on its own without this package's source open
// alongside it.
func ParseOutcome(s string) Outcome {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "succeeded":
		return OutcomeSucceeded
	case "partial":
		return OutcomePartial
	case "failed":
		return OutcomeFailed
	case "cancelled":
		return OutcomeCancelled
	default:
		return OutcomeNone
	}
}

// Operation is one recorded job's own lifecycle — Tracker's own Start
// builds one, SetState/Finish update it in place, and List/Active hand
// out copies (see Tracker's own doc comment on why a copy, not a live
// pointer, is what every caller outside this package ever sees).
//
// Type is a plain string, not this package's own enum — see this
// package's own doc comment on why it deliberately reuses notify.
// Category's/activitylog.Category's existing string values ("paste",
// "rsync", ...) instead of defining a third name for the same thing.
//
// Source/Target are paths (or, for a remote endpoint, whatever short
// label the caller already shows elsewhere — e.g. a connection name) —
// never a full file list: see this package's own doc comment on why
// nothing here holds more than a job's own shape, not its contents.
//
// Errors holds short, already-summarized strings (what a caller would
// already show in a status line or error dialog), not raw error
// values or a full per-file breakdown — the same restraint Source/
// Target already apply to what this package is willing to hold onto.
type Operation struct {
	ID        uint64
	Type      string
	Source    string
	Target    string
	StartTime time.Time
	EndTime   time.Time
	State     State
	Outcome   Outcome
	Succeeded int
	Failed    int
	Skipped   int
	Errors    []string
}
