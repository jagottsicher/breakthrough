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

	root, err := ui.NewRoot(start)
	if err != nil {
		return err
	}

	app := tview.NewApplication().EnableMouse(true)
	return app.SetRoot(root, true).Run()
}
