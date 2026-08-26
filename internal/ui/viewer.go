package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/viewer"
)

const viewerPage = "viewer"

// viewerMinWidth/Height are viewerSize's own floor — a small terminal
// still gets a usable Look window, just not the generous 90%/80% share
// of the screen a bigger one does (see viewerSize) — the same numbers
// helpSize uses, for the same reason (see its own doc comment).
const viewerMinWidth, viewerMinHeight = 60, 20

// newViewerView builds the Look overlay's built-in pager: a single,
// scrollable, read-only TextView — deliberately the same shape as
// newHelpView (see its own doc comment for why no span-tracking is
// needed here either). Escape/Enter/Tab/Backtab all dismiss it
// (TextView.SetDoneFunc fires for all four), as does a click outside or
// Ctrl+C.
//
// SetDynamicColors is on (needed for the truncation footer's own muted
// color — see showBuiltinLook), which means the file's own raw content
// must be escaped before it's ever set as this view's text: an ordinary
// source file or log line containing a literal "[" (Go's "[]byte", a log
// line's "[ERROR]", ...) would otherwise be misparsed as one of tview's
// own style tags instead of shown as-is (see showBuiltinLook's own call
// to tview.Escape, and help.go's doc comment on the same mechanism).
func (r *Root) newViewerView() *tview.TextView {
	v := tview.NewTextView()
	v.SetDynamicColors(true)
	v.SetWrap(true)
	v.SetBorderPadding(0, 0, 1, 1)
	v.SetDoneFunc(func(tcell.Key) { r.hideOverlay() })
	return v
}

// viewerSize sizes Look generously against the whole terminal, the same
// as Help (see helpSize's own doc comment for the rationale: a read-only
// view whose content is often long enough that more visible room
// genuinely means less scrolling, not tied to the panel's own narrower
// context the way a form like Properties is).
func (r *Root) viewerSize() (width, height int) {
	_, _, screenWidth, screenHeight := r.GetRect()
	width = screenWidth * 9 / 10
	height = screenHeight * 8 / 10
	if width < viewerMinWidth {
		width = viewerMinWidth
	}
	if height < viewerMinHeight {
		height = viewerMinHeight
	}
	return width, height
}

// openLook is the Look button/context-menu entry/Ctrl+L's actual action
// — read-only, unlike Edit (see runEditor): it never lets the file be
// modified, and (in its default "builtin" path — see
// config.Settings.Pager) works without $VISUAL/$EDITOR being set to
// anything at all. A no-op on the ".." row or an empty panel, same as
// editCurrentEntry.
func (r *Root) openLook() {
	_, path, ok := r.panel.CurrentRowPath()
	if !ok {
		return
	}

	if r.settings.Pager == "external" {
		r.runExternalPager(path)
		return
	}
	r.showBuiltinLook(path)
}

// lookCurrentEntry is openLook under the name the context menu's own
// "Look" item and the status bar's ^L button call it by — kept as a
// distinct, addressable method for the same reason editCurrentEntry is:
// a right-click already moves the table's cursor to the clicked row
// before the menu opens, so openLook's own CurrentRowPath read targets
// the same entry either way.
func (r *Root) lookCurrentEntry() {
	r.openLook()
}

// showBuiltinLook is openLook's default path: reads path through
// viewer.Load (see internal/viewer — a bounded, in-process read, no
// external tool involved) and shows the result in r.viewerView. A file
// viewer.Load can't even open (permission denied, path vanished between
// the row being drawn and Look being triggered) or that it reads but
// doesn't recognize as text (see viewer.Sniff) is reported via showError
// instead of ever reaching the overlay — the same "decline clearly
// rather than render garbage" approach the rest of this app takes for a
// filesystem operation that can't proceed.
func (r *Root) showBuiltinLook(path string) {
	result, err := viewer.Load(path, viewer.DefaultPreviewLimit)
	if err != nil {
		r.showError(fmt.Errorf("look %s: %w", path, err))
		return
	}
	if result.Kind != viewer.KindText {
		r.showError(fmt.Errorf("%s: no built-in viewer yet for this file type — try configuring pager = external (see README), or check back in a later release", filepath.Base(path)))
		return
	}

	text := tview.Escape(result.Content) // see newViewerView's own doc comment on why
	if result.Truncated {
		text += fmt.Sprintf("\n\n[%s]— showing only the first part of this file (larger than Look's own preview limit) — use Tail -f to follow it live instead[-]", colorTag(r.theme.PlaceholderText))
	}
	r.viewerView.SetText(text)
	r.viewerView.ScrollToBeginning()

	width, height := r.viewerSize()
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.viewerView.SetRect(x, y, width, height)

	r.showOverlay(viewerPage, r.viewerView)
}

// LookShortcut is Ctrl+L's global action — see cmd/breakthrough and
// acceptsGlobalShortcut for why it checks its own precondition first,
// the same as Ctrl+E/Ctrl+R/Ctrl+G/Ctrl+O/Ctrl+F.
func (r *Root) LookShortcut() {
	if r.acceptsGlobalShortcut() {
		r.openLook()
	}
}

// batBinary returns "bat" or "batcat", whichever is actually on $PATH —
// "" if neither is. bat's own binary is named "batcat" on Debian/Ubuntu
// specifically, renamed there to avoid a clash with an unrelated,
// pre-existing "bat" package — the same class of platform-specific
// naming quirk editorCommand's own selectedEditor already documents for
// select-editor(1).
func batBinary() string {
	for _, name := range []string{"bat", "batcat"} {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return ""
}

// externalPagerCommand returns the shell command Look runs when
// config.Settings.Pager is "external" (see runExternalPager) instead of
// the built-in overlay: bat/batcat first (see batBinary — its own
// internal pager, plus syntax highlighting), then $PAGER, then "less",
// falling back to "more" only if even less can't be found — less and
// more are both POSIX-guaranteed to exist on every platform this app
// targets, so this always resolves to something runnable, the same
// guarantee editorCommand's own final "vi" fallback gives Edit.
func externalPagerCommand() string {
	if bin := batBinary(); bin != "" {
		return bin + " --paging=always"
	}
	if p := os.Getenv("PAGER"); p != "" {
		return p
	}
	if _, err := exec.LookPath("less"); err == nil {
		return "less"
	}
	return "more"
}

// runExternalPager suspends the TUI (see runShellCommand's own doc
// comment for why) and runs externalPagerCommand on path — the same
// "run through the shell via a trailing '\"$@\"'" approach runEditor
// uses for $VISUAL/$EDITOR, and for the same reason: externalPagerCommand
// can legitimately be more than one word (e.g. a $PAGER of "less -R"),
// and only the shell can be trusted to split that as intended while
// still passing path through exactly as given.
func (r *Root) runExternalPager(path string) {
	var runErr error
	r.app.Suspend(func() {
		script := externalPagerCommand() + ` "$@"`
		cmd := exec.Command(userShell(), "-c", script, "sh", path)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
	})
	if runErr != nil {
		r.showError(fmt.Errorf("look %s: %w", path, runErr))
	}
}

// tailCurrentEntry is the context menu's "Tail -f" item — see
// runTailFollow.
func (r *Root) tailCurrentEntry() {
	_, path, ok := r.panel.CurrentRowPath()
	if !ok {
		return
	}
	r.runTailFollow(path)
}

// runTailFollow suspends the TUI and runs "tail -f" on path, live-
// following whatever's appended to it from here on — the same
// suspend-and-hand-over-the-real-terminal approach runShellCommand uses,
// so tail's own output streams straight to the screen exactly like it
// would in a real shell. Ctrl+C (SIGINT) is the expected, ordinary way
// to stop watching and return to breakthrough — that reports back as a
// non-zero exit from a killing signal, not a real failure worth
// surfacing as an error.
//
// Deliberately a separate action from Look/openLook (which reads a
// bounded snapshot up front — see viewer.ReadPreview) rather than
// something Look itself does automatically: only a log genuinely being
// appended to right now benefits from following it live, and suspending
// the whole TUI to babysit "tail -f" isn't something every Look should
// default to.
func (r *Root) runTailFollow(path string) {
	var runErr error
	r.app.Suspend(func() {
		cmd := exec.Command("tail", "-f", path)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
	})
	if runErr == nil {
		return
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) && exitErr.ExitCode() == -1 {
		return // stopped by a signal (Ctrl+C) — the expected way out, not a failure
	}
	r.showError(fmt.Errorf("tail -f %s: %w", path, runErr))
}
