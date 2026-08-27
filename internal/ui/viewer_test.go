package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// TestShowBuiltinLookRendersRealImage pins the Phase 2 image path end
// to end: a real PNG opens Look (not an error) and its own content
// renders as half-block cells (see renderImageHalfBlocks) — checked by
// looking for the glyph itself, not by pixel-matching the exact colors
// this test doesn't otherwise care about.
func TestShowBuiltinLookRendersRealImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 6, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 6; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x * 40), G: uint8(y * 60), B: 100, A: 255})
		}
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.showBuiltinLook(path)

	if r.activePage != viewerPage {
		t.Fatalf("activePage = %q, want %q for a real image", r.activePage, viewerPage)
	}
	if got := r.viewerView.GetText(true); !strings.Contains(got, "▀") {
		t.Errorf("viewerView text doesn't contain the half-block glyph: %q", got)
	}
}

// TestShowUnsupportedLookRecommendsToolForKnownImageExtension pins that
// a file this package has no decoder for at all, but whose own
// extension unambiguously marks it as an image (see
// probablyImageExtensions), still opens Look — with a tool
// recommendation as its content — rather than falling back to the
// plain "no built-in viewer" error overlay.
func TestShowUnsupportedLookRecommendsToolForKnownImageExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.heic")
	// A real HEIC file's own actual magic bytes ("ftypheic" ISO base
	// media box, per the format's real spec — not guessed) followed by
	// a NUL byte so Sniff's own binary check fires the same way it
	// would for the genuine, much larger container format this stands
	// in for: none of this package's registered decoders recognize
	// HEIC at all, so Sniff still lands on KindUnsupported regardless.
	if err := os.WriteFile(path, []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.showBuiltinLook(path)

	if r.activePage != viewerPage {
		t.Fatalf("activePage = %q, want %q (Look should still open with a recommendation)", r.activePage, viewerPage)
	}
	if got := r.viewerView.GetText(true); !strings.Contains(got, "chafa") {
		t.Errorf("viewerView text doesn't mention chafa: %q", got)
	}
}

// TestShowUnsupportedLookRecommendsToolWhenLoadReportsReason pins the
// other route to the same recommendation: a file Sniff recognized as
// an image by its own header (so viewer.Load actually tried to decode
// it), but whose body is corrupt enough that the real decode failed —
// Result.Reason carries why, and it's shown alongside the same
// recommendation.
func TestShowUnsupportedLookRecommendsToolWhenLoadReportsReason(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.png")

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	// A real PNG header (enough for Sniff's own DecodeConfig check to
	// succeed) followed by a body chopped short enough that the real
	// pixel decode fails, but still far short of ImagePreviewLimit —
	// this must NOT be the "too large" branch, just a genuinely corrupt
	// one.
	truncated := buf.Bytes()[:len(buf.Bytes())-10]
	if err := os.WriteFile(path, truncated, 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.showBuiltinLook(path)

	if r.activePage != viewerPage {
		t.Fatalf("activePage = %q, want %q (Look should still open with a recommendation)", r.activePage, viewerPage)
	}
	got := r.viewerView.GetText(true)
	if !strings.Contains(got, "chafa") {
		t.Errorf("viewerView text doesn't mention chafa: %q", got)
	}
	if !strings.Contains(got, "decoded") {
		t.Errorf("viewerView text doesn't include Load's own Reason: %q", got)
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
