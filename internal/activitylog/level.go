package activitylog

import "strings"

// Level is how much detail the activity log records — a single dial,
// each step a superset of the one before it, independent of Category's
// own per-subsystem on/off switch (see this package's own doc
// comment for why both exist rather than just one).
type Level int

const (
	// LevelOff records nothing at all — the default: this is an
	// opt-in feature, never a background service breakthrough runs
	// without being asked to.
	LevelOff Level = iota
	// LevelErrors records only a failed action — the bare minimum
	// worth keeping: what went wrong, and enough context to know what
	// was being attempted.
	LevelErrors
	// LevelActions additionally records every successful, state-
	// changing action (Copy, Move, Rename, Trash/Remove, Compress/
	// Extract, chmod/chown, a Rsync run, ...) — one line per action,
	// the level the user's own "nachvollziehbar machen, was man mit
	// breakthrough gemacht hat" and future-Undo goals actually need.
	LevelActions
	// LevelDetailed additionally records an action's own sub-steps —
	// each file within a batch Copy/Paste, each conflict resolution,
	// each file Rsync itself reports transferring.
	LevelDetailed
	// LevelDebug additionally records internal diagnostic detail — the
	// exact command line an external tool was handed, connection
	// handshake steps, timing — for troubleshooting breakthrough
	// itself, not for reviewing what a user did with it.
	LevelDebug
)

// String renders l the same lowercase word ParseLevel reads back and
// config.Settings.LogLevel itself stores — so a value round-trips
// through Options/the config file without this package and
// internal/config ever risking two different spellings for the same
// level.
func (l Level) String() string {
	switch l {
	case LevelErrors:
		return "errors"
	case LevelActions:
		return "actions"
	case LevelDetailed:
		return "detailed"
	case LevelDebug:
		return "debug"
	default:
		return "off"
	}
}

// ParseLevel reads back String's own output — case-insensitively, so a
// config file edited by hand isn't fussy about it — defaulting to
// LevelOff for anything unrecognized (an empty value, a typo, or a
// config file predating this feature) rather than failing outright:
// the same "a bad value degrades to the safe default, never a crash"
// principle this project's own config parsing already follows
// elsewhere (see config.Settings.SetKey's own parseInt/parseBool).
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "errors":
		return LevelErrors
	case "actions":
		return LevelActions
	case "detailed":
		return LevelDetailed
	case "debug":
		return LevelDebug
	default:
		return LevelOff
	}
}

// Label is String's own human-readable form, for the Options screen's
// dropdown — String itself stays the stable, lowercase config-file
// spelling.
func (l Level) Label() string {
	switch l {
	case LevelErrors:
		return "Errors"
	case LevelActions:
		return "Actions"
	case LevelDetailed:
		return "Detailed"
	case LevelDebug:
		return "Debug"
	default:
		return "Off"
	}
}

// Levels is every level in ascending order of detail, for the Options
// screen's own dropdown and for anything else that needs to enumerate
// them (e.g. a test) without hand-maintaining a second copy of this
// list.
func Levels() []Level {
	return []Level{LevelOff, LevelErrors, LevelActions, LevelDetailed, LevelDebug}
}
