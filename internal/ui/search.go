package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/search"
)

const searchPage = "search"

// searchFormWidth/Height and searchResultsWidth/Height are the search
// dialog's own two sizes — one for the fields page (see newSearchDialog),
// a noticeably bigger one for the results window (see runSearch/
// backToSearchForm), per the user's own request: MC's own results
// window "darf auch gerne etwas größer sein" than the input form that
// opens it.
const (
	searchFormWidth, searchFormHeight       = 84, 19
	searchResultsWidth, searchResultsHeight = 96, 32
)

// searchEngineOption pairs one Engine choice's own label with the
// search.Engine it actually means — needed because, unlike a plain
// checkbox, this choice group's own option list is built conditionally
// (see buildSearchEngineOptions): once an option in the middle is left
// out (e.g. no "locate" because search.LocateAvailable() is false),
// the group's own selected index no longer lines up with the
// corresponding enum's own numeric value, so it has to be looked up
// through this pairing instead of cast directly.
type searchEngineOption struct {
	label  string
	engine search.Engine
}

// buildSearchEngineOptions lists find (always) and locate (only where
// search.LocateAvailable() — see its own doc comment on why that's not
// guaranteed even when the binary itself is present, e.g. an unbuilt
// database on macOS).
func buildSearchEngineOptions() []searchEngineOption {
	opts := []searchEngineOption{{"find", search.EngineFind}}
	if search.LocateAvailable() {
		opts = append(opts, searchEngineOption{"locate", search.EngineLocate})
	}
	return opts
}

// searchSpan is one clickable/keyboard-focusable region within one of
// the search dialog's three TextViews (searchTop/searchLeft/
// searchRight) — the same idea as Properties' own propertySpan
// (row/column range plus what it does), generalized with a single
// activate callback instead of a field-kind switch: for a text field,
// activate opens the shared inline editor (see activateSearchTextField);
// for a choice option or checkbox, it just runs its own toggle/select
// action directly and re-renders — there's nothing further to type.
// tag optionally names a span for later lookup by searchSpanIndex (see
// openSearch's own "first focus is Filename" requirement) — most spans
// leave it empty, since ordinary Tab traversal only needs each span's
// own position in searchSpans, not a name.
type searchSpan struct {
	widget           *tview.TextView
	row              int
	startCol, endCol int
	activate         func()
	tag              string
}

// searchBuilder assembles one of the search dialog's three TextViews'
// text, tracking each clickable region's row/column span as it goes —
// the same running-column-count idea propertiesBuilder uses, generalized
// to (a) target one of three separate TextViews rather than always the
// same one and (b) append into a single, shared span slice covering all
// three, so Tab traversal (see moveSearchFocus) can walk them as one
// continuous sequence regardless of which TextView a given span is
// actually drawn in.
type searchBuilder struct {
	root   *Root
	widget *tview.TextView
	b      strings.Builder
	row    int
	col    int
}

func (sb *searchBuilder) tag(s string) { sb.b.WriteString(s) }

// text advances col by s's display width (tview.TaggedStringWidth, not
// a plain rune count — see propertiesBuilder.text's own doc comment on
// why that distinction matters for span accuracy).
func (sb *searchBuilder) text(s string) {
	sb.b.WriteString(s)
	sb.col += tview.TaggedStringWidth(s)
}

func (sb *searchBuilder) newline() {
	sb.b.WriteByte('\n')
	sb.row++
	sb.col = 0
}

// focusTag mirrors propertiesBuilder's own — same colors, same
// brighter/bold style for whichever span index currently has focus
// (idx is simply len(r.searchSpans) at the point a new span is about
// to be appended, i.e. the index it's about to occupy).
func (sb *searchBuilder) focusTag(idx int) (tag, reset string) {
	if idx == sb.root.searchFocusedIdx {
		return fmt.Sprintf("[:%s:b]", colorTag(sb.root.theme.FocusedBackground)), "[:-:-]"
	}
	return fmt.Sprintf("[:%s]", colorTag(sb.root.theme.EditableBackground)), "[:-]"
}

// dimTag is the "not applicable right now" style — Ignored dirs'
// value while its own checkbox is off, Content's value while Search in
// is still "File names" — gray, same as dimmableField's own choice
// elsewhere in this app, but trivial to apply here since this whole
// dialog paints its own text directly rather than fighting tview.Form's
// per-frame color reset (see dimmableField's own doc comment for that
// story).
const dimTag = "[gray]"

// span appends one clickable region for whatever was just written
// between start and sb.col, under activate — the shared tail every
// span-producing builder method below ends with.
func (sb *searchBuilder) span(start int, activate func(), tagName string) {
	sb.root.searchSpans = append(sb.root.searchSpans, searchSpan{
		widget: sb.widget, row: sb.row, startCol: start, endCol: sb.col,
		activate: activate, tag: tagName,
	})
}

// textField writes value (or placeholder, dimTag'd, while empty) as
// one clickable/editable span, min-width padded (via minWidth — the
// field's own clickable/editable region never draws narrower than
// this, even blank) so it stays clickable even blank. dimmed
// additionally forces the gray "not applicable" style regardless of
// focus — see dimTag. maxWidth truncates only what gets DRAWN (0 means
// no limit) — never what the shared inline editor is prefilled with on
// activation, which always uses the real, untruncated value; a value
// long enough to need this (only Start-at's own path realistically is)
// would otherwise push whatever's written right after it (the Tree
// button) past the dialog's own edge, or — worse, before
// SetWrap(false) was added — onto a wrapped second line, silently
// misaligning every span's own row below it (a real bug, not just a
// theoretical one).
func (sb *searchBuilder) textField(value, placeholder string, dimmed bool, maxWidth, minWidth int, set func(string), tagName string) {
	idx := len(sb.root.searchSpans)
	shown := value
	if shown == "" {
		shown = placeholder
	}
	if maxWidth > 0 {
		shown = truncateForDisplay(shown, maxWidth)
	}
	for tview.TaggedStringWidth(shown) < minWidth {
		shown += " "
	}

	if dimmed {
		sb.tag(dimTag)
	} else {
		t, _ := sb.focusTag(idx)
		sb.tag(t)
	}
	start := sb.col
	sb.text(shown)
	sb.tag("[-:-:-]")

	sb.span(start, func() { sb.root.activateSearchTextField(idx, value, set) }, tagName)
}

// truncateForDisplay shortens s to at most maxWidth display columns by
// dropping characters from its own start (keeping the tail — usually
// the more identifying part of a long path) — see textField's own doc
// comment on why this only ever affects what's drawn.
func truncateForDisplay(s string, maxWidth int) string {
	if tview.TaggedStringWidth(s) <= maxWidth {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && tview.TaggedStringWidth("…"+string(runes)) > maxWidth {
		runes = runes[1:]
	}
	return "…" + string(runes)
}

// choice writes one "○/● label" option, immediately selecting it on
// activation (see checkboxText) — used for every Engine/Mode/Search-in
// option and for plain checkboxes (Ignore dirs enable, Case sensitive,
// Skip hidden) alike; a checkbox is just a choice group of one.
func (sb *searchBuilder) choice(selected bool, label string, action func()) {
	idx := len(sb.root.searchSpans)
	t, _ := sb.focusTag(idx)
	sb.tag(t)
	start := sb.col
	sb.text(checkboxText(selected) + " " + label)
	sb.tag("[-:-:-]")
	sb.span(start, func() { action(); sb.root.rerenderSearchDialog() }, "")
}

// newSearchDialog builds the search overlay after MC's own Find File
// dialog, per the user's own request — including reusing Properties'
// own editing paradigm (see newPropertiesView's doc comment for the
// original of this pattern): fields render as plain text
// (searchTop/searchLeft/searchRight, matching MC's own "Start
// at/Ignore dirs" block above a two-column Filename/Content section),
// click or Enter opens the one shared inline editor
// (searchEditField) positioned right over whichever field that is,
// and Cancel/Search sit in their own always-visible button row
// (searchButtons), styled the same as propertiesButtons.
//
// One deliberate difference from Properties: three separate TextViews
// instead of one, so MC's own two-column layout (Filename options
// left, Content options right) can exist at all — searchSpans is
// still one single, shared slice spanning all three (see searchSpan's
// own widget field), so Tab traversal treats them as one continuous
// sequence regardless of which TextView a given span actually lives
// in, the same as if it were all one widget.
//
// Deliberately NOT a tview.Form, unlike this dialog's own previous
// version: Form.Draw repaints every one of its items' colors from its
// own one shared style on every single redraw (see dimmableField's own
// doc comment, from that previous version, for the bug that caused —
// dimmableField itself is gone now, replaced by this file's own direct
// color control via dimTag), and a blank area inside a Form isn't
// reliably claimed by anything, letting clicks there fall through to
// the panel underneath (this dialog's own earlier click-through bug).
// TextView's own MouseHandler unconditionally claims every click
// inside its rect regardless of whether it lands on a specific span
// (confirmed by reading tview's own source, not guessed), so neither
// problem exists here.
func (r *Root) newSearchDialog() *tview.Pages {
	r.searchEngineOptions = buildSearchEngineOptions()
	// MC's own real defaults (see its screenshot): "Find recursively"
	// and "Using shell patterns" both start checked — every other
	// checkbox here defaults to Go's own bool zero value, false, which
	// already matches MC's own defaults for those.
	r.searchRecursive = true
	r.searchShellPatterns = true

	// SetWrap(false) on all three: searchSpan's own row is the count of
	// literal '\n's written (see searchBuilder.newline), which only
	// stays in sync with what's actually drawn if tview never wraps a
	// long line onto an extra visual row of its own — confirmed as a
	// real bug, not just a theoretical one: a long Start-at path
	// pushed the Tree button (written right after it, same line) onto
	// a wrapped second line before this fix, silently misaligning
	// every span's own row for the rest of the dialog below it.
	r.searchTop = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	r.searchTop.SetBorderPadding(0, 0, 1, 1)
	r.searchLeft = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	r.searchLeft.SetBorderPadding(0, 0, 1, 1)
	r.searchRight = tview.NewTextView().SetDynamicColors(true).SetWrap(false)
	r.searchRight.SetBorderPadding(0, 0, 1, 1)

	r.searchEditField = tview.NewInputField()
	r.searchEditField.SetDoneFunc(r.finishSearchEdit)

	r.searchButtons = r.newSearchButtons()

	columns := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.searchLeft, 0, 1, false).
		AddItem(r.searchRight, 0, 1, false)

	fields := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.searchTop, 7, 0, true).
		AddItem(columns, 0, 1, false).
		AddItem(r.searchButtons, 1, 0, false)
	// Installed on fields itself, the shared ancestor of searchTop/
	// searchLeft/searchRight/searchButtons — an ancestor's own
	// SetInputCapture/SetMouseCapture runs before delegating to
	// whichever descendant currently has real focus (confirmed by
	// reading tview's own WrapInputHandler/WrapMouseHandler source),
	// which is what makes Tab/Enter/Space and clicks work the same
	// regardless of which of the three TextViews actually holds it.
	fields.SetInputCapture(r.captureSearchKey)
	fields.SetMouseCapture(r.captureSearchMouse)
	// No border, same as Properties' own overlay (see newPropertiesView):
	// every child (searchTop/searchLeft/searchRight/searchButtons) gets
	// its own AccentBackground fill (see applyTheme), and their own
	// fixed/proportional sizes tile fields' whole rect exactly (6 fixed
	// + proportional + 1 fixed, no leftover row for the Flex's own
	// unstyled background to show through) — a border here only ate
	// into the dialog's own content space for no visual benefit.

	r.searchFieldsPages = tview.NewPages()
	r.searchFieldsPages.AddPage("fields", fields, true, true)
	r.searchFieldsPages.AddPage("editfield", r.searchEditField, false, false)

	r.searchList = tview.NewList().ShowSecondaryText(false)
	r.searchList.SetHighlightFullLine(true)
	r.searchList.SetBorderPadding(0, 0, 1, 1)
	r.searchList.SetDoneFunc(r.backToSearchForm) // Escape while the results list has focus

	// searchStatus is the results window's own bottom line: an
	// animated "still working" indicator naming the directory of the
	// most recently found match as a stand-in for "currently scanning"
	// (see streamSearchResults/animateSearchProgress) — the closest
	// approximation available without breakthrough doing its own
	// directory traversal instead of shelling out to find/locate/grep,
	// which don't report that kind of progress themselves. Once the
	// search finishes, this shows a final "Done — N found" instead.
	r.searchStatus = tview.NewTextView().SetDynamicColors(true)
	r.searchStatus.SetBorderPadding(0, 0, 1, 1)

	r.searchResultsView = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.searchList, 0, 1, true).
		AddItem(r.searchStatus, 1, 0, false)
	// No border — see fields' own doc comment above; searchList/
	// searchStatus's own AccentBackground fill (applyTheme) already
	// covers the whole rect, and both together tile it exactly the same
	// way (proportional + one fixed row, no leftover).

	pages := tview.NewPages()
	pages.AddPage("form", r.searchFieldsPages, true, true)
	pages.AddPage("results", r.searchResultsView, true, false)
	return pages
}

// newSearchButtons builds the always-visible Cancel/Search row — the
// same shape newPropertiesButtons already has (see its own doc
// comment): SetExitFunc hands Tab/Backtab navigation back to
// moveSearchFocus once it reaches either button, the same way Enter
// already does via SetSelectedFunc, and spaceAlsoActivates adds the
// same for Space, per the user's own request that either key activate
// a focused button.
func (r *Root) newSearchButtons() *tview.Flex {
	r.searchCancelBtn = tview.NewButton("Cancel").SetSelectedFunc(r.closeSearch)
	r.searchSearchBtn = tview.NewButton("Search").SetSelectedFunc(r.runSearch)
	r.searchCancelBtn.SetInputCapture(spaceAlsoActivates(r.closeSearch))
	r.searchSearchBtn.SetInputCapture(spaceAlsoActivates(r.runSearch))

	exitFunc := func(key tcell.Key) {
		if key == tcell.KeyBacktab {
			r.moveSearchFocus(-1)
		} else {
			r.moveSearchFocus(1)
		}
	}
	r.searchCancelBtn.SetExitFunc(exitFunc)
	r.searchSearchBtn.SetExitFunc(exitFunc)

	// Each button gets equal proportion (0, 1) and nothing else, so the
	// two together fill the whole row edge to edge, split exactly in
	// half — the same shape newPropertiesButtons already uses, per the
	// user's own request that Cancel/Search match Properties' own
	// buttons, this time width-wise too, not just their look.
	return tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(r.searchCancelBtn, 0, 1, false).
		AddItem(r.searchSearchBtn, 0, 1, false)
}

// rerenderSearchDialog rebuilds searchTop/searchLeft/searchRight's own
// text and searchSpans from scratch — cheap enough (a handful of short
// lines) to just call after every state change (a choice picked, a
// checkbox flipped, an edit committed) rather than trying to patch
// anything incrementally, the same "always rebuild" approach
// rerenderProperties already takes.
func (r *Root) rerenderSearchDialog() {
	r.searchSpans = nil

	top := &searchBuilder{root: r, widget: r.searchTop}
	top.newline() // blank margin row — the dialog no longer has a border of its own to create this breathing room
	top.text("Engine       ")
	for i, opt := range r.searchEngineOptions {
		i := i
		top.choice(r.searchEngineIdx == i, opt.label, func() { r.searchEngineIdx = i })
		top.text("   ")
	}
	top.newline()
	top.newline()

	scopeDimmed := r.searchEngineOptions[r.searchEngineIdx].engine == search.EngineLocate
	top.text("Start at     ")
	// maxWidth=50: long enough for most real paths in full, short
	// enough to always leave room for "   [Tree]" after it within
	// searchFormWidth — see textField's own doc comment on why only
	// the *display* is ever shortened, never the value editing starts
	// from.
	top.textField(r.searchScopeValue, "(current directory)", scopeDimmed, 50, 22, func(s string) {
		r.searchScopeValue = s
	}, "start-at")
	top.text("   ")
	top.button("Tree", r.openSearchTreePicker)
	top.newline()
	top.newline()

	top.choice(r.searchIgnoreEnabled, "Ignore dirs:", func() { r.searchIgnoreEnabled = !r.searchIgnoreEnabled })
	top.text(" ")
	top.textField(r.searchIgnoreValue, "(none)", !r.searchIgnoreEnabled, 0, 22, func(s string) {
		r.searchIgnoreValue = s
	}, "")
	r.searchTop.SetText(top.b.String())

	// Filename column — after MC's own Find File dialog (verified
	// against its real find.c source, not guessed): Find recursively/
	// Follow symlinks/Using shell patterns/Case sensitive/Skip hidden,
	// in that order. "Using shell patterns" replaces this dialog's
	// previous Glob/Keyword/Regex choice group entirely, per the user's
	// own request — checked means a shell-glob pattern (FindArgs'
	// ModeGlob), unchecked means a regex one (ModeRegex) — MC has no
	// separate "keyword" concept for file names: an unanchored regex
	// (what a bare keyword becomes) already matches as a substring
	// search on its own. MC's own "All charsets" checkbox is
	// deliberately not offered here: it's about MC's own internal,
	// locale-aware matching engine, with no equivalent flag on any
	// find/grep this app shells out to — nothing to wire it to.
	left := &searchBuilder{root: r, widget: r.searchLeft}
	left.text("Filename")
	left.newline()
	left.textField(r.searchFilenameValue, "(type a pattern)", false, 0, 36, func(s string) {
		r.searchFilenameValue = s
	}, "filename")
	left.newline()
	left.newline()
	left.choice(r.searchRecursive, "Find recursively", func() { r.searchRecursive = !r.searchRecursive })
	left.newline()
	left.choice(r.searchFollowSymlinks, "Follow symlinks", func() { r.searchFollowSymlinks = !r.searchFollowSymlinks })
	left.newline()
	left.choice(r.searchShellPatterns, "Using shell patterns", func() { r.searchShellPatterns = !r.searchShellPatterns })
	left.newline()
	left.choice(r.searchCaseSensitive, "Case sensitive", func() { r.searchCaseSensitive = !r.searchCaseSensitive })
	left.newline()
	left.choice(r.searchSkipHidden, "Skip hidden", func() { r.searchSkipHidden = !r.searchSkipHidden })
	r.searchLeft.SetText(left.b.String())

	// Content column. No more "Search in" selector (this app's own
	// earlier addition beyond MC, for choosing plain grep vs. zgrep vs.
	// zipgrep) — removed for now per the user's own request, along with
	// the choice of which content search tool runs: it's decided purely
	// by whether Content itself is filled in (see runSearch), always
	// plain grep while it stays that way. This also puts Content's own
	// value field on the very same row as Filename's own value field to
	// its left, with nothing above either one but its own header.
	// Whole words/Regular expression/Case sensitive/First hit, in that
	// order, mirror MC's own real layout — Regular expression here is
	// entirely independent of Filename's own "Using shell patterns" (MC
	// keeps content pattern syntax and filename pattern syntax as two
	// separate choices, never shared — see runSearch's own doc comment
	// on why one shared Mode choice used to be wrong). Case sensitive is
	// the *same* underlying searchCaseSensitive as Filename's own
	// checkbox above — MC shows it in both columns, but this app never
	// runs a filename and a content search at once (see
	// search.Request's own doc comment), so one shared value serves
	// both without any real behavior lost.
	right := &searchBuilder{root: r, widget: r.searchRight}
	right.text("Content")
	right.newline()
	right.textField(r.searchContentValue, "(type a pattern)", false, 0, 36, func(s string) {
		r.searchContentValue = s
	}, "")
	right.newline()
	right.newline()
	right.choice(r.searchWholeWords, "Whole words", func() { r.searchWholeWords = !r.searchWholeWords })
	right.newline()
	right.choice(r.searchContentRegex, "Regular expression", func() { r.searchContentRegex = !r.searchContentRegex })
	right.newline()
	right.choice(r.searchCaseSensitive, "Case sensitive", func() { r.searchCaseSensitive = !r.searchCaseSensitive })
	right.newline()
	right.choice(r.searchFirstHit, "First hit", func() { r.searchFirstHit = !r.searchFirstHit })
	r.searchRight.SetText(right.b.String())
}

// button writes one "[Label]" clickable action, immediately running
// action on activation — the Tree button's own shape, distinct from
// choice (which toggles/selects state and re-renders) since Tree runs
// a one-off action (opening the directory picker) that manages its own
// re-render separately.
func (sb *searchBuilder) button(label string, action func()) {
	idx := len(sb.root.searchSpans)
	t, _ := sb.focusTag(idx)
	sb.tag(t)
	start := sb.col
	sb.text(tview.Escape("[" + label + "]")) // unescaped, "[Tree]" parses as an (invalid, silently dropped) style tag instead of visible text — see tview's own doc.go on its color-tag syntax
	sb.tag("[-:-:-]")
	sb.span(start, action, "")
}

// searchSpanIndex returns the index of the one span tagged tagName
// (see searchSpan's own doc comment) — used only to seed
// searchFocusedIdx on open (see openSearch's own "first focus is
// Filename" requirement, the user's own request) and while editing
// Start-at specifically (see activateSearchTextField's own
// Tab-completion wiring).
func (r *Root) searchSpanIndex(tagName string) (int, bool) {
	for i, s := range r.searchSpans {
		if s.tag == tagName {
			return i, true
		}
	}
	return 0, false
}

// setSearchFocus moves keyboard focus to searchSpans[idx] (or, once
// idx reaches len(searchSpans)/len(searchSpans)+1, to the Cancel/
// Search buttons) and re-renders so the newly-focused span's own
// focusTag highlight actually shows — the same shape
// setPropertiesFocus already has.
func (r *Root) setSearchFocus(idx int) {
	r.searchFocusedIdx = idx
	r.rerenderSearchDialog()

	n := len(r.searchSpans)
	switch {
	case idx == n:
		r.app.SetFocus(r.searchCancelBtn)
	case idx == n+1:
		r.app.SetFocus(r.searchSearchBtn)
	case idx >= 0 && idx < n:
		r.app.SetFocus(r.searchSpans[idx].widget)
	}
}

// moveSearchFocus advances searchFocusedIdx by delta (+1 for Tab, -1
// for Backtab) among the span/button stops, wrapping past either end —
// the same shape movePropertiesFocus already has.
func (r *Root) moveSearchFocus(delta int) {
	n := len(r.searchSpans)
	idx := r.searchFocusedIdx + delta
	switch {
	case idx < 0:
		idx = n + 1 // wrap to Search
	case idx >= n+2:
		idx = 0 // wrap to the first span
	}
	r.setSearchFocus(idx)
}

// activateSearchTextField positions and shows the shared inline edit
// field over searchSpans[idx], pre-filled with prefill — the same
// shape activateInlineTextField already has. Tab-completion (see
// captureSearchScopeKey) is wired in only while editing the Start-at
// field specifically (identified by its own "start-at" tag), removed
// otherwise so Tab falls through to finishSearchEdit's own field-
// navigation instead.
func (r *Root) activateSearchTextField(idx int, prefill string, commit func(string)) {
	if idx < 0 || idx >= len(r.searchSpans) {
		return
	}
	span := r.searchSpans[idx]
	r.searchEditCommit = commit
	r.searchEditField.SetText(prefill)
	if span.tag == "start-at" {
		r.searchEditField.SetInputCapture(r.captureSearchScopeKey)
	} else {
		r.searchEditField.SetInputCapture(nil)
	}

	rectX, rectY, _, _ := span.widget.GetInnerRect()
	width := span.endCol - span.startCol
	if width < 22 {
		width = 22
	}
	r.searchEditField.SetRect(rectX+span.startCol, rectY+span.row, width, 1)

	r.searchFieldsPages.ShowPage("editfield")
	r.app.SetFocus(r.searchEditField)
}

// finishSearchEdit handles Enter, Tab, and Backtab (commit) and Escape
// (discard) in the shared inline edit field — the same shape
// finishPropertyEdit already has, including Tab/Backtab committing
// (not discarding) before continuing the outer navigation they were
// already asking for.
func (r *Root) finishSearchEdit(key tcell.Key) {
	text := r.searchEditField.GetText()
	commit := r.searchEditCommit
	r.searchFieldsPages.HidePage("editfield")

	if key == tcell.KeyEnter || key == tcell.KeyTab || key == tcell.KeyBacktab {
		if commit != nil {
			commit(text)
		}
	}

	switch key {
	case tcell.KeyTab:
		r.moveSearchFocus(1)
	case tcell.KeyBacktab:
		r.moveSearchFocus(-1)
	default: // Enter or Escape: conclude editing, stay on the same field
		r.setSearchFocus(r.searchFocusedIdx)
	}
}

// captureSearchScopeKey adds bash-style Tab completion while editing
// Start-at — the same longest-common-prefix completion Panel's own
// path header already has (see Panel.completePath), reusing its own
// completions/resolvePath directly rather than reimplementing it, per
// the user's own request that every dialog with a path field support
// it the same way.
func (r *Root) captureSearchScopeKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() != tcell.KeyTab {
		return event
	}
	matches := r.panel.completions(r.searchEditField.GetText())
	if len(matches) == 0 {
		return nil
	}
	r.searchEditField.SetText(longestCommonPrefix(matches))
	return nil
}

// captureSearchKey is the search fields' own shared keyboard capture
// (see newSearchDialog's own doc comment on why one ancestor-level
// capture covers all three TextViews): Tab/Backtab move focus among
// spans/buttons, Enter/Space activate whichever one currently has it —
// the click-driven counterpart is captureSearchMouse below.
func (r *Root) captureSearchKey(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyTab:
		r.moveSearchFocus(1)
		return nil
	case tcell.KeyBacktab:
		r.moveSearchFocus(-1)
		return nil
	case tcell.KeyEnter:
		r.activateFocusedSearchSpan()
		return nil
	case tcell.KeyRune:
		if event.Rune() == ' ' {
			r.activateFocusedSearchSpan()
			return nil
		}
	}
	return event
}

func (r *Root) activateFocusedSearchSpan() {
	if r.searchFocusedIdx < 0 || r.searchFocusedIdx >= len(r.searchSpans) {
		return
	}
	r.searchSpans[r.searchFocusedIdx].activate()
}

// captureSearchMouse is a click's own counterpart to captureSearchKey:
// finds whichever span (if any) is at the click's position (see
// searchSpanAt) and both focuses and activates it. Unlike the previous
// tview.Form-based version of this dialog, this needs no separate
// "swallow blank-space clicks so they don't leak to the panel" fix —
// TextView's own MouseHandler already unconditionally claims every
// click inside its rect regardless of whether it lands on a span (see
// newSearchDialog's own doc comment) — so a click that doesn't match
// anything here is simply returned unchanged, and still gets consumed
// normally by whichever TextView it landed in.
func (r *Root) captureSearchMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick {
		return action, event
	}
	if span, idx, ok := r.searchSpanAt(event.Position()); ok {
		r.setSearchFocus(idx)
		span.activate()
		return tview.MouseConsumed, nil
	}
	return action, event
}

// searchSpanAt finds whichever searchSpans entry (if any) contains
// (x, y) — checked against its own widget's InRect first, then its
// exact row/column range within that widget's inner rect, the same
// two-step check propertySpanAt already does.
func (r *Root) searchSpanAt(x, y int) (searchSpan, int, bool) {
	for i, s := range r.searchSpans {
		if !s.widget.InRect(x, y) {
			continue
		}
		rectX, rectY, _, _ := s.widget.GetInnerRect()
		if y-rectY == s.row && x-rectX >= s.startCol && x-rectX < s.endCol {
			return s, i, true
		}
	}
	return searchSpan{}, 0, false
}

// openSearchTreePicker is the Tree button's own action: opens the
// directory picker (see openDirPicker) seeded at whatever Start-at
// currently holds, writing the chosen directory back into it on
// Select. Left untouched on Cancel.
func (r *Root) openSearchTreePicker() {
	r.openDirPicker(r.searchScopeValue, func(path string) {
		r.searchScopeValue = path
		r.rerenderSearchDialog()
	}, nil)
}

// openSearch shows the search dialog, centered on screen, on its own
// "form" page, sized for the fields (see searchFormWidth/Height —
// runSearch resizes up to searchResultsWidth/Height once it switches
// to the results page). Filename/Content always start empty; Start at
// always resets to wherever the panel currently is (the far more
// common starting point than whatever was typed the last time the
// dialog was open); Engine/Mode/Ignored dirs/Search in/Case sensitive/
// Skip hidden are left exactly as they were, since there's no
// similarly obvious reason to reset those every time. First focus goes
// to Filename (the "filename" tagged span — see searchSpanIndex),
// per the user's own request, not Engine.
func (r *Root) openSearch() {
	r.searchFilenameValue = ""
	r.searchContentValue = ""
	r.searchScopeValue = r.panel.path
	r.searchPages.SwitchToPage("form")
	r.searchFieldsPages.SwitchToPage("fields")

	r.rerenderSearchDialog()
	if idx, ok := r.searchSpanIndex("filename"); ok {
		r.setSearchFocus(idx)
	}

	r.resizeSearchPages(searchFormWidth, searchFormHeight)
	r.showOverlay(searchPage, r.searchPages)
}

// resizeSearchPages centers a width x height rect on screen (clamped
// to the panel — see clampToPanel) and applies it to r.searchPages —
// shared by openSearch (the fields' own smaller size) and runSearch/
// backToSearchForm (the results window's own bigger one), so the
// dialog's footprint always matches whichever of its two pages is
// currently showing rather than staying pinned to the fields' size.
func (r *Root) resizeSearchPages(width, height int) {
	x, y := r.centeredOnScreen(width, height)
	x, y, w, h := r.clampToPanel(x, y, width, height)
	r.searchPages.SetRect(x, y, w, h)
}

// closeSearch cancels any in-flight search (see cancelSearch) and
// closes the dialog entirely — Escape from the fields page, or picking
// a result (see openSearchResult).
func (r *Root) closeSearch() {
	r.cancelSearch()
	r.hideOverlay()
}

// backToSearchForm cancels any in-flight search and returns to the
// fields page (back to the smaller of the dialog's two sizes — see
// resizeSearchPages) without closing the dialog — Escape from the
// results page, so refining a search that came back wrong (or empty)
// doesn't need reopening the whole dialog from scratch.
func (r *Root) backToSearchForm() {
	r.cancelSearch()
	r.searchPages.SwitchToPage("form")
	r.searchFieldsPages.SwitchToPage("fields")
	r.resizeSearchPages(searchFormWidth, searchFormHeight)
	r.setSearchFocus(r.searchFocusedIdx)
}

// cancelSearch stops whatever search.Run call is currently in flight,
// if any (see runSearch/streamSearchResults), and its paired
// animateSearchProgress ticker (both share the same ctx/searchCancel)
// — so a slow "find /" started by an earlier search never keeps
// working, or racing a newer one's own results into the same list,
// once the user has moved on from it.
func (r *Root) cancelSearch() {
	if r.searchCancel != nil {
		r.searchCancel()
		r.searchCancel = nil
	}
}

// parseIgnoreDirs splits the Ignored dirs field's comma-separated text
// into individual directory names/patterns (see search.Request.
// IgnoreDirs' own doc comment on how each is matched) — surrounding
// whitespace trimmed, empty entries (a trailing comma, or the field
// left blank) dropped.
func parseIgnoreDirs(text string) []string {
	var dirs []string
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			dirs = append(dirs, part)
		}
	}
	return dirs
}

// runSearch is the Search button's own action: builds a search.Request
// from the dialog's current state — Filename's own value if Content is
// left blank, or Content's otherwise (always a plain grep search — see
// this func's own contentMode, and rerenderSearchDialog's own doc
// comment on Content's column for why there's no explicit tool choice
// any more) — cancels whatever search was previously running (see
// cancelSearch), and starts the new one on the bigger results page
// (see resizeSearchPages), streaming its matches in as they're found
// (see streamSearchResults) alongside an animated "still working"
// status line (see animateSearchProgress). Ignored dirs combines the
// Ignore-dirs-enable checkbox's own field (if checked) with a ".*"
// entry for Skip hidden (if checked) — see search.Request.IgnoreDirs'
// own doc comment on why the latter needs no separate mechanism of its
// own.
func (r *Root) runSearch() {
	contentMode := search.ContentNone
	if r.searchContentValue != "" {
		contentMode = search.ContentGrep
	}

	// Filename search and content search each get their own independent
	// Mode, computed from their own MC-style checkbox — never a shared
	// "Glob/Keyword/Regex" selector (see this file's own history: the
	// two were deliberately split apart to match MC's real dialog, where
	// "Using shell patterns" only ever affects the filename match and
	// "Regular expression" only ever affects the content match). Only
	// one of the two ever ends up in req.Mode below, since filename and
	// content search are mutually exclusive per search.Request's own doc
	// comment — so nothing is lost by computing both unconditionally
	// here rather than branching first.
	filenameMode := search.ModeRegex
	if r.searchShellPatterns {
		filenameMode = search.ModeGlob
	}
	contentSearchMode := search.ModeKeyword
	if r.searchContentRegex {
		contentSearchMode = search.ModeRegex
	}

	pattern := r.searchFilenameValue
	mode := filenameMode
	if contentMode != search.ContentNone {
		pattern = r.searchContentValue
		mode = contentSearchMode
	}
	if pattern == "" {
		return
	}
	scope := r.panel.resolvePath(r.searchScopeValue)

	var ignoreDirs []string
	if r.searchIgnoreEnabled {
		ignoreDirs = parseIgnoreDirs(r.searchIgnoreValue)
	}
	if r.searchSkipHidden {
		ignoreDirs = append(ignoreDirs, ".*")
	}

	req := search.Request{
		Pattern:        pattern,
		Scope:          scope,
		Mode:           mode,
		Engine:         r.searchEngineOptions[r.searchEngineIdx].engine,
		Content:        contentMode,
		IgnoreDirs:     ignoreDirs,
		CaseSensitive:  r.searchCaseSensitive,
		NonRecursive:   !r.searchRecursive,
		FollowSymlinks: r.searchFollowSymlinks,
		WholeWords:     r.searchWholeWords,
		FirstHit:       r.searchFirstHit,
	}

	r.cancelSearch()
	ctx, cancel := context.WithCancel(context.Background())
	r.searchCancel = cancel

	r.searchList.Clear()
	r.searchAnimFrame = 0
	r.searchLastDir = ""
	r.searchStartDir = scope
	r.renderSearchStatus()
	r.searchPages.SwitchToPage("results")
	r.resizeSearchPages(searchResultsWidth, searchResultsHeight)
	r.app.SetFocus(r.searchList)

	go r.animateSearchProgress(ctx)
	results, errs := searchRun(ctx, req)
	go r.streamSearchResults(ctx, req, results, errs)
}

// searchRun is search.Run — a package-level var, not called directly,
// so a test can override it (see search_test.go) and verify runSearch's
// own request-building without depending on real find/grep/locate
// being on $PATH, and without a background goroutine ever reaching
// r.app.QueueUpdateDraw with nothing running the event loop to drain
// it (see StartClock's own doc comment for the same concern this
// avoids the same way loadInitialSettings/userConfigFilePath already
// do for config I/O).
var searchRun = search.Run

// animateSearchProgress advances searchAnimFrame every
// hashAnimationInterval until ctx is done (see runSearch/cancelSearch)
// — the same ticker-driven "in progress" animation computeHashes' own
// animateHashProgress already uses, reusing hashAnimationFrames
// directly rather than a second, separately-defined set of frames.
func (r *Root) animateSearchProgress(ctx context.Context) {
	ticker := time.NewTicker(hashAnimationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			r.app.QueueUpdateDraw(func() {
				if ctx.Err() != nil {
					return
				}
				r.searchAnimFrame++
				r.renderSearchStatus()
			})
		case <-ctx.Done():
			return
		}
	}
}

// renderSearchStatus paints searchStatus' own text: the current
// animation frame plus whatever directory streamSearchResults last saw
// a match in (searchLastDir), falling back to Start at's own value
// (searchStartDir) until the first result arrives.
func (r *Root) renderSearchStatus() {
	frame := hashAnimationFrames[r.searchAnimFrame%len(hashAnimationFrames)]
	dir := r.searchLastDir
	if dir == "" {
		dir = r.searchStartDir
	}
	r.searchStatus.SetText(frame + " " + dir)
}

// noSearchResultsText is what the results list shows when a search
// finished without a single match — see streamSearchResults' own doc
// comment on why that needs to be an explicit, visible state rather
// than just leaving the list empty. For a locate-engine filename
// search specifically, it also names the single most likely reason:
// locate answers entirely from its own prebuilt index (updatedb),
// which — unlike a live find/grep — can be hours or days stale and
// simply doesn't know about a file created (or renamed, or deleted)
// since the last run.
func noSearchResultsText(req search.Request) string {
	if req.Engine == search.EngineLocate && req.Content == search.ContentNone {
		return "No matches found (locate's own index may be stale — see Engine: find for a live search instead)"
	}
	return "No matches found"
}

// streamSearchResults drains results/errs (see search.Run) on a
// background goroutine, appending each match to searchList via
// QueueUpdateDraw — the same "background work, draw updates queued
// onto the UI goroutine" shape StartClock's own ticker already uses.
// Every queued update first checks ctx.Err(): once a newer search has
// cancelled this one (see cancelSearch/runSearch), any of this
// goroutine's own updates still sitting in the queue skip themselves
// instead of appending stale results (or a stale error) on top of the
// new search's own, already-cleared list. Each arriving result also
// updates searchLastDir — see renderSearchStatus.
//
// If the search finishes with zero matches and no error, one
// noSearchResultsText(req) item is added instead of leaving the list
// looking exactly like it did before the search ran — easily read as
// "the search didn't do anything" (a real user report). Either way,
// the status line's own animation stops and settles on a final
// "Done — N found" once the search is over.
func (r *Root) streamSearchResults(ctx context.Context, req search.Request, results <-chan search.Result, errs <-chan error) {
	count := 0
	for res := range results {
		res := res // captured per-iteration, not the shared loop variable
		count++
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.searchLastDir = filepath.Dir(res.Path)
			r.searchList.AddItem(formatSearchResult(res), "", 0, func() {
				r.openSearchResult(res)
			})
		})
	}
	if err := <-errs; err != nil {
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.showError(err)
			// See below: animateSearchProgress's own ticker otherwise
			// keeps running (and overwriting whatever's set here) until
			// something else eventually cancels it.
			r.cancelSearch()
		})
		return
	}
	if count == 0 {
		r.app.QueueUpdateDraw(func() {
			if ctx.Err() != nil {
				return
			}
			r.searchList.AddItem(noSearchResultsText(req), "", 0, nil)
		})
	}
	r.app.QueueUpdateDraw(func() {
		if ctx.Err() != nil {
			return
		}
		r.searchStatus.SetText(fmt.Sprintf("Done — %d found", count))
		// The search itself is over, but nothing else would ever stop
		// animateSearchProgress's own ticker on its own — cancelSearch
		// is otherwise only called by a *newer* search starting, or the
		// dialog closing, both of which are still in the future here.
		// Left running, it would silently overwrite the "Done" text set
		// just above with the next animation frame within
		// hashAnimationInterval.
		r.cancelSearch()
	})
}

// formatSearchResult renders one search.Result as a searchList item:
// just the path for a filename match (Line == 0), or
// "path:line: text" for a content match.
func formatSearchResult(res search.Result) string {
	if res.Line == 0 {
		return res.Path
	}
	return fmt.Sprintf("%s:%d: %s", res.Path, res.Line, res.Text)
}

// openSearchResult is what picking a result does: closes the dialog
// and jumps the panel straight to it (see Panel.navigateAndSelect) —
// for both a filename and a content match, since neither this app nor
// the panel itself can yet open a file to a specific line (a job for
// the "View" action instead).
func (r *Root) openSearchResult(res search.Result) {
	r.closeSearch()
	r.showError(r.panel.navigateAndSelect(res.Path))
}
