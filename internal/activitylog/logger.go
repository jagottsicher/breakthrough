package activitylog

import "time"

// timeNow is a package-level swappable var — the same mockable idiom
// this project's own internal/firewall (runUFWStatus and friends)
// already uses — so a test can pin exactly what a logged Entry's own
// timestamp reads without a real clock's own jitter making that
// unpredictable.
var timeNow = time.Now

// Logger is the live, in-process sink internal/ui reaches for at every
// real action's own success/failure point. Nil-safe throughout — every
// method on a nil *Logger is a no-op — so a call site never has to ask
// "is logging even on" itself; internal/ui always has a *Logger
// (possibly nil, possibly non-nil but filtering everything out — see
// New's own doc comment) and calls it unconditionally.
type Logger struct {
	level      Level
	categories map[Category]bool
	w          *Writer
}

// New builds a Logger from level/categories — already resolved from
// config.Settings by the caller, since this package itself knows
// nothing about config.Settings (see this package's own doc comment) —
// and w, the real sink to write through. w may be nil, the same as
// level == LevelOff: either one simply makes every Log call a no-op,
// so a caller with logging turned off, or one that couldn't open a
// real Writer at all (see ResolvePath's own doc comment on why that
// can happen), needs no special-case handling of its own.
func New(level Level, categories map[Category]bool, w *Writer) *Logger {
	return &Logger{level: level, categories: categories, w: w}
}

// Log records one entry, but only once every filter agrees it should
// exist at all: this Logger's own configured level has to be at least
// as detailed as level (LevelOff never satisfies this for any real
// level, so a Logger built with LevelOff — or a nil *Logger, whose
// zero-value level already reads as LevelOff — silently drops
// everything without a separate check), and category's own switch has
// to be on. message should read as a complete, standalone line (see
// sanitizeMessage's own doc comment for what happens if it isn't).
func (l *Logger) Log(level Level, category Category, message string) {
	if l == nil || l.w == nil {
		return
	}
	if level == LevelOff || l.level < level {
		return
	}
	if !l.categories[category] {
		return
	}
	_ = l.w.WriteEntry(Entry{Time: timeNow(), Level: level, Category: category, Message: sanitizeMessage(message)})
}

// Error records a failed action — LevelErrors, the one level that's
// still recorded even when the user has otherwise dialed detail all
// the way down, short of turning logging off entirely.
func (l *Logger) Error(category Category, message string) { l.Log(LevelErrors, category, message) }

// Action records a successful, state-changing action — LevelActions,
// the level this package's own doc comment calls out as the real
// audit trail/future-Undo foundation.
func (l *Logger) Action(category Category, message string) { l.Log(LevelActions, category, message) }

// Detail records one sub-step of a larger action — LevelDetailed.
func (l *Logger) Detail(category Category, message string) { l.Log(LevelDetailed, category, message) }

// Debug records internal diagnostic detail — LevelDebug.
func (l *Logger) Debug(category Category, message string) { l.Log(LevelDebug, category, message) }

// Close closes the underlying Writer, if any — safe to call on a nil
// *Logger.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	return l.w.Close()
}
