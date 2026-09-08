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
	"runtime/debug"
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/ui"
)

// version, commit, date, and builtBy are set via "go build -ldflags -X
// ..." by the release pipeline (see .goreleaser.yaml, which relies on
// exactly these four names existing here). builtBy stays "source" for
// any other build, always accurately — nothing else ever sets it. The
// other three would likewise stay at these bare, uninformative literals
// for a plain "go build ./..." too, if applyDevBuildVersion's own init
// call below didn't step in first: it overwrites version/commit/date
// from the binary's own embedded VCS info whenever version is still
// exactly this literal "dev", so a contributor's own ordinary build
// reports a real commit and dirty-tree flag instead — see version.go
// for the full reasoning.
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

	// Catches a panic on the main goroutine — a key handler, a mouse
	// handler, a draw callback, tview's own event loop — which is the
	// one place internal/ui's safeGo cannot reach.
	//
	// Without this such a crash left no evidence anywhere: tview's Run
	// restores the terminal and re-panics, so the traceback goes to a
	// stderr that has just been switched out of the alternate screen
	// buffer and is usually scrolled away or wiped before it can be
	// read, and nothing writes it to a file. A real, regularly
	// reproducible crash was consequently undiagnosable.
	//
	// Deliberately not an attempt to keep going: see ui.ReportPanic.
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		path := ui.ReportPanic("main", rec)
		fmt.Fprintf(os.Stderr, "\nbreakthrough: internal error: %v\n", rec)
		if path != "" {
			fmt.Fprintf(os.Stderr, "A full report was written to %s\n", path)
			fmt.Fprintln(os.Stderr, "Please include it when reporting this: https://github.com/jagottsicher/breakthrough/issues")
		}
		// The stack to stderr as well as to the log: someone watching a
		// terminal that did keep its scrollback should not have to go
		// find a file to see what happened.
		fmt.Fprintf(os.Stderr, "\n%s", debug.Stack())
		os.Exit(2)
	}()

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
	// Every other Ctrl-letter binding this app once had has since been
	// retired, each once its own action gained a plain-letter or chord
	// home on the primary keyboard layer (see internal/ui/keymap.go's
	// own package doc) that covers the exact same ground — including,
	// for Look/Details/hashes/directory-size/metadata, the same "also
	// works while Properties is open" reach those five once needed a
	// dedicated Ctrl combination for at all (see
	// plainCommand.alsoOverProperties and acceptsPropertiesAwareKey in
	// keymap.go). Options (formerly Ctrl+O) and mouse reporting
	// (formerly Ctrl+_, which fired completely unconditionally — even
	// with a dialog open or the command line focused, since a plain
	// letter never safely can) moved to the "o" chord (oo/om) despite
	// that trade-off, per the user's own explicit, deliberate choice: no
	// Ctrl-letter binding is worth keeping just for that one edge case.
	//
	// One exception remains here, for a reason a plain letter genuinely
	// can't replace:
	//
	//   - Ctrl+T (tab switcher) stays alongside "t" for one capability
	//     "t" alone can't have: pressing it again while the switcher
	//     itself is the open overlay walks to the next tab, which needs
	//     to keep working precisely in the one state (an overlay
	//     already open) every plain letter is correctly blocked in — see
	//     its own case below.
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
	// terminals - and separately, most terminal emulators intercept
	// Ctrl+Tab themselves for their own tab-switching before it would
	// ever reach an application at all. Plain Tab is safe here
	// specifically because it's genuinely unclaimed while any of those
	// has focus: none of them installs a SetDoneFunc, so it was already
	// a pure no-op in every state this repurposes it for.
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// The primary keyboard layer (see internal/ui/keymap.go): plain
		// letters, and the g/p/z chords built on top of them. Checked
		// next, ahead of every Ctrl-letter/F-key case below, so a chord
		// waiting on its second key intercepts it before anything else
		// gets a chance to — see HandlePlainKey's own doc comment for
		// why nothing may fall through while a chord is pending.
		if root.HandlePlainKey(event) {
			return nil
		}

		switch event.Key() {
		case tcell.KeyCtrlQ:
			root.RequestQuit()
			return nil
		case tcell.KeyCtrlC:
			root.RequestCancel()
			return nil
		case tcell.KeyCtrlT:
			// A second keyboard path to the tab switcher, alongside "t"
			// (see Root.TabSwitcherShortcut) — kept for the one thing
			// "t" alone can't do: press it again while the switcher
			// itself is already the open overlay to walk to the next
			// tab, rather than being blocked outright the way every
			// plain letter correctly is once any overlay is open (see
			// acceptsPlainKeyCommand). Ctrl+T's own gating below is
			// narrower than that on purpose, precisely to keep this one
			// case working.
			//
			// Falls through to bashLine's own default handling (readline-
			// style Ctrl+T is "transpose characters") while it has focus,
			// rather than always consuming the key the way Ctrl+Q/Ctrl+C
			// above do.
			//
			// Checked via BashLineHasFocus alone, not the full
			// AcceptsGlobalShortcut: that also refuses whenever *any*
			// overlay is open, and the tab switcher is one — so pressing
			// Ctrl+T again to walk to the next tab (see
			// Root.TabSwitcherShortcut) never reached it and did nothing
			// at all. A real bug, and one the unit tests missed entirely
			// by calling the shortcut method directly rather than through
			// this dispatch. TabSwitcherShortcut applies the remaining
			// guard itself, so every other overlay still blocks it.
			if root.BashLineHasFocus() {
				return event
			}
			root.TabSwitcherShortcut()
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
		case tcell.KeyRune:
			// Ctrl+1..Ctrl+9/Ctrl+0 and Alt+1..Alt+9/Alt+0 both jump
			// straight to a tab by its own number, with …+0 meaning the
			// tenth — the numbering the tab strip itself shows, per the
			// user's own explicit request. Two modifiers bound to the
			// same action because neither is reliable everywhere on its
			// own — a terminal missing one of them is the whole reason
			// the other exists here too, not redundancy.
			//
			// Ctrl+digit depends on the terminal: the classic control-
			// code encoding has no room for most Ctrl+digit combinations
			// (Ctrl+3 is byte-identical to Escape, Ctrl+8 to Delete, and
			// Ctrl+1/9/0 have no encoding whatsoever), so they only
			// become distinguishable once one of the enhanced keyboard
			// protocols is in play — kitty's CSI-u or xterm's
			// modifyOtherKeys, both of which tcell requests at startup
			// on any xterm-like terminal, but which several real
			// terminals (VTE-based ones among them, per a real user
			// report) simply don't implement.
			//
			// Alt+digit instead relies on the older, near-universal
			// ESC-prefixed "Meta" convention (plain ESC followed by the
			// key) — verified live, not assumed, the same as the
			// Ctrl+digit path above. Its own tradeoff: this is
			// inherently ambiguous with a bare Escape keypress followed
			// immediately by an unrelated digit, told apart only by
			// tcell's own short timing heuristic after seeing ESC alone
			// — the same mechanism vim/tmux already rely on for their
			// own Alt+key bindings, and an accepted, low-probability
			// tradeoff per the user's own explicit request to add this
			// anyway.
			//
			// Whichever modifier a given terminal can't report, the
			// bound one simply never matches this case at all and falls
			// through exactly as before — which is why binding both
			// costs nothing where neither is supported. F4 above is the
			// always-available way to the same feature regardless.
			if event.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0 && event.Rune() >= '0' && event.Rune() <= '9' {
				n := int(event.Rune() - '0')
				if n == 0 {
					n = 10 // …+0 is the tenth tab, continuing the row of digits
				}
				root.SwitchToTabShortcut(n)
				return nil
			}
			return event
		case tcell.KeyDelete:
			// Entf triggers the safe action (Trash), matching both the
			// physical key's own label and the near-universal
			// file-manager convention — see TrashShortcut's own doc
			// comment for the full reasoning. Ctrl+Delete for Remove is
			// best-effort: tcell's own EventKey.Modifiers doc notes "it
			// will not always be possible" to detect a modifier together
			// with a non-alphanumeric key across every terminal — the
			// plain-letter layer's "D" (see openRemoveConfirm/keymap.go)
			// is the reliable path to Remove regardless of what this
			// resolves to on any given terminal.
			//
			// Falls through un-consumed while bashLine has focus — Entf
			// is tview's own TextArea binding for "delete forward" (the
			// same as the physical Delete key), and losing that while
			// typing a command would be a real, working feature silently
			// broken.
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
