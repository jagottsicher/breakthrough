package ui

import (
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// accentBackgroundColor sets the header bar and floating elements (context
// menu, rename prompt) apart from the plain listing by background color —
// deliberately chosen over a box-drawing border, which reads as dated.
const accentBackgroundColor = tcell.ColorDarkSlateGray

// Panel is the single directory-listing view for Phase 0/1: a one-line
// path header above a list of entries, navigable with the arrow keys
// (built into tview.List) and Enter, plus (via Root) a right-click
// context menu. No borders anywhere — see accentBackgroundColor.
type Panel struct {
	*tview.Flex

	header *tview.TextView
	list   *tview.List

	// path is the absolute path currently shown.
	path string

	// headerSpans locates each clickable path component within the
	// header's text (see buildHeaderSpans), rebuilt on every load().
	headerSpans []headerSpan
}

// headerSpan is one clickable path component in the header: [start, end)
// is its half-open column range within the header's text (relative to the
// header's own inner rect), target is the path clicking it navigates to.
type headerSpan struct {
	start, end int
	target     string
}

// NewPanel creates a Panel rooted at path.
func NewPanel(path string) (*Panel, error) {
	p := &Panel{
		Flex:   tview.NewFlex().SetDirection(tview.FlexRow),
		header: tview.NewTextView().SetTextColor(tcell.ColorWhite),
		list:   tview.NewList().ShowSecondaryText(false),
	}
	p.header.SetBackgroundColor(accentBackgroundColor)
	p.header.SetMouseCapture(p.captureHeaderMouse)
	p.list.SetHighlightFullLine(true)

	p.AddItem(p.header, 1, 0, false) // fixed one-line header
	p.AddItem(p.list, 0, 1, true)    // fills the rest, holds focus

	if err := p.load(path); err != nil {
		return nil, err
	}
	p.list.SetSelectedFunc(p.onSelect)

	return p, nil
}

// load replaces the panel's contents with the entries of dir. It only
// mutates the panel's state (path, header, list items) once ListDir has
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

	p.list.Clear()
	p.path = abs

	text, spans := buildHeaderSpans(abs)
	p.header.SetText(text)
	p.headerSpans = spans

	if parent := filepath.Dir(abs); parent != abs {
		p.list.AddItem("..", "", 0, nil)
	}
	for _, e := range entries {
		label := e.Name
		if e.IsDir {
			label += "/"
		}
		p.list.AddItem(label, "", 0, nil)
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

// EntryAt returns the item name shown at screen row y, or ok=false if y
// is outside the list or doesn't correspond to an item.
//
// tview.List has an equivalent indexAtPoint, but it's unexported, so this
// reimplements it for the fixed configuration this Panel always uses
// (single-line items, i.e. ShowSecondaryText(false) — see NewPanel).
func (p *Panel) EntryAt(y int) (name string, ok bool) {
	_, rectY, _, height := p.list.GetInnerRect()
	if y < rectY || y >= rectY+height {
		return "", false
	}

	offset, _ := p.list.GetOffset()
	index := y - rectY + offset
	if index < 0 || index >= p.list.GetItemCount() {
		return "", false
	}

	main, _ := p.list.GetItemText(index)
	return main, true
}

// buildHeaderSpans renders abs as " " + abs (unchanged from before) and
// computes one clickable span per path component — the leading "/" plus
// each name in between — so that e.g. clicking "b" in "/a/b/c/d" jumps to
// "/a/b". Column offsets are in runes, which is exact for the common case
// but, like the rest of Phase 0/1, doesn't yet account for double-width
// (e.g. CJK) characters in file names.
func buildHeaderSpans(abs string) (text string, spans []headerSpan) {
	const prefix = " "
	text = prefix + abs

	col := len([]rune(prefix))
	spans = append(spans, headerSpan{start: col, end: col + 1, target: "/"})
	col++

	rest := strings.TrimPrefix(abs, "/")
	if rest == "" {
		return text, spans
	}

	current := ""
	for _, part := range strings.Split(rest, "/") {
		current += "/" + part
		width := len([]rune(part))
		spans = append(spans, headerSpan{start: col, end: col + width, target: current})
		col += width + 1 // + 1 for the "/" separator before the next part
	}

	return text, spans
}

// captureHeaderMouse makes the path header's components clickable without
// letting tview's default TextView mouse handling run at all: that
// handler grabs focus on MouseLeftDown, which would silently break
// arrow-key navigation in the list below (it'd start scrolling the
// header's text instead). So every mouse event landing within the
// header's bounds is consumed here — acted on if it's a left click on a
// component, otherwise just swallowed.
func (p *Panel) captureHeaderMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if !p.header.InRect(event.Position()) {
		return action, event
	}

	if action == tview.MouseLeftClick {
		x, _ := event.Position()
		rectX, _, _, _ := p.header.GetInnerRect()
		col := x - rectX

		for _, span := range p.headerSpans {
			if col >= span.start && col < span.end {
				_ = p.load(span.target)
				break
			}
		}
	}

	return tview.MouseConsumed, nil
}
