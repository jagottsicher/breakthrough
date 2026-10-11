package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/activitylog"
)

// attachTestActivityLog points r.activityLog at a real, freshly opened
// log file — LevelDebug, every category on, so nothing this package's
// own instrumentation logs is ever filtered out here — and returns a
// function that rereads its current contents. Used throughout this
// package's own tests to assert on exactly what a real action logged,
// the same append-only text format production code writes, never a
// mock of Logger itself.
func attachTestActivityLog(t *testing.T, r *Root) func() string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "activity.log")
	w, err := activitylog.OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	categories := make(map[activitylog.Category]bool, len(activitylog.Categories()))
	for _, c := range activitylog.Categories() {
		categories[c] = true
	}
	r.activityLog = activitylog.New(activitylog.LevelDebug, categories, w)
	return func() string {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		return string(got)
	}
}

// isolateActivityLogPaths points activitylog.SystemLogDir at a fresh,
// writable subdirectory of a temp dir (never the real
// /var/log/breakthrough) and isolates $XDG_STATE_HOME too, so none of
// the tests below ever touch anything on the real filesystem outside
// t.TempDir() — the same reasoning internal/activitylog's own
// writer_test.go already gives for isolating this identical var.
func isolateActivityLogPaths(t *testing.T) (systemDir string) {
	t.Helper()
	orig := activitylog.SystemLogDir
	t.Cleanup(func() { activitylog.SystemLogDir = orig })
	systemDir = filepath.Join(t.TempDir(), "system-log")
	activitylog.SystemLogDir = systemDir
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return systemDir
}

func TestNewActivityLoggerIsANoOpWhenLevelIsOff(t *testing.T) {
	systemDir := isolateActivityLogPaths(t)
	r, _, _ := newTestRootWithFile(t)
	r.settings.LogLevel = "off"

	logger, notice, _ := r.newActivityLogger()

	if notice != "" {
		t.Errorf("notice = %q, want empty when logging is off", notice)
	}
	logger.Action(activitylog.CategoryFileOps, "should never be written")
	if _, err := os.Stat(systemDir); !os.IsNotExist(err) {
		t.Errorf("Stat(systemDir): err = %v, want a not-exist error — LevelOff must never even try to resolve a path", err)
	}
}

func TestNewActivityLoggerOpensTheResolvedPathWhenLevelIsOn(t *testing.T) {
	isolateActivityLogPaths(t)
	r, _, _ := newTestRootWithFile(t)
	r.settings.LogLevel = "actions"
	r.settings.LogCategoryFileOps = true

	logger, notice, _ := r.newActivityLogger()
	defer func() { _ = logger.Close() }()

	if notice != "" {
		t.Fatalf("notice = %q, want empty: the system log dir is writable", notice)
	}
	logger.Action(activitylog.CategoryFileOps, "copied a to b")

	path := filepath.Join(activitylog.SystemLogDir, "breakthrough.log")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if len(got) == 0 {
		t.Error("log file is empty, want the logged entry")
	}
}

func TestNewActivityLoggerRespectsEachCategoryIndependently(t *testing.T) {
	isolateActivityLogPaths(t)
	r, _, _ := newTestRootWithFile(t)
	r.settings.LogLevel = "actions"
	r.settings.LogCategoryFileOps = false
	r.settings.LogCategoryRsync = true

	logger, _, _ := r.newActivityLogger()
	defer func() { _ = logger.Close() }()

	logger.Action(activitylog.CategoryFileOps, "should be dropped")
	logger.Action(activitylog.CategoryRsync, "should be kept")

	path := filepath.Join(activitylog.SystemLogDir, "breakthrough.log")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	entry, ok := activitylog.ParseLine(string(got))
	if !ok {
		t.Fatalf("ParseLine(%q) failed", got)
	}
	if entry.Category != activitylog.CategoryRsync {
		t.Errorf("logged Category = %q, want %q (fileops should have been dropped)", entry.Category, activitylog.CategoryRsync)
	}
}

func TestReopenActivityLogAppliesUpdatedSettings(t *testing.T) {
	isolateActivityLogPaths(t)
	r, _, _ := newTestRootWithFile(t)
	r.settings.LogLevel = "errors"
	r.settings.LogCategoryFileOps = true
	r.activityLog, _, _ = r.newActivityLogger()

	r.activityLog.Action(activitylog.CategoryFileOps, "should be dropped at LevelErrors")
	// OpenWriter's own os.O_CREATE already creates the file the moment
	// the Writer opens, empty or not — checking its length, not mere
	// existence, is what actually proves LevelErrors dropped this
	// Action-level entry.
	if got, err := os.ReadFile(filepath.Join(activitylog.SystemLogDir, "breakthrough.log")); err != nil || len(got) != 0 {
		t.Fatalf("setup: log file = %q, err = %v, want it empty (LevelErrors should drop an Action-level entry)", got, err)
	}

	r.settings.LogLevel = "actions"
	r.reopenActivityLog()
	r.activityLog.Action(activitylog.CategoryFileOps, "should now be kept")

	got, err := os.ReadFile(filepath.Join(activitylog.SystemLogDir, "breakthrough.log"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(got) == 0 {
		t.Error("log file is empty, want the entry logged after reopening at LevelActions")
	}
}

// TestReopenActivityLogWarnsAboutAFallbackOnlyOnce pins the per-run
// half of the "don't repeat this notice" contract — see
// TestReopenActivityLogRoutesTheFallbackThroughItsOwnDialog just below
// for why activityLogNoticePage, not errorPage, is the overlay this
// now opens: a real activity-log error (logging stays off) still goes
// through showError, but the fallback (logging just moved) has had its
// own dialog since the "don't show again" option was added.
func TestReopenActivityLogWarnsAboutAFallbackOnlyOnce(t *testing.T) {
	isolateActivityLogPaths(t)
	// A file, not a directory, at the system log dir's own path forces
	// the fallback — the same technique internal/activitylog's own
	// writer_test.go already uses for the identical reason.
	if err := os.WriteFile(activitylog.SystemLogDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _, _ := newTestRootWithFile(t)
	r.settings.LogLevel = "actions"

	r.reopenActivityLog()
	if r.activePage != activityLogNoticePage {
		t.Fatalf("activePage = %q, want the fallback notice dialog the first time", r.activePage)
	}
	r.hideOverlay()

	r.reopenActivityLog()
	if r.activePage == activityLogNoticePage {
		t.Error("the fallback notice should not be shown a second time in the same run")
	}
}

// TestReopenActivityLogRoutesTheFallbackThroughItsOwnDialog and
// TestReopenActivityLogStillUsesShowErrorForARealFailure pin the split
// newActivityLogger's own isFallbackNotice return value drives: the
// benign "logging just moved" case gets the dialog with its own
// "don't show again" checkbox, a genuine failure (logging stays off)
// still gets the plain error overlay, since there's nothing to offer
// "don't show again" about — it isn't a one-time heads-up, it's broken.
func TestReopenActivityLogRoutesTheFallbackThroughItsOwnDialog(t *testing.T) {
	isolateActivityLogPaths(t)
	if err := os.WriteFile(activitylog.SystemLogDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _, _ := newTestRootWithFile(t)
	r.settings.LogLevel = "actions"

	r.reopenActivityLog()

	if r.activePage != activityLogNoticePage {
		t.Errorf("activePage = %q, want %q", r.activePage, activityLogNoticePage)
	}
}

func TestReopenActivityLogStillUsesShowErrorForARealFailure(t *testing.T) {
	isolateActivityLogPaths(t)
	// Make the resolved path itself unusable (a directory where the log
	// file should be) so OpenWriter fails outright — a real error, not
	// a fallback.
	if err := os.MkdirAll(filepath.Join(activitylog.SystemLogDir, "breakthrough.log"), 0o755); err != nil {
		t.Fatal(err)
	}
	r, _, _ := newTestRootWithFile(t)
	r.settings.LogLevel = "actions"

	r.reopenActivityLog()

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q (a real failure, not the fallback dialog)", r.activePage, errorPage)
	}
}

// TestOpenActivityLogFallbackNoticeShowsTheMessage pins that the
// dialog's own message view carries the exact notice text it was
// given, and that the title bar is the fixed "Important note" header
// (plus its own "✕") rather than the message itself — the redesign
// that replaced openConfirm's own "the message IS the header" shape
// for this one dialog specifically, per the user's own explicit
// request for a real header row with a close glyph.
func TestOpenActivityLogFallbackNoticeShowsTheMessage(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)

	r.openActivityLogFallbackNotice("activity log: /var/log/breakthrough isn't writable — logging to /tmp/x instead")

	got := r.activityLogNoticeMessageView.GetText(true)
	// wrapText wraps at r.errorWidth(), which at this point in a test
	// (no real terminal size yet) can be narrow enough to insert a line
	// break where the original message had a plain space — so compare
	// with newlines folded back to spaces rather than demanding the
	// exact unwrapped phrase.
	if !strings.Contains(strings.Join(strings.Fields(got), " "), "isn't writable") {
		t.Errorf("message view = %q, want it to contain the notice message", got)
	}
	header := r.activityLogNoticeTitleBar.GetText(true)
	if !strings.Contains(header, "Important note") {
		t.Errorf("title bar = %q, want it to contain %q", header, "Important note")
	}
	if strings.Contains(header, "isn't writable") {
		t.Errorf("title bar = %q, should not carry the notice message itself anymore", header)
	}
	if !strings.Contains(header, "✕") {
		t.Errorf("title bar = %q, want it to contain the close glyph", header)
	}
	if r.activePage != activityLogNoticePage {
		t.Errorf("activePage = %q, want %q", r.activePage, activityLogNoticePage)
	}
}

// TestToggleActivityLogNoticeDontShowAgainFlipsTheCheckboxGlyph pins
// the checkboxText-prefixed List item (see this file's own
// activityLogNoticeCheckboxText) actually flips both the stored bool
// and the rendered glyph, the same ●/○ pair checkboxText already uses
// elsewhere in this package (sedFlagsList).
func TestToggleActivityLogNoticeDontShowAgainFlipsTheCheckboxGlyph(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	r.openActivityLogFallbackNotice("activity log: fallback")

	if r.activityLogNoticeDontShowAgain {
		t.Fatal("setup: should start unchecked")
	}
	text, _ := r.activityLogNoticeList.GetItemText(0)
	if !strings.Contains(text, "○") {
		t.Errorf("checkbox item text = %q, want the unchecked ○ glyph", text)
	}

	r.toggleActivityLogNoticeDontShowAgain()

	if !r.activityLogNoticeDontShowAgain {
		t.Error("toggling once should check the box")
	}
	text, _ = r.activityLogNoticeList.GetItemText(0)
	if !strings.Contains(text, "●") {
		t.Errorf("checkbox item text = %q, want the checked ● glyph after toggling", text)
	}

	r.toggleActivityLogNoticeDontShowAgain()
	if r.activityLogNoticeDontShowAgain {
		t.Error("toggling twice should uncheck the box again")
	}
}

// TestAcceptActivityLogNoticePersistsOnlyWhenChecked pins the
// dialog's own actual point: "OK" alone, with the box left unchecked,
// never touches HideActivityLogFallbackNotice — only checking the box
// first does, and that persists via persistSetting the same as the
// matching Options toggle would.
func TestAcceptActivityLogNoticePersistsOnlyWhenChecked(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	r.openActivityLogFallbackNotice("activity log: fallback")

	r.acceptActivityLogNotice()

	if r.settings.HideActivityLogFallbackNotice {
		t.Error("accepting without checking the box should not set HideActivityLogFallbackNotice")
	}
	if r.activePage == activityLogNoticePage {
		t.Error("accepting should close the dialog")
	}
}

func TestAcceptActivityLogNoticeWithTheBoxCheckedPersistsTheSetting(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	r.openActivityLogFallbackNotice("activity log: fallback")
	r.toggleActivityLogNoticeDontShowAgain()

	r.acceptActivityLogNotice()

	if !r.settings.HideActivityLogFallbackNotice {
		t.Error("accepting with the box checked should set HideActivityLogFallbackNotice")
	}
	if r.activePage == activityLogNoticePage {
		t.Error("accepting should close the dialog")
	}
}

// TestCancelActivityLogNoticeDiscardsEvenWhenChecked pins Cancel's own
// point: unlike OK (TestAcceptActivityLogNoticeWithTheBoxCheckedPersistsTheSetting),
// it closes the dialog without ever persisting HideActivityLogFallbackNotice,
// regardless of the checkbox's current state.
func TestCancelActivityLogNoticeDiscardsEvenWhenChecked(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	r.openActivityLogFallbackNotice("activity log: fallback")
	r.toggleActivityLogNoticeDontShowAgain()

	r.cancelActivityLogNotice()

	if r.settings.HideActivityLogFallbackNotice {
		t.Error("cancel should never persist HideActivityLogFallbackNotice, even with the box checked")
	}
	if r.activePage == activityLogNoticePage {
		t.Error("cancel should close the dialog")
	}
}

// TestActivityLogNoticeTitleBarMouseCaptureClickingTheXCancels pins
// that a left click on the "✕" column acts exactly like Cancel — see
// toolWindowCloseButtonCol's own identical one-column-in-from-the-right
// convention this mirrors.
func TestActivityLogNoticeTitleBarMouseCaptureClickingTheXCancels(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	r.openActivityLogFallbackNotice("activity log: fallback")
	r.toggleActivityLogNoticeDontShowAgain()

	x, y, width, _ := r.activityLogNoticeTitleBar.GetRect()
	event := tcell.NewEventMouse(x+width-2, y, tcell.ButtonNone, tcell.ModNone)
	action, _ := r.activityLogNoticeTitleBarMouseCapture(tview.MouseLeftDown, event)

	if action != tview.MouseConsumed {
		t.Errorf("mouse action = %v, want MouseConsumed", action)
	}
	if r.settings.HideActivityLogFallbackNotice {
		t.Error("clicking the X should never persist HideActivityLogFallbackNotice")
	}
	if r.activePage == activityLogNoticePage {
		t.Error("clicking the X should close the dialog")
	}
}

// TestActivityLogNoticeTitleBarMouseCaptureIgnoresClicksElsewhere pins
// that only the "✕" column itself reacts — a click anywhere else on
// the header bar passes through untouched and never closes the dialog.
func TestActivityLogNoticeTitleBarMouseCaptureIgnoresClicksElsewhere(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	r.openActivityLogFallbackNotice("activity log: fallback")

	x, y, _, _ := r.activityLogNoticeTitleBar.GetRect()
	event := tcell.NewEventMouse(x, y, tcell.ButtonNone, tcell.ModNone)
	action, _ := r.activityLogNoticeTitleBarMouseCapture(tview.MouseLeftDown, event)

	if action == tview.MouseConsumed {
		t.Error("a click away from the X should not be consumed")
	}
	if r.activePage != activityLogNoticePage {
		t.Error("a click away from the X should not close the dialog")
	}
}

// TestActivityLogNoticeTabCyclesThroughAllThreeFocusTargets pins the
// fixed three-way ring captureActivityLogNoticeListKey/
// captureActivityLogNoticeButtonKey together establish: checkbox ->
// Cancel -> OK -> back to the checkbox, and the reverse via Backtab.
func TestActivityLogNoticeTabCyclesThroughAllThreeFocusTargets(t *testing.T) {
	r, _, _ := newTestRootWithFile(t)
	r.openActivityLogFallbackNotice("activity log: fallback")

	tab := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)

	if result := r.captureActivityLogNoticeListKey(tab); result != nil {
		t.Error("Tab from the checkbox should be consumed")
	}
	if r.app.GetFocus() != r.activityLogNoticeCancelButton {
		t.Error("Tab from the checkbox should move focus to Cancel")
	}

	if result := r.captureActivityLogNoticeButtonKey(r.activityLogNoticeOKButton, r.activityLogNoticeList)(tab); result != nil {
		t.Error("Tab from Cancel should be consumed")
	}
	if r.app.GetFocus() != r.activityLogNoticeOKButton {
		t.Error("Tab from Cancel should move focus to OK")
	}

	if result := r.captureActivityLogNoticeButtonKey(r.activityLogNoticeList, r.activityLogNoticeCancelButton)(tab); result != nil {
		t.Error("Tab from OK should be consumed")
	}
	if r.app.GetFocus() != r.activityLogNoticeList {
		t.Error("Tab from OK should move focus back to the checkbox")
	}

	backtab := tcell.NewEventKey(tcell.KeyBacktab, 0, tcell.ModNone)
	if result := r.captureActivityLogNoticeListKey(backtab); result != nil {
		t.Error("Backtab from the checkbox should be consumed")
	}
	if r.app.GetFocus() != r.activityLogNoticeOKButton {
		t.Error("Backtab from the checkbox should move focus to OK")
	}
}

// TestActivityLogNoticeEscapeCancelsFromTheCheckboxOrEitherButton pins
// that Escape is a third way to reach cancelActivityLogNotice from any
// of the three focus targets, matching the title bar's own "✕".
func TestActivityLogNoticeEscapeCancelsFromTheCheckboxOrEitherButton(t *testing.T) {
	escape := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)

	t.Run("from the checkbox", func(t *testing.T) {
		r, _, _ := newTestRootWithFile(t)
		r.openActivityLogFallbackNotice("activity log: fallback")
		if result := r.captureActivityLogNoticeListKey(escape); result != nil {
			t.Error("Escape should be consumed")
		}
		if r.activePage == activityLogNoticePage {
			t.Error("Escape should close the dialog")
		}
	})

	t.Run("from Cancel", func(t *testing.T) {
		r, _, _ := newTestRootWithFile(t)
		r.openActivityLogFallbackNotice("activity log: fallback")
		capture := r.captureActivityLogNoticeButtonKey(r.activityLogNoticeOKButton, r.activityLogNoticeList)
		if result := capture(escape); result != nil {
			t.Error("Escape should be consumed")
		}
		if r.activePage == activityLogNoticePage {
			t.Error("Escape should close the dialog")
		}
	})

	t.Run("from OK", func(t *testing.T) {
		r, _, _ := newTestRootWithFile(t)
		r.openActivityLogFallbackNotice("activity log: fallback")
		capture := r.captureActivityLogNoticeButtonKey(r.activityLogNoticeList, r.activityLogNoticeCancelButton)
		if result := capture(escape); result != nil {
			t.Error("Escape should be consumed")
		}
		if r.activePage == activityLogNoticePage {
			t.Error("Escape should close the dialog")
		}
	})
}

// TestNewActivityLoggerSkipsTheFallbackNoticeWhenHidden pins the
// setting's own actual gate inside newActivityLogger: once
// HideActivityLogFallbackNotice is on, a genuine fallback still opens
// the Writer (logging keeps working, just quietly), but produces no
// notice and reports isFallbackNotice as false — never "silence a
// notice that still fires", an actual absence of one.
func TestNewActivityLoggerSkipsTheFallbackNoticeWhenHidden(t *testing.T) {
	isolateActivityLogPaths(t)
	if err := os.WriteFile(activitylog.SystemLogDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, _, _ := newTestRootWithFile(t)
	r.settings.LogLevel = "actions"
	r.settings.HideActivityLogFallbackNotice = true

	logger, notice, isFallback := r.newActivityLogger()

	if notice != "" {
		t.Errorf("notice = %q, want empty when the fallback notice is hidden", notice)
	}
	if isFallback {
		t.Error("isFallbackNotice = true, want false when the notice itself was suppressed")
	}

	// Logging itself must still work despite the notice being
	// suppressed — ResolvePath's own fallback path is exercised the
	// same way TestNewActivityLoggerOpensTheResolvedPathWhenLevelIsOn
	// proves it for the non-fallback case, via Close erroring were the
	// Writer actually nil or broken.
	if err := logger.Close(); err != nil {
		t.Errorf("logger.Close() = %v, want the fallback Writer to have opened cleanly", err)
	}
}
