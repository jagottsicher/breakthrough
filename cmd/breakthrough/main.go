// Command breakthrough is the entry point of the breakthrough TUI file
// manager — see internal/ui.Root for what it actually shows (see also
// docs/whitepaper.md for the overall concept and vision).
//
// The initial directory is the process's working directory unless a path
// is given on the command line, e.g. "breakthrough /var/log" — see
// startDir. That directory also becomes the header's Start button target.
// "breakthrough --version" (or "-v") prints version information instead
// of starting the TUI — see the version/commit/date/builtBy vars below.
package main

import (
	"fmt"
	"os"

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
	// Ctrl+E/Ctrl+L/Ctrl+R/Ctrl+G/Ctrl+O/Ctrl+F (Edit/Look/Rename/toggle
	// hidden files/Options/Search — see the bottom bar's own buttons)
	// check their own preconditions before acting (see
	// Root.acceptsGlobalShortcut) rather than always firing the way
	// Ctrl+Q/Ctrl+C do: unlike those two, they'd otherwise step on the
	// bash line's own typing. Ctrl+H is deliberately not one of them —
	// it's indistinguishable from Backspace at the terminal protocol
	// level (both send the same 0x08 byte), so Ctrl+G was used for
	// "toggle hidden files" instead. Ctrl+O, previously left unclaimed,
	// is Options (Ctrl+X used to be, before the dialog itself was
	// renamed from Settings to Options and the shortcut moved to match).
	//
	// F1 (Help) sits alongside Ctrl+Q/Ctrl+C, not the five above: it
	// works from literally anywhere, including in the middle of typing
	// a bash command or editing a field in another dialog, the same
	// "always fires" reasoning as those two — see Root.HelpShortcut's
	// own doc comment. F1, not a Ctrl combination, both sidesteps ever
	// needing a free letter (every obvious one is already claimed) and
	// matches the one keybinding this app's own stated inspiration,
	// Midnight Commander, uses for exactly the same purpose.
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
			root.RenameShortcut()
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
		case tcell.KeyCtrlT:
			// Falls through to bashLine's own default handling (readline-
			// style Ctrl+T is "transpose characters") while it has focus,
			// rather than always consuming the key the way the six above
			// do - see Root.AcceptsGlobalShortcut's own doc comment.
			if !root.AcceptsGlobalShortcut() {
				return event
			}
			root.TrashShortcut()
			return nil
		case tcell.KeyCtrlP:
			// bashLine's own captureBashLineKey binds Ctrl+P to command-
			// history recall - falling through here (not consuming the
			// event) while it has focus is what keeps that working; see
			// Root.AcceptsGlobalShortcut's own doc comment for why this one
			// specifically can't just always return nil the way the six
			// above do.
			if !root.AcceptsGlobalShortcut() {
				return event
			}
			root.PurgeShortcut()
			return nil
		case tcell.KeyDelete:
			// Entf triggers the safe action (Trash), matching both the
			// physical key's own label and the near-universal
			// file-manager convention — see TrashShortcut's own doc
			// comment for the full reasoning. Ctrl+Delete for Purge is
			// best-effort: tcell's own EventKey.Modifiers doc notes "it
			// will not always be possible" to detect a modifier together
			// with a non-alphanumeric key across every terminal —
			// Ctrl+P above is the reliable path to Purge regardless of
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
func startDir() (string, error) {
	if len(os.Args) > 1 {
		return os.Args[1], nil
	}
	return os.Getwd()
}
