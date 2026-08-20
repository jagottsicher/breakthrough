// Command breakthrough is the entry point of the breakthrough TUI file
// manager.
//
// Phase 0/1: a single panel showing the current directory, navigable with
// the arrow keys and Enter, plus a right-click context menu with exactly
// one action (Rename). Further actions and a second panel land in later,
// vertically-sliced feature branches (see docs/whitepaper.md for the
// overall concept).
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
	start, err := os.Getwd()
	if err != nil {
		return err
	}

	app := tview.NewApplication().EnableMouse(true)

	root, err := ui.NewRoot(app, start)
	if err != nil {
		return err
	}

	// tcell puts the terminal in raw mode, so Ctrl+X arrives as a regular
	// key event instead of a signal — without this, there would be no way
	// to quit at all. This is a global capture (not tied to any one
	// primitive) so it works regardless of what currently has focus. It
	// only opens a confirmation overlay (Root.RequestQuit) rather than
	// stopping immediately, since a stray Ctrl+X shouldn't lose your
	// place without asking first.
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlX {
			root.RequestQuit()
			return nil
		}
		return event
	})

	return app.SetRoot(root, true).Run()
}
