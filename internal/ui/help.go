package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const helpPage = "help"

// helpMinWidth/Height are helpSize's own floor — a small terminal
// still gets a usable Help window, just not the width helpContentWidth
// would otherwise give it, or the generous 80% height share a bigger
// terminal gets (see helpSize).
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
  F4              Open the tab switcher — see "Tabs" below
  Ctrl+Q          Quit (asks first)
  Ctrl+C          Cancel/back out of whatever's open — never quits

[::b]File panel[::-]

  Ctrl+E          Edit the selected file
  Ctrl+L          Look at the selected file (read-only)
  F2              Rename the selected file
  Ctrl+G          Toggle hidden files — the button bar's own label
                  flips between Hide/Unhide to match
  Ctrl+F          Find
  Ctrl+O          Options — see "Options screen" below
  Ctrl+P          Properties
  Ctrl+D          Toggle the Details sidebar — a read-only, live-
                  updating panel of file info (stat fields; for an
                  image or PDF, a preview with its own click zone/
                  Ctrl+L for fullscreen; hashes, or for a directory, its
                  total size) for whichever entry is currently selected.
                  The "<" button right after the filter box expands it
                  the same way; once open, the ">" button in its own
                  top-right corner collapses it again
  Ctrl+K          Compute hashes (SHA-256/SHA-1/MD5/SHA-512/BLAKE2b-512)
                  for Properties if that's open, otherwise the Details
                  sidebar; shown in both at once if both are open on
                  the same file, however it was triggered
  Ctrl+N          Load an image's metadata in the Details sidebar
                  (EXIF etc. — not implemented yet)
  Ctrl+U          Compute a directory's total size (du -hs) in the
                  Details sidebar, for whichever directory is currently
                  selected — on demand, since it can take a real,
                  visible amount of time on a large tree
  Ctrl+S          Sed Replace on the selected file(s)
  Ctrl+B          Go to Trash — browse it directly
  Delete          Move the selection to Trash (reversible); already
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
                  Paste, chown, chmod, sed, Mass rename*, Move to Trash,
                  Remove, Go to Trash, Restore from Trash, Empty Trash,
                  New tab, Close tab, Switch tab..., Ping (test), grep*,
                  zgrep*, du*, df*, and three toggles: hidden files, size
                  format, modified-time format — *planned, not built yet)

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

[::b]Options screen (Ctrl+O)[::-]

  Categories down the left, that category's settings on the right.

  Up / Down         Move between categories, or between settings
  Tab / Shift+Tab   Move between the categories, the settings and the
                    buttons underneath them
  Enter             Change the selected setting — toggles a yes/no
                    directly, opens a list for a choice, or a field for
                    a number (Enter commits it, Escape discards)
  ? or F1           Explain the selected setting in a small window
  Escape            Close the Options screen

  There is no save button: every change takes effect and is written to
  your config file the moment you make it.

  Each setting shows where its value comes from — "default",
  "system-wide" (set in /etc/breakthrough/config), or "changed by you".
  "Reset category" and "Reset all" remove your own overrides rather than
  writing defaults over them, so a value falls back to the system-wide
  setting where there is one.

  "Edit config file" opens your config in your editor, creating it first
  with every setting listed and commented out if you don't have one yet.
  "New color scheme" copies the current scheme and opens that for
  editing; either way the change is picked up when the editor closes.

[::b]Tabs[::-]

  Several directories open at once in the same window, one visible at a
  time. Each tab keeps its own history, filter, sort order, selection
  and cursor position — switching away and back leaves everything
  exactly as you left it.

  Ctrl+1 ... Ctrl+0  Jump straight to that tab (...+0 is the tenth) —
  Alt+1 ... Alt+0    either modifier, whichever your terminal reports
  Ctrl+Tab           Open the switcher on the next tab; press again to
  Ctrl+Shift+Tab     keep moving, Enter to go there, Escape to stay put
  F4 / Ctrl+T        Open the switcher on the current tab; press again
                     to walk to the next one

  In the switcher: Up/Down picks a tab, Enter or Space goes to it,
  Escape stays put, and the last row opens a new one. Right steps onto
  that row's own "✕" button and Enter or Space there closes the tab
  (Delete does the same from anywhere in the row); the cursor then moves
  to the row above. The first tab has no "✕" — one tab always stays
  open.

  The numbered strip after the filter box shows the open tabs; the
  highlighted number is the one you're on. Click a number to switch,
  click "+" for a new tab, click anywhere else in the strip to open the
  switcher — which lists every tab's full directory, since the numbers
  themselves deliberately don't say what any tab holds. The context
  menu's own "Tabs" section reaches New tab, Close tab and the switcher
  too. With only one tab open the strip is just a "+" — no numbers to
  show yet, but still the one place to start.

  Ctrl+1...Ctrl+0, Alt+1...Alt+0, and Ctrl+Tab/Ctrl+Shift+Tab each
  depend on the terminal actually reporting that key combination —
  most modern terminals report at least one of Ctrl or Alt, some older
  ones report neither, in which case nothing happens. F4/Ctrl+T, the
  button bar's own "F4 Tabs", the strip, and the context menu all work
  regardless of what your terminal can report.

  The open tabs are saved when you quit and reopened next time. Starting
  breakthrough with an explicit directory ("breakthrough /some/path")
  opens just that instead; setting "restore_tabs = false" in the config
  turns the whole thing off.

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

// aboutText builds the Help overlay's own About section — version,
// license, and a disclaimer, per the user's own explicit request. The
// tagline and copyright line are the exact same wording README.md/
// NOTICE already use, not a separate description invented here.
//
// A method, not a plain const the way helpText itself is: appVersion/
// appCommit/appBuildDate/appBuiltBy only become known once main calls
// SetVersionInfo, which happens after helpText's own package-level
// initializer has already run — see fullHelpText's own doc comment for
// where this actually gets appended.
func (r *Root) aboutText() string {
	return fmt.Sprintf(strings.TrimLeft(`
[::b]About[::-]

  breakthrough — a mouse-and-menu-driven TUI file manager for your
  POSIX-compliant terminal, built around a real embedded bash shell.

  Version    %s (commit %s, built %s by %s)
  License    Apache License, Version 2.0
  Copyright  2026 jagottsicher
  Homepage   github.com/jagottsicher/breakthrough

  Provided "AS IS", WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
  either express or implied — see the LICENSE file for the full text.
`, "\n"), r.appVersion, r.appCommit, r.appBuildDate, r.appBuiltBy)
}

// fullHelpText is what helpView actually shows (see openHelp) —
// helpText plus aboutText, joined with a blank line the same way every
// other section break in helpText already has one. Computed fresh on
// every open rather than cached: version info never changes once
// SetVersionInfo has been called, so this costs nothing worth avoiding,
// and it sidesteps ever wondering whether a cache is stale.
func (r *Root) fullHelpText() string {
	return helpText + "\n" + r.aboutText()
}

// newHelpView builds the Help overlay's own scrollable content (see
// helpText/aboutText) — no interactive fields, so none of the
// span-tracking machinery Properties/Search need for their own
// clickable text. Escape/Enter/Tab/Backtab all dismiss it
// (TextView.SetDoneFunc fires for all four — see errorView's own doc
// comment for the same shape), as does a click outside (Root's own
// captureOutsideClick, unchanged), Ctrl+C, or the title bar's own close
// button (see captureHelpTitleBarMouse).
//
// Its own text is set by openHelp instead of here (see fullHelpText's
// own doc comment): appVersion/appCommit/appBuildDate/appBuiltBy aren't
// known yet this early — SetVersionInfo only runs once NewRoot itself
// has already returned.
func (r *Root) newHelpView() *tview.TextView {
	v := tview.NewTextView()
	v.SetDynamicColors(true)
	v.SetWrap(true)
	v.SetBorderPadding(0, 0, 1, 1)
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
	r.helpView.SetText(r.fullHelpText())
	width, height := r.helpSize()
	x, y := r.centeredOnScreen(width, height)
	x, y, width, height = r.clampToScreen(x, y, width, height)
	r.renderHelpTitleBar(width)
	r.helpLayout.SetRect(x, y, width, height)
	r.pushOverlay(helpPage, r.helpLayout, nil)
}

// helpContentWidth returns the widest line helpText/aboutText actually
// have between them (via tview.TaggedStringWidth, so a "[::b]"/"[::-]"
// bold tag around a section heading doesn't count against it) —
// helpSize's own real target width, plus helpView's own 1-column
// left/right border padding (see newHelpView's SetBorderPadding).
// helpText is hand-wrapped at a fixed width for readability in the
// source, not reflowed to fill whatever width the window happens to be
// (SetWrap(true) only wraps a line *longer* than the window, never
// un-wraps one that already fits to use more of it) — a real,
// user-reported problem this fixes: sizing the window against a
// screen-width percentage the way it used to left most of a wide
// terminal's own width as dead space down the right side, with every
// line still breaking at the same narrow point regardless.
func (r *Root) helpContentWidth() int {
	width := 0
	for _, line := range strings.Split(r.fullHelpText(), "\n") {
		if w := tview.TaggedStringWidth(line); w > width {
			width = w
		}
	}
	return width + 2
}

// helpSize sizes Help against its own content's real width
// (helpContentWidth) — not clamped to one panel the way most overlays
// are (see clampToPanel), since it's a read-only reference, not a form
// tied to the current panel's own context — and generously tall
// (80% of the whole terminal): its content is long enough that more
// visible height genuinely means less scrolling, unlike width, which
// past helpContentWidth just leaves dead space (see its own doc
// comment).
func (r *Root) helpSize() (width, height int) {
	_, _, screenWidth, screenHeight := r.GetRect()
	width = r.helpContentWidth()
	if width > screenWidth {
		width = screenWidth
	}
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
