// Command breakthrough is the entry point of the breakthrough TUI file
// manager — see internal/ui.Root for what it actually shows (see also
// docs/whitepaper.md for the overall concept and vision).
//
// The initial directory is the process's working directory unless a path
// is given on the command line, e.g. "breakthrough /var/log" — see
// startDir. That directory also becomes the header's Start button target.
// "breakthrough --version" (or "-v") prints version information instead
// of starting the TUI — see the version/commit/date/builtBy vars below.
// "breakthrough --debug" redirects this process's own stderr to a log
// file for the whole run instead of starting normally — see
// enableDebugMode (debug.go) — a real, user-reported crash that came
// back as only a single, truncated stack frame is what this is for: a
// panic in one of this app's own background goroutines takes the whole
// process down without ever restoring the terminal first (see
// internal/ui/crash.go's own doc comment for the full, hand-verified
// reasoning), easy to miss on a broken terminal even when it prints in
// full — a durable log file survives that regardless.
package main

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/ui"
)

// version, commit, date, and builtBy are set via "go build -ldflags -X
// ..." by the release pipeline (see .goreleaser.yaml, which relies on
// exactly these four names existing here) — the defaults below are what
// a plain "go build", with no ldflags at all, reports instead.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
	builtBy = "source"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("breakthrough %s (commit %s, built %s by %s)\n", version, commit, date, builtBy)
		return
	}

	if slices.Contains(os.Args[1:], "--debug") {
		logPath, err := enableDebugMode()
		if err != nil {
			fmt.Fprintln(os.Stderr, "breakthrough: --debug:", err)
			os.Exit(1)
		}
		// To the real, still-unredirected stdout — enableDebugMode
		// already moved stderr to logPath by this point, so this is the
		// last thing that'll actually reach the visible terminal until
		// the TUI itself takes over the screen.
		fmt.Println("breakthrough: debug mode — stderr (including any crash) is being written to", logPath)
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "breakthrough:", err)
		os.Exit(1)
	}
}

func run() error {
	start, err := startDir()
	if err != nil {
		return err
	}
	explicitDir := startDirWasGiven()

	// EnablePaste turns on tcell's bracketed-paste support: without it, a
	// terminal paste arrives as a burst of ordinary key events, which for
	// a multi-line clipboard (or one containing a stray control
	// character) can misfire an InputField's own key handling instead of
	// just inserting the text. This is what makes pasting into any of
	// breakthrough's text fields — the new bash line (see
	// ui.Root.StartClock's neighbors in bottombar.go) most of all —
	// reliable.
	app := tview.NewApplication().EnableMouse(true).EnablePaste(true)

	root, err := ui.NewRoot(app, start)
	if err != nil {
		return err
	}
	// For the Help overlay's own About section — see Root.SetVersionInfo's
	// own doc comment for why this can't just be read directly from
	// within internal/ui itself.
	root.SetVersionInfo(version, commit, date, builtBy)

	// Reopen the tabs the last run left behind (see
	// Root.RestoreSavedTabs, which additionally honours the restore_tabs
	// setting). Skipped entirely when a directory was named on the
	// command line: "breakthrough /some/path" says where to open in
	// unambiguous terms, and burying that under a restored layout would
	// be answering a different question than the one asked.
	if !explicitDir {
		root.RestoreSavedTabs()
	}

	// Only meaningful once Application.Run is about to start draining its
	// update queue — see StartClock's own doc comment for why NewRoot
	// itself doesn't do this.
	stopClock := root.StartClock()
	defer stopClock()

	// tcell puts the terminal in raw mode, so these arrive as regular key
	// events rather than signals. These are global captures (not tied to
	// any one primitive) so they work regardless of what currently has
	// focus.
	//
	// Ctrl+Q only opens a confirmation overlay rather than stopping
	// immediately, since a stray keypress shouldn't lose your place
	// without asking first. Ctrl+C deliberately does not quit at all —
	// it backs out of whatever is open, like Escape.
	//
	// Ctrl+E/Ctrl+L/Ctrl+G/Ctrl+O/Ctrl+F/Ctrl+R (Edit/Look/toggle hidden
	// files/Options/Search/Remove — see the bottom bar's own buttons)
	// check their own preconditions before acting (see
	// Root.acceptsGlobalShortcut) rather than always firing the way
	// Ctrl+Q/Ctrl+C do: unlike those two, they'd otherwise step on the
	// bash line's own typing. Ctrl+H is deliberately not one of them —
	// it's indistinguishable from Backspace at the terminal protocol
	// level (both send the same 0x08 byte), so Ctrl+G was used for
	// "toggle hidden files" instead. Ctrl+O, previously left unclaimed,
	// is Options (Ctrl+X used to be, before the dialog itself was
	// renamed from Settings to Options and the shortcut moved to match).
	// Rename moved off Ctrl+R to F2 (see below) once Remove needed a
	// letter matching its own name and Ctrl+R was the only one left.
	//
	// F1 (Help) sits alongside Ctrl+Q/Ctrl+C, not the six above: it
	// works from literally anywhere, including in the middle of typing
	// a bash command or editing a field in another dialog, the same
	// "always fires" reasoning as those two — see Root.HelpShortcut's
	// own doc comment. F1, not a Ctrl combination, both sidesteps ever
	// needing a free letter (every obvious one is already claimed) and
	// matches the one keybinding this app's own stated inspiration,
	// Midnight Commander, uses for exactly the same purpose. F2 (Rename)
	// sits with the six Ctrl-letter actions instead, not with F1: it
	// still checks its own precondition the same way they do, unlike F1
	//
	// F3 (toggle mouse reporting, see Root.ToggleMouseShortcut's own doc
	// comment for why this exists at all) sits with F1/Ctrl+Q/Ctrl+C too,
	// for the same "always fires" reason — the whole point is grabbing
	// text via the terminal's own native selection, which needs to work
	// no matter what else is currently open. Every Ctrl-letter is
	// genuinely unavailable by this point (each one is either already
	// claimed above, natively bound by tview's own TextArea — verified
	// directly against its source the same way every claim in this
	// comment block is — or dead at the terminal protocol level, like
	// Ctrl+H/I/M/J: byte-identical to Backspace/Tab/Enter/breakthrough's
	// own bash-line "insert newline", respectively), so this continues
	// the same F-key sequence F1/F2 already started rather than reaching
	// for an increasingly obscure modifier combination a terminal might
	// not even deliver reliably (Alt+key relies on a terminal actually
	// sending the ESC-prefixed Meta sequence, which isn't universal).
	// — an F-key rather than Ctrl+R purely because Ctrl+R was needed
	// elsewhere, not because Rename should now work from anywhere.
	// F2/Rename is the near-universal convention across GUI file
	// managers (Windows Explorer, Nautilus, Dolphin) and several
	// terminal ones, so it was the natural key to free Ctrl+R with,
	// rather than picking an arbitrary unclaimed letter instead.
	//
	// Ctrl+D (the Details sidebar, see internal/ui/detailssidebar.go)
	// falls through to bashLine's own handling instead of always
	// consuming the event, the same as Ctrl+T/Ctrl+S/Ctrl+P/Ctrl+B below:
	// tview's own TextArea binds Ctrl+D to "delete forward" (verified
	// directly against tview's own source, the same way every claim in
	// this comment block is - see also the Ctrl+H note above for why
	// guessing instead has already gone wrong once), the same key the
	// physical Delete key already sends. Chosen over the alphabet's other
	// options (A/D/K/U/W/Y all have some real, native TextArea binding of
	// their own; H/I/M collide with Backspace/Tab/Enter at the terminal
	// protocol level; V/X/Z are earmarked for planned Paste/Cut/Undo
	// features instead) for its "Details" mnemonic and because losing
	// delete-forward specifically, while bashLine has focus, is the
	// least disruptive of the available real trade-offs - it's also the
	// one native binding this app already has its own guarded equivalent
	// for, on the physical Delete key itself, right below.
	//
	// Unlike those four, though, Ctrl+D is checked here via
	// BashLineHasFocus alone, not the full AcceptsGlobalShortcut - it
	// needs to keep working while Properties specifically is open too,
	// per the user's own explicit request to open or close Details
	// *alongside* Properties (see ToggleDetailsSidebarShortcut's own doc
	// comment), not just while plainly browsing. This is the same shape
	// Ctrl+K (compute hashes) and Ctrl+N (fetch metadata), both for the
	// Details sidebar, already have for the same reason - Ctrl+H (the
	// obvious mnemonic for "hash", matching Properties' own bare 'h')
	// and Ctrl+M (the obvious one for "metadata") were both considered
	// and rejected: Ctrl+H is the Backspace collision already noted
	// above, and Ctrl+M is the same kind of collision with Enter (both
	// send byte 0x0D) - no mnemonic survives that. Ctrl+K/Ctrl+N were
	// picked from what's left as the two with the least real cost:
	// TextArea's own Ctrl+K deletes to end of line, a real but
	// infrequently-reached-for edit; Ctrl+N is only breakthrough's own
	// bash-history-forward alias for Down (see the bash line's own help
	// text) - Down itself is untouched. All three - Ctrl+D, Ctrl+K,
	// Ctrl+N - need to keep working while Properties is open, not just
	// while plainly browsing (see Root.ComputeHashesShortcut's own doc
	// comment for why), which the full AcceptsGlobalShortcut Ctrl+T/
	// Ctrl+P/Ctrl+S/Ctrl+B below still use would otherwise block.
	//
	// Tab cycles focus among the panel, the Details sidebar (if it's
	// showing) and every currently open tool window (see
	// Root.CycleFocusShortcut) - considered and rejected first: Ctrl+Tab,
	// the more obvious "cycle between things" mnemonic. Verified directly
	// against tcell's own key.go, not assumed: there's no KeyCtrlTab
	// constant at all, because the classic terminal encoding tcell parses
	// here has no room left to represent it - Tab itself already
	// occupies the one byte (0x09) Ctrl+I would also use, so a real
	// Ctrl+Tab keypress arrives as plain Tab, indistinguishable, on most
	// terminals (the same class of problem as Ctrl+H/Ctrl+M above, just
	// with no extended-protocol fallback tcell's decoder even attempts
	// here) - and separately, most terminal emulators intercept Ctrl+Tab
	// themselves for their own tab-switching before it would ever reach
	// an application at all. Plain Tab is safe here specifically because
	// it's genuinely unclaimed while any of those has focus: none of
	// them installs a SetDoneFunc, so it was already a pure no-op in
	// every state this repurposes it for.
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlQ:
			root.RequestQuit()
			return nil
		case tcell.KeyCtrlC:
			root.RequestCancel()
			return nil
		case tcell.KeyCtrlE:
			root.EditShortcut()
			return nil
		case tcell.KeyCtrlL:
			root.LookShortcut()
			return nil
		case tcell.KeyCtrlR:
			root.PurgeShortcut()
			return nil
		case tcell.KeyCtrlG:
			root.ToggleHiddenShortcut()
			return nil
		case tcell.KeyCtrlO:
			root.OptionsShortcut()
			return nil
		case tcell.KeyCtrlF:
			root.SearchShortcut()
			return nil
		case tcell.KeyF1:
			root.HelpShortcut()
			return nil
		case tcell.KeyF2:
			root.RenameShortcut()
			return nil
		case tcell.KeyF3:
			root.ToggleMouseShortcut()
			return nil
		case tcell.KeyCtrlT:
			// Falls through to bashLine's own default handling (readline-
			// style Ctrl+T is "transpose characters") while it has focus,
			// rather than always consuming the key the way the seven above
			// do - see Root.AcceptsGlobalShortcut's own doc comment.
			if !root.AcceptsGlobalShortcut() {
				return event
			}
			root.TrashShortcut()
			return nil
		case tcell.KeyCtrlS:
			// Falls through while bashLine has focus for the same reason
			// as Ctrl+T just above - readline-style Ctrl+S is "forward
			// incremental search" in many shells' own line editing, even
			// though bashLine itself doesn't implement that.
			if !root.AcceptsGlobalShortcut() {
				return event
			}
			root.SedReplaceShortcut()
			return nil
		case tcell.KeyCtrlP:
			// bashLine's own captureBashLineKey binds Ctrl+P to command-
			// history recall - falling through here (not consuming the
			// event) while it has focus is what keeps that working; see
			// Root.AcceptsGlobalShortcut's own doc comment for why this one
			// specifically can't just always return nil the way the seven
			// above do.
			if !root.AcceptsGlobalShortcut() {
				return event
			}
			root.PropertiesShortcut()
			return nil
		case tcell.KeyCtrlD:
			// Falls through while bashLine has focus for the same reason
			// as Ctrl+T/Ctrl+P/Ctrl+S/Ctrl+B above - tview's own TextArea
			// binds Ctrl+D to "delete forward" (see the doc comment above
			// this switch), a real, working feature that would otherwise
			// be silently swallowed while typing a command. Checked
			// directly here, not via the full AcceptsGlobalShortcut those
			// four still use below - like Ctrl+K/Ctrl+N just below, this
			// also needs to keep working while Properties specifically is
			// open (see ToggleDetailsSidebarShortcut's own doc comment),
			// which the fuller check would otherwise block there too.
			if root.BashLineHasFocus() {
				return event
			}
			root.ToggleDetailsSidebarShortcut()
			return nil
		case tcell.KeyCtrlK:
			// Falls through while bashLine has focus for the same reason
			// as Ctrl+D just above - tview's own TextArea binds Ctrl+K to
			// "delete to end of line" (see the doc comment above this
			// switch).
			if root.BashLineHasFocus() {
				return event
			}
			root.ComputeHashesShortcut()
			return nil
		case tcell.KeyCtrlN:
			// Falls through while bashLine has focus for the same reason
			// as Ctrl+K just above - it's breakthrough's own bash-history-
			// forward alias (see the doc comment above this switch), not a
			// native TextArea binding, but still a real, working one.
			if root.BashLineHasFocus() {
				return event
			}
			root.FetchMetadataShortcut()
			return nil
		case tcell.KeyTab:
			// Ctrl+Tab steps through the panel tabs (see
			// Root.NextTabShortcut); plain Tab keeps its existing
			// meaning, below. Both arrive as KeyTab — the modifier is
			// the only thing telling them apart, which is exactly why
			// this is one case with a check rather than two.
			if event.Modifiers()&tcell.ModCtrl != 0 {
				root.NextTabShortcut()
				return nil
			}
			// Only consumed when it actually means something (see
			// Root.CycleFocusShortcut's own doc comment) - cycling focus
			// among the panel, the Details sidebar once it's shown, and
			// any open tool window, so each one's own already-built-in
			// scrolling works once its content outgrows the space it has.
			// Everywhere else (a Properties field, the header path edit,
			// the filter box, bash-line completion, ...), it reports
			// false and this falls through completely untouched, exactly
			// as Tab already worked everywhere before this existed.
			if root.CycleFocusShortcut() {
				return nil
			}
			return event
		case tcell.KeyBacktab:
			// Ctrl+Shift+Tab, the other direction. tcell folds Shift+Tab
			// into KeyBacktab and strips ModShift while doing it
			// (verified directly against its own NewEventKey, and live
			// against a real terminal), so what arrives here for
			// Ctrl+Shift+Tab is KeyBacktab still carrying ModCtrl —
			// which is what distinguishes it from a plain Shift+Tab,
			// left untouched below.
			if event.Modifiers()&tcell.ModCtrl != 0 {
				root.PrevTabShortcut()
				return nil
			}
			return event
		case tcell.KeyF4:
			// Opens the tab switcher (see Root.TabSwitcherShortcut's own
			// doc comment) — the keyboard path that works on every
			// terminal, including the ones that can't report Ctrl+Tab or
			// Ctrl+digit at all. Continues the F1/F2/F3 sequence for the
			// same reason F3 did: no unclaimed Ctrl letter is left.
			root.TabSwitcherShortcut()
			return nil
		case tcell.KeyRune:
			// Ctrl+1..Ctrl+9 and Ctrl+0 jump straight to a tab by its own
			// number, with Ctrl+0 meaning the tenth — the numbering the
			// tab strip itself shows, per the user's own explicit
			// request.
			//
			// Whether these arrive at all depends on the terminal: the
			// classic control-code encoding has no room for most
			// Ctrl+digit combinations (Ctrl+3 is byte-identical to
			// Escape, Ctrl+8 to Delete, and Ctrl+1/9/0 have no encoding
			// whatsoever), so they only become distinguishable once one
			// of the enhanced keyboard protocols is in play — kitty's
			// CSI-u or xterm's modifyOtherKeys, both of which tcell
			// requests at startup on any xterm-like terminal. Verified
			// live against a real terminal, not assumed: with the
			// protocol active all ten arrive here as KeyRune carrying
			// ModCtrl; without it they never match this case at all and
			// fall through exactly as before, which is why binding them
			// costs nothing on a terminal too old to send them. F4 above
			// is the always-available way to the same feature.
			if event.Modifiers()&tcell.ModCtrl != 0 && event.Rune() >= '0' && event.Rune() <= '9' {
				n := int(event.Rune() - '0')
				if n == 0 {
					n = 10 // Ctrl+0 is the tenth tab, continuing the row of digits
				}
				root.SwitchToTabShortcut(n)
				return nil
			}
			return event
		case tcell.KeyCtrlB:
			// Falls through while bashLine has focus for the same reason
			// as Ctrl+T/Ctrl+P above - readline-style Ctrl+B is
			// "backward-char", and tview's TextArea binds it to its own
			// PgUp-style movement.
			if !root.AcceptsGlobalShortcut() {
				return event
			}
			root.TrashbinShortcut()
			return nil
		case tcell.KeyDelete:
			// Entf triggers the safe action (Trash), matching both the
			// physical key's own label and the near-universal
			// file-manager convention — see TrashShortcut's own doc
			// comment for the full reasoning. Ctrl+Delete for Remove is
			// best-effort: tcell's own EventKey.Modifiers doc notes "it
			// will not always be possible" to detect a modifier together
			// with a non-alphanumeric key across every terminal —
			// Ctrl+R above is the reliable path to Remove regardless of
			// what this resolves to on any given terminal.
			//
			// Falls through un-consumed while bashLine has focus, the same
			// as Ctrl+T/Ctrl+P above - otherwise this would eat a plain
			// forward-delete keystroke while typing a command.
			if !root.AcceptsGlobalShortcut() {
				return event
			}
			if event.Modifiers()&tcell.ModCtrl != 0 {
				root.PurgeShortcut()
			} else {
				root.TrashShortcut()
			}
			return nil
		}
		return event
	})

	return app.SetRoot(root, true).Run()
}

// startDir picks the directory breakthrough opens in: an explicit
// "breakthrough /some/path" argument if one was given, otherwise the
// process's current working directory. Whichever it resolves to also
// becomes the panel's "Start" button target (see ui.Panel's header) — an
// invalid or unreadable argument is left for Panel's own load to reject,
// rather than duplicating that validation here.
//
// Skips over any flag (--debug, --version, -v, or anything else
// starting with "-") rather than blindly taking os.Args[1] — otherwise
// "breakthrough --debug" would try to open a directory literally named
// "--debug" instead of enabling debug mode and opening the current
// directory, and "breakthrough --debug /some/path" wouldn't find its
// own path argument at all.
func startDir() (string, error) {
	if dir, ok := explicitStartDir(); ok {
		return dir, nil
	}
	return os.Getwd()
}

// startDirWasGiven reports whether a directory was actually named on the
// command line, as opposed to startDir having fallen back to the working
// directory.
//
// The distinction matters only to the saved-tab restore (see run): an
// explicitly named directory suppresses it, while a bare "breakthrough"
// reopens the previous layout. startDir alone can't answer this — the
// working directory it falls back to is a perfectly ordinary path, and
// indistinguishable afterwards from the same path having been typed.
func startDirWasGiven() bool {
	_, ok := explicitStartDir()
	return ok
}

// explicitStartDir is the argument scan both of the two above share: the
// first non-flag argument, if there is one.
func explicitStartDir() (string, bool) {
	for _, arg := range os.Args[1:] {
		if !strings.HasPrefix(arg, "-") {
			return arg, true
		}
	}
	return "", false
}
