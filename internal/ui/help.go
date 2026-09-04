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
  F3              Toggle mouse reporting on/off (see the status bar's
                  own "Mouse on/off") — off gives your terminal's own
                  native text selection/copy back, e.g. to grab a
                  filename; most terminals also support their own
                  override gesture (often Shift-drag) without needing
                  this, but not everyone knows it
  Ctrl+Q          Quit (asks first)
  Ctrl+C          Cancel/back out of whatever's open — never quits

[::b]File panel[::-]

  Ctrl+E          Edit the selected file
  Ctrl+L          Look at the selected file (read-only)
  F2              Rename the selected file
  Ctrl+G          Toggle hidden files — the button bar's own label
                  flips between Hide/Unhide to match
  Ctrl+F          Find
  Ctrl+O          Options
  Ctrl+P          Properties
  Ctrl+D          Toggle the Details sidebar — a read-only, live-
                  updating panel of file info (stat fields; for an
                  image or PDF, a preview with its own click zone/
                  Ctrl+L for fullscreen; hashes) for whichever entry
                  is currently selected
  Ctrl+K          Compute hashes (SHA-256/SHA-1/MD5/SHA-512/BLAKE2b-512)
                  for Properties if that's open, otherwise the Details
                  sidebar; shown in both at once if both are open on
                  the same file, however it was triggered
  Ctrl+N          Load an image's metadata in the Details sidebar
                  (EXIF etc. — not implemented yet)
  Ctrl+S          Sed Replace on the selected file(s)
  Ctrl+B          Go to Trash — browse it directly
  Ctrl+T / Delete Move the selection to Trash (reversible); already
                  inside the trash itself, does a Remove instead —
                  nowhere left to move an already-trashed item to
  Ctrl+R          Remove — permanently delete the selection (asks
                  first); Ctrl+Delete does the same, best-effort
  Enter           Open the selected directory, or try Look on a file —
                  double-clicking a name does the same either way
  Space           Select/deselect the selected file
  Click, pause,   Rename — the pause is deliberately generous (about a
  click again     second), so an unhurried second click still counts;
                  slower than that is just a fresh first click again
  Right-click     Context menu (Look, Rename, Edit, tail -f, Properties,
                  Select all/Deselect all/Select +/Select -, Copy, Cut,
                  Paste, chown, chmod, sed, Move to Trash, Remove, Go to
                  Trash, Restore from Trash, Empty Trash, Ping (test),
                  and three toggles: hidden files, size format,
                  modified-time format)

  Click a path segment in the header to jump straight there; click
  the path itself to type a new one (Tab completes it, Enter goes);
  click a column heading to sort by it; type into the filter box to
  narrow the list live (its own button switches between a plain glob
  and a regular expression).

  While plainly browsing (not editing the path, not in the filter box)
  and the Details sidebar is shown, Tab moves keyboard focus into it —
  its own scrolling (arrow keys, PageUp/PageDown, Home/End, mouse
  wheel) then works once its content is longer than it has room for —
  and Tab again moves focus back to the panel. A click anywhere in the
  sidebar that isn't one of its own click zones also focuses it, the
  same way.

[::b]Properties dialog[::-]

  Tab / Shift+Tab   Move between fields
  Enter / Space     Activate or commit the focused one
  Escape            Cancel and close

  With a permission bit focused: r / w / x sets that bit directly,
  Space toggles it, Delete or - clears it.

  The octal value field opens pre-filled with the current mode: typing
  a digit (0-7 only) overwrites the one under the cursor and moves on,
  rather than replacing the whole thing — type just the digits you
  mean to change, leave the rest as they were.

  Ctrl+K (see the file panel's own entry above) computes hashes here
  too — click the hash hint works as well.

[::b]Look (Ctrl+L, or Enter/double-click a file)[::-]

  Escape            Close
  PageUp / PageDown  On a PDF: turn a page instead of scrolling (a
                    rendered page already fits the screen, so there's
                    nothing to scroll within one anyway)
  g / t             On a PDF: force graphic (rasterized) or text
                    rendering for the current page — a text-heavy page
                    can turn to illegible mush as a raster image at a
                    realistic terminal size, so this is a manual
                    override, not just whichever tier auto-detection
                    picked

[::b]Chmod dialog (context menu's "chmod")[::-]

  Tab / Shift+Tab   Move between fields/checkboxes
  Enter / Space     Toggle the focused checkbox, or open the focused
                    octal field for typing
  Escape            Cancel and close

  Permission bits and the octal value field work exactly like
  Properties' own above, for both the Directory and Files rows.

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

  The results page's own header also carries a real, ordinary path
  breadcrumb next to the status line — click a button or segment (or
  edit the path directly) to keep browsing normally, the same as
  clicking a result already does, without needing to pick one first.

[::b]Other dialogs (Options, Rename, pickers, Tree)[::-]

  Tab / Shift+Tab   Move between fields or buttons
  Enter / Space     Activate the focused one
  Escape            Cancel and close

[::b]Tool windows (context menu's "Ping (test)", more to come)[::-]

  A small floating window running one command's live output — unlike
  every dialog above, not modal: the panel underneath (and any other
  open tool window) stays fully usable while it floats on top.

  Escape                  Close it, stopping the process if it's still
                           running
  Drag the title bar       Move the window; Alt+arrow keys do the same
  Click the title bar's ✕  Close it, same as Escape
  Drag the bottom-right ◢  Resize it — the title, its own close button,
                           and one content row plus the handle's own
                           row are the smallest it'll ever get
  Arrow keys/PageUp/       Scroll the output once it's longer than the
  PageDown/mouse wheel     window currently shows

[::b]Bash line[::-]

  Click to expand         Grows upward toward mid-screen while focused
                           (a legend of these same keys shows above it),
                           collapses back to one row on Escape/click away
  Enter                    Run the buffer — a real terminal, with your
                           shell's own ~/.bashrc, aliases and functions
                           sourced first, for every command, the same as
                           Midnight Commander's own command line. Once
                           it's done, press Escape to return (its own
                           output stays on screen to read until then)
  Ctrl+J / Alt+Enter        Insert a newline (compose a multi-line script)
                           — Ctrl+J always works; Alt+Enter is intercepted
                           by some terminal emulators for their own use
  Up / Down or             Recall the previous/next command from history
  Ctrl+P / Ctrl+N          — Up/Down only at the first/last line of a
                           multi-line buffer, otherwise they move the
                           cursor as usual; Ctrl+P/Ctrl+N always recall
                           regardless of cursor position
  Tab                      Complete the filename/directory at the cursor
                           against the panel's own current directory —
                           several equally-possible matches show a
                           scrollable pick list instead of doing nothing
`, "\n")

// newHelpView builds the Help overlay's own scrollable content (see
// helpText) — no interactive fields, so none of the span-tracking
// machinery Properties/Search need for their own clickable text.
// Escape/Enter/Tab/Backtab all dismiss it (TextView.SetDoneFunc fires
// for all four — see errorView's own doc comment for the same shape),
// as does a click outside (Root's own captureOutsideClick, unchanged),
// Ctrl+C, or the title bar's own close button (see
// captureHelpTitleBarMouse).
func (r *Root) newHelpView() *tview.TextView {
	v := tview.NewTextView()
	v.SetDynamicColors(true)
	v.SetWrap(true)
	v.SetBorderPadding(0, 0, 1, 1)
	v.SetText(helpText)
	v.SetDoneFunc(func(tcell.Key) { r.hideOverlay() })
	return v
}

// newHelpTitleBar builds Help's own one-row title bar — the same shape
// toolWindow's/Details' own title bars have (see toolwindow.go/
// detailssidebar.go), plus a close button (see captureHelpTitleBarMouse),
// per the user's own explicit request that Help get both. Its own text
// (the "Help" label plus the close glyph, right-aligned) is set by
// renderHelpTitleBar, not here, since it depends on the bar's own
// width, not known yet at construction time.
func (r *Root) newHelpTitleBar() *tview.TextView {
	bar := tview.NewTextView()
	bar.SetWrap(false)
	bar.SetBackgroundColor(r.theme.EditableBackground)
	bar.SetMouseCapture(r.captureHelpTitleBarMouse)
	return bar
}

// renderHelpTitleBar sets helpTitleBar's own text to " Help ", padded
// out to width columns, with the close glyph placed
// toolWindowCloseButtonCol's own one-column-in-from-the-edge spacing —
// the same convention toolWindow's own close button uses (see
// toolwindow.go), reused here rather than duplicated with a different
// number, per the user's own explicit request that this button behave
// consistently everywhere it appears. Called from openHelp, since width
// can change (a live terminal resize) between one open and the next —
// there's no live-resize-while-open support for Help yet (see
// handleBeforeDraw's own doc comment), so this only needs to run at
// open time, not on every Draw the way toolWindow's own Draw override
// does for its own close button.
func (r *Root) renderHelpTitleBar(width int) {
	const label = " Help "
	closeCol := toolWindowCloseButtonCol(0, width)
	padding := closeCol - len(label)
	if padding < 0 {
		padding = 0
	}
	r.helpTitleBar.SetText(label + strings.Repeat(" ", padding) + string(toolWindowCloseGlyph) + " ")
}

// captureHelpTitleBarMouse closes Help when the click lands exactly on
// its own close glyph (see renderHelpTitleBar/toolWindowCloseButtonCol)
// — every other click on the title bar is otherwise inert, the same as
// every other modal overlay's own title bar in this app (Properties',
// the context menu's, ...), none of which are draggable the way a
// non-modal toolWindow's is.
func (r *Root) captureHelpTitleBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick {
		return action, event
	}
	x, y := event.Position()
	rectX, rectY, width, _ := r.helpTitleBar.GetRect()
	if y != rectY || x != rectX+toolWindowCloseButtonCol(0, width) {
		return action, event
	}
	r.hideOverlay()
	return tview.MouseConsumed, nil
}

// openHelp shows the Help overlay, centered and sized generously (see
// helpSize) — pushed on top of whatever's already open (pushOverlay),
// not replacing it the way every other overlay in this app does (see
// showOverlay) — so pulling up Help in the middle of something else
// (Properties half-edited, Search's own fields half-filled) doesn't
// lose any of it. Closing Help (see newHelpView's own SetDoneFunc,
// captureHelpTitleBarMouse, or captureOutsideClick) returns to exactly
// that, still intact — the same "floats on top rather than replacing"
// behavior openOwnerGroupPicker already has over Properties.
//
// Sizes/positions helpLayout (the title bar + helpView, stacked — see
// NewRoot), not helpView directly, so the title bar always occupies
// the same rect helpView itself used to.
func (r *Root) openHelp() {
	width, height := r.helpSize()
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.renderHelpTitleBar(width)
	r.helpLayout.SetRect(x, y, width, height)
	r.pushOverlay(helpPage, r.helpLayout, nil)
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
// Ctrl+E/Ctrl+G/Ctrl+O/Ctrl+F/Ctrl+P/Ctrl+R (see acceptsGlobalShortcut)
// or F2, F1 works from literally anywhere, the same as Ctrl+Q/Ctrl+C:
// getting help in the middle of something else is exactly the point,
// not a case to guard against. Re-pressing F1 while Help is already the
// front overlay is a no-op rather than pushing a second copy of it on
// top of itself.
func (r *Root) HelpShortcut() {
	if r.activePage == helpPage {
		return
	}
	r.openHelp()
}
