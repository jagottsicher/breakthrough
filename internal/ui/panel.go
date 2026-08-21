package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

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

// Table columns — colSize/colModified are also mirrored, cell for cell,
// in columnHeader (see buildColumnHeader): the icon columns get a blank
// header cell (self-explanatory via their own glyphs), Name/Size/
// Modified get a clickable label that sorts by that column.
const (
	colCheckbox = iota
	colType
	colModifier
	colName
	colSizeSep // a bare "│" — see columnSeparator — not a real data column
	colSize
	colModifiedSep // likewise, before colModified
	colModified
)

// Panel is the single directory-listing view: a one-line, browser-
// address-bar-style path header above a table of entries. The path
// header has Start/Home/Back/Forward buttons followed by the path, whose
// components are individually clickable (clicking "b" in /a/b/c/d jumps
// to /a/b); clicking anywhere else in it (e.g. the empty space after the
// path) switches it to a plain editable text field, like a browser URL
// bar. Below that sits a second, one-line column-header row (see
// columnHeader/buildColumnHeader) labelling Name/Size/Modified — clicking
// one sorts by it. The table itself is navigable with the arrow keys
// (built into tview.Table) and Enter, has a clickable checkbox column for
// marking entries, and (via Root) a right-click context menu. No borders
// anywhere — see accentBackgroundColor.
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

	// columnHeader is a second, single-row table sitting between the path
	// bar and the data table (table) — its own doc comment (see
	// buildColumnHeader) explains why a second table, not a shared row 0
	// of table itself (tview.Table.SetFixed would do that, but at the
	// cost of shifting every existing row index used throughout this
	// package and its tests by one).
	columnHeader *tview.Table
	table        *tview.Table

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

	// sortKey/sortDescending, sizeBytes/mtimeUnix, and showHidden are all
	// session-scoped display preferences, not per-directory state:
	// load() re-applies whichever is currently set on every call,
	// including when navigating to a new directory, so all of them stick
	// as you browse rather than resetting each time.
	//
	// sortKey/sortDescending pick which of ListDir's fields to sort by
	// and which direction — see applySortPreference, and
	// buildColumnHeader/setSortKey for how a column-header click changes
	// them. Directories always stay grouped first either way; only the
	// order within that group (and within the files that follow) changes.
	//
	// sizeBytes/mtimeUnix pick the Size/Modified columns' display format
	// — see formatSizeCell/formatModTimeCell — toggled via Root's
	// "Globals" menu (Root.toggleSizeBytes/toggleMtimeUnix), not from the
	// column header itself: a click there is unambiguously "sort by this
	// column", with no separate click zone competing for the same cell.
	//
	// showHidden defaults to true (set in NewPanel) — dotfiles are shown
	// unless toggled off. When false, dotfile entries (name starting with
	// ".") are filtered out of the listing entirely in load() — see
	// filterHidden — rather than kept as rows that are merely skipped
	// elsewhere. That's what makes every row-based operation (selectAll,
	// selectByPattern, arrow-key navigation, ...) exclude them for free
	// once hidden, without each one needing its own "is this row actually
	// hidden right now" check.
	sortKey        sortKey
	sortDescending bool
	sizeBytes      bool
	mtimeUnix      bool
	showHidden     bool

	// onError reports failures the user should see (a directory that
	// can't be read, a refused rename) to whoever owns the UI's error
	// display — Root wires this to its error overlay. Panel deliberately
	// doesn't own that overlay itself: it has no business deciding how
	// errors are presented, only which ones are worth reporting.
	onError func(error)

	// onLoad reports every successful load() (navigation, a hidden-files
	// toggle, anything that reloads the directory currently on screen) —
	// Root wires this to refresh the status bar's disk-usage display,
	// which depends on the current directory. Not called for the very
	// first load (inside NewPanel, before Root has anything to wire this
	// to yet) — Root re-syncs once explicitly right after wiring it
	// instead, the same "defensive initial sync" showMenu already does
	// for its own menu labels.
	onLoad func(path string)

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
	actionStart                        // go to the directory breakthrough was launched from
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

	// size/modTime mirror fsops.Entry's own fields, for the Size/Modified
	// columns (see addRow/formatSizeCell/formatModTimeCell).
	size    int64
	modTime time.Time
}

// NewPanel creates a Panel rooted at path. app is needed to move keyboard
// focus into the header's edit field on click and back to the list
// afterwards — see Panel.openEdit.
func NewPanel(app *tview.Application, path string) (*Panel, error) {
	p := &Panel{
		Flex:         tview.NewFlex().SetDirection(tview.FlexRow),
		app:          app,
		table:        tview.NewTable(),
		columnHeader: tview.NewTable(),
		showHidden:   true, // default: dotfiles shown — see the field's own doc comment
	}
	p.table.SetBorders(false)
	p.table.SetSelectable(true, false) // whole rows, not individual cells
	p.table.SetSelectedFunc(func(row, column int) { p.activateRow(row) })
	p.table.SetInputCapture(p.captureTableKey) // space toggles the checkbox

	p.columnHeader.SetBorders(false)
	p.columnHeader.SetBackgroundColor(accentBackgroundColor)
	p.columnHeader.SetSelectable(false, false) // labels only, not a second navigable row

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

	p.AddItem(p.headerPages, 1, 0, false)  // fixed one-line path bar
	p.AddItem(p.columnHeader, 1, 0, false) // fixed one-line column header
	p.AddItem(p.table, 0, 1, true)         // fills the rest, holds focus

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
	applySortPreference(entries, p.sortKey, p.sortDescending)

	p.table.Clear()
	p.selected = make(map[string]bool)
	p.path = abs

	text, spans := buildHeaderSpans(abs)
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
			size:       e.Size,
			modTime:    e.ModTime,
		})
		row++
	}

	// After the rows, not before: buildColumnHeader's checkbox reflects
	// allSelected(), which reads the table's own rows — right now that's
	// moot (selected was just reset above, so allSelected() is false
	// either way), but building it from the real, populated table is the
	// robust way to state that instead of relying on that always holding.
	p.buildColumnHeader()

	if p.onLoad != nil {
		p.onLoad(p.path)
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

// sortKey picks which of an Entry's fields Panel sorts by — see
// applySortPreference and buildColumnHeader/setSortKey (a column-header
// click).
type sortKey int

const (
	sortByName sortKey = iota
	sortBySize
	sortByModified
)

// applySortPreference reorders entries in place: directories first
// (ListDir's own grouping, kept regardless of key/direction — merging
// files and directories into one order isn't what any of these keys are
// for), then by key within each group, in the given direction.
func applySortPreference(entries []fsops.Entry, key sortKey, descending bool) {
	split := len(entries)
	for i, e := range entries {
		if !e.IsDir {
			split = i
			break
		}
	}
	sortGroup(entries[:split], key, descending)
	sortGroup(entries[split:], key, descending)
}

// sortGroup sorts one directories-or-files group by key, breaking ties
// (and doing the entire comparison, for sortByName itself) by name — the
// same case-insensitive comparison ListDir already sorted the group by,
// so a sortByName, ascending call here is a no-op on already-sorted
// input rather than doing needless work.
func sortGroup(entries []fsops.Entry, key sortKey, descending bool) {
	less := func(i, j int) bool {
		switch key {
		case sortBySize:
			if entries[i].Size != entries[j].Size {
				return entries[i].Size < entries[j].Size
			}
		case sortByModified:
			if !entries[i].ModTime.Equal(entries[j].ModTime) {
				return entries[i].ModTime.Before(entries[j].ModTime)
			}
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	}
	if descending {
		asc := less
		less = func(i, j int) bool { return asc(j, i) }
	}
	sort.SliceStable(entries, less)
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

	// ".." (checkable false) has no real Entry behind it, so ref.size/
	// modTime are just zero values — blank cells instead of formatting
	// those into a nonsense "0B"/"0001-01-01 00:00:00".
	sizeText, mtimeText := "", ""
	if ref.checkable {
		sizeText = formatSizeCell(ref.size, p.sizeBytes)
		mtimeText = formatModTimeCell(ref.modTime, p.mtimeUnix)
	}
	p.table.SetCell(row, colSizeSep, columnSeparator())
	p.table.SetCell(row, colSize, tview.NewTableCell(sizeText).SetTextColor(tcell.ColorWhite))
	p.table.SetCell(row, colModifiedSep, columnSeparator())
	p.table.SetCell(row, colModified, tview.NewTableCell(mtimeText).SetTextColor(tcell.ColorWhite))
}

// columnSeparator is a new cell for the bare "│" columns dividing
// Name/Size/Modified (colSizeSep/colModifiedSep) — a thin rule between
// columns rather than a full box-drawing border around them (see
// accentBackgroundColor's own doc comment on why this codebase avoids
// those generally; this is the one deliberate exception, at the user's
// own request, scoped to just these column boundaries). Always present,
// even on the ".." row: it's structural, not data, so there's nothing to
// blank out the way Size/Modified's own values are for that row.
func columnSeparator() *tview.TableCell {
	return tview.NewTableCell("│").SetTextColor(tcell.ColorWhite)
}

// sizeColumnWidth is the fixed width every Size cell — data or header —
// is formatted to, so toggling between byte and human-readable format
// (see Root's "Globals" menu) never reflows the column. Wide enough for
// the exact byte count of a multi-terabyte file (13 digits) plus a
// little breathing room.
const sizeColumnWidth = 14

// formatSizeCell renders size right-aligned within sizeColumnWidth, as
// either the exact byte count (bytesMode) or humanSize's shorthand.
func formatSizeCell(size int64, bytesMode bool) string {
	s := humanSize(size)
	if bytesMode {
		s = strconv.FormatInt(size, 10)
	}
	return fmt.Sprintf("%*s", sizeColumnWidth, s)
}

// modColumnWidth is Modified's counterpart to sizeColumnWidth: the
// width of "2026-08-19 09:12:03" (19 characters), which a Unix
// timestamp (10 digits until the year 2286) comfortably fits within too
// — so, again, toggling the format never reflows the column.
const modColumnWidth = 19

// formatModTimeCell renders t right-aligned within modColumnWidth, as
// either a Unix timestamp (unixMode) or the same "2006-01-02 15:04:05"
// layout the Properties overlay's Modified field uses.
func formatModTimeCell(t time.Time, unixMode bool) string {
	s := t.Format("2006-01-02 15:04:05")
	if unixMode {
		s = strconv.FormatInt(t.Unix(), 10)
	}
	return fmt.Sprintf("%*s", modColumnWidth, s)
}

// sortArrow is the small suffix buildColumnHeader appends to whichever
// column label is the current sort key, showing its direction. Plain
// arrows (U+2191/U+2193), not a geometric triangle: those are Unicode
// "ambiguous width", a rendering risk not worth taking here when a
// narrow, universally single-width alternative exists (the checkbox's
// circles are this codebase's one deliberate exception, at the user's
// explicit request — not a precedent to keep spending here).
func sortArrow(descending bool) string {
	if descending {
		return " ↓"
	}
	return " ↑"
}

// buildColumnHeader (re)builds columnHeader's one row: the checkbox
// column gets a clickable ○/● of its own (see toggleSelectAllViaHeader)
// — an additional way to trigger the context menu's Select all/Deselect
// all, right where the data rows' own checkboxes are; type/modifier get
// a blank cell (self-explanatory via their own glyphs, no label needed);
// Name/Size/Modified get their own clickable cell — click sorts by that
// column (see setSortKey), starting ascending if it wasn't already the
// active key, or reversing direction if it was. The active column's
// label gets sortArrow's suffix.
//
// This table's columns only end up matching table's own widths by
// construction, not any explicit synchronization: colCheckbox/colType/
// colModifier are always exactly 1 character wide in both tables (their
// content is always exactly that long), and colSize/colModified are
// always formatted to a fixed width (see formatSizeCell/
// formatModTimeCell) regardless of value or format — since
// tview.Table sizes each column to its widest cell, two separate tables
// with the same per-column content-width characteristics size
// identically without needing to coordinate.
func (p *Panel) buildColumnHeader() {
	p.columnHeader.Clear()

	checkboxHeader := tview.NewTableCell(checkboxText(p.allSelected())).SetTextColor(tcell.ColorWhite)
	checkboxHeader.SetClickedFunc(func() bool {
		p.toggleSelectAllViaHeader()
		return false
	})
	p.columnHeader.SetCell(0, colCheckbox, checkboxHeader)
	p.columnHeader.SetCell(0, colType, tview.NewTableCell(" ").SetTextColor(tcell.ColorWhite))
	p.columnHeader.SetCell(0, colModifier, tview.NewTableCell(" ").SetTextColor(tcell.ColorWhite))

	nameLabel := "Name"
	if p.sortKey == sortByName {
		nameLabel += sortArrow(p.sortDescending)
	}
	nameCell := tview.NewTableCell(nameLabel).SetTextColor(tcell.ColorWhite)
	nameCell.SetExpansion(1)
	nameCell.SetClickedFunc(func() bool {
		p.setSortKey(sortByName)
		return false
	})
	p.columnHeader.SetCell(0, colName, nameCell)

	p.columnHeader.SetCell(0, colSizeSep, columnSeparator())
	p.setColumnHeaderCell(colSize, sizeColumnWidth, "Size", sortBySize)
	p.columnHeader.SetCell(0, colModifiedSep, columnSeparator())
	p.setColumnHeaderCell(colModified, modColumnWidth, "Modified", sortByModified)
}

// setColumnHeaderCell builds one of columnHeader's fixed-width, right-
// aligned Size/Modified cells — see buildColumnHeader.
func (p *Panel) setColumnHeaderCell(col, width int, label string, key sortKey) {
	text := label
	if p.sortKey == key {
		text += sortArrow(p.sortDescending)
	}
	cell := tview.NewTableCell(fmt.Sprintf("%*s", width, text)).SetTextColor(tcell.ColorWhite)
	cell.SetClickedFunc(func() bool {
		p.setSortKey(key)
		return false
	})
	p.columnHeader.SetCell(0, col, cell)
}

// setSortKey is what clicking a column header does: switch to sorting by
// key, starting ascending — or, if key is already the active one, flip
// direction instead. The same "click a new column: ascending; click the
// active one again: reverse" convention most file managers use.
func (p *Panel) setSortKey(key sortKey) {
	if p.sortKey == key {
		p.sortDescending = !p.sortDescending
	} else {
		p.sortKey = key
		p.sortDescending = false
	}
	p.reportError(p.load(p.path))
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
// all need to set a specific target state rather than toggle. Every
// selection path funnels through here — including live drag-toggling —
// which is why refreshHeaderCheckbox lives here too, rather than needing
// each of those callers to remember to keep the column header's own
// checkbox in sync themselves.
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
	p.refreshHeaderCheckbox()
}

// refreshHeaderCheckbox keeps the column header's own checkbox glyph
// (see buildColumnHeader/toggleSelectAllViaHeader) in sync with whether
// every checkable row is currently selected.
func (p *Panel) refreshHeaderCheckbox() {
	if cell := p.columnHeader.GetCell(0, colCheckbox); cell != nil {
		cell.SetText(checkboxText(p.allSelected()))
	}
}

// allSelected reports whether every checkable row in the current listing
// is currently selected. False for a listing with nothing checkable in
// it at all (just "..", or nothing loaded yet) — an all-unchecked master
// checkbox is what "nothing to select" should look like, not a
// misleadingly-filled one.
func (p *Panel) allSelected() bool {
	anyCheckable := false
	for row := 0; row < p.table.GetRowCount(); row++ {
		ref, ok := p.rowRef(row)
		if !ok || !ref.checkable {
			continue
		}
		anyCheckable = true
		if !p.selected[ref.path] {
			return false
		}
	}
	return anyCheckable
}

// toggleSelectAllViaHeader is the column header's checkbox's own action
// (see buildColumnHeader): selects everything if it isn't all selected
// yet, deselects everything if it already is — the same two actions the
// context menu's Select all/Deselect all already offer, just reachable
// without opening the menu.
func (p *Panel) toggleSelectAllViaHeader() {
	if p.allSelected() {
		p.deselectAll()
	} else {
		p.selectAll()
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

// CurrentRowPath is RowAt's keyboard equivalent: the row and absolute
// path of whichever entry the table's own cursor (arrow-key navigation)
// currently sits on, rather than one under a screen position. Used by
// Root's keyboard-triggered actions (Ctrl+E Edit, Ctrl+R Rename) that
// have no right-clicked position to work from. ok is false for the ".."
// row (not a file operation target, matching RowAt) or an empty table.
func (p *Panel) CurrentRowPath() (row int, path string, ok bool) {
	row, _ = p.table.GetSelection()
	ref, ok := p.rowRef(row)
	if !ok || ref.name == ".." {
		return 0, "", false
	}
	return row, ref.path, true
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

// previousPath returns the directory navigate() most recently moved
// away from — one step back in the browser-style history (see
// back/forward) — for the bash line's own "cd -" (see
// Root.changeDirectory). Not a true shell OLDPWD toggle (repeated
// "cd -" walks further back through history rather than swapping
// between exactly two directories the way a real shell's does), but
// close enough for what this is actually used for: a quick way back to
// wherever the panel just was.
func (p *Panel) previousPath() (string, bool) {
	if p.historyIdx <= 0 {
		return "", false
	}
	return p.history[p.historyIdx-1], true
}

// buildHeaderSpans renders the header's display text — Start/Home/Back/
// Forward button glyphs followed by the path, one clickable span per path
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
	button("^", actionStart)
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
