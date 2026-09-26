// activitylog.go wires this app's own optional activity log (see
// internal/activitylog's own package doc comment) into Root: building
// its Logger from the current settings, and rebuilding it whenever
// Options changes LogLevel or a log_category_* toggle (see
// optioncatalog.go's own "Activity log" category) — the Logger itself
// holds an immutable snapshot of both, not a live view of r.settings,
// so a toggle has no effect until this runs again.
//
// Every real call site elsewhere in this package (Copy/Cut/Paste,
// Rename, Trash/Remove, Compress/Extract, chmod/chown, Rsync, ...)
// reaches r.activityLog directly and calls its own Error/Action/
// Detail/Debug methods unconditionally — all nil-receiver-safe, and a
// no-op whenever the configured level or category says so — so none of
// those call sites need to ask "is logging even on" themselves.
package ui

import (
	"fmt"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
)

// newActivityLogger builds a fresh Logger from r.settings — a Writer-
// less one (silently dropping every Log call, per Logger's own doc
// comment) whenever LogLevel is "off", or resolving/opening the real
// log file failed for any reason. notice, if non-empty, is one line
// worth surfacing to the user once: a real resolve/open failure, or
// the one-time "fell back to a per-user directory instead of
// /var/log/breakthrough" case (see activitylog.ResolvePath's own doc
// comment) — never repeated on a later reopen once already reported
// (see activityLogFallbackWarned).
func (r *Root) newActivityLogger() (logger *activitylog.Logger, notice string) {
	level := activitylog.ParseLevel(r.settings.LogLevel)
	categories := map[activitylog.Category]bool{
		activitylog.CategoryFileOps:     r.settings.LogCategoryFileOps,
		activitylog.CategoryPermissions: r.settings.LogCategoryPermissions,
		activitylog.CategoryArchive:     r.settings.LogCategoryArchive,
		activitylog.CategoryTextOps:     r.settings.LogCategoryTextOps,
		activitylog.CategoryRsync:       r.settings.LogCategoryRsync,
		activitylog.CategoryRemote:      r.settings.LogCategoryRemote,
		activitylog.CategoryShell:       r.settings.LogCategoryShell,
		activitylog.CategoryFirewall:    r.settings.LogCategoryFirewall,
	}
	if level == activitylog.LevelOff {
		return activitylog.New(level, categories, nil), ""
	}

	path, fellBack, err := activitylog.ResolvePath()
	if err != nil {
		return activitylog.New(level, categories, nil),
			fmt.Sprintf("activity log: %v — logging stays off until this is fixed", err)
	}
	w, err := activitylog.OpenWriter(path)
	if err != nil {
		return activitylog.New(level, categories, nil),
			fmt.Sprintf("activity log: %v — logging stays off until this is fixed", err)
	}
	if fellBack && !r.activityLogFallbackWarned {
		r.activityLogFallbackWarned = true
		return activitylog.New(level, categories, w),
			fmt.Sprintf("activity log: /var/log/breakthrough isn't writable — logging to %s instead", path)
	}
	return activitylog.New(level, categories, w), ""
}

// reopenActivityLog closes r.activityLog (if any) and replaces it with
// a fresh one built from the current settings — called whenever
// Options changes LogLevel or a log_category_* toggle (see
// optioncatalog.go), since newActivityLogger's own Logger is an
// immutable snapshot, not a live view of r.settings.
func (r *Root) reopenActivityLog() {
	_ = r.activityLog.Close()
	logger, notice := r.newActivityLogger()
	r.activityLog = logger
	if notice != "" {
		r.showError(fmt.Errorf("%s", notice))
	}
}
