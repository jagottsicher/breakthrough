package ui

import (
	"os"
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

const (
	headerDisplayPage = "display"
	headerEditPage    = "edit"
)

// Panel is the single directory-listing view for Phase 0/1: a one-line,
// browser-address-bar-style header above a list of entries. The header has
// Home/Back/Forward buttons followed by the path, whose components are
// individually clickable (clicking "b" in /a/b/c/d jumps to /a/b);
// clicking anywhere else in the header (e.g. the empty space after the
// path) switches it to a plain editable text field, like a browser URL
// bar. The list below is navigable with the arrow keys (built into
// tview.List) and Enter, plus (via Root) a right-click context menu. No
// borders anywhere — see accentBackgroundColor.
type Panel struct {
	*tview.Flex

	app *tview.Application

	headerPages *tview.Pages
	header      *tview.TextView   // display mode: buttons + path breadcrumbs
	headerEdit  *tview.InputField // edit mode: raw, freely editable path

	list *tview.List

	// path is the absolute path currently shown.
	path string

	// headerSpans locates each clickable region in the header's display
	// text (see buildHeaderSpans), rebuilt on every load().
	headerSpans []headerSpan

	// history is a browser-style navigation history: history[historyIdx]
	// is the current path. navigate() appends to it (truncating any
	// forward entries first); back()/forward() only move historyIdx.
	history    []string
	historyIdx int

	// editing is true while the header's edit field is shown. Root's
	// captureMouse calls captureOutsideEdit before its own logic (only
	// one SetMouseCapture can be installed on Panel, and Root already
	// owns that slot for right-click detection), so it needs this to
	// know whether a click landing outside the edit field should cancel
	// editing.
	editing bool
}

// headerAction identifies what a headerSpan does when clicked.
type headerAction int

const (
	actionNavigate headerAction = iota // go to target
	actionHome                         // go to the user's home directory
	actionBack                         // step back in history
	actionForward                      // step forward in history
)

// headerSpan is one clickable region in the header's display text:
// [start, end) is its half-open column range (relative to the header's
// own inner rect).
type headerSpan struct {
	start, end int
	action     headerAction
	target     string // only meaningful for actionNavigate
}

// NewPanel creates a Panel rooted at path. app is needed to move keyboard
// focus into the header's edit field on click and back to the list
// afterwards — see Panel.openEdit.
func NewPanel(app *tview.Application, path string) (*Panel, error) {
	p := &Panel{
		Flex: tview.NewFlex().SetDirection(tview.FlexRow),
		app:  app,
		list: tview.NewList().ShowSecondaryText(false),
	}
	p.list.SetHighlightFullLine(true)

	p.header = tview.NewTextView().SetTextColor(tcell.ColorWhite)
	p.header.SetWrap(false)
	p.header.SetBackgroundColor(accentBackgroundColor)
	p.header.SetMouseCapture(p.captureHeaderMouse)

	p.headerEdit = tview.NewInputField()
	p.headerEdit.SetFieldBackgroundColor(accentBackgroundColor)
	p.headerEdit.SetBackgroundColor(accentBackgroundColor)
	p.headerEdit.SetFieldTextColor(tcell.ColorWhite)
	p.headerEdit.SetDoneFunc(p.finishEdit)
	p.headerEdit.SetAutocompleteFunc(p.autocompletePath)
	p.headerEdit.SetAutocompleteStyles(
		accentBackgroundColor,
		tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(accentBackgroundColor),
		tcell.StyleDefault.Foreground(accentBackgroundColor).Background(tcell.ColorWhite),
	)

	p.headerPages = tview.NewPages()
	p.headerPages.AddPage(headerDisplayPage, p.header, true, true)
	p.headerPages.AddPage(headerEditPage, p.headerEdit, true, false)

	p.AddItem(p.headerPages, 1, 0, false) // fixed one-line header
	p.AddItem(p.list, 0, 1, true)         // fills the rest, holds focus

	if err := p.navigate(path); err != nil {
		return nil, err
	}
	p.list.SetSelectedFunc(p.onSelect)

	return p, nil
}

// load replaces the panel's contents with the entries of dir. It only
// mutates the panel's state (path, header, list items) once ListDir has
// succeeded, so a failed load leaves the panel showing whatever it showed
// before. It does not touch history — see navigate, back, forward.
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

// navigate is load plus history bookkeeping: on success it records the
// new path as the current history entry, discarding any "forward" entries
// beyond where we were — the same behavior a browser's address bar has.
// Every user-initiated jump (opening a directory, a breadcrumb click, the
// Home button, submitting the edit field) goes through this; back() and
// forward() call load() directly instead, since they must not themselves
// create new history entries.
func (p *Panel) navigate(dir string) error {
	if err := p.load(dir); err != nil {
		return err
	}

	if len(p.history) == 0 {
		p.history = []string{p.path}
		p.historyIdx = 0
		return nil
	}

	if p.history[p.historyIdx] == p.path {
		return nil // already there; don't push a duplicate entry
	}

	p.history = append(p.history[:p.historyIdx+1], p.path)
	p.historyIdx = len(p.history) - 1
	return nil
}

// back steps one entry back in history, if possible.
func (p *Panel) back() {
	if p.historyIdx <= 0 {
		return
	}
	p.historyIdx--
	// A failed load (e.g. the directory was since removed) leaves history
	// pointing at a path the panel no longer displays; Phase 1 has no
	// error dialog yet to surface that, so it's silently left as is.
	_ = p.load(p.history[p.historyIdx])
}

// forward steps one entry forward in history, if possible.
func (p *Panel) forward() {
	if p.historyIdx >= len(p.history)-1 {
		return
	}
	p.historyIdx++
	_ = p.load(p.history[p.historyIdx])
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

	_ = p.navigate(target)
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

// buildHeaderSpans renders the header's display text — Home/Back/Forward
// button glyphs followed by the path, one clickable span per path
// component (the leading "/" plus each name in between), e.g. clicking
// "b" in "/a/b/c/d" jumps to "/a/b". Column offsets are in runes, which is
// exact for the common case but, like the rest of Phase 0/1, doesn't yet
// account for double-width (e.g. CJK) characters in file names.
//
// A click that lands in the header but doesn't hit any of these spans
// (e.g. on a "/" separator, or in empty space after the path) is handled
// by captureHeaderMouse as "switch to edit mode" — deliberately not
// represented as a span here, since it's everything else.
func buildHeaderSpans(abs string) (text string, spans []headerSpan) {
	var b strings.Builder
	col := 0

	button := func(glyph string, action headerAction) {
		b.WriteByte(' ')
		col++
		start := col
		b.WriteString(glyph)
		col += len([]rune(glyph))
		spans = append(spans, headerSpan{start: start, end: col, action: action})
	}
	button("~", actionHome)
	button("<", actionBack)
	button(">", actionForward)
	b.WriteString("  ")
	col += 2

	rootStart := col
	b.WriteString("/")
	col++
	spans = append(spans, headerSpan{start: rootStart, end: col, action: actionNavigate, target: "/"})

	rest := strings.TrimPrefix(abs, "/")
	if rest != "" {
		current := ""
		parts := strings.Split(rest, "/")
		for i, part := range parts {
			current += "/" + part
			width := len([]rune(part))
			spans = append(spans, headerSpan{start: col, end: col + width, action: actionNavigate, target: current})
			b.WriteString(part)
			col += width
			if i < len(parts)-1 {
				b.WriteByte('/')
				col++
			}
		}
	}

	return b.String(), spans
}

// captureHeaderMouse drives the header's buttons and breadcrumbs, and
// falls back to opening the edit field for any other click within the
// header. It deliberately never lets tview's default TextView mouse
// handling run: that handler grabs focus on MouseLeftDown, which would
// silently break arrow-key navigation in the list below (it'd start
// scrolling the header's text instead). So every mouse event landing
// within the header's bounds is consumed here.
func (p *Panel) captureHeaderMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if !p.header.InRect(event.Position()) {
		return action, event
	}

	if action == tview.MouseLeftClick {
		x, _ := event.Position()
		rectX, _, _, _ := p.header.GetInnerRect()
		col := x - rectX

		if span, ok := p.spanAt(col); ok {
			p.runHeaderAction(span)
		} else {
			p.openEdit()
		}
	}

	return tview.MouseConsumed, nil
}

// spanAt returns the headerSpan covering column col, if any.
func (p *Panel) spanAt(col int) (headerSpan, bool) {
	for _, span := range p.headerSpans {
		if col >= span.start && col < span.end {
			return span, true
		}
	}
	return headerSpan{}, false
}

// runHeaderAction executes a clicked button or breadcrumb.
func (p *Panel) runHeaderAction(span headerSpan) {
	switch span.action {
	case actionHome:
		if home, err := os.UserHomeDir(); err == nil {
			_ = p.navigate(home)
		}
	case actionBack:
		p.back()
	case actionForward:
		p.forward()
	case actionNavigate:
		_ = p.navigate(span.target)
	}
}

// openEdit switches the header to its editable text field, pre-filled
// with the current path, and moves keyboard focus there.
func (p *Panel) openEdit() {
	p.editing = true
	p.headerEdit.SetText(p.path)
	p.headerPages.SwitchToPage(headerEditPage)
	p.app.SetFocus(p.headerEdit)
}

// closeEdit switches back to the display header and returns focus to the
// list. Shared by finishEdit's Enter case and cancelEdit.
func (p *Panel) closeEdit() {
	p.editing = false
	p.headerPages.SwitchToPage(headerDisplayPage)
	p.app.SetFocus(p.list)
}

// cancelEdit closes the edit field without navigating anywhere — used
// when a click lands outside it, the same way a browser's address bar
// reverts to the current URL instead of acting on whatever was typed.
func (p *Panel) cancelEdit() {
	p.closeEdit()
}

// captureOutsideEdit is called by Root.captureMouse (see the editing
// field's doc comment for why it can't just be Panel's own
// SetMouseCapture) before Root does anything else. If the header is being
// edited and the click landed outside the edit field, it cancels editing
// and reports that it handled the click, so Root doesn't also act on it
// (e.g. by opening the context menu on a stray right-click).
func (p *Panel) captureOutsideEdit(action tview.MouseAction, event *tcell.EventMouse) (consumed bool) {
	if !p.editing {
		return false
	}
	if action != tview.MouseLeftClick && action != tview.MouseRightClick {
		return false
	}
	x, y := event.Position()
	if primitiveContains(p.headerEdit, x, y) {
		return false // click landed on the edit field itself
	}

	p.cancelEdit()
	return true
}

// finishEdit handles the header edit field's special keys. Only Enter
// ends editing by submitting; a click outside the field (see
// captureOutsideEdit) is the only other way out — Escape and Backtab are
// deliberately no-ops here so they can't be hit by accident while typing
// a path. Tab triggers bash-style completion: SetAutocompleteFunc is only
// invoked here (and not by tview itself opening a drop-down) when Tab is
// pressed while no drop-down is currently open — once one is open, Tab
// instead selects the highlighted entry without ever reaching this
// function (see InputField's InputHandler).
func (p *Panel) finishEdit(key tcell.Key) {
	switch key {
	case tcell.KeyEnter:
		if typed := p.headerEdit.GetText(); typed != "" {
			_ = p.navigate(typed)
		}
		p.closeEdit()
	case tcell.KeyTab:
		p.headerEdit.Autocomplete()
	}
}

// autocompletePath is the header edit field's autocomplete callback:
// bash-style completion of the path component after the last "/" against
// the actual directory listing, returned as full strings ready to replace
// the field's entire text (matching InputField's default "replace on
// selection" behavior) — directories get a trailing "/" so completion can
// be chained into their contents.
func (p *Panel) autocompletePath(currentText string) []string {
	dir, prefix := "", currentText
	if idx := strings.LastIndex(currentText, "/"); idx >= 0 {
		dir, prefix = currentText[:idx+1], currentText[idx+1:]
	}

	lookupDir := dir
	if lookupDir == "" {
		lookupDir = "."
	}

	entries, err := fsops.ListDir(lookupDir)
	if err != nil {
		return nil
	}

	var matches []string
	lowerPrefix := strings.ToLower(prefix)
	for _, e := range entries {
		if !strings.HasPrefix(strings.ToLower(e.Name), lowerPrefix) {
			continue
		}
		name := e.Name
		if e.IsDir {
			name += "/"
		}
		matches = append(matches, dir+name)
	}
	return matches
}
