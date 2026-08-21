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

// Table columns. There is deliberately no header row labelling them (the
// path bar above already orients the user); these just index into
// table.SetCell/GetCell.
const (
	colCheckbox = iota
	colType
	colModifier
	colName
)

// Panel is the single directory-listing view for Phase 0/1: a one-line,
// browser-address-bar-style header above a table of entries. The header
// has Start/Home/Back/Forward buttons followed by the path, whose components
// are individually clickable (clicking "b" in /a/b/c/d jumps to /a/b);
// clicking anywhere else in the header (e.g. the empty space after the
// path) switches it to a plain editable text field, like a browser URL
// bar. The table below is navigable with the arrow keys (built into
// tview.Table) and Enter, has a clickable checkbox column for marking
// entries, and (via Root) a right-click context menu. No borders anywhere
// — see accentBackgroundColor.
//
// A tview.Table, not tview.List: a checkbox column needs a second column
// at all, which List doesn't have, and Table.CellAt/TableCell.Reference
// give exact hit-testing and per-row data for free — no more hand-rolled
// coordinate math (see RowAt, and contrast with Phase 1's since-removed
// EntryAt, which had to reimplement List's own unexported indexAtPoint).
type Panel struct {
	*tview.Flex

	app *tview.Application

	headerPages *tview.Pages
	header      *tview.TextView   // display mode: buttons + path breadcrumbs
	headerEdit  *tview.InputField // edit mode: raw, freely editable path

	table *tview.Table

	// path is the absolute path currently shown.
	path string

	// selected holds the absolute paths currently checked in the checkbox
	// column. Reset on every successful load() — selection is scoped to
	// the directory currently on screen, not carried across navigation,
	// matching how most file managers treat it.
	selected map[string]bool

	// headerSpans locates each clickable region in the header's display
	// text (see buildHeaderSpans), rebuilt on every load().
	headerSpans []headerSpan

	// history is a browser-style navigation history: history[historyIdx]
	// is the current path. navigate() appends to it (truncating any
	// forward entries first); back()/forward() only move historyIdx.
	history    []string
	historyIdx int

	// sortDescending and showHidden are session-scoped display
	// preferences, not per-directory state: load() re-applies whichever
	// is currently set on every call, including when navigating to a new
	// directory, so both stick as you browse rather than resetting each
	// time (see runHeaderAction's actionSortToggle and Root.toggleHidden).
	//
	// sortDescending reverses ListDir's alphabetical order within each of
	// its two groups (directories, then files) independently — see
	// reverseSortOrder — rather than reversing the whole listing, which
	// would also swap which group comes first.
	//
	// showHidden defaults to true (set in NewPanel) — dotfiles are shown
	// unless toggled off. When false, dotfile entries (name starting with
	// ".") are filtered out of the listing entirely in load() — see
	// filterHidden — rather than kept as rows that are merely skipped
	// elsewhere. That's what makes every row-based operation (selectAll,
	// selectByPattern, arrow-key navigation, ...) exclude them for free
	// once hidden, without each one needing its own "is this row actually
	// hidden right now" check.
	sortDescending bool
	showHidden     bool

	// onError reports failures the user should see (a directory that
	// can't be read, a refused rename) to whoever owns the UI's error
	// display — Root wires this to its error overlay. Panel deliberately
	// doesn't own that overlay itself: it has no business deciding how
	// errors are presented, only which ones are worth reporting.
	onError func(error)

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
	actionNavigate   headerAction = iota // go to target
	actionStart                          // go to the directory breakthrough was launched from
	actionHome                           // go to the user's home directory
	actionBack                           // step back in history
	actionForward                        // step forward in history
	actionSortToggle                     // flip ascending/descending sort order
)

// headerSpan is one clickable region in the header's display text:
// [start, end) is its half-open column range (relative to the header's
// own inner rect).
type headerSpan struct {
	start, end int
	action     headerAction
	target     string // only meaningful for actionNavigate
}

// rowRef is attached to each row's name cell via TableCell.SetReference,
// so a row's data can be read straight off the cell instead of being
// re-derived from its displayed text or its index.
type rowRef struct {
	path      string // absolute path; for the ".." row, its parent directory
	name      string // display name, ".." for the parent-directory row
	isDir     bool
	checkable bool // false for "..", which can't be a file operation target

	// entryType/linkTarget/nlink/mountPoint/mode mirror the corresponding
	// fsops.Entry fields — see typeGlyph and modifierGlyph, the only
	// things that read them, to render the type and modifier columns. The
	// ".." row gets a plain TypeDir with nothing else set (see load):
	// distinguishing whether going up crosses a filesystem boundary would
	// need an extra stat purely for that row, not worth it for what's
	// otherwise always just "..".
	entryType  fsops.EntryType
	linkTarget string
	nlink      uint64
	mountPoint bool
	mode       os.FileMode
}

// NewPanel creates a Panel rooted at path. app is needed to move keyboard
// focus into the header's edit field on click and back to the list
// afterwards — see Panel.openEdit.
func NewPanel(app *tview.Application, path string) (*Panel, error) {
	p := &Panel{
		Flex:       tview.NewFlex().SetDirection(tview.FlexRow),
		app:        app,
		table:      tview.NewTable(),
		showHidden: true, // default: dotfiles shown — see the field's own doc comment
	}
	p.table.SetBorders(false)
	p.table.SetSelectable(true, false) // whole rows, not individual cells
	p.table.SetSelectedFunc(func(row, column int) { p.activateRow(row) })
	p.table.SetInputCapture(p.captureTableKey) // space toggles the checkbox

	p.header = tview.NewTextView().SetTextColor(tcell.ColorWhite)
	p.header.SetWrap(false)
	p.header.SetBackgroundColor(accentBackgroundColor)
	p.header.SetMouseCapture(p.captureHeaderMouse)

	p.headerEdit = tview.NewInputField()
	p.headerEdit.SetFieldBackgroundColor(accentBackgroundColor)
	p.headerEdit.SetBackgroundColor(accentBackgroundColor)
	p.headerEdit.SetFieldTextColor(tcell.ColorWhite)
	p.headerEdit.SetDoneFunc(p.finishEdit)

	p.headerPages = tview.NewPages()
	p.headerPages.AddPage(headerDisplayPage, p.header, true, true)
	p.headerPages.AddPage(headerEditPage, p.headerEdit, true, false)

	p.AddItem(p.headerPages, 1, 0, false) // fixed one-line header
	p.AddItem(p.table, 0, 1, true)        // fills the rest, holds focus

	if err := p.navigate(path); err != nil {
		return nil, err
	}

	return p, nil
}

// load replaces the panel's contents with the entries of dir. It only
// mutates the panel's state (path, header, table rows) once ListDir has
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
	if !p.showHidden {
		entries = filterHidden(entries)
	}
	if p.sortDescending {
		reverseSortOrder(entries)
	}

	p.table.Clear()
	p.selected = make(map[string]bool)
	p.path = abs

	text, spans := buildHeaderSpans(abs, p.sortDescending)
	p.header.SetText(text)
	p.headerSpans = spans

	row := 0
	if parent := filepath.Dir(abs); parent != abs {
		p.addRow(row, rowRef{path: parent, name: "..", isDir: true, checkable: false, entryType: fsops.TypeDir})
		row++
	}
	for _, e := range entries {
		p.addRow(row, rowRef{
			path:       filepath.Join(abs, e.Name),
			name:       e.Name,
			isDir:      e.IsDir,
			checkable:  true,
			entryType:  e.Type,
			linkTarget: e.LinkTarget,
			nlink:      e.Nlink,
			mountPoint: e.MountPoint,
			mode:       e.Mode,
		})
		row++
	}

	return nil
}

// filterHidden removes dotfile entries (name starting with ".") from
// entries — load()'s effect when showHidden has been toggled off (the
// default is true — dotfiles shown — see Panel.showHidden). This
// happens before any row is ever added to the table, rather than adding
// a row that's then somehow marked hidden: every row-based operation
// (selectAll, selectByPattern, arrow-key navigation, ...) excludes a
// hidden entry for free that way, with no operation needing its own "is
// this one actually hidden right now" check.
func filterHidden(entries []fsops.Entry) []fsops.Entry {
	visible := entries[:0] // reuses entries' backing array; entries itself isn't read again after this call
	for _, e := range entries {
		if !strings.HasPrefix(e.Name, ".") {
			visible = append(visible, e)
		}
	}
	return visible
}

// reverseSortOrder reverses entries in place within each of ListDir's two
// already-sorted groups (directories, then files) independently, keeping
// directories first either way — load()'s effect when sortDescending is
// true. A plain whole-slice reverse would also swap which group comes
// first, which isn't what a sort-direction toggle is for.
func reverseSortOrder(entries []fsops.Entry) {
	split := len(entries)
	for i, e := range entries {
		if !e.IsDir {
			split = i
			break
		}
	}
	reverseEntries(entries[:split])
	reverseEntries(entries[split:])
}

func reverseEntries(entries []fsops.Entry) {
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
}

// addRow renders one table row for ref at the given row index.
func (p *Panel) addRow(row int, ref rowRef) {
	checkbox := tview.NewTableCell(checkboxText(false)).SetTextColor(tcell.ColorWhite)
	if !ref.checkable {
		checkbox.SetText(" ")
	} else {
		checkbox.SetClickedFunc(func() bool {
			p.toggleCheckbox(row)
			return false // also let the row become selected/highlighted,
			// the same visible feedback a click anywhere else in the row
			// gives — a checked/unchecked glyph flipping is easy to miss
			// on its own, especially if the click landed a column off.
		})
	}
	p.table.SetCell(row, colCheckbox, checkbox)

	color := entryColor(ref)

	typeCell := tview.NewTableCell(string(typeGlyph(ref))).SetTextColor(color)
	p.table.SetCell(row, colType, typeCell)

	modCell := tview.NewTableCell(string(modifierGlyph(ref))).SetTextColor(tcell.ColorWhite)
	p.table.SetCell(row, colModifier, modCell)

	label := ref.name
	if ref.entryType == fsops.TypeDir {
		label += "/"
	}
	if ref.linkTarget != "" {
		label += " -> " + ref.linkTarget
	}
	name := tview.NewTableCell(label).SetTextColor(color)
	name.SetReference(ref)
	name.SetExpansion(1) // consume the rest of the row's width
	name.SetClickedFunc(func() bool {
		p.activateRow(row)
		return false // still let the row become selected/highlighted
	})
	p.table.SetCell(row, colName, name)
}

// sortGlyph renders the header's sort-toggle button — an arrow showing
// the currently active direction (rather than a fixed icon), so the
// button doubles as an indicator, not just a control. Plain arrows
// (U+2191/U+2193), not the checkbox's circles: those are Unicode
// "ambiguous width" and only got a pass because the user explicitly
// wanted circles there; there's no reason to take on that same rendering
// risk again here when a narrow, universally single-width alternative
// exists.
func sortGlyph(descending bool) string {
	if descending {
		return "↓"
	}
	return "↑"
}

// checkboxText renders the checkbox column's two states as an outline vs.
// filled circle (○/●) — breakthrough already commits to UTF-8 support
// (see docs/whitepaper.md). A blank space for "unchecked" was considered
// and rejected: it would make the checkbox column invisible whenever a
// row isn't checked, losing the "there's something clickable here" cue
// entirely rather than just being subtle about it (see the ballot-box
// glyphs this replaced, and addRow's now-highlight-on-click for the
// other half of that fix).
func checkboxText(checked bool) string {
	if checked {
		return "●"
	}
	return "○"
}

// typeGlyph renders ref's type character, matching Midnight Commander's
// own single-character "type" panel field exactly (confirmed against
// source.midnight-commander.org/man/mc.html — not guessed): '*' for an
// executable file, '/' directory, '~' symlink to a directory, '@'
// symlink to a file, '!' a broken symlink, '=' socket, '|' FIFO, '-'
// character device, '+' block device. A plain, non-executable file gets
// no character at all (blank), same as MC.
//
// This is deliberately just the one character MC itself uses — no
// modifier tacked on, unlike an earlier version of this column that
// packed a mount-point/hard-link marker in as a second character next to
// it: the two glyphs then had no gap between them (e.g. "~>"), reading as
// cramped compared to every other row's single character plus real
// column spacing. See modifierGlyph, colModifier for where that
// information moved instead: its own column, so the table's own
// inter-column gap does the separating rather than a hand-picked
// character.
func typeGlyph(ref rowRef) byte {
	switch ref.entryType {
	case fsops.TypeDir:
		return '/'
	case fsops.TypeSymlinkDir:
		return '~'
	case fsops.TypeSymlinkFile:
		return '@'
	case fsops.TypeSymlinkBroken:
		return '!'
	case fsops.TypeSocket:
		return '='
	case fsops.TypeFIFO:
		return '|'
	case fsops.TypeCharDevice:
		return '-'
	case fsops.TypeBlockDevice:
		return '+'
	case fsops.TypeFile:
		if ref.mode&0o111 != 0 { // any executable bit (owner, group, or other)
			return '*'
		}
	}
	return ' '
}

// entryColor sets a row's type character and name apart by color for the
// two cases worth flagging beyond the glyph alone — a broken symlink in
// red (something that will fail if acted on) and an executable file in
// green, matching Midnight Commander's own default skin, which colors an
// executable's whole name, not just its '*'. Applied to both the type
// cell and the name cell (see addRow) for the same reason MC colors the
// whole entry rather than a lone prefix character: it reads at a glance
// across the row, not just in the narrow type column.
func entryColor(ref rowRef) tcell.Color {
	switch {
	case ref.entryType == fsops.TypeSymlinkBroken:
		return tcell.ColorRed
	case ref.entryType == fsops.TypeFile && ref.mode&0o111 != 0:
		return tcell.ColorGreen
	default:
		return tcell.ColorWhite
	}
}

// modifierGlyph renders ref's modifier column — information MC's own
// type field doesn't carry at all, so unlike typeGlyph this isn't
// borrowed from anywhere, just breakthrough's own addition: '>' for a
// mount point (a directory, or directory symlink, sitting on a different
// filesystem than the one being listed — a separate partition, an NFS
// share, an fstab bind mount, ...), or '&' for a plain file with more
// than one hard link (content that also exists under another name — see
// rowRef's own doc comment on why that's not tracked for ".."). The two
// never apply to the same entry, so one column comfortably covers both.
func modifierGlyph(ref rowRef) byte {
	switch {
	case ref.mountPoint && (ref.entryType == fsops.TypeDir || ref.entryType == fsops.TypeSymlinkDir):
		return '>'
	case ref.entryType == fsops.TypeFile && ref.nlink > 1:
		return '&'
	}
	return ' '
}

// rowRef returns the rowRef attached to row's name cell, if any.
func (p *Panel) rowRef(row int) (rowRef, bool) {
	cell := p.table.GetCell(row, colName)
	if cell == nil {
		return rowRef{}, false
	}
	ref, ok := cell.GetReference().(rowRef)
	return ref, ok
}

// toggleCheckbox flips whether row's entry is selected and updates the
// checkbox cell's text to match. A no-op for the ".." row (checkable
// false) or a row with no rowRef at all — the latter shouldn't be
// reachable, but this stays defensive rather than assume it. The ".."
// guard matters here specifically because, unlike a plain mouse click,
// captureTableKey's Space shortcut reaches this directly regardless of
// which row is currently selected.
func (p *Panel) toggleCheckbox(row int) {
	ref, ok := p.rowRef(row)
	if !ok || !ref.checkable {
		return
	}
	p.setChecked(row, !p.selected[ref.path])
}

// setChecked sets row's checkbox state directly to checked (rather than
// flipping it, as toggleCheckbox does) and updates the cell's text to
// match. Shares toggleCheckbox's guards, and is what it's built on top
// of; also used directly by selectAll/deselectAll/selectByPattern, which
// all need to set a specific target state rather than toggle.
func (p *Panel) setChecked(row int, checked bool) {
	ref, ok := p.rowRef(row)
	if !ok || !ref.checkable {
		return
	}

	if checked {
		p.selected[ref.path] = true
	} else {
		delete(p.selected, ref.path)
	}

	if cell := p.table.GetCell(row, colCheckbox); cell != nil {
		cell.SetText(checkboxText(checked))
	}
}

// selectAll checks every checkable row in the current listing — the
// context menu's "Select all".
func (p *Panel) selectAll() {
	for row := 0; row < p.table.GetRowCount(); row++ {
		p.setChecked(row, true)
	}
}

// deselectAll unchecks every row in the current listing — the context
// menu's "Deselect all".
func (p *Panel) deselectAll() {
	for row := 0; row < p.table.GetRowCount(); row++ {
		p.setChecked(row, false)
	}
}

// selectByPattern sets checked on every row whose name matches pattern —
// shell glob syntax, as filepath.Match understands it (the same
// convention the header's path completion already uses). Backs the
// context menu's "Select +" (checked=true) and "Select -" (checked=
// false), the classic file-manager pair for marking/unmarking a whole
// group by wildcard instead of clicking each entry. Returns the number of
// rows that matched; an error is only ever a malformed pattern
// (filepath.ErrBadPattern) — matching nothing is not an error, just zero.
func (p *Panel) selectByPattern(pattern string, checked bool) (matched int, err error) {
	for row := 0; row < p.table.GetRowCount(); row++ {
		ref, ok := p.rowRef(row)
		if !ok || !ref.checkable {
			continue
		}
		hit, err := filepath.Match(pattern, ref.name)
		if err != nil {
			return matched, err
		}
		if hit {
			p.setChecked(row, checked)
			matched++
		}
	}
	return matched, nil
}

// SelectedPaths returns the absolute paths currently checked, in no
// particular order. Used by Root's Copy/Cut to capture what the clipboard
// should act on.
func (p *Panel) SelectedPaths() []string {
	paths := make([]string, 0, len(p.selected))
	for path := range p.selected {
		paths = append(paths, path)
	}
	return paths
}

// captureTableKey handles the one key the table needs beyond its built-in
// navigation: Space toggles the checkbox on the currently selected row,
// the same action a click on that row's checkbox performs.
func (p *Panel) captureTableKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyRune && event.Rune() == ' ' {
		row, _ := p.table.GetSelection()
		p.toggleCheckbox(row)
		return nil
	}
	return event
}

// activateRow is what Enter and a click on the name cell both do: enter
// the row's directory, or do nothing for a regular file — opening/viewing
// files is a later phase. Ignores the checkbox column: a click there is
// handled by its own TableCell.ClickedFunc instead (see addRow), which
// returns true specifically so this never also runs for it.
func (p *Panel) activateRow(row int) {
	ref, ok := p.rowRef(row)
	if !ok || !ref.isDir {
		return
	}
	p.reportError(p.navigate(ref.path))
}

// nameCellRect returns the on-screen position and width row's name cell
// occupied the last time the table was drawn (see TableCell.GetLastPosition),
// or ok=false if row doesn't exist. Used to position the rename field
// exactly over the name column without also covering the checkbox
// column, so the checkbox stays visible — showing what's otherwise
// selected — while renaming, without becoming part of what's editable.
func (p *Panel) nameCellRect(row int) (x, y, width int, ok bool) {
	if _, ok := p.rowRef(row); !ok {
		return 0, 0, 0, false
	}
	x, y, width = p.table.GetCell(row, colName).GetLastPosition()
	return x, y, width, true
}

// RowAt returns the absolute path of the entry at screen position (x, y),
// or ok=false if that position isn't a selectable entry — outside the
// table, past the last row, or the ".." row, which isn't a file operation
// target. Used by Root to find which entry was right-clicked.
func (p *Panel) RowAt(x, y int) (path string, ok bool) {
	row, ok := p.rowIndexAt(x, y)
	if !ok {
		return "", false
	}
	ref, _ := p.rowRef(row) // rowIndexAt already confirmed this succeeds
	if ref.name == ".." {
		return "", false
	}
	return ref.path, true
}

// rowIndexAt returns the row index at screen position (x, y), or
// ok=false if that position doesn't correspond to a populated row.
// Unlike RowAt, this doesn't exclude "..": a caller working with a
// contiguous range of rows (see selectRange) needs the real index either
// way, and toggling ".." is already a no-op (not checkable).
func (p *Panel) rowIndexAt(x, y int) (row int, ok bool) {
	row, _ = p.table.CellAt(x, y)
	if _, ok := p.rowRef(row); !ok {
		return 0, false
	}
	return row, true
}

// focusRow moves the table's own current-row highlight — and, via arrow
// keys, where keyboard navigation continues from — to row. This is
// distinct from the checkbox column's per-entry marking (see selected,
// toggleCheckbox): a right-click or right-drag on a row that wasn't
// already the highlighted one would otherwise leave the highlight
// pointing somewhere else while the context menu or the just-toggled
// checkboxes are clearly about a different row.
func (p *Panel) focusRow(row int) {
	p.table.Select(row, colName)
}

// applyDragDelta toggles exactly the rows whose membership in the range
// [start, to] differs from their membership in [start, from] — the rows
// a right-button drag has newly entered or newly left since the last
// update, each toggled exactly once. This is what lets Root.captureMouse
// update the toggled state live, once per MouseMove, without re-toggling
// (and so silently cancelling) rows the drag already passed over: calling
// this repeatedly as the range grows, shrinks, or reverses direction
// always leaves each row's state consistent with whether it's currently
// inside [start, to], not with how many times a naive re-toggle-the-whole-
// range would have flipped it. toggleCheckbox already no-ops for a row
// that isn't checkable (the ".." row, if it falls within either range),
// so that's handled for free here too.
func (p *Panel) applyDragDelta(start, from, to int) {
	oldLo, oldHi := min(start, from), max(start, from)
	newLo, newHi := min(start, to), max(start, to)

	for row := min(oldLo, newLo); row <= max(oldHi, newHi); row++ {
		inOld := row >= oldLo && row <= oldHi
		inNew := row >= newLo && row <= newHi
		if inOld != inNew {
			p.toggleCheckbox(row)
		}
	}
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

// reportError hands err to whoever is displaying errors, if anyone is.
// A nil error is ignored, so callers can pass a result through directly.
func (p *Panel) reportError(err error) {
	if err != nil && p.onError != nil {
		p.onError(err)
	}
}

// back steps one entry back in history, if possible.
//
// The index only moves once the load has actually succeeded: if the
// directory has since been deleted, moving it anyway would leave
// historyIdx pointing at a path the panel isn't showing, and the next
// navigate() would then truncate the forward entries from the wrong
// position — silently dropping a page the user never left.
func (p *Panel) back() {
	if p.historyIdx <= 0 {
		return
	}
	if err := p.load(p.history[p.historyIdx-1]); err != nil {
		p.reportError(err) // stay put rather than lie about where we are
		return
	}
	p.historyIdx--
}

// forward steps one entry forward in history, if possible. As in back(),
// the index only moves once the load has succeeded.
func (p *Panel) forward() {
	if p.historyIdx >= len(p.history)-1 {
		return
	}
	if err := p.load(p.history[p.historyIdx+1]); err != nil {
		p.reportError(err)
		return
	}
	p.historyIdx++
}

// buildHeaderSpans renders the header's display text — Start/Home/Back/
// Forward/sort-toggle button glyphs followed by the path, one clickable
// span per path component (the leading "/" plus each name in between),
// e.g. clicking "b" in "/a/b/c/d" jumps to "/a/b". Column offsets are in
// runes, which is exact for the common case but, like the rest of
// Phase 0/1, doesn't yet account for double-width (e.g. CJK) characters
// in file names.
//
// sortDescending picks the sort-toggle button's glyph (see sortGlyph) —
// passed in explicitly, the same as abs, rather than read off a Panel
// receiver, so this stays a pure function callers can test directly with
// arbitrary input instead of having to construct a whole Panel first.
//
// A click that lands in the header but doesn't hit any of these spans
// (e.g. on a "/" separator, or in empty space after the path) is handled
// by captureHeaderMouse as "switch to edit mode" — deliberately not
// represented as a span here, since it's everything else.
func buildHeaderSpans(abs string, sortDescending bool) (text string, spans []headerSpan) {
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
	button("^", actionStart)
	button("~", actionHome)
	button("<", actionBack)
	button(">", actionForward)
	button(sortGlyph(sortDescending), actionSortToggle)
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
	case actionStart:
		// history[0] is the first directory this Panel ever successfully
		// loaded — NewPanel's initial navigate() call sets it and nothing
		// afterwards ever removes it, so it's a stable record of where
		// breakthrough started, independent of the OS home directory.
		p.reportError(p.navigate(p.history[0]))
	case actionHome:
		home, err := os.UserHomeDir()
		if err != nil {
			p.reportError(err)
			return
		}
		p.reportError(p.navigate(home))
	case actionBack:
		p.back()
	case actionForward:
		p.forward()
	case actionSortToggle:
		// A re-render of the directory already on screen with a
		// different display preference, not navigation — load() directly
		// rather than navigate(), so this doesn't push a history entry or
		// otherwise touch back/forward.
		p.sortDescending = !p.sortDescending
		p.reportError(p.load(p.path))
	case actionNavigate:
		p.reportError(p.navigate(span.target))
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
// table. Shared by finishEdit's Enter case and cancelEdit.
func (p *Panel) closeEdit() {
	p.editing = false
	p.headerPages.SwitchToPage(headerDisplayPage)
	p.app.SetFocus(p.table)
}

// cancelEdit closes the edit field without navigating anywhere — used
// when a click lands outside it, the same way a browser's address bar
// reverts to the current URL instead of acting on whatever was typed.
// Safe to call unconditionally: it does nothing if no edit is in
// progress.
func (p *Panel) cancelEdit() {
	if !p.editing {
		return
	}
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
// a path. Tab completes the path in place, bash-style.
func (p *Panel) finishEdit(key tcell.Key) {
	switch key {
	case tcell.KeyEnter:
		if typed := p.headerEdit.GetText(); typed != "" {
			p.reportError(p.navigate(p.resolvePath(typed)))
		}
		p.closeEdit()
	case tcell.KeyTab:
		p.completePath()
	}
}

// completePath is the Tab handler: it extends whatever is typed to the
// longest unambiguous completion, exactly like bash does — a single match
// completes fully, several matches complete as far as they agree, and no
// match leaves the text alone.
//
// This deliberately does not use tview's InputField autocomplete
// (SetAutocompleteFunc), even though it exists: that renders a drop-down
// *outside* the field's own rect, which the header's mouse capture would
// have to know about to avoid swallowing clicks meant for it, and its
// callback fires on every keystroke — a full directory read per typed
// character. Completing in place has neither problem and is closer to
// what bash actually does.
func (p *Panel) completePath() {
	matches := p.completions(p.headerEdit.GetText())
	if len(matches) == 0 {
		return
	}
	p.headerEdit.SetText(longestCommonPrefix(matches))
}

// completions returns the possible expansions of currentText: the path
// component after the last "/" matched, case-insensitively, against the
// real contents of the directory before it. Directories come back with a
// trailing "/" so completion chains into them. Results keep the text the
// user typed (including a leading "~" or a relative path) rather than the
// resolved absolute form, so completing doesn't rewrite the field into
// something the user didn't type.
func (p *Panel) completions(currentText string) []string {
	dir, prefix := "", currentText
	if idx := strings.LastIndex(currentText, "/"); idx >= 0 {
		dir, prefix = currentText[:idx+1], currentText[idx+1:]
	}

	entries, err := fsops.ListDir(p.resolvePath(dir))
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

// resolvePath turns text typed into the header into an absolute path: a
// leading "~" expands to the user's home directory (the header has a "~"
// button doing the same thing, so users reasonably expect it), and a
// relative path resolves against the directory the panel is currently
// showing — not the process's working directory, which stops matching
// what the user sees the moment they navigate anywhere.
func (p *Panel) resolvePath(input string) string {
	switch {
	case input == "~":
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	case strings.HasPrefix(input, "~/"):
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, input[len("~/"):])
		}
	}

	if filepath.IsAbs(input) {
		return input
	}
	return filepath.Join(p.path, input)
}

// longestCommonPrefix returns the longest prefix shared by all values,
// compared by rune so it can never cut a multi-byte character in half.
func longestCommonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}

	prefix := []rune(values[0])
	for _, value := range values[1:] {
		runes := []rune(value)
		if len(runes) < len(prefix) {
			prefix = prefix[:len(runes)]
		}
		for i := range prefix {
			if prefix[i] != runes[i] {
				prefix = prefix[:i]
				break
			}
		}
	}
	return string(prefix)
}
