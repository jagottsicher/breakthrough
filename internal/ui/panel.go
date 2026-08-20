package ui

import (
	"path/filepath"
	"strings"

	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// Panel is the single directory-listing view for Phase 0: it shows the
// entries of one directory and lets the user navigate with the arrow keys
// (built into tview.List) and Enter. No menu, no right-click yet — that's
// later phases.
type Panel struct {
	*tview.List

	// path is the absolute path currently shown.
	path string
}

// NewPanel creates a Panel rooted at path.
func NewPanel(path string) (*Panel, error) {
	p := &Panel{
		List: tview.NewList().ShowSecondaryText(false),
	}
	p.SetBorder(true)

	if err := p.load(path); err != nil {
		return nil, err
	}
	p.SetSelectedFunc(p.onSelect)

	return p, nil
}

// load replaces the panel's contents with the entries of dir. It only
// mutates the panel's state (path, title, list items) once ListDir has
// succeeded, so a failed load leaves the panel showing whatever it showed
// before.
func (p *Panel) load(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	entries, err := fsops.ListDir(abs)
	if err != nil {
		return err
	}

	p.Clear()
	p.path = abs
	p.SetTitle(" " + abs + " ")

	if parent := filepath.Dir(abs); parent != abs {
		p.AddItem("..", "", 0, nil)
	}
	for _, e := range entries {
		label := e.Name
		if e.IsDir {
			label += "/"
		}
		p.AddItem(label, "", 0, nil)
	}

	return nil
}

// onSelect handles Enter (or a mouse click) on the currently highlighted
// item. Entering a directory reloads the panel there; a regular file is a
// no-op for now — opening/viewing files is a later phase.
func (p *Panel) onSelect(index int, mainText, secondaryText string, shortcut rune) {
	var target string
	switch {
	case mainText == "..":
		target = filepath.Dir(p.path)
	case strings.HasSuffix(mainText, "/"):
		target = filepath.Join(p.path, strings.TrimSuffix(mainText, "/"))
	default:
		return
	}

	// Errors (e.g. permission denied) are swallowed for now: Phase 0 has
	// no error dialog yet, and load() only mutates state on success, so
	// the panel simply stays on its current listing.
	_ = p.load(target)
}
