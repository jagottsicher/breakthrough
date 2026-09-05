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

	"github.com/jagottsicher/breakthrough/internal/fsops"
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

// TestCycleFocusShortcutTogglesBetweenPanelAndSidebar pins Tab's
// own two-way action: from the panel, it moves focus onto the sidebar
// (so its own already-built-in scrolling works); pressed again, it
// moves focus back.
func TestCycleFocusShortcutTogglesBetweenPanelAndSidebar(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.app.SetFocus(r.panel.table)
	r.showDetailsSidebar()

	if !r.CycleFocusShortcut() {
		t.Fatal("first Tab (panel -> sidebar) should report true")
	}
	if got := r.app.GetFocus(); got != r.detailsSidebar {
		t.Errorf("focus after first Tab = %v, want the details sidebar", got)
	}

	if !r.CycleFocusShortcut() {
		t.Fatal("second Tab (sidebar -> panel) should report true")
	}
	if got := r.app.GetFocus(); got != r.panel.table {
		t.Errorf("focus after second Tab = %v, want the panel's own table", got)
	}
}

// TestCycleFocusShortcutReturnsFalseWhenNeitherApplies pins the
// half of Tab's contract cmd/breakthrough actually depends on: it must
// report false — so Tab falls through untouched — whenever neither the
// panel nor the sidebar is what currently has focus (here: Properties,
// which needs its own Tab for moving between fields), and also
// whenever the sidebar isn't even shown at all.
func TestCycleFocusShortcutReturnsFalseWhenNeitherApplies(t *testing.T) {
	dir := fixtureDir(t)
	path := filepath.Join(dir, "apple.txt")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)

	if r.CycleFocusShortcut() {
		t.Error("should report false when the sidebar isn't shown at all")
	}

	r.target = path
	r.openProperties()
	if r.CycleFocusShortcut() {
		t.Error("should report false while Properties (not the panel or the sidebar) has focus")
	}
	if got := r.app.GetFocus(); got == r.detailsSidebar || got == r.panel.table {
		t.Errorf("focus should still be on Properties, got %v", got)
	}
}

// TestHideDetailsSidebarRedirectsFocusWhenSidebarWasFocused is a
// regression guard: preserveFocusAcross alone would restore focus onto
// the very widget hideDetailsSidebar is about to hide, if that's what
// had it — see hideDetailsSidebar's own doc comment.
func TestHideDetailsSidebarRedirectsFocusWhenSidebarWasFocused(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.app.SetFocus(r.panel.table)
	r.showDetailsSidebar()
	r.CycleFocusShortcut() // panel -> sidebar
	if got := r.app.GetFocus(); got != r.detailsSidebar {
		t.Fatalf("setup: focus = %v, want the details sidebar", got)
	}

	r.hideDetailsSidebar()

	if got := r.app.GetFocus(); got != r.panel.table {
		t.Errorf("focus after hiding a focused sidebar = %v, want redirected to the panel's own table", got)
	}
}

// TestLoadDetailsTargetResetsScrollPosition pins that a new target
// always starts showing from its own top, not wherever the previous
// one happened to be scrolled to.
func TestLoadDetailsTargetResetsScrollPosition(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(1)
	r.showDetailsSidebar()

	r.detailsSidebar.ScrollTo(3, 0)
	if row, _ := r.detailsSidebar.GetScrollOffset(); row != 3 {
		t.Fatalf("setup: scroll offset row = %d, want 3", row)
	}

	r.panel.focusRow(2)
	if row, _ := r.detailsSidebar.GetScrollOffset(); row != 0 {
		t.Errorf("scroll offset row after moving to a new target = %d, want 0 (reset)", row)
	}
}

// TestDetailsSidebarBackgroundReflectsFocusState pins the visual cue
// SetFocusFunc/SetBlurFunc give (see newDetailsSidebarView's own doc
// comment): the content area stays a constant AccentBackground
// regardless of focus, while detailsTitleBar swaps to FocusedBackground
// (a dark cyan/"petrol" tone) while Details itself has real keyboard
// focus, and EditableBackground while it doesn't.
func TestDetailsSidebarBackgroundReflectsFocusState(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.app.SetFocus(r.panel.table)
	r.showDetailsSidebar()

	// The content area's own background never changes — see
	// newDetailsSidebarView's own doc comment: it's detailsTitleBar
	// below, not this, that shows the current focus state, per the
	// user's own explicit request that every panel floating over the
	// main one (toolWindow's own content included — see toolwindow.go)
	// share this one constant "normal panel background".
	if got, want := r.detailsSidebar.GetBackgroundColor(), r.theme.AccentBackground; got != want {
		t.Errorf("content background = %v, want the constant AccentBackground %v", got, want)
	}

	if got, want := r.detailsTitleBar.GetBackgroundColor(), r.theme.EditableBackground; got != want {
		t.Errorf("title bar background before focus = %v, want EditableBackground %v", got, want)
	}

	r.CycleFocusShortcut()
	if got, want := r.detailsSidebar.GetBackgroundColor(), r.theme.AccentBackground; got != want {
		t.Errorf("content background while focused = %v, want still the constant AccentBackground %v", got, want)
	}
	if got, want := r.detailsTitleBar.GetBackgroundColor(), r.theme.FocusedBackground; got != want {
		t.Errorf("title bar background while focused = %v, want FocusedBackground %v", got, want)
	}

	r.CycleFocusShortcut()
	if got, want := r.detailsTitleBar.GetBackgroundColor(), r.theme.EditableBackground; got != want {
		t.Errorf("title bar background after losing focus again = %v, want EditableBackground %v", got, want)
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

// TestToggleDetailsSidebarShortcutWorksWhilePropertiesOpen pins the
// user's own explicit request: unlike every other overlay (see
// TestToggleDetailsSidebarShortcutNoOpsWhileAnOverlayIsOpen just above),
// Properties specifically must NOT block Ctrl+D — Details should open
// and close alongside it, not require closing Properties first.
func TestToggleDetailsSidebarShortcutWorksWhilePropertiesOpen(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.target = filepath.Join(dir, "apple.txt")
	r.openProperties()

	r.ToggleDetailsSidebarShortcut()
	if !r.detailsSidebarVisible {
		t.Error("Ctrl+D should show the Details sidebar while Properties is open")
	}
	if r.activePage != propertiesPage {
		t.Errorf("activePage = %q, want Properties to stay open", r.activePage)
	}

	r.ToggleDetailsSidebarShortcut()
	if r.detailsSidebarVisible {
		t.Error("a second Ctrl+D should hide the Details sidebar again")
	}
	if r.activePage != propertiesPage {
		t.Errorf("activePage = %q, want Properties to still be open", r.activePage)
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

	// detailsSidebarLayout, not detailsSidebar itself: the Flex wrapping
	// title bar + content (see repositionDetailsSidebar's own doc
	// comment) only actually distributes that rect down to its own
	// children on its next real Draw() — verified directly against
	// tview's own flex.go, the same "resize=true only cascades during
	// Draw()" caveat chmoddialog.go's own openChmod doc comment already
	// documents for tview.Pages. detailsSidebarLayout's own rect is what
	// repositionDetailsSidebar actually sets synchronously, and what
	// determines the sidebar's real on-screen position/size either way.
	x, y, width, height := r.detailsSidebarLayout.GetRect()
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

// TestDetailsFullscreenHintNeverWrapsAtMinWidth is a regression guard
// for a real, observed bug: the original, longer wording of this hint
// ("Press Ctrl+L or click here for fullscreen") wrapped at the
// sidebar's own minimum width, silently mis-numbering every row after
// it — the same class of bug already fixed once for hashes and once for
// Modified.
func TestDetailsFullscreenHintNeverWrapsAtMinWidth(t *testing.T) {
	usableWidth := detailsSidebarMinWidth - 2
	if w := len([]rune(detailsFullscreenHint)); w > usableWidth {
		t.Errorf("detailsFullscreenHint is %d columns wide, want at most %d (the sidebar's own minimum usable width): %q", w, usableWidth, detailsFullscreenHint)
	}
}

// TestDetailsMetadataHintAndStubNeverWrapAtMinWidth is a regression
// guard for a real, observed bug: the original, longer wording of both
// of these strings wrapped at the sidebar's own minimum width — unlike
// detailsFullscreenHint (which sits with nothing after it to
// mis-number), both of these are always followed by the stat block,
// so wrapping here silently threw off every click zone below it.
func TestDetailsMetadataHintAndStubNeverWrapAtMinWidth(t *testing.T) {
	usableWidth := detailsSidebarMinWidth - 2
	for _, s := range []string{detailsMetadataHint, detailsMetadataStubMessage} {
		if w := len([]rune(s)); w > usableWidth {
			t.Errorf("%q is %d columns wide, want at most %d (the sidebar's own minimum usable width)", s, w, usableWidth)
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

// TestDetailsExpandButtonShowsSidebar pins the user's own explicit
// request for a mouse alternative to Ctrl+D: the header row's own "<"
// button (see Panel.detailsExpandBtn/onExpandDetails) shows the
// sidebar, the same as clicking the button bar's own Details button —
// but only ever that one direction (expand), unlike the button bar's
// own toggle.
func TestDetailsExpandButtonShowsSidebar(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	if r.detailsSidebarVisible {
		t.Fatal("setup: sidebar should start hidden")
	}

	r.panel.detailsExpandBtn.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(tview.Primitive) {})

	if !r.detailsSidebarVisible {
		t.Error("clicking the \"<\" button should show the sidebar")
	}
}

// TestDetailsTitleBarCollapsesOnCollapseButtonClick pins the user's own
// explicit request for a ">" collapse button on the sidebar's own
// title bar, mirroring the header row's own "<" expand button in the
// other direction — the same one-column-in-from-the-edge spacing
// toolWindow's/Help's own close buttons use (toolWindowCloseButtonCol).
func TestDetailsTitleBarCollapsesOnCollapseButtonClick(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.showDetailsSidebar()
	if !r.detailsSidebarVisible {
		t.Fatal("setup: sidebar should be visible")
	}

	x, y, width, _ := r.detailsTitleBar.GetRect()
	closeX := x + toolWindowCloseButtonCol(0, width)
	captured, _ := r.captureDetailsTitleBarMouse(tview.MouseLeftClick, tcell.NewEventMouse(closeX, y, tcell.ButtonNone, 0))

	if captured != tview.MouseConsumed {
		t.Error("clicking the collapse button should consume the click")
	}
	if r.detailsSidebarVisible {
		t.Error("clicking the collapse button should hide the sidebar")
	}
}

// TestDetailsTitleBarClickElsewhereDoesNothing pins that only the exact
// collapse-button cell does anything — the same "otherwise inert" shape
// Help's own title bar has (see TestHelpTitleBarClickElsewhereDoesNothing).
func TestDetailsTitleBarClickElsewhereDoesNothing(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.showDetailsSidebar()

	x, y, _, _ := r.detailsTitleBar.GetRect()
	captured, _ := r.captureDetailsTitleBarMouse(tview.MouseLeftClick, tcell.NewEventMouse(x+1, y, tcell.ButtonNone, 0))

	if captured == tview.MouseConsumed {
		t.Error("a click away from the collapse button should not be consumed")
	}
	if !r.detailsSidebarVisible {
		t.Error("the sidebar should still be visible")
	}
}

// TestCaptureDetailsSidebarMouseSwallowsEveryActionInsideItsRect pins
// the fix for a real gap: tview.Box's own default MouseHandler only
// ever consumes MouseLeftDown, so without this capture, a right-click or
// scroll landing on the sidebar would fall straight through to the
// panel underneath, sharing that same screen space.
func TestCaptureDetailsSidebarMouseSwallowsUnhandledActionsInsideItsRect(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 90, 40)
	r.showDetailsSidebar()

	x, y, width, _ := r.detailsSidebar.GetRect()
	insideX, insideY := x+width/2, y

	// MouseRightClick isn't one of the actions captureDetailsSidebarMouse
	// deliberately lets through (see its own doc comment: scroll,
	// MouseLeftDown, and a non-click-zone MouseLeftClick) — still
	// swallowed, so it can't leak through to the panel underneath.
	action, event := r.captureDetailsSidebarMouse(tview.MouseRightClick, tcell.NewEventMouse(insideX, insideY, tcell.ButtonNone, 0))
	if action != tview.MouseConsumed || event != nil {
		t.Errorf("inside click: action=%v event=%v, want (MouseConsumed, nil)", action, event)
	}

	outsideX := x - 1
	action, event = r.captureDetailsSidebarMouse(tview.MouseRightClick, tcell.NewEventMouse(outsideX, insideY, tcell.ButtonNone, 0))
	if action != tview.MouseRightClick || event == nil {
		t.Errorf("outside click: action=%v event=%v, want passed through unchanged", action, event)
	}
}

// TestCaptureDetailsSidebarMouseLetsScrollAndFocusThrough pins the fix
// for the user's own explicit report: mouse-wheel scrolling (and a
// plain click that focuses the sidebar via tview's own MouseLeftDown
// handling — see CycleFocusShortcut for the Tab-driven way in)
// must reach the TextView's own default MouseHandler, not be swallowed
// here the way every other action still is.
func TestCaptureDetailsSidebarMouseLetsScrollAndFocusThrough(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 90, 40)
	r.showDetailsSidebar()

	x, y, width, _ := r.detailsSidebar.GetRect()
	insideX, insideY := x+width/2, y

	for _, tc := range []tview.MouseAction{tview.MouseScrollUp, tview.MouseScrollDown, tview.MouseLeftDown} {
		action, event := r.captureDetailsSidebarMouse(tc, tcell.NewEventMouse(insideX, insideY, tcell.ButtonNone, 0))
		if action != tc || event == nil {
			t.Errorf("%v: action=%v event=%v, want passed through unchanged", tc, action, event)
		}
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
// Details: a directory gets no hash hint/section at all — a directory
// size hint (see the test right below) takes that section's place
// instead, per the user's own explicit request, rather than the empty
// space this used to leave there.
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
	if strings.Contains(text, "SHA-256") {
		t.Errorf("a directory's Details sidebar should not offer to compute a hash, got:\n%s", text)
	}
	if r.detailsHashRowStart != -1 {
		t.Errorf("detailsHashRowStart = %d, want -1 (no hash section) for a directory", r.detailsHashRowStart)
	}
}

// TestDetailsSidebarShowsDirSizeHintForDirectory is the other half of the
// test just above: what actually takes the hash section's place for a
// directory — an idle "press Ctrl+U or click" hint until triggered, the
// same shape the hash section itself has before Ctrl+K.
func TestDetailsSidebarShowsDirSizeHintForDirectory(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.loadDetailsTarget(filepath.Join(dir, "app-data"))
	r.detailsSidebarVisible = true

	text := r.detailsSidebar.GetText(true)
	if !strings.Contains(text, "Ctrl+U") || !strings.Contains(text, "du -hs") {
		t.Errorf("a directory's Details sidebar should hint at Ctrl+U/du -hs, got:\n%s", text)
	}
	if r.detailsDirSizeRowStart < 0 {
		t.Errorf("detailsDirSizeRowStart = %d, want >= 0 for a directory", r.detailsDirSizeRowStart)
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

// TestPropagateHashResultUpdatesBothWhenShowingSameTarget pins the
// user's own explicit request: a hash computed via either side shows up
// in both, whenever they're showing the very same file at once — tested
// against propagateHashResult directly (see its own doc comment on why
// it's the sole place either side's own display actually gets updated,
// not just a bolt-on for the other one), since the real async paths
// that call it never actually complete in this test style (nothing
// drains QueueUpdateDraw — see isolateHashFile's own doc comment).
func TestPropagateHashResultUpdatesBothWhenShowingSameTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(1) // the only real entry
	r.showDetailsSidebar()
	r.target = path
	r.openProperties()

	if r.detailsTarget != path {
		t.Fatalf("setup: detailsTarget = %q, want %q — both must be showing the same file for this test", r.detailsTarget, path)
	}

	r.propagateHashResult(path, fsops.Hashes{MD5: "abc123"})

	if r.propertiesHashes == nil || r.propertiesHashes.MD5 != "abc123" {
		t.Errorf("propertiesHashes = %v, want MD5 abc123", r.propertiesHashes)
	}
	if r.detailsHashes == nil || r.detailsHashes.MD5 != "abc123" {
		t.Errorf("detailsHashes = %v, want MD5 abc123", r.detailsHashes)
	}
}

// TestPropagateHashResultIgnoresUnrelatedTarget pins the other half:
// a result computed for some other path — not what either side is
// currently showing — must not overwrite either one's own display.
func TestPropagateHashResultIgnoresUnrelatedTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(1)
	r.showDetailsSidebar()
	r.target = path
	r.openProperties()

	r.propagateHashResult(filepath.Join(dir, "unrelated.txt"), fsops.Hashes{MD5: "xyz"})

	if r.propertiesHashes != nil {
		t.Errorf("propertiesHashes = %v, want nil — the result was for a different file", r.propertiesHashes)
	}
	if r.detailsHashes != nil {
		t.Errorf("detailsHashes = %v, want nil — the result was for a different file", r.detailsHashes)
	}
}

// TestOpenPropertiesAdoptsExistingDetailsHash pins the "adopt on open"
// half of the user's own explicit request: opening Properties on a file
// Details already has a computed hash for shows that result right away
// — no fresh "press Ctrl+K" hint, no redundant recomputation.
func TestOpenPropertiesAdoptsExistingDetailsHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(1)
	r.showDetailsSidebar()
	r.detailsHashes = &fsops.Hashes{MD5: "abc123"} // simulating an already-computed result

	r.target = path
	r.openProperties()

	if r.propertiesHashes == nil || r.propertiesHashes.MD5 != "abc123" {
		t.Errorf("propertiesHashes = %v, want adopted MD5 abc123 from Details", r.propertiesHashes)
	}
}

// TestLoadDetailsTargetAdoptsExistingPropertiesHash is
// TestOpenPropertiesAdoptsExistingDetailsHash's own mirror image: Details
// loading a target Properties already has open and hashed adopts that
// result immediately too.
func TestLoadDetailsTargetAdoptsExistingPropertiesHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(1)
	r.target = path
	r.openProperties()
	r.propertiesHashes = &fsops.Hashes{MD5: "xyz789"} // simulating an already-computed result

	r.showDetailsSidebar() // Details loads for the first time, same target

	if r.detailsHashes == nil || r.detailsHashes.MD5 != "xyz789" {
		t.Errorf("detailsHashes = %v, want adopted MD5 xyz789 from Properties", r.detailsHashes)
	}
}

// TestSavePropertiesEditRefreshesDetailsShowingSameFile pins the user's
// own explicit request: committing an edit in Properties (Return on a
// field, then Save) immediately updates Details too, if it's showing
// that same file — including following a rename to the file's own new
// path, not just refreshing stale info still filed under the old one.
// A real, observed gap otherwise: a same-directory reload (see
// savePropertiesEdit's own doc comment) never repositions the table's
// selection, so nothing would otherwise tell Details a rename even
// happened.
func TestSavePropertiesEditRefreshesDetailsShowingSameFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apple.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(1)
	r.showDetailsSidebar()
	if r.detailsTarget != path {
		t.Fatalf("setup: detailsTarget = %q, want %q", r.detailsTarget, path)
	}

	r.target = path
	r.openProperties()
	r.stagedName = "banana.txt"
	r.savePropertiesEdit()

	wantPath := filepath.Join(dir, "banana.txt")
	if r.detailsTarget != wantPath {
		t.Errorf("detailsTarget after rename via Save = %q, want %q", r.detailsTarget, wantPath)
	}
	if got := r.detailsSidebar.GetText(true); !strings.Contains(got, "banana.txt") {
		t.Errorf("details sidebar text should show the new name, got:\n%s", got)
	}
}

// TestSavePropertiesEditIgnoresDetailsShowingADifferentFile pins the
// other half: Details tracking some other file entirely must not be
// disturbed by an edit to whatever Properties happens to be open on.
func TestSavePropertiesEditIgnoresDetailsShowingADifferentFile(t *testing.T) {
	dir := t.TempDir()
	applePath := filepath.Join(dir, "apple.txt")
	bananaPath := filepath.Join(dir, "banana.txt")
	for _, p := range []string{applePath, bananaPath} {
		if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.panel.focusRow(1)
	r.showDetailsSidebar()
	if r.detailsTarget != applePath {
		t.Fatalf("setup: detailsTarget = %q, want %q", r.detailsTarget, applePath)
	}

	r.target = bananaPath
	r.openProperties()
	r.stagedMode = 0o600
	r.savePropertiesEdit()

	if r.detailsTarget != applePath {
		t.Errorf("detailsTarget = %q, want unchanged %q — Properties was editing a different file", r.detailsTarget, applePath)
	}
}

// TestClickingDetailsHashZoneDefersToOpenProperties pins the fix for
// the inconsistency the user's own report surfaced: the click zone used
// to call computeDetailsHashes directly, bypassing the "Properties wins
// while it's open" rule Ctrl+K already followed (see
// ComputeHashesShortcut) — a click was a second way around it that
// pressing the key wasn't.
func TestClickingDetailsHashZoneDefersToOpenProperties(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

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

	if r.detailsHashRowStart < 0 {
		t.Fatal("setup: detailsHashRowStart should be set for a non-directory target")
	}
	_, rectY, _, _ := r.detailsSidebar.GetInnerRect()
	x, _, _, _ := r.detailsSidebar.GetRect()
	action, _ := r.captureDetailsSidebarMouse(tview.MouseLeftClick, tcell.NewEventMouse(x, rectY+r.detailsHashRowStart, tcell.Button1, 0))
	if action != tview.MouseConsumed {
		t.Fatalf("action = %v, want MouseConsumed", action)
	}
	<-started

	if !r.hashInProgress {
		t.Error("clicking Details' hash zone while Properties is open should target Properties, not start its own computation")
	}
	if r.detailsHashInProgress {
		t.Error("should not also start Details' own independent computation")
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

// isolateDirSize mirrors isolateHashFile's own doc comment exactly —
// same blocking-fake shape, same LIFO-cleanup-ordering caveat, just for
// dirSize/fsops.DirSize instead of hashFile/fsops.Hash.
func isolateDirSize(t *testing.T) <-chan struct{} {
	t.Helper()
	original := dirSize
	started := make(chan struct{})
	unblock := make(chan struct{})
	dirSize = func(dir string) (int64, string, bool) {
		close(started)
		<-unblock
		return 0, dir, false
	}
	t.Cleanup(func() {
		close(unblock)
		dirSize = original
	})
	return started
}

// TestComputeDetailsDirSizeShowsAnimationImmediately mirrors
// TestComputeHashesShowsAnimationImmediately for the directory-size
// section: du can take a real, visible amount of time on a large tree,
// so this must show a moving "in progress" indicator right away.
func TestComputeDetailsDirSizeShowsAnimationImmediately(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	started := isolateDirSize(t)
	t.Cleanup(r.cancelDetailsDirSizeComputation)

	r.showDetailsSidebar()
	r.loadDetailsTarget(filepath.Join(dir, "app-data"))
	r.computeDetailsDirSize()
	<-started // wait for dirSize's one-time read (see isolateDirSize) before this test can safely end

	if !r.detailsDirSizeInProgress {
		t.Fatal("detailsDirSizeInProgress should be true right after computeDetailsDirSize starts")
	}
	text := r.detailsSidebar.GetText(true)
	if !strings.Contains(text, hashAnimationFrames[0]) || !strings.Contains(text, "Computing size") {
		t.Errorf("detailsSidebar should show the first animation frame (%q) and \"Computing size\", got:\n%s", hashAnimationFrames[0], text)
	}
}

// TestComputeDetailsDirSizeStoresResult pins the render side of the
// happy path: a computed result shows as a human-readable size, in
// place of the idle hint. Mirrors TestComputeHashesUpdatesPropertiesText
// exactly (see its own doc comment on why): computeDetailsDirSize itself
// only ever *starts* a background computation, and the actual result
// only ever lands via r.app.QueueUpdateDraw, which nothing here drains
// (see isolateDirSize's own doc comment) — so this sets detailsDirSize
// directly and pins renderDetailsSidebar's own handling of it instead.
func TestComputeDetailsDirSizeStoresResult(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.showDetailsSidebar()
	r.loadDetailsTarget(filepath.Join(dir, "app-data"))

	before := r.detailsSidebar.GetText(true)
	if !strings.Contains(before, "Ctrl+U") {
		t.Errorf("before computing a size, the sidebar should show the hint, got:\n%s", before)
	}

	size := int64(5 * 1024 * 1024) // 5.0M
	r.detailsDirSize = &size
	r.renderDetailsSidebar()

	after := r.detailsSidebar.GetText(true)
	// A real, observed bug once had the value flush against the colon
	// with no space (see this line's own doc comment in
	// renderDetailsSidebar) — pinned explicitly here, not just via a
	// bare "5.0M" substring check that a regression like that would
	// still pass.
	if !strings.Contains(after, "Size (du -hs): 5.0M") {
		t.Errorf("detailsSidebar after computing a size should show \"Size (du -hs): 5.0M\", got:\n%s", after)
	}
	if strings.Contains(after, "Ctrl+U") {
		t.Errorf("detailsSidebar after computing a size should no longer show the hint, got:\n%s", after)
	}
}

// TestComputeDetailsDirSizeSkipsNonDirectories mirrors
// TestComputeHashesSkipsDirectories in reverse: a plain file has hashes,
// not a directory-size section, so triggering this on one must do
// nothing.
func TestComputeDetailsDirSizeSkipsNonDirectories(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	original := dirSize
	called := false
	dirSize = func(dir string) (int64, string, bool) { called = true; return 0, dir, true }
	t.Cleanup(func() { dirSize = original })

	r.showDetailsSidebar()
	r.loadDetailsTarget(filepath.Join(dir, "apple.txt"))

	r.computeDetailsDirSize()

	if called {
		t.Error("computeDetailsDirSize ran dirSize against a plain file")
	}
	if r.detailsDirSizeInProgress {
		t.Error("detailsDirSizeInProgress should stay false for a plain file")
	}
}

// TestHideDetailsSidebarCancelsInProgressDirSizeComputation mirrors
// TestHideDetailsSidebarCancelsInProgressHashComputation: an expensive
// computation running purely for the sidebar's own benefit stops the
// moment the sidebar itself is hidden again.
func TestHideDetailsSidebarCancelsInProgressDirSizeComputation(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	started := isolateDirSize(t)
	t.Cleanup(r.cancelDetailsDirSizeComputation)

	r.showDetailsSidebar()
	r.loadDetailsTarget(filepath.Join(dir, "app-data"))
	r.computeDetailsDirSize()
	<-started
	if !r.detailsDirSizeInProgress {
		t.Fatal("setup: detailsDirSizeInProgress should be true right after computeDetailsDirSize starts")
	}

	r.hideDetailsSidebar()

	if r.detailsDirSizeInProgress {
		t.Error("hiding the sidebar should cancel an in-progress directory-size computation")
	}
}

// TestDetailsDirSizeClickZoneTriggersComputation pins the mouse path:
// clicking the hint row starts the same computation Ctrl+U does.
func TestDetailsDirSizeClickZoneTriggersComputation(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	started := isolateDirSize(t)
	t.Cleanup(r.cancelDetailsDirSizeComputation)

	r.showDetailsSidebar()
	r.loadDetailsTarget(filepath.Join(dir, "app-data"))
	if r.detailsDirSizeRowStart < 0 {
		t.Fatal("setup: expected a directory-size row")
	}

	_, rectY, _, _ := r.detailsSidebar.GetInnerRect()
	x, _, _, _ := r.detailsSidebar.GetRect()
	action, _ := r.captureDetailsSidebarMouse(tview.MouseLeftClick, tcell.NewEventMouse(x, rectY+r.detailsDirSizeRowStart, tcell.Button1, 0))
	if action != tview.MouseConsumed {
		t.Fatalf("action = %v, want MouseConsumed", action)
	}
	<-started

	if !r.detailsDirSizeInProgress {
		t.Error("clicking the directory-size hint should start a computation")
	}
}

// TestComputeDirSizeShortcutNoOpsWhenDetailsNotVisible pins Ctrl+U's own
// precondition: nothing to compute a size for if Details isn't even
// showing.
func TestComputeDirSizeShortcutNoOpsWhenDetailsNotVisible(t *testing.T) {
	dir := fixtureDir(t)
	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	original := dirSize
	called := false
	dirSize = func(dir string) (int64, string, bool) { called = true; return 0, dir, true }
	t.Cleanup(func() { dirSize = original })

	r.ComputeDirSizeShortcut()

	if called {
		t.Error("ComputeDirSizeShortcut ran dirSize while Details isn't visible")
	}
}

// TestDetailsSidebarShowsPDFPageCount pins the part of PDF support that
// doesn't depend on pdftoppm being installed at all (see
// TestDetailsSidebarShowsPDFPreviewWhenPdftoppmAvailable for the part
// that does): PDFPageCount alone, via ledongthuc/pdf, always works.
func TestDetailsSidebarShowsPDFPageCount(t *testing.T) {
	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.loadDetailsTarget(path)
	r.detailsSidebarVisible = true

	if r.detailsPDFPageCount != 1 {
		t.Fatalf("detailsPDFPageCount = %d, want 1 (the fixture is a single-page PDF)", r.detailsPDFPageCount)
	}
	text := r.detailsSidebar.GetText(true)
	for _, want := range []string{"Type:", "PDF", "Pages:", "1"} {
		if !strings.Contains(text, want) {
			t.Errorf("details sidebar text should contain %q, got:\n%s", want, text)
		}
	}
}

// TestDetailsSidebarShowsPDFPreviewWhenPdftoppmAvailable pins the actual
// rasterized-page-1 preview path — skipped where pdftoppm isn't
// installed, mirroring TestShowBuiltinLookRendersPDFPage's own same
// concern for Look.
func TestDetailsSidebarShowsPDFPreviewWhenPdftoppmAvailable(t *testing.T) {
	requireCommand(t, "pdftoppm")

	dir := t.TempDir()
	path := writePDFFixture(t, dir, "doc.pdf", "Hello PDF")

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 40)
	r.loadDetailsTarget(path)
	r.detailsSidebarVisible = true

	if r.detailsImage == nil {
		t.Fatal("detailsImage should be set once pdftoppm actually rasterized page 1")
	}
	text := r.detailsSidebar.GetText(true)
	if !strings.Contains(text, "▀") {
		t.Errorf("details sidebar text should contain a half-block preview, got:\n%s", text)
	}
	if !strings.Contains(text, detailsFullscreenHint) {
		t.Errorf("details sidebar text should show the fullscreen hint, got:\n%s", text)
	}
	if r.detailsPreviewRowStart < 0 || r.detailsPreviewRowEnd < r.detailsPreviewRowStart {
		t.Errorf("detailsPreviewRowStart/End = %d/%d, want a valid non-negative range", r.detailsPreviewRowStart, r.detailsPreviewRowEnd)
	}
	if r.detailsMetaRowStart >= 0 {
		t.Error("a PDF should not get the image-only EXIF-style metadata hint")
	}
}

// TestClickingPreviewOpensLook pins the user's own explicit request: a
// click on the preview section (image or rasterized PDF page alike)
// opens the same fullscreen view Ctrl+L/the Look button already does —
// tested here against an image target, which needs no external tool
// dependency; the PDF case reuses the exact same click-zone/dispatch
// code (see captureDetailsSidebarMouse), not a separate path.
func TestClickingPreviewOpensLook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 6, 4))); err != nil {
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
	r.panel.focusRow(1) // off ".." onto photo.png, the only real entry
	r.showDetailsSidebar()

	if r.detailsPreviewRowStart < 0 {
		t.Fatal("setup: detailsPreviewRowStart should be set for an image target")
	}
	x, y, _, _ := r.detailsSidebar.GetInnerRect()

	action, _ := r.captureDetailsSidebarMouse(tview.MouseLeftClick, tcell.NewEventMouse(x, y+r.detailsPreviewRowStart, tcell.Button1, 0))
	if action != tview.MouseConsumed {
		t.Fatalf("action = %v, want MouseConsumed", action)
	}
	if r.activePage != viewerPage {
		t.Errorf("activePage = %q, want %q — clicking the preview should open Look", r.activePage, viewerPage)
	}
}

// TestDetailsImagePreviewHasNoExtraVerticalGap is a regression guard for
// a real, observed bug: the preview box was always reserved at exactly
// a third of the sidebar's own height, so a scaled image shorter than
// that (letterboxed by renderImageHalfBlocks' own centering — see
// detailsImageBoxSize's own doc comment) left a strangely large blank
// gap before whatever came next. A wide, short image scaled into a much
// taller sidebar — width-constrained, so its real scaled height ends up
// well under the reserved maximum — is exactly the case that exposed
// it.
func TestDetailsImagePreviewHasNoExtraVerticalGap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wide.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 400, 10))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := NewRoot(tview.NewApplication(), dir)
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.SetRect(0, 0, 100, 90) // tall: a third (30 rows) is far more than a 400x10 image scaled into ~90 columns will ever need
	r.loadDetailsTarget(path)
	r.detailsSidebarVisible = true

	text := r.detailsSidebar.GetText(true)
	lines := strings.Split(text, "\n")
	formatIdx := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "Format:") {
			formatIdx = i
			break
		}
	}
	if formatIdx < 2 {
		t.Fatalf("couldn't find a Format: line with at least two lines before it in:\n%s", text)
	}
	if lines[formatIdx-1] != "" {
		t.Errorf("the line right before Format: should be the single blank paragraph separator, got %q", lines[formatIdx-1])
	}
	if strings.TrimSpace(lines[formatIdx-2]) == "" {
		t.Errorf("the line two before Format: should already be real preview content (no second, extra blank line) — full text:\n%s", text)
	}
}
