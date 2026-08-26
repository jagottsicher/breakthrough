package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const helpPage = "help"

// helpMinWidth/Height are helpSize's own floor — a small terminal
// still gets a usable Help window, just not the generous 90%/80%
// share of the screen a bigger one does (see helpSize).
const helpMinWidth, helpMinHeight = 60, 20

// helpText is the Help overlay's own content — every real keyboard
// shortcut in this app, grouped the way a user actually runs into
// them (global, then the file panel, then each dialog that layers its
// own extra keys on top) rather than alphabetically or by internal
// package. Verified directly against this app's own source, section
// by section, rather than recalled from memory — a stale hint here
// would be worse than no hint at all. "[::b]"/"[::-]" bold a heading;
// tview.Escape isn't needed anywhere here since nothing below happens
// to contain a literal "[".
var helpText = strings.TrimLeft(`
[::b]Global — work anywhere, even inside another dialog[::-]

  F1              This help
  Ctrl+Q          Quit (asks first)
  Ctrl+C          Cancel/back out of whatever's open — never quits

[::b]File panel[::-]

  Ctrl+E          Edit the selected file
  Ctrl+L          Look at the selected file (read-only)
  Ctrl+R          Rename the selected file
  Ctrl+G          Toggle hidden files
  Ctrl+F          Find
  Ctrl+O          Options
  Enter           Open the selected directory
  Space           Select/deselect the selected file
  Right-click     Context menu (Properties, Edit, Look, Tail -f,
                  Rename, Select all/Deselect all/Select +/Select -,
                  Copy, Cut, Paste, chown, chmod, and three toggles:
                  hidden files, size format, modified-time format)

  Click a path segment in the header to jump straight there; click
  the path itself to type a new one (Tab completes it, Enter goes);
  click a column heading to sort by it; type into the filter box to
  narrow the list live (its own button switches between a plain glob
  and a regular expression).

[::b]Properties dialog[::-]

  Tab / Shift+Tab   Move between fields
  Enter / Space     Activate or commit the focused one
  Escape            Cancel and close
  h                 Compute file hashes (MD5/SHA-1/SHA-256)

  With a permission bit focused: r / w / x sets that bit directly,
  Space toggles it, Delete or - clears it.

[::b]Search dialog (Ctrl+F)[::-]

  Tab / Shift+Tab   Move between fields
  Enter             Commit a field and stay — except in Filename,
                    where Enter runs the search immediately, same
                    as clicking Find
  Escape            Cancel and close; from the results page, back
                    to the form instead, with everything exactly as
                    it was left

  Start-at's own Tab always completes the path instead — it never
  moves on to the next field this way; click elsewhere, or Shift+Tab,
  to actually leave it.

[::b]Other dialogs (Options, Rename, pickers, Tree)[::-]

  Tab / Shift+Tab   Move between fields or buttons
  Enter / Space     Activate the focused one
  Escape            Cancel and close

[::b]Bash line[::-]

  Up / Down       Recall the previous/next command from history
  Enter           Run the command
`, "\n")

// newHelpView builds the Help overlay: a single, scrollable, read-only
// TextView (see helpText) — no interactive fields, so none of the
// span-tracking machinery Properties/Search need for their own
// clickable text. Escape/Enter/Tab/Backtab all dismiss it
// (TextView.SetDoneFunc fires for all four — see errorView's own doc
// comment for the same shape), as does a click outside (Root's own
// captureOutsideClick, unchanged) or Ctrl+C.
func (r *Root) newHelpView() *tview.TextView {
	v := tview.NewTextView()
	v.SetDynamicColors(true)
	v.SetWrap(true)
	v.SetBorderPadding(0, 0, 1, 1)
	v.SetText(helpText)
	v.SetDoneFunc(func(tcell.Key) { r.hideOverlay() })
	return v
}

// openHelp shows the Help overlay, centered and sized generously (see
// helpSize) — pushed on top of whatever's already open (pushOverlay),
// not replacing it the way every other overlay in this app does (see
// showOverlay) — so pulling up Help in the middle of something else
// (Properties half-edited, Search's own fields half-filled) doesn't
// lose any of it. Closing Help (see newHelpView's own SetDoneFunc,
// or captureOutsideClick) returns to exactly that, still intact —
// the same "floats on top rather than replacing" behavior
// openOwnerGroupPicker already has over Properties.
func (r *Root) openHelp() {
	width, height := r.helpSize()
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.helpView.SetRect(x, y, width, height)
	r.pushOverlay(helpPage, r.helpView, nil)
}

// helpSize sizes Help generously against the whole terminal, not
// clamped to one panel the way most overlays are (see clampToPanel):
// it's a read-only reference, not a form tied to the current panel's
// own context, and its content is long enough that more visible room
// genuinely means less scrolling.
func (r *Root) helpSize() (width, height int) {
	_, _, screenWidth, screenHeight := r.GetRect()
	width = screenWidth * 9 / 10
	height = screenHeight * 8 / 10
	if width < helpMinWidth {
		width = helpMinWidth
	}
	if height < helpMinHeight {
		height = helpMinHeight
	}
	return width, height
}

// HelpShortcut is F1's own action — see cmd/breakthrough. Unlike
// Ctrl+E/Ctrl+R/Ctrl+G/Ctrl+O/Ctrl+F (see acceptsGlobalShortcut), F1
// works from literally anywhere, the same as Ctrl+Q/Ctrl+C: getting
// help in the middle of something else is exactly the point, not a
// case to guard against. Re-pressing F1 while Help is already the
// front overlay is a no-op rather than pushing a second copy of it on
// top of itself.
func (r *Root) HelpShortcut() {
	if r.activePage == helpPage {
		return
	}
	r.openHelp()
}
