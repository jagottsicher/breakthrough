package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/search"
)

const searchPage = "search"

// searchFormWidth/Height is the search dialog's own fixed size — the
// dialog is always this one size now: results show directly in the
// panel's own normal file overview area instead of a second, bigger
// page of this same overlay (see runSearch/Panel.showSearchResults),
// per the user's own request.
const (
	searchFormWidth, searchFormHeight = 84, 19
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

	// spanArea wraps searchTop/columns (i.e. searchLeft/searchRight)
	// only — deliberately NOT searchButtons — and carries
	// captureSearchKey/captureSearchMouse, still shared between all
	// three TextViews the same way as before. It has to stop above
	// searchButtons, not above it: an ancestor's own SetInputCapture
	// runs before delegating to whichever descendant currently has
	// real focus (confirmed by reading tview's own WrapInputHandler
	// source), so installing it one level higher, on fields itself
	// (the shared ancestor of spanArea AND searchButtons), meant
	// Enter/Space bound for a focused Cancel/Search button (see
	// newSearchButtons' own SetSelectedFunc/spaceAlsoActivates) never
	// reached the button at all — captureSearchKey's own unconditional
	// KeyEnter/' ' cases swallowed them first. Properties never had
	// this bug for the same structural reason: capturePropertiesKey
	// sits on propertiesText alone, already a sibling of
	// propertiesButtons rather than an ancestor of it (see
	// newPropertiesView).
	spanArea := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.searchTop, 7, 0, true).
		AddItem(columns, 0, 1, false)
	spanArea.SetInputCapture(r.captureSearchKey)
	spanArea.SetMouseCapture(r.captureSearchMouse)

	fields := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(spanArea, 0, 1, true).
		AddItem(r.searchButtons, 1, 0, false)
	// No border, same as Properties' own overlay (see newPropertiesView):
	// every child (searchTop/searchLeft/searchRight, nested inside
	// spanArea, and searchButtons alongside it) gets its own
	// AccentBackground fill (see applyTheme), and their own fixed/
	// proportional sizes tile fields' whole rect exactly (7 fixed +
	// proportional, nested inside spanArea's own proportional slot,
	// + 1 fixed for searchButtons — no leftover row for the Flex's own
	// unstyled background to show through) — a border here only ate
	// into the dialog's own content space for no visual benefit.

	r.searchFieldsPages = tview.NewPages()
	r.searchFieldsPages.AddPage("fields", fields, true, true)
	r.searchFieldsPages.AddPage("editfield", r.searchEditField, false, false)

	pages := tview.NewPages()
	pages.AddPage("form", r.searchFieldsPages, true, true)
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

	// Escape reaches here (not captureSearchKey — that only ever runs
	// while a span/checkbox/field has real focus, never a button, the
	// exact same reason Enter/Space needed their own case on each
	// button too, not just captureSearchKey's) whenever Cancel or
	// Search itself currently has focus — tview.Button's own
	// InputHandler routes Escape to SetExitFunc, the same as Tab/
	// Backtab, not to whatever's watching from an ancestor's own
	// SetInputCapture. Without this case, Escape here fell into the
	// same branch as Tab, silently just moving focus onward instead of
	// closing anything — matching Properties' own identical two-button
	// SetExitFunc cases (see newPropertiesButtons) exactly.
	exitFunc := func(key tcell.Key) {
		switch key {
		case tcell.KeyBacktab:
			r.moveSearchFocus(-1)
		case tcell.KeyEscape:
			r.closeSearch()
		default:
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
		if opt.engine == search.EngineLocate {
			// Plain informational text, not a span — nothing to click
			// or focus here. Locate answers entirely from updatedb's own
			// prebuilt index, which — since Ubuntu 10.04 — never
			// includes an eCryptfs-encrypted home directory at all (not
			// just stale, permanently excluded — see PRUNEFS in
			// /etc/updatedb.conf): a deliberate security decision, since
			// indexing it would mean storing real, decrypted filenames
			// in a plaintext database outside the encrypted volume. Per
			// a real user report — searching their own home directory
			// with locate found nothing, for exactly this reason.
			top.tag(dimTag)
			top.text(" (skips eCryptfs by design)")
			top.tag("[-:-:-]")
		}
		top.text("   ")
	}
	top.newline()
	top.newline()

	// Dimmed whenever locate itself would ignore Start-at entirely — a
	// plain filename search (Content empty), or Filename filled in
	// (locate's own index does the narrowing there, Scope still never
	// enters into it — see search.listThenGrep). The one exception: a
	// *plain* content search (Content filled in, Filename left blank —
	// nothing for locate to narrow with) still runs a live grep from
	// Start-at exactly the same way EngineFind's own content search
	// always has — see runSearch's own doc comment on why Engine is
	// meant to be irrelevant there entirely, per the user's own
	// explicit request — so the field showing as genuinely active
	// (not dimmed) here is accurate, not a leftover oversight.
	plainContentSearch := r.searchContentValue != "" && r.searchFilenameValue == ""
	scopeDimmed := r.searchEngineOptions[r.searchEngineIdx].engine == search.EngineLocate && !plainContentSearch
	top.text("Start at     ")
	// The field fills exactly the rest of the row, right up to where
	// "   [Tree]" itself starts — searchTopTextWidth is searchTop's own
	// usable width (searchFormWidth minus its SetBorderPadding's 1 col
	// left + 1 col right); treeSuffixWidth is "   [Tree]"'s own rendered
	// width (3 spaces + the 6-char "[Tree]" button — see button's own
	// doc comment on why that's visible text, not a style tag). Same
	// value for both maxWidth and minWidth: the field never draws
	// narrower OR wider than this, so it always reaches the button
	// exactly, never short of it or pushing it off the row — see
	// textField's own doc comment on why only the *display* is ever
	// shortened, never the value editing starts from.
	const searchTopTextWidth = searchFormWidth - 2
	const treeSuffixWidth = 9 // len("   [Tree]")
	fieldWidth := searchTopTextWidth - top.col - treeSuffixWidth
	top.textField(r.searchScopeValue, "(current directory)", scopeDimmed, fieldWidth, fieldWidth, func(s string) {
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
//
// Enter specifically in Filename also runs the search immediately,
// per the user's own request — scoped to that one field alone (not
// Content, Start-at, or Ignored dirs): Filename is the field first
// focus lands on (see openSearch), the one MC's own dialog treats as
// the "type it and go" field, so Enter there matching Search's own
// button is the one place it reads as obviously intended rather than
// surprising.
func (r *Root) finishSearchEdit(key tcell.Key) {
	text := r.searchEditField.GetText()
	commit := r.searchEditCommit
	editedTag := ""
	if r.searchFocusedIdx >= 0 && r.searchFocusedIdx < len(r.searchSpans) {
		editedTag = r.searchSpans[r.searchFocusedIdx].tag
	}
	r.searchEditCommit = nil // see commitPendingSearchEdit's own doc comment on why this must go back to nil, not just get overwritten on the next activate
	r.searchFieldsPages.HidePage("editfield")

	if key == tcell.KeyEnter || key == tcell.KeyTab || key == tcell.KeyBacktab {
		if commit != nil {
			commit(text)
		}
	}

	if key == tcell.KeyEnter && editedTag == "filename" {
		r.runSearch()
		return
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
// path header already has (see Panel.completePath), but through
// Panel.dirCompletions rather than its own plain completions: Start-at
// can only ever be a directory, never a file, so completion here is
// both directory-only and case-sensitive — see dirCompletions' own doc
// comment for why, including a real user report its own
// directory-only filtering fixes on its own.
//
// Tab is always consumed here, whether or not completion has anything
// left to add — Start-at is deliberately exempted from the Tab-cycles-
// through-every-option behavior every other field in this dialog has
// (see finishSearchEdit), per the user's own explicit request: Tab
// here means "complete", full stop, never "leave the field" — Backtab
// (untouched by this capture) or a click elsewhere (see
// commitPendingSearchEdit) are how you actually leave it. An earlier
// version let Tab fall through to field-navigation once nothing was
// left to complete, but that made a *second*, disambiguating Tab press
// (typing further and pressing Tab again) impossible to tell apart
// from "done editing, move on" — completion kept silently losing the
// keystroke to navigation instead.
func (r *Root) captureSearchScopeKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() != tcell.KeyTab {
		return event
	}
	matches := r.panel.dirCompletions(r.searchEditField.GetText())
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
	case tcell.KeyEscape:
		// Per the user's own explicit request: Escape means Cancel here,
		// the same as everywhere else in this app (Properties' own
		// capturePropertiesKey has the identical case) — closes the
		// dialog outright rather than doing nothing, which is what a
		// span/checkbox/field having focus (as opposed to a button —
		// see newSearchButtons' own matching case) left it doing before.
		r.closeSearch()
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
//
// commitPendingSearchEdit runs first, unconditionally: a click that
// reaches this func at all is necessarily outside the shared inline
// edit field's own current rect (tview routes a click on that rect
// straight to the field itself instead — see newSearchDialog's own
// doc comment on why "fields" and "editfield" can both be showing at
// once), i.e. exactly a "leave the field" click, whether or not it
// then goes on to land on another span.
func (r *Root) captureSearchMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick {
		return action, event
	}
	r.commitPendingSearchEdit()
	if span, idx, ok := r.searchSpanAt(event.Position()); ok {
		r.setSearchFocus(idx)
		span.activate()
		return tview.MouseConsumed, nil
	}
	return action, event
}

// commitPendingSearchEdit writes back whatever the shared inline edit
// field currently holds and hides it, if it's actually showing (see
// searchEditCommit's own nil-when-idle convention, set in
// activateSearchTextField and cleared again in finishSearchEdit) — the
// click-elsewhere/blur equivalent of finishSearchEdit's own Enter case
// (commit, not discard). Without this, a click that lands on a
// *different* span while Start-at was still being edited (e.g. right
// after refining a Tree pick by hand, then clicking Ignore dirs) threw
// the in-progress text away outright — captureSearchMouse's own span
// lookup and activation never went anywhere near finishSearchEdit, so
// nothing ever committed it — and the next render silently fell back
// to whatever Start-at held before that edit began. A real bug the
// user ran into, not just a theoretical one.
func (r *Root) commitPendingSearchEdit() {
	if r.searchEditCommit == nil {
		return
	}
	commit := r.searchEditCommit
	text := r.searchEditField.GetText()
	r.searchEditCommit = nil
	r.searchFieldsPages.HidePage("editfield")
	commit(text)
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

// openSearch shows the search dialog, centered on screen. Filename/
// Content/Start-at reset to blank/the panel's current directory only
// when this opens *fresh* — the panel isn't already showing search
// results (see Panel.searchMode) — the far more common case; reopening
// it to refine an already-running or already-finished search (Ctrl+F
// again, or Escape — see backToSearchForm, this func's only other
// caller) leaves everything exactly as it was left instead, per the
// user's own explicit request that this not reinitialize. Engine/Mode/
// Ignored dirs/Search in/Case sensitive/Skip hidden are never reset
// either way — there's no similarly obvious reason to. First focus
// goes to Filename (the "filename" tagged span — see
// searchSpanIndex), per the user's own request, not Engine — even when
// refining, since it's still the field most searches start from.
func (r *Root) openSearch() {
	if !r.panel.searchMode {
		r.searchFilenameValue = ""
		r.searchContentValue = ""
		r.searchScopeValue = r.panel.path
	}
	r.searchFieldsPages.SwitchToPage("fields")

	r.rerenderSearchDialog()
	if idx, ok := r.searchSpanIndex("filename"); ok {
		r.setSearchFocus(idx)
	}

	r.resizeSearchPages()
	r.showOverlay(searchPage, r.searchPages)
}

// resizeSearchPages centers the dialog's own fixed searchFormWidth/
// Height on screen, clamped to the current panel like every other
// overlay in this app (see clampToPanel) — results no longer resize
// this overlay at all now that they show directly in the panel's own
// area instead (see Panel.showSearchResults), so there's only ever
// this one size to apply.
func (r *Root) resizeSearchPages() {
	x, y := r.centeredOnScreen(searchFormWidth, searchFormHeight)
	x, y, w, h := r.clampToPanel(x, y, searchFormWidth, searchFormHeight)
	r.searchPages.SetRect(x, y, w, h)
}

// showSearchError shows msg as the panel's own (otherwise empty)
// search-results status line, in EntryError's own red (the same color
// a broken symlink gets in the panel itself — see entryColor) — used
// for a search that was refused before it ever ran (see runSearch's
// own non-existent-Start-at check) rather than Root's own global error
// overlay (see showError), which used to close the whole search dialog
// outright over what's typically just a typo — discarding whatever was
// already typed into Filename/Content/Start-at and needing the entire
// dialog reopened from scratch just to fix it. Per the user's own
// explicit request: showing it here instead means Escape (see Panel.
// onSearchEscape, wired to backToSearchForm) goes straight back to the
// form, with everything exactly as it was left.
//
// Deliberately skips starting animateSearchProgress/streamSearchResults
// (searchCancel stays nil) — there's no real search running here for
// either of those to track, only this one static line to show; the
// next real runSearch resets the header's own color back to normal
// (see Panel.setSearchStatusColor) before showing anything else.
func (r *Root) showSearchError(msg string) {
	r.cancelSearch()
	r.hideOverlay() // close the form, revealing the panel underneath
	r.panel.showSearchResults()
	r.panel.setSearchStatusColor(r.theme.EntryError)
	r.setSearchStatus(msg)
}

// closeSearch cancels any in-flight search (see cancelSearch), closes
// the dialog, and — if search results are currently showing (see
// Panel.searchMode) — discards them, restoring the real directory the
// panel was showing before the search that produced them ever ran (see
// Panel.exitSearchResults). Escape/Cancel from the fields page: "I
// don't want this search, or these results, any more" — the one
// explicit way out of search mode that isn't picking a result (see
// Panel.activateRow's own searchMode branch).
func (r *Root) closeSearch() {
	r.cancelSearch()
	r.hideOverlay()
	r.showError(r.panel.exitSearchResults())
}

// backToSearchForm cancels any in-flight search and reopens the form
// (see openSearch, which — since the panel is already showing search
// results by the time this runs, see Panel.onSearchEscape — leaves
// every field exactly as it already was, not reset) without touching
// whatever the panel currently has on screen: Escape while search
// results are showing (wired as Panel.onSearchEscape in NewRoot), so
// refining a search that came back wrong (or empty) doesn't lose the
// results already gathered, and doesn't need reopening the whole
// dialog from scratch either.
func (r *Root) backToSearchForm() {
	r.cancelSearch()
	r.openSearch()
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
// IgnoreDirs' own doc comment on how each is matched — against a
// single path *component*, via find's own -name test for EngineFind or
// filepath.Match for EngineLocate, never a full or partial path) —
// surrounding whitespace trimmed, empty entries (a trailing comma, or
// the field left blank) dropped.
//
// Also trims any leading/trailing "/" from each entry — a real user
// report: typing "/development" (reasonably expecting it to mean "the
// development directory") silently excluded nothing at all, since
// find's own -name test matches a bare basename, which can never
// contain a "/" to begin with — "/development" and "development/" are
// both patterns nothing could ever match, not stricter versions of
// "development". This can't turn a genuinely different exclusion into
// the wrong one (a bare name never had significant leading/trailing
// slashes to begin with), only turn an entry that could never match
// anything into the one the user almost certainly meant.
func parseIgnoreDirs(text string) []string {
	var dirs []string
	for _, part := range strings.Split(text, ",") {
		part = strings.Trim(strings.TrimSpace(part), "/")
		if part != "" {
			dirs = append(dirs, part)
		}
	}
	return dirs
}

// runSearch is the Search button's own action: builds a search.Request
// from the dialog's current state — Content left blank means a plain
// filename search on Filename's own value; Content filled in means a
// grep content search on Content's own value (always plain grep — see
// this func's own contentMode, and rerenderSearchDialog's own doc
// comment on Content's column for why there's no explicit tool choice
// any more), additionally restricted to files matching Filename first
// if that's *also* filled in (see Request.NamePattern's own doc
// comment) — cancels whatever search was previously running (see
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
	// "Regular expression" only ever affects the content match).
	filenameMode := search.ModeRegex
	if r.searchShellPatterns {
		filenameMode = search.ModeGlob
	}
	contentSearchMode := search.ModeKeyword
	if r.searchContentRegex {
		contentSearchMode = search.ModeRegex
	}

	if r.searchFilenameValue == "" && r.searchContentValue == "" {
		return
	}
	scope := r.panel.resolvePath(r.searchScopeValue)
	engine := r.searchEngineOptions[r.searchEngineIdx].engine

	// A *plain* content search (Content filled in, Filename left blank
	// — nothing for locate itself to narrow with, so there's no
	// name-matching step for Engine to drive at all — see
	// runContentSearch's own doc comment) always uses Start-at exactly
	// the same way EngineFind's own content search already does,
	// completely regardless of which Engine is actually selected — per
	// the user's own explicit, repeated request that Engine be entirely
	// irrelevant here, not just "less relevant." An earlier version of
	// this tried to special-case locate by substituting the panel's own
	// current directory instead — wrong, and a real regression: Start-at
	// is exactly what a user reasonably expects a "start point" field to
	// mean, typed value and all, whether or not it happens to match
	// wherever the panel itself is currently sitting.
	plainContentSearch := contentMode != search.ContentNone && r.searchFilenameValue == ""

	// Checked for EngineFind, and now also for locate's own plain
	// content search (plainContentSearch, just above) — the one
	// EngineLocate case that genuinely uses scope for anything (every
	// other EngineLocate case leaves it dimmed and unused — see
	// scopeDimmed's own identical condition in rerenderSearchDialog,
	// which must stay in sync with this one).
	//
	// Without this, a typo'd or since-deleted Start-at directory ran
	// find(1) anyway — it exits complaining to stderr, this app's own
	// runner.go deliberately never treats a non-zero exit as an error
	// (see its own doc comment on why), so the search "succeeded" and
	// silently came back with nothing, indistinguishable from a real,
	// well-formed search that simply found no matches. The user's own
	// report: typing a real but non-existent path and searching just
	// says "No matches found" — this catches that case explicitly,
	// before ever shelling out, with a message that actually says what
	// went wrong — via showSearchError, not Root's own global error
	// overlay (see its own doc comment on why: closing the whole dialog
	// over a typo'd path was "so ein Game-Killer" for a real mistake
	// this cheap to fix).
	if engine == search.EngineFind || plainContentSearch {
		if info, err := os.Stat(scope); err != nil || !info.IsDir() {
			r.showSearchError(fmt.Sprintf("Search directory does not exist: %s", scope))
			return
		}
	}

	var ignoreDirs []string
	if r.searchIgnoreEnabled {
		ignoreDirs = parseIgnoreDirs(r.searchIgnoreValue)
	}
	if r.searchSkipHidden {
		ignoreDirs = append(ignoreDirs, ".*")
	}

	req := search.Request{
		Scope:          scope,
		Engine:         engine,
		Content:        contentMode,
		IgnoreDirs:     ignoreDirs,
		CaseSensitive:  r.searchCaseSensitive,
		NonRecursive:   !r.searchRecursive,
		FollowSymlinks: r.searchFollowSymlinks,
		WholeWords:     r.searchWholeWords,
		FirstHit:       r.searchFirstHit,
	}
	// Content == ContentNone: Pattern is the filename match itself, run
	// through Engine directly. Otherwise: Pattern is what grep actually
	// searches for, and — per the user's own explicit request — a
	// Filename value alongside it additionally restricts *which* files
	// get grepped (NamePattern/NameMode — see Request's own doc comment)
	// rather than being silently ignored the moment Content has
	// anything typed into it; left blank, a content search still runs
	// across every file under Scope exactly as before.
	if contentMode == search.ContentNone {
		req.Pattern = r.searchFilenameValue
		req.Mode = filenameMode
	} else {
		req.Pattern = r.searchContentValue
		req.Mode = contentSearchMode
		req.NamePattern = r.searchFilenameValue
		req.NameMode = filenameMode
	}

	r.cancelSearch()
	ctx, cancel := context.WithCancel(context.Background())
	r.searchCancel = cancel

	r.hideOverlay() // close the form, revealing the panel underneath
	r.panel.showSearchResults()
	r.panel.setSearchStatusColor(r.theme.Text) // undo showSearchError's own red, if a previous run left it set
	r.searchAnimFrame = 0
	r.searchLastDir = ""
	r.searchStartDir = scope
	r.renderSearchStatus()

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

// renderSearchStatus paints the panel's own header status line (see
// Panel.setSearchStatus): the current animation frame plus whatever
// directory streamSearchResults last saw a match in (searchLastDir),
// falling back to Start at's own value (searchStartDir) until the
// first result arrives.
func (r *Root) renderSearchStatus() {
	frame := hashAnimationFrames[r.searchAnimFrame%len(hashAnimationFrames)]
	dir := r.searchLastDir
	if dir == "" {
		dir = r.searchStartDir
	}
	r.setSearchStatus(frame + " " + dir)
}

// searchEscHint reminds the user, regardless of a search's own outcome
// (still running, done, no matches, or the non-existent-Start-at error
// — see showSearchError) that Escape goes back to the form — per the
// user's own explicit request. setSearchStatus is the one place the
// panel's own header status text actually gets set while search
// results are showing (see its two call sites: here and
// streamSearchResults' own final status — showSearchError goes through
// this too), so the hint can never be left off some future third one.
const searchEscHint = "(Esc: back to search)"

func (r *Root) setSearchStatus(text string) {
	r.panel.setSearchStatus(strings.TrimSpace(text + " " + searchEscHint))
}

// noSearchResultsText is folded into the final status line (see
// streamSearchResults) when a search finishes without a single match,
// instead of just "Done — 0 found" on its own — easily read as "the
// search didn't do anything" otherwise (a real user report). For a
// locate-engine filename search specifically, it also names the single
// most likely reason: locate answers entirely from its own prebuilt
// index (updatedb), which — unlike a live find/grep — can be hours or
// days stale and simply doesn't know about a file created (or
// renamed, or deleted) since the last run.
func noSearchResultsText(req search.Request) string {
	if req.Engine == search.EngineLocate && req.Content == search.ContentNone {
		return "Done — 0 found (locate's own index may be stale — see Engine: find for a live search instead)"
	}
	return "Done — 0 found"
}

// streamSearchResults drains results/errs (see search.Run) on a
// background goroutine, appending each match to the panel via
// QueueUpdateDraw — the same "background work, draw updates queued
// onto the UI goroutine" shape StartClock's own ticker already uses.
// Every queued update first checks ctx.Err(): once a newer search has
// cancelled this one (see cancelSearch/runSearch), any of this
// goroutine's own updates still sitting in the queue skip themselves
// instead of appending stale results (or a stale error) on top of the
// new search's own, already-cleared listing. Each arriving result also
// updates searchLastDir — see renderSearchStatus.
//
// The status line's own animation stops and settles on a final
// "Done — N found" once the search is over — noSearchResultsText(req)
// instead, specifically for N == 0, since an empty listing on its own
// reads exactly like a real directory that's simply empty, not "your
// search found nothing" (see its own doc comment).
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
			r.panel.appendSearchResult(res)
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
	r.app.QueueUpdateDraw(func() {
		if ctx.Err() != nil {
			return
		}
		status := fmt.Sprintf("Done — %d found", count)
		if count == 0 {
			status = noSearchResultsText(req)
		}
		r.setSearchStatus(status)
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
