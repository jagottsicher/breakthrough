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

	"github.com/rivo/tview"

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
func (r *Root) newActivityLogger() (logger *activitylog.Logger, notice string, isFallbackNotice bool) {
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
		return activitylog.New(level, categories, nil), "", false
	}

	path, fellBack, err := activitylog.ResolvePath()
	if err != nil {
		return activitylog.New(level, categories, nil),
			fmt.Sprintf("activity log: %v — logging stays off until this is fixed", err), false
	}
	w, err := activitylog.OpenWriter(path)
	if err != nil {
		return activitylog.New(level, categories, nil),
			fmt.Sprintf("activity log: %v — logging stays off until this is fixed", err), false
	}
	if fellBack && !r.activityLogFallbackWarned {
		r.activityLogFallbackWarned = true
		if !r.settings.HideActivityLogFallbackNotice {
			return activitylog.New(level, categories, w),
				fmt.Sprintf("activity log: /var/log/breakthrough isn't writable — logging to %s instead", path), true
		}
	}
	return activitylog.New(level, categories, w), "", false
}

// activityLogNoticeCheckboxText renders the "Don't show this again" list
// item — checkboxText's own ●/○ prefix (see panel.go), the same
// pattern sedFlagsList already uses for a toggle that lives in a List
// rather than a tview.Form (see this file's own activityLogNotice*
// field doc comment on Root for why).
func activityLogNoticeCheckboxText(checked bool) string {
	return fmt.Sprintf("%s  Don't show this again", checkboxText(checked))
}

// toggleActivityLogNoticeDontShowAgain flips the checkbox item's own
// state and re-renders it — selecting this item never closes the
// dialog itself, only "OK" (or Escape, the same thing here) does.
func (r *Root) toggleActivityLogNoticeDontShowAgain() {
	r.activityLogNoticeDontShowAgain = !r.activityLogNoticeDontShowAgain
	r.activityLogNoticeList.SetItemText(0, activityLogNoticeCheckboxText(r.activityLogNoticeDontShowAgain), "")
}

// acceptActivityLogNotice is "OK" (and Escape — see newRoot's own
// SetDoneFunc wiring): persists HideActivityLogFallbackNotice only if
// the checkbox was actually checked, then closes. Never persists
// "false" over a previous "true" from here — there is no way to
// *unset* the setting from this one-time notice, only from the
// Options screen's own matching toggle, since this dialog may not even
// appear again once the setting is on.
func (r *Root) acceptActivityLogNotice() {
	if r.activityLogNoticeDontShowAgain {
		r.settings.HideActivityLogFallbackNotice = true
		r.persistSetting("hide_activity_log_fallback_notice", "true")
	}
	r.hideOverlay()
}

// openActivityLogFallbackNotice shows message (newActivityLogger's own
// fallback notice) in its own small dialog rather than through
// showError — see this file's own activityLogNotice* field doc comment
// on Root for why a real dialog instead of the generic error overlay.
// Sized and centered the same way openConfirm sizes confirmDialog: the
// message becomes the one-row title bar's own text (so a very long
// path can still make the dialog wider than the two-item list below it
// would otherwise need), clamped to the screen exactly like showError
// already is for the identical "called before Root's first real Draw"
// reason (see showError's own doc comment).
func (r *Root) openActivityLogFallbackNotice(message string) {
	r.activityLogNoticeDontShowAgain = false
	r.activityLogNoticeTitleBar.SetText(" " + message + " ")
	r.activityLogNoticeList.SetItemText(0, activityLogNoticeCheckboxText(false), "")

	width, height := listSize(r.activityLogNoticeList)
	if headerWidth := tview.TaggedStringWidth(r.activityLogNoticeTitleBar.GetText(false)); headerWidth > width {
		width = headerWidth
	}
	height++ // the title bar's own row (see activityLogNoticeLayout)
	_, _, screenWidth, screenHeight := r.GetRect()
	x, y, width, height := r.clampToScreen((screenWidth-width)/2, (screenHeight-height)/2, width, height)

	r.activityLogNoticeLayout.SetRect(x, y, width, height)
	// r.activityLogNoticeList's own rect is also set, to the same full
	// area — see openConfirm's own identical line for why
	// captureOutsideClick's bounds check needs this even though
	// activityLogNoticeLayout is what's actually drawn/positioned.
	r.activityLogNoticeList.SetRect(x, y, width, height)
	r.activityLogNoticeList.SetCurrentItem(1) // "OK", not the checkbox — matches openConfirm's own "land on the safe default" choice
	r.pushOverlay(activityLogNoticePage, r.activityLogNoticeList, nil)
}

// reopenActivityLog closes r.activityLog (if any) and replaces it with
// a fresh one built from the current settings — called whenever
// Options changes LogLevel or a log_category_* toggle (see
// optioncatalog.go), since newActivityLogger's own Logger is an
// immutable snapshot, not a live view of r.settings.
func (r *Root) reopenActivityLog() {
	_ = r.activityLog.Close()
	logger, notice, isFallbackNotice := r.newActivityLogger()
	r.activityLog = logger
	switch {
	case notice == "":
		return
	case isFallbackNotice:
		r.openActivityLogFallbackNotice(notice)
	default:
		r.showError(fmt.Errorf("%s", notice))
	}
}
