package notify

import (
	"strings"
	"time"
)

// Level is how a notification's own trigger resolved — mirrors the
// ✔/✘ binary this project's own Sessions screen already uses for a
// background job's outcome (see sessionsAttachedGlyph/
// sessionsStatusColor), plus LevelInfo for a trigger that isn't itself
// a plain success/failure (the firewall self-lockout rollback firing
// is neither: the rule really did get reverted, which is the intended
// outcome of a safety mechanism, not a failure of one).
type Level int

const (
	LevelInfo Level = iota
	LevelSuccess
	LevelError
)

// String renders l as the lowercase word Save writes to the persisted
// JSON file (see persist.go) and ParseLevel reads back — a real,
// human-readable word there rather than Level's own raw underlying
// int, per the user's own explicit request for an easily readable
// persistence format.
func (l Level) String() string {
	switch l {
	case LevelSuccess:
		return "success"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}

// ParseLevel reads back String's own output, case-insensitively —
// defaulting to LevelInfo for anything unrecognized (a hand-edited
// file, a future value an older build doesn't know yet) rather than
// failing outright, the same "a bad value degrades to the safe
// default" principle this app's own config parsing already follows.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "success":
		return LevelSuccess
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// Category groups a Message by which trigger produced it — one
// constant per real, wired-up call site in internal/ui, the same
// "never invented ahead of need" restraint internal/activitylog's own
// Category already follows.
type Category string

const (
	// CategoryRsync covers a backgrounded Rsync run's own completion —
	// rsyncjob.go's finishRsyncJob.
	CategoryRsync Category = "rsync"
	// CategoryPaste covers the paste/move queue's own completion —
	// pasteconflict.go's finishPasteJob.
	CategoryPaste Category = "paste"
	// CategoryFirewall covers the Firewall screen's own automatic
	// self-lockout rollback firing — firewalladdrule.go's
	// rollbackFirewallRule.
	CategoryFirewall Category = "firewall"
)

// Message is one recorded notification: Store's own Push builds one,
// hands it to whatever Subscribe registered, and keeps it in its own
// ring buffer (see Store's own doc comment) until it ages out or a
// later screen (not part of this package) removes it by ID.
type Message struct {
	ID       uint64
	Time     time.Time
	Level    Level
	Category Category
	Text     string
	Read     bool
}
