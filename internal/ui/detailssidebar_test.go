package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestToggleDetailsSidebarShortcutShowsAndHides pins Ctrl+D's own basic
// show/hide action, and that it's tracked outside activePage/
// overlayStack — see newDetailsSidebarView's own doc comment on why.
func TestToggleDetailsSidebarShortcutShowsAndHides(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	if r.detailsSidebarVisible {
		t.Fatal("setup: details sidebar should start hidden")
	}
	if slices.Contains(r.GetPageNames(true), detailsSidebarPage) {
		t.Fatal("setup: details sidebar page should not be visible yet")
	}

	r.ToggleDetailsSidebarShortcut()
	if !r.detailsSidebarVisible {
		t.Error("first toggle should show the sidebar")
	}
	if !slices.Contains(r.GetPageNames(true), detailsSidebarPage) {
		t.Error("details sidebar page should be visible in Pages after showing")
	}
	if r.activePage != "" {
		t.Errorf("activePage = %q, want unchanged (\"\") — showing the sidebar isn't modal", r.activePage)
	}

	r.ToggleDetailsSidebarShortcut()
	if r.detailsSidebarVisible {
		t.Error("second toggle should hide the sidebar again")
	}
	if slices.Contains(r.GetPageNames(true), detailsSidebarPage) {
		t.Error("details sidebar page should not be visible in Pages after hiding")
	}
}

// TestShowDetailsSidebarPreservesKeyboardFocus is a regression guard for
// a real, observed bug: tview's own Pages.ShowPage/SendToFront (and
// HidePage) silently hand real keyboard focus to whichever page is now
// last among the currently-visible ones (see preserveFocusAcross's own
// doc comment) — for detailsSidebarPage, that was itself, the instant it
// was shown. The panel's arrow keys stopped navigating it at all for as
// long as the sidebar stayed open, since real focus had quietly moved
// off it onto a TextView that never itself handles arrow keys.
func TestShowDetailsSidebarPreservesKeyboardFocus(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.app.SetFocus(r.panel.table)

	r.showDetailsSidebar()
	if got := r.app.GetFocus(); got != r.panel.table {
		t.Errorf("focus after showing the sidebar = %v, want unchanged (the panel's own table)", got)
	}

	r.hideDetailsSidebar()
	if got := r.app.GetFocus(); got != r.panel.table {
		t.Errorf("focus after hiding the sidebar = %v, want still unchanged", got)
	}
}

// TestToggleDetailsSidebarShortcutNoOpsWhileAnOverlayIsOpen mirrors
// TestTrashbinShortcutNoOpsWhileAnOverlayIsOpen (see trash_test.go) for
// Ctrl+D: like every other guarded shortcut, it must not act while some
// other overlay is already open.
func TestToggleDetailsSidebarShortcutNoOpsWhileAnOverlayIsOpen(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	r.openOptions() // any overlay; makes acceptsGlobalShortcut false
	r.ToggleDetailsSidebarShortcut()

	if r.detailsSidebarVisible {
		t.Error("ToggleDetailsSidebarShortcut acted while an overlay was open")
	}
}

// TestInfoSidebarSizeIsAtLeastOneThirdWidthAndFullHeight pins the
// sizing contract from the user's own request: at least a third of the
// screen's width, flush against its right edge, and — for now — its
// full height top to bottom (see detailsSidebarSize's own doc comment
// on why the top/bottom margin is deliberately not done here yet).
func TestInfoSidebarSizeIsAtLeastOneThirdWidthAndFullHeight(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 90, 40)

	r.showDetailsSidebar()

	x, y, width, height := r.detailsSidebar.GetRect()
	if want := 90 / 3; width < want {
		t.Errorf("width = %d, want at least a third of the screen (%d)", width, want)
	}
	if x+width != 90 {
		t.Errorf("sidebar isn't flush against the right edge: x=%d width=%d, screen width=90", x, width)
	}
	if y != 0 || height != 40 {
		t.Errorf("y,height = %d,%d, want 0,40 (the screen's full height)", y, height)
	}
}

// TestDetailsSidebarSizeRespectsMinWidthFloor pins detailsSidebarMinWidth
// as a floor for a terminal narrow enough that a literal third of it
// would otherwise be unusable.
func TestDetailsSidebarSizeRespectsMinWidthFloor(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 30, 20) // a third of this (10) is below the floor

	width, _ := r.detailsSidebarSize()
	if width != detailsSidebarMinWidth {
		t.Errorf("width = %d, want the floor %d", width, detailsSidebarMinWidth)
	}
}

// TestInfoFieldDateTimeNeverWrapsAtMinWidth is a regression guard for a
// real, observed bug: a plain single-line infoField("Modified", ...)
// wrapped by exactly one column at the sidebar's own minimum width (see
// detailsSidebarMinWidth's own doc comment) — silently pushing whatever
// came after it down by one line. infoFieldDateTime's own two-line split
// must stay narrow enough that this can never happen again.
func TestInfoFieldDateTimeNeverWrapsAtMinWidth(t *testing.T) {
	usableWidth := detailsSidebarMinWidth - 2 // the sidebar's own 1-column left/right border padding
	got := infoFieldDateTime("Modified", time.Date(2026, 8, 29, 11, 57, 33, 0, time.UTC))
	for _, line := range strings.Split(got, "\n") {
		if w := len([]rune(line)); w > usableWidth {
			t.Errorf("infoFieldDateTime produced a %d-column line, want at most %d (the sidebar's own minimum usable width): %q", w, usableWidth, line)
		}
	}
}

// TestCaptureButtonBarMouseDetailsClickTogglesSidebar pins the "^D
// Details" button (see buildButtonBar/runButtonBarAction) to the same
// toggleDetailsSidebar Ctrl+D already runs — one action, two ways to
// reach it, and unlike Ctrl+D, unguarded (see toggleDetailsSidebar's own
// doc comment on why a click doesn't need acceptsGlobalShortcut).
func TestCaptureButtonBarMouseDetailsClickTogglesSidebar(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	span, ok := buttonBarSpanFor(r, buttonActionDetails)
	if !ok {
		t.Fatal("no Details span found")
	}

	clickButtonBar(t, r, span.startCol)
	if !r.detailsSidebarVisible {
		t.Error("clicking Details should show the sidebar")
	}

	clickButtonBar(t, r, span.startCol)
	if r.detailsSidebarVisible {
		t.Error("clicking Details again should hide the sidebar")
	}
}

// TestCaptureDetailsSidebarMouseSwallowsEveryActionInsideItsRect pins
// the fix for a real gap: tview.Box's own default MouseHandler only
// ever consumes MouseLeftDown, so without this capture, a right-click or
// scroll landing on the sidebar would fall straight through to the
// panel underneath, sharing that same screen space.
func TestCaptureDetailsSidebarMouseSwallowsEveryActionInsideItsRect(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 90, 40)
	r.showDetailsSidebar()

	x, y, width, _ := r.detailsSidebar.GetRect()
	insideX, insideY := x+width/2, y

	action, event := r.captureDetailsSidebarMouse(tview.MouseScrollUp, tcell.NewEventMouse(insideX, insideY, tcell.ButtonNone, 0))
	if action != tview.MouseConsumed || event != nil {
		t.Errorf("inside click: action=%v event=%v, want (MouseConsumed, nil)", action, event)
	}

	outsideX := x - 1
	action, event = r.captureDetailsSidebarMouse(tview.MouseScrollUp, tcell.NewEventMouse(outsideX, insideY, tcell.ButtonNone, 0))
	if action != tview.MouseScrollUp || event == nil {
		t.Errorf("outside click: action=%v event=%v, want passed through unchanged", action, event)
	}
}

// TestShowDetailsSidebarLoadsCurrentEntryStat pins that showing the
// sidebar actually loads real content for whatever the panel's cursor
// is on — not just the empty shell this feature started as.
func TestShowDetailsSidebarLoadsCurrentEntryStat(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(1) // off ".." onto a real entry
	_, path, ok := r.panel.CurrentRowPath()
	if !ok {
		t.Fatal("setup: row 1 should be a real entry")
	}

	r.showDetailsSidebar()

	if r.detailsTarget != path {
		t.Fatalf("detailsTarget = %q, want %q", r.detailsTarget, path)
	}
	text := r.detailsSidebar.GetText(true)
	for _, want := range []string{"Type:", "Permissions:", "Size:", "Modified:", path} {
		if !strings.Contains(text, want) {
			t.Errorf("details sidebar text should contain %q, got:\n%s", want, text)
		}
	}
}

// TestRefreshDetailsSidebarUpdatesOnSelectionChange pins the whole point
// of wiring SetSelectionChangedFunc in NewRoot: the sidebar's content
// follows the cursor live, not just once when first shown.
func TestRefreshDetailsSidebarUpdatesOnSelectionChange(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	r.panel.focusRow(1)
	_, path1, ok := r.panel.CurrentRowPath()
	if !ok {
		t.Fatal("setup: row 1 should be a real entry")
	}
	r.showDetailsSidebar()
	if r.detailsTarget != path1 {
		t.Fatalf("detailsTarget = %q, want %q", r.detailsTarget, path1)
	}

	r.panel.focusRow(2)
	_, path2, ok := r.panel.CurrentRowPath()
	if !ok || path2 == path1 {
		t.Fatal("setup: row 2 should be a different real entry")
	}

	if r.detailsTarget != path2 {
		t.Errorf("detailsTarget after moving the selection = %q, want %q — the sidebar should follow the cursor live", r.detailsTarget, path2)
	}
}

// TestRefreshDetailsSidebarShowsPlaceholderForDotDot pins the
// nothing-meaningfully-selected case: CurrentRowPath reports ok=false
// for "..", and the sidebar should say so rather than show stale or
// garbage content.
func TestRefreshDetailsSidebarShowsPlaceholderForDotDot(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(0) // ".."

	r.showDetailsSidebar()

	if r.detailsTarget != "" {
		t.Errorf("detailsTarget = %q, want \"\" while \"..\" is selected", r.detailsTarget)
	}
	if text := r.detailsSidebar.GetText(true); !strings.Contains(text, "nothing selected") {
		t.Errorf("details sidebar text should say nothing is selected, got:\n%s", text)
	}
}

// TestDetailsSidebarSkipsHashSectionForDirectory mirrors
// TestComputeHashesSkipsDirectories (see properties_test.go) for
// Details: a directory gets no hash hint/section at all.
func TestDetailsSidebarSkipsHashSectionForDirectory(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.loadDetailsTarget(filepath.Join(dir, "app-data"))
	r.detailsSidebarVisible = true // renderDetailsSidebar itself doesn't check this — showDetailsSidebar already has by the time it's called for real

	text := r.detailsSidebar.GetText(true)
	if strings.Contains(text, "compute") || strings.Contains(text, "SHA-256") {
		t.Errorf("a directory's Details sidebar should not offer to compute a hash, got:\n%s", text)
	}
	if r.detailsHashRowStart != -1 {
		t.Errorf("detailsHashRowStart = %d, want -1 (no hash section) for a directory", r.detailsHashRowStart)
	}
}

// TestDetailsSidebarShowsImagePreviewAndDimensionsForImageFile pins the
// Phase 2-style image path end to end for Details: a real PNG gets a
// half-block preview (see renderImageHalfBlocks), its real pixel
// dimensions, and the metadata hint — mirrors
// TestShowBuiltinLookRendersRealImage (see viewer_test.go) for Look.
func TestDetailsSidebarShowsImagePreviewAndDimensionsForImageFile(t *testing.T) {
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
	r.SetRect(0, 0, 100, 40)

	r.loadDetailsTarget(path)
	r.detailsSidebarVisible = true

	if r.detailsImage == nil {
		t.Fatal("detailsImage should be set for a real PNG")
	}
	text := r.detailsSidebar.GetText(true)
	if !strings.Contains(text, "▀") {
		t.Errorf("details sidebar text should contain a half-block preview, got:\n%s", text)
	}
	if !strings.Contains(text, "6 × 4 px") {
		t.Errorf("details sidebar text should show the image's real dimensions (6 × 4 px), got:\n%s", text)
	}
	if !strings.Contains(text, "PNG") {
		t.Errorf("details sidebar text should show the image format (PNG), got:\n%s", text)
	}
	if r.detailsMetaRowStart < 0 || r.detailsMetaRowEnd < r.detailsMetaRowStart {
		t.Errorf("detailsMetaRowStart/End = %d/%d, want a valid non-negative range for an image target", r.detailsMetaRowStart, r.detailsMetaRowEnd)
	}
}

// TestFetchDetailsMetadataShowsStubMessage pins that the metadata hint's
// click zone/keyboard shortcut already does something real end to end —
// see fetchDetailsMetadata's own doc comment on why what it shows is
// still a stub rather than actual EXIF data.
func TestFetchDetailsMetadataShowsStubMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.loadDetailsTarget(path)
	r.detailsSidebarVisible = true

	before := r.detailsSidebar.GetText(true)
	if !strings.Contains(before, "Ctrl+N") {
		t.Errorf("before fetching, the sidebar should show the Ctrl+N hint, got:\n%s", before)
	}

	r.FetchMetadataShortcut()

	after := r.detailsSidebar.GetText(true)
	if !strings.Contains(after, detailsMetadataStubMessage) {
		t.Errorf("after FetchMetadataShortcut, the sidebar should show the stub message, got:\n%s", after)
	}
}

// TestFetchMetadataShortcutNoOpsForNonImageTarget pins that the
// metadata action is image-specific: a plain text file has no metadata
// section to trigger at all.
func TestFetchMetadataShortcutNoOpsForNonImageTarget(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(1)
	r.showDetailsSidebar()

	before := r.detailsSidebar.GetText(true)
	r.FetchMetadataShortcut()
	after := r.detailsSidebar.GetText(true)

	if before != after {
		t.Errorf("FetchMetadataShortcut on a non-image target should be a no-op, text changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestComputeHashesShortcutTargetsPropertiesWhenOpen pins the user's own
// explicit request for when both overlays are open at once: Ctrl+K acts
// on Properties (which holds real keyboard focus, being modal), not on
// a Details sidebar sitting unfocused behind it.
func TestComputeHashesShortcutTargetsPropertiesWhenOpen(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	started := isolateHashFile(t)
	t.Cleanup(r.cancelHashComputation)
	t.Cleanup(r.cancelDetailsHashComputation)

	r.panel.focusRow(1)
	r.showDetailsSidebar()
	r.target = path
	r.openProperties()

	r.ComputeHashesShortcut()
	<-started

	if !r.hashInProgress {
		t.Error("hashInProgress should be true — Ctrl+K should target the open Properties overlay")
	}
	if r.detailsHashInProgress {
		t.Error("detailsHashInProgress should stay false while Properties is the one holding focus")
	}
}

// TestComputeHashesShortcutTargetsDetailsWhenPropertiesNotOpen pins the
// other half: with Properties not open, Ctrl+K acts on Details instead.
func TestComputeHashesShortcutTargetsDetailsWhenPropertiesNotOpen(t *testing.T) {
	// A plain temp dir with a single file, not fixtureDir: fixtureDir
	// also has an "app-data" subdirectory that happens to sort before
	// the files (even alphabetically — "app-data" < "apple.txt"), so
	// row 1 there isn't reliably a file computeDetailsHashes would ever
	// actually hash (see isDirish's own early return) — this hung
	// waiting on <-started before being caught and fixed here.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	started := isolateHashFile(t)
	t.Cleanup(r.cancelDetailsHashComputation)

	r.panel.focusRow(1)
	r.showDetailsSidebar()

	r.ComputeHashesShortcut()
	<-started

	if !r.detailsHashInProgress {
		t.Error("detailsHashInProgress should be true — Ctrl+K should target Details when Properties isn't open")
	}
	if r.hashInProgress {
		t.Error("hashInProgress (Properties') should stay false — Properties was never opened")
	}
}

// TestHideDetailsSidebarCancelsInProgressHashComputation pins the
// original design intent (see feature_ideas.txt): an expensive
// computation running purely for the sidebar's own benefit stops the
// moment the sidebar itself is hidden again.
func TestHideDetailsSidebarCancelsInProgressHashComputation(t *testing.T) {
	// A plain temp dir with a single file, not fixtureDir — see
	// TestComputeHashesShortcutTargetsDetailsWhenPropertiesNotOpen's own
	// comment on why fixtureDir's row 1 isn't reliably a file.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	started := isolateHashFile(t)
	t.Cleanup(r.cancelDetailsHashComputation)

	r.panel.focusRow(1)
	r.showDetailsSidebar()
	r.computeDetailsHashes()
	<-started
	if !r.detailsHashInProgress {
		t.Fatal("setup: detailsHashInProgress should be true right after computeDetailsHashes starts")
	}

	r.hideDetailsSidebar()

	if r.detailsHashInProgress {
		t.Error("hiding the sidebar should cancel an in-progress hash computation")
	}
}
