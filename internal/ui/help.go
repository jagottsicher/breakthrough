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
[::b]Keyboard — plain letters, while browsing[::-]

  No modifier key to get wrong, no terminal-layout risk — the same
  approach ranger/nnn/lf/vifm all use. Only active while the file
  listing itself has keyboard focus: typing in the filter box, the path
  editor, or the bash line is never affected by any of this, and
  neither is any open dialog.

  c   Copy              d   Move to Trash        i   Properties
  x   Cut               D   Remove permanently    I   Details sidebar
  v   Paste             u   Undo last rename       h  Compute hashes
  r   Rename            e   Edit                   k  Directory size
  m   Context menu      f   Find                   M  Image metadata
  n   New tab           w   Close tab             l   Look
  s   Split view                                  t   Tab switcher
  a   Select all         *  Invert selection      .   Toggle hidden
                                                   +/- Select/deselect
                                                       by pattern
  B   Batch rename       E  Sed Replace           G   Go to last row
  q   Quit                ? This help              :  Bash command line

  h/k/M target whichever of Properties/Details is relevant (Properties
  first if both are open on the same file), opening the Details sidebar
  first if neither is — "select something, press the key" works from
  plain browsing, not only once one of the two is already open. h, k,
  M, l, and I all also work while Properties specifically is open, on
  top of their usual reach, so Look, Details, and the tool trio stay
  reachable without first closing it.

  A capital letter is the bigger sibling of its own lowercase one where
  both exist: "d" is reversible (the Trash), "D" is not (asks first).
  "d" itself never asks either, unless "Confirm before moving to Trash"
  is turned on under Options (off by default) — "D" always does either
  way. While browsing the Trash itself, "r" restores and "D" empties it,
  instead of their ordinary meaning — the same two letters, read
  differently in the one place that makes sense. "V" is "v" Paste's own
  bigger sibling too, for symlinks specifically: plain "v" pastes using
  whichever "Follow symlinks" default is configured under Options for
  Copy or Move (off unless you've turned it on) — recreating a symlink
  as a symlink at the destination if that default is off, or replacing
  it (and any symlink nested inside a pasted folder) with a real copy
  of whatever it points to if it's on. "V" flips that default for this
  one paste only, without changing the setting itself. Only asks first
  when it's actually turning dereferencing on (the default was off) —
  a small, instant symlink can turn into an arbitrarily large copy that
  way; flipping it off instead never needs to ask. For a Cut that does
  dereference, only the original link itself is removed afterward,
  never its target, however far away that lives.

  Chords — a letter, then within about four seconds one more (see the
  status bar's own shrinking countdown while one is pending, and the
  button bar for what the second key can be). That timeout itself is
  "Chord timeout (ms)" under Options (Behavior, Miscellaneous),
  4000ms by default:

    g  go to    gg top · gh home · gu up · gp back · gn forward ·
                gr / (root) · gb Trashbin
    p  perms    pm chmod · po chown
    z  display  zs size format · zt time format · zo split orientation ·
                zw swap panes · zr reload
    o  options  oo Options screen · om Mouse reporting on/off
    y  yank     yp/yn/ya full path/name/all selected — reserved, not
                built yet (needs its own system-clipboard design first)

  Escape cancels a pending chord; any other key that isn't one of its
  own members cancels it too and says so. Letting it simply time out
  (the status bar's own countdown reaching empty) cancels silently —
  that's "changed my mind", not a mistake worth a message.

  Only two Ctrl-letter shortcuts remain, documented section by section
  below, for the one thing this layer genuinely can't do on its own
  (Quit, Cancel — see "Global" right below) — everything else, Options
  and the mouse-reporting toggle included, has exactly one keyboard
  path, the plain letter/chord above. Unlike its own former Ctrl-letter
  binding, "om" toggling mouse reporting only works while plainly
  browsing, not with a dialog open or the bash line focused — a
  deliberate trade-off, since a plain letter never safely can fire
  unconditionally the way a Ctrl combination could.

[::b]Global — work anywhere, even inside another dialog[::-]

  Ctrl+Q          Quit (asks first)
  Ctrl+C          Cancel/back out of whatever's open — never quits

[::b]File panel[::-]

  Ctrl+T          Tab switcher — same as "t", plus one thing "t" alone
                  can't: pressing it again while the switcher is already
                  open walks to the next tab
  Delete          Move the selection to Trash (reversible) — same as
                  "d"; already inside the trash itself, does a Remove
                  instead — nowhere left to move an already-trashed
                  item to
  Ctrl+Delete     Remove — permanently delete the selection (asks
                  first), best-effort depending on your terminal — same
                  as "D" regardless, which always works
  Enter           Open the selected directory, or try Look on a file —
                  double-clicking a name does the same either way
  Space           Select/deselect the selected file
  Click, pause,   Rename — the pause is deliberately generous (about a
  click again     second), so an unhurried second click still counts;
                  slower than that is just a fresh first click again
  Right-click     Context menu (Look, Rename, Edit, Copy, Cut, Multiply,
                  Paste, Move to Trash, Properties, and submenus for
                  rarer actions — tail -f/chown/chmod/sed/Batch
                  rename/Undo last rename/Remove/Paste following
                  symlinks, Selection, Tabs & Split). "m" opens the
                  same menu from the keyboard. Once it's open,
                  "l"/"e"/"r"/"c"/"x"/"d"/"i" — the same letters those
                  seven already have on their own — fire that entry
                  directly, without arrowing down to it first. "m"
                  again ("mm") does too, for Multiply specifically —
                  the one entry with no plain-key equivalent of its
                  own to mirror, since it only ever opens from here.

[::b]Details sidebar ("I")[::-]

  A read-only, live-updating panel of file info (stat fields; for an
  image or PDF, a preview with its own click zone/"l" for fullscreen;
  hashes, or for a directory, its total size) for whichever entry is
  currently selected. The "<" button at the far end of the path bar
  (right after the tab strip) expands it the same way "I" does; once
  open, the ">" button in its own top-right corner collapses it again.

  h   Compute hashes (SHA-256/SHA-1/MD5/SHA-512/BLAKE2b-512) for
      Properties if that's open, otherwise the Details sidebar; shown in
      both at once if both are open on the same file, however it was
      triggered
  k   Compute a directory's total size (du -hs), for whichever
      directory is currently selected — on demand, since it can take a
      real, visible amount of time on a large tree
  M   Load an image's metadata (EXIF etc. — not implemented yet)

  Click a path segment in the header to jump straight there; click the
  path itself to type a new one (Tab completes it, Enter goes); click
  a column heading to sort by it; click the "Y" button near the right
  edge of the path bar (an "Nx" count appears before it once one or
  more are actually narrowing the listing, turning red if a filter is
  hiding everything a directory would otherwise show) — or press "/" —
  to open the filter dropdown, three independently combinable (AND)
  rows, each narrowing the listing live as you type:

    Glob/regex      type to narrow the list live; its own button
                    switches glob/regex, its own checkbox disables it
                    without clearing what's typed
    Size            "> 1m", ">= 500k", "= 0", or a range joined with
                    "and" ("> 1m and < 1g"); units b/k/m/g/t, binary
                    (1024-based) — a bare number means plain bytes
    Modified time   before/after/between <moment>, or a bare moment
                    alone meaning "within the last ..."; a moment is
                    absolute ("2026-09-01", optionally with a time) or
                    relative ("7 days", "2 hours ago", "last 30
                    minutes") — sec/min/hour/day/week/month/year,
                    singular or plural

  Typing into a field auto-activates its own row. Tab/Shift+Tab cycle
  all seven of the dropdown's own pieces; "/" — once the dropdown is
  already open — jumps straight to the next of the three fields
  instead, the same "press it again to advance further" trick Ctrl+T
  uses for the tab switcher. Escape closes it. By default
  (filter_persistent) all three carry over across a directory change,
  so browsing a whole tree with the same filter on is the normal way
  to use it; set filter_persistent = false to have every new directory
  start unfiltered instead.

  While plainly browsing (not editing the path, not in the filter
  dropdown) and the Details sidebar is shown, Tab moves keyboard focus
  into it —
  its own scrolling (arrow keys, PageUp/PageDown, Home/End, mouse
  wheel) then works once its content is longer than it has room for —
  and Tab again moves focus back to the panel. A click anywhere in the
  sidebar that isn't one of its own click zones also focuses it, the
  same way.

[::b]Options screen ("oo")[::-]

  Categories down the left, that category's settings on the right.

  Up / Down         Move between categories, or between settings
  Tab / Shift+Tab   Move between the categories, the settings and the
                    buttons underneath them
  Enter             Change the selected setting — toggles a yes/no
                    directly, opens a list for a choice, or a field for
                    a number (Enter commits it, Escape discards)
  ?                 Explain the selected setting in a small window
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

[::b]Split view ("s")[::-]

  Two of your open tabs on screen at once, instead of one at a time.

  s                 Split / unsplit — the button bar's own "s Split"
                    does the same
  z then o          Flip between side by side and above/below (the "z"
                    chord's own display-toggle family)
  Tab               Move between the two panes; clicking a pane does the
                    same

  The pane you are in keeps the keyboard, the context menu, and every
  shortcut — the other one just sits there until you move to it, and the
  highlighted row shows you which is which at a glance. Each pane's own
  number strip marks the tab it holds.

  With only one tab open, "s" opens a second one on the same directory,
  which is the usual starting point for copying between two places in
  one tree. Otherwise it pairs you with the tab you last split with, or
  the next one along.

  To pick the other pane yourself, open the tab list ("t") and use a
  row's "◫" button — on a real tab to show that one beside the current
  one, or on the "+ New tab" row to make a fresh tab and split with it
  in one go.

  Switching tabs while split (Ctrl+1...Ctrl+0, or the list) replaces
  what the pane you are in shows; the other pane stays put, and neither
  ever swaps sides. Closing either pane's own tab ends the split.

  Whether panes sit side by side or stacked is a setting
  ("split_stacked", also in Options → Behavior), so it survives a
  restart — as does the split itself, along with the tabs.

[::b]Batch rename (context menu's "Batch rename")[::-]

  Steps down the left, that step's own settings on the right, a live
  preview of every selected file underneath — updated on every change,
  no separate "Preview" button to press first.

  Left / Right      Move between the steps and the settings
  Up / Down         Move between steps, or between one step's settings
  Enter / Space     Change the selected setting — toggles a yes/no
                    directly, cycles a choice, or opens a field for
                    text/a number (Enter commits it, Escape discards)
  Tab / Shift+Tab   Move between the steps, the settings, the preview
                    and the buttons underneath them
  Escape            Close without renaming anything

  The steps always run in this order: Search & Replace, Case, Trim,
  Numbering, Extension — a step left at its default setting does
  nothing, there's no separate on/off switch to also set. Search &
  Replace and Case only ever touch the name, never the extension;
  Extension only ever touches the extension.

  The preview shows every selected file, changed or not: an unchanged
  name is dimmed, a conflict (would collide with another renamed file,
  or with something already on disk) is shown in red with why, right
  where it's about to happen — nothing is written until "Rename" is
  pressed and confirmed. "Reset all steps" clears the whole pipeline
  without closing the screen; "Undo last rename" (context menu, right
  below "Batch rename") reverses whatever the last confirmed rename
  actually did.

[::b]Tabs[::-]

  Several directories open at once in the same window, one visible at a
  time. Each tab keeps its own history, filter, sort order, selection
  and cursor position — switching away and back leaves everything
  exactly as you left it.

  Ctrl+1 ... Ctrl+0  Jump straight to that tab (...+0 is the tenth) —
  Alt+1 ... Alt+0    either modifier, whichever your terminal reports
  Ctrl+Tab           Open the switcher on the next tab; press again to
  Ctrl+Shift+Tab     keep moving, Enter to go there, Escape to stay put
  t / Ctrl+T         Open the switcher on the current tab; press again
                     to walk to the next one

  In the switcher: Up/Down picks a tab, Enter or Space goes to it,
  Escape stays put, and the last row opens a new one. Right steps onto
  that row's own "✕" button and Enter or Space there closes the tab
  (Delete does the same from anywhere in the row); the cursor then moves
  to the row above. The first tab has no "✕" — one tab always stays
  open.

  The numbered strip after the filter button shows the open tabs; the
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
  ones report neither, in which case nothing happens. "t", Ctrl+T, the
  strip, and the context menu all work regardless of what your terminal
  can report.

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

  "h" (see the Details sidebar's own entry above) computes hashes here
  too — clicking the hash hint works as well.

[::b]Look ("l", or Enter/double-click a file)[::-]

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

[::b]Multiply dialog ("mm", or context menu's "Multiply")[::-]

  Creates one or more copies of the selection right beside it, each
  named by the current strategy — an ordinary Copy underneath (works on
  a directory just as well as a file), never a Move. Guided, not
  combined: the Strategy dropdown shows only the fields that strategy
  actually uses, never all three strategies' own fields at once.

  Target                 Read-only — what this Multiply run is for
  Strategy               Numbered (default): counts up from 1 until a
                         free name is found. Fixed suffix text: the
                         same literal text every time — duplicating the
                         result again doubles it ("_copy_copy"), rather
                         than retrying automatically. Date/time: a
                         timestamp, computed once
  Separator              Sits between the original name and the
                         strategy's own suffix — shown for every
                         strategy
  Suffix text            "Fixed suffix text"'s own literal suffix —
                         shown only for that strategy
  Number padding         Zero-pads "Numbered"'s own number ("_001"
                         instead of "_1") — shown only for that
                         strategy
  Date/time format type  Go format string (default), Strftime-style
                         Format, or Unix timestamp — a second dropdown,
                         shown only for the Date/time strategy
  Date/time format       Go's own reference-time layout or a
                         strftime-style format, each with its own
                         independently-edited example text that
                         survives switching back and forth; disabled
                         and showing today's real Unix timestamp
                         instead once "Unix timestamp" above is picked
  Number of duplicates   How many copies this one run creates, capped
                         by "Maximum number of duplicates" under
                         Options

  The suffix always lands after the original name's own *entire* text,
  extension included, never before it: "archive.tar.gz" duplicates to
  "archive.tar.gz_1", not "archive.tar_1.gz" or "archive_1.tar.gz" —
  nothing about a basename counts as more "real" than the rest of it
  just because it follows a dot. A live Preview line above
  Cancel/Duplicate shows the exact name the first duplicate would get
  right now, given every field's current value.

  Whatever's chosen here becomes the new default shown next time (all
  of the above, under Options → Behavior → Duplicate) — the one
  setting group in this whole app that adapts itself this way, per its
  own explicit design. Only on actually pressing "Duplicate": editing a
  field and then Cancel never touches the sticky default at all.

[::b]Search dialog ("f")[::-]

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

[::b]Paste conflicts ("v", when a destination already exists)[::-]

  With "Auto-merge directories" on under Options (off by default, set
  separately for Copy and Move), a conflict between two directories
  skips this dialog entirely and merges immediately, every time — the
  same outcome "Merge into existing folder" below gives by hand. A
  conflict between two files, or a file and a directory, always still
  opens this dialog regardless, since merging has no meaning there.

  Up / Down         Move between the options
  Enter / Space     Apply the highlighted option
  Escape            Skip — same as the preselected default

  Overwrite               Replace this one entry entirely (a
                          directory ends up identical to the source —
                          nothing extra left over), decide the next
                          conflict separately
  Overwrite all           Same, and apply it to every conflict this
                          Paste still runs into, with no further
                          asking
  Merge into existing folder      Directory conflicts only: copy the
                          source's files over it, keeping whatever's
                          already there the source doesn't have —
                          identical to Overwrite for a plain file
  Merge all into existing folders Same, for every conflict this
                          Paste still runs into
  Skip                    Leave the existing entry untouched, decide
                          the next conflict separately
  Skip all               Same, for every conflict this Paste still
                          runs into
  Overwrite all if source is newer      Overwrite only where the
                          copied file's own modified time is newer
                          than what's already there; skip the rest —
                          applies to every conflict, like the other
                          "all" options
  Overwrite all if source is not empty  Overwrite only where the
                          copied file actually has content; skip a
                          zero-byte source instead of replacing
                          something real with nothing — also applies
                          to every conflict

  Everything else in the Paste keeps copying/moving in the background
  while this dialog is open — a conflict found before this one is
  answered queues behind it instead of opening a second dialog on top,
  shown as "(N more waiting)" right in this one's own message.

  Ctrl+C stops the whole Paste outright, dialog open or not — whatever
  was already mid-write finishes normally where it was headed, nothing
  still queued starts. A different overlay merely open while a Paste
  continues in the background is unaffected.

  Whatever's currently on the clipboard shows two ways: every row it
  holds gets a full-row grey tint (a lighter shade for Cut than Copy),
  across every open tab showing that row, not just the one Copy/Cut
  was pressed in; and the status bar names it — "Copy: 3 files, 1
  dir" — right after the chord countdown's own spot. Once Paste
  actually starts, that same spot shows its own live progress instead
  — a spinner, how many items are done, a two-row bar packed into one
  line of half-block characters (top half: item count, bottom half:
  the current file's own byte progress), and the real file currently
  being written. Once a background scan of the whole selection's size
  finishes (started the moment Paste was pressed, never delaying it),
  a leading character also fills in showing overall byte progress, and
  an estimated remaining duration appears after the bar. A
  same-filesystem move is instant regardless of size, so it usually
  finishes before any of this ever shows anything at all — expected,
  not a missed update.

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
	bar.SetBackgroundColor(r.theme.InputBackground)
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
	// A no-op while Help is already the front overlay, rather than
	// pushing a second copy of it on top of itself — every caller gets
	// this for free rather than each having to guard it separately (see
	// "?"'s own action in keymap.go, currently the only one).
	if r.activePage == helpPage {
		return
	}
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
