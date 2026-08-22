package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const errorPage = "error"

// errorViewMaxWidth caps how wide an error overlay grows before its text
// starts wrapping instead. Filesystem errors embed full paths, which are
// easily wider than the terminal and read badly as one long line.
const errorViewMaxWidth = 60

// newErrorView creates the overlay used to report failures the user
// needs to know about — a rename that was refused, a directory that
// can't be read. Escape, Enter, Tab, and Backtab all dismiss it
// (TextView.SetDoneFunc fires for all four), as does a click outside or
// Ctrl+C (see Root.RequestCancel).
func (r *Root) newErrorView() *tview.TextView {
	v := tview.NewTextView()
	v.SetBorderPadding(0, 0, 1, 1)
	v.SetDoneFunc(func(tcell.Key) { r.hideOverlay() })
	return v
}

// showError displays err in a centered overlay. It is the single place
// errors surface in the UI: Panel reports its own failures through the
// callback Root installs (see Panel.onError), and Root's own actions call
// this directly. A nil error is ignored, so callers can pass a result
// straight through without checking first.
func (r *Root) showError(err error) {
	if err == nil {
		return
	}

	text := strings.Join(wrapText(err.Error(), r.errorWidth()), "\n")
	r.errorView.SetText(text)

	width, height := textSize(text)
	_, _, screenWidth, screenHeight := r.GetRect() // Root fills the whole screen
	x, y, width, height := r.clampToPanel((screenWidth-width)/2, (screenHeight-height)/2, width, height)

	r.errorView.SetRect(x, y, width, height)
	r.showOverlay(errorPage, r.errorView)
}

// errorWidth returns the column width error text is wrapped to: the
// configured maximum, or less if the panel itself is narrower.
func (r *Root) errorWidth() int {
	width := errorViewMaxWidth

	// Leave room for the overlay's own 1-column padding on each side.
	if _, _, panelWidth, _ := r.panel.GetInnerRect(); panelWidth > 2 && width > panelWidth-2 {
		width = panelWidth - 2
	}
	if width < 1 {
		width = 1
	}
	return width
}

// wrapText breaks text into lines of at most width columns, preferring to
// break at spaces. A single word longer than the whole line — a long path
// in an error message, typically — is hard-split rather than allowed to
// overflow. Widths are counted in runes, so a multi-byte character is
// never cut in half.
func wrapText(text string, width int) []string {
	if width < 1 {
		width = 1
	}

	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		line := ""
		flush := func() {
			// Hard-split anything still too long for one line.
			for len([]rune(line)) > width {
				runes := []rune(line)
				lines = append(lines, string(runes[:width]))
				line = string(runes[width:])
			}
		}

		for _, word := range strings.Fields(paragraph) {
			switch {
			case line == "":
				line = word
			case len([]rune(line))+1+len([]rune(word)) <= width:
				line += " " + word
			default:
				flush()
				lines = append(lines, line)
				line = word
			}
		}

		flush()
		lines = append(lines, line)
	}

	return lines
}
