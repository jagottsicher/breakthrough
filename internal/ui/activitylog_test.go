package ui

import (
	"os"
	"path/filepath"
	"testing"

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

	logger, notice := r.newActivityLogger()

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

	logger, notice := r.newActivityLogger()
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

	logger, _ := r.newActivityLogger()
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
	r.activityLog, _ = r.newActivityLogger()

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
	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the error overlay naming the fallback the first time", r.activePage)
	}
	r.hideOverlay()

	r.reopenActivityLog()
	if r.activePage == errorPage {
		t.Error("the fallback notice should not be shown a second time in the same run")
	}
}
