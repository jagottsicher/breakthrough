package ui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rivo/tview"
)

// TestLookShortcutOpensBuiltinViewer pins Ctrl+L's default action: the
// built-in Look overlay opens over whichever entry the table's cursor is
// currently on, without needing $VISUAL/$EDITOR or any external tool at
// all (config.Settings.Pager defaults to "builtin" — see
// config.DefaultSettings).
func TestLookShortcutOpensBuiltinViewer(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1) // off ".." onto notes.txt, the only other entry here

	r.LookShortcut()

	if r.activePage != viewerPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, viewerPage)
	}
}

// TestLookShortcutRespectsGuard pins that Look's own action, like
// Edit/Rename/etc., is a real no-op while the guard says no (see
// TestToggleHiddenShortcutRespectsGuard) — not just individually
// harmless.
func TestLookShortcutRespectsGuard(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.app.SetFocus(r.bashLine)
	r.LookShortcut()

	if r.activePage == viewerPage {
		t.Error("LookShortcut should no-op while the bash line has focus")
	}
}

// TestShowBuiltinLookEscapesFileContent pins the escaping showBuiltinLook
// applies before ever handing a file's own raw content to a
// SetDynamicColors(true) TextView (see newViewerView's own doc comment):
// a literal "[ERROR]"-style bracket in the file must show up unchanged,
// not be silently swallowed as an (invalid) style tag.
func TestShowBuiltinLookEscapesFileContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	content := "2026-08-26 [ERROR] disk full\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.showBuiltinLook(path)

	if r.activePage != viewerPage {
		t.Fatalf("activePage = %q, want %q", r.activePage, viewerPage)
	}
	if got := r.viewerView.GetText(true); got != content {
		t.Errorf("viewerView text = %q, want the file's own content %q, unmangled by tag parsing", got, content)
	}
}

// TestShowBuiltinLookOnBinaryFileShowsError pins the "decline clearly
// rather than render garbage" behavior for content viewer.Sniff doesn't
// recognize as text — see viewer.KindUnsupported.
func TestShowBuiltinLookOnBinaryFileShowsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.bin")
	if err := os.WriteFile(path, []byte("\x89PNG\x00\x00\x00\rIHDR"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.showBuiltinLook(path)

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q for unsupported (binary) content", r.activePage, errorPage)
	}
}

// TestShowBuiltinLookOnDirectoryShowsError pins that Look on a directory
// entry (CurrentRowPath doesn't exclude these — only ".." — see its own
// doc comment) reports a clear error instead of trying to render one.
func TestShowBuiltinLookOnDirectoryShowsError(t *testing.T) {
	dir := fixtureDir(t) // includes an "app-data" subdirectory
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.showBuiltinLook(filepath.Join(dir, "app-data"))

	if r.activePage != errorPage {
		t.Errorf("activePage = %q, want %q for a directory target", r.activePage, errorPage)
	}
}

// TestExternalPagerCommandChain pins externalPagerCommand's own
// documented fallback order (bat/batcat, then $PAGER, then less, then
// more) by isolating $PATH to a fresh, controlled directory each step —
// the same env-isolation approach TestEditorCommandPrecedence already
// uses for $VISUAL/$EDITOR, just for $PATH-resolved binaries instead of
// plain environment variables.
func TestExternalPagerCommandChain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executables below rely on a POSIX executable bit")
	}

	empty := t.TempDir()
	t.Setenv("PATH", empty)
	t.Setenv("PAGER", "")

	if got := externalPagerCommand(); got != "more" {
		t.Errorf("externalPagerCommand() = %q, want the last-resort %q (PATH has neither bat, $PAGER, nor less)", got, "more")
	}

	t.Setenv("PAGER", "custom-pager -X")
	if got := externalPagerCommand(); got != "custom-pager -X" {
		t.Errorf("externalPagerCommand() = %q, want $PAGER's own value %q", got, "custom-pager -X")
	}

	t.Setenv("PAGER", "") // clear before testing less's own fallback, otherwise $PAGER would still win (checked first — see externalPagerCommand's own doc comment)
	withLess := t.TempDir()
	writeFakeExecutable(t, withLess, "less")
	t.Setenv("PATH", withLess)
	if got := externalPagerCommand(); got != "less" {
		t.Errorf("externalPagerCommand() = %q, want %q to win over $PAGER once it's actually on PATH", got, "less")
	}

	withBatcat := t.TempDir()
	writeFakeExecutable(t, withBatcat, "less")
	writeFakeExecutable(t, withBatcat, "batcat")
	t.Setenv("PATH", withBatcat)
	if got := externalPagerCommand(); got != "batcat --paging=always" {
		t.Errorf("externalPagerCommand() = %q, want batcat to win over less/$PAGER (Debian/Ubuntu's own binary name for bat)", got)
	}

	withBat := t.TempDir()
	writeFakeExecutable(t, withBat, "bat")
	writeFakeExecutable(t, withBat, "batcat")
	t.Setenv("PATH", withBat)
	if got := externalPagerCommand(); got != "bat --paging=always" {
		t.Errorf("externalPagerCommand() = %q, want plain %q preferred over %q when both are present", got, "bat", "batcat")
	}
}

// writeFakeExecutable creates dir/name as an empty, executable file — good
// enough for exec.LookPath's own purposes (it only checks the executable
// bit, never actually runs anything here).
func writeFakeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestOpenLookDispatchesToExternalPagerWhenConfigured pins that
// config.Settings.Pager == "external" bypasses the built-in overlay
// entirely (see openLook) — app.Suspend runs its callback directly with
// no real screen behind r.app in this test (same as
// TestContextMenuEditRunsEditCurrentEntry's own doc comment explains for
// runEditor), so this only pins the dispatch itself, not that a real
// pager's own UI actually painted anything.
func TestOpenLookDispatchesToExternalPagerWhenConfigured(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.panel.focusRow(1)
	r.settings.Pager = "external"

	r.openLook()

	if r.activePage == viewerPage {
		t.Error("openLook should not have opened the built-in overlay while Pager is \"external\"")
	}
}
