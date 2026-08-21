// Command breakthrough is the entry point of the breakthrough TUI file
// manager.
//
// Phase 0/1: a single panel showing the current directory, navigable with
// the arrow keys, Enter, and a browser-style path header (Start/Home/Back/
// Forward, clickable breadcrumbs, click-to-edit), plus a right-click
// context menu (Info, Rename). A second panel and further actions land in
// later, vertically-sliced feature branches (see docs/whitepaper.md for
// the overall concept).
//
// The initial directory is the process's working directory unless a path
// is given on the command line, e.g. "breakthrough /var/log" — see
// startDir. That directory also becomes the header's Start button target.
package main

import (
	"fmt"
	"os"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/ui"
)

func main() {
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

	app := tview.NewApplication().EnableMouse(true)

	root, err := ui.NewRoot(app, start)
	if err != nil {
		return err
	}

	// tcell puts the terminal in raw mode, so these arrive as regular key
	// events rather than signals. These are global captures (not tied to
	// any one primitive) so they work regardless of what currently has
	// focus.
	//
	// Ctrl+Q only opens a confirmation overlay rather than stopping
	// immediately, since a stray keypress shouldn't lose your place
	// without asking first. Ctrl+C deliberately does not quit at all —
	// it backs out of whatever is open, like Escape. Ctrl+X is
	// deliberately left unclaimed (available for a future binding),
	// having previously also quit.
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlQ:
			root.RequestQuit()
			return nil
		case tcell.KeyCtrlC:
			root.RequestCancel()
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
