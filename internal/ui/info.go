package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

const infoPage = "info"

// newInfoView creates the read-only overlay used to show the "Info"
// action's output. Escape, Enter, Tab, and Backtab all dismiss it
// (TextView.SetDoneFunc fires for all four).
func (r *Root) newInfoView() *tview.TextView {
	v := tview.NewTextView().SetTextColor(tcell.ColorWhite)
	v.SetBackgroundColor(accentBackgroundColor)
	v.SetBorderPadding(0, 0, 1, 1)
	v.SetDoneFunc(func(tcell.Key) { r.closeInfo() })
	return v
}

// openInfo is the context menu's "Info" action: it shows everything
// breakthrough currently knows about the target — roughly what
// `ls -halF` prints for one entry — gathered natively via fsops.Stat
// rather than by shelling out to and parsing ls (see the Phase 1 design
// discussion for why generic command-output parsing doesn't scale here).
func (r *Root) openInfo() {
	info, err := fsops.Stat(r.target)
	if err != nil {
		r.hideOverlay() // close the context menu before reporting
		r.showError(err)
		return
	}

	text := formatInfo(info)
	r.info.SetText(text)

	x, y, _, _ := r.menu.GetRect()
	width, height := textSize(text)
	x, y, width, height = r.clampToPanel(x, y, width, height)
	r.info.SetRect(x, y, width, height)

	r.showOverlay(infoPage, r.info)
}

// closeInfo hides the info overlay (Escape, Enter, or Tab).
func (r *Root) closeInfo() {
	r.hideOverlay()
}

// formatInfo renders info as labeled lines — the same facts `ls -halF`
// would show for one entry, but as a small properties list instead of a
// single packed line.
func formatInfo(info fsops.Info) string {
	kind := "file"
	switch {
	case info.IsSymlink:
		kind = "symlink"
	case info.IsDir:
		kind = "directory"
	}

	field := func(label, value string) string {
		return fmt.Sprintf("%-13s%s", label+":", value)
	}

	lines := []string{
		field("Name", info.Name),
		field("Type", kind),
		field("Permissions", fmt.Sprintf("%s (%04o)", permString(info.Mode), info.Mode.Perm())),
		field("Owner", info.Owner),
		field("Group", info.Group),
		field("Size", humanSize(info.Size)),
		field("Modified", info.ModTime.Format("2006-01-02 15:04:05")),
		field("Path", info.Path),
	}
	if info.IsSymlink && info.LinkTarget != "" {
		lines = append(lines, field("Link target", info.LinkTarget))
	}

	return strings.Join(lines, "\n")
}

// permString renders mode roughly the way `ls -l` does: a one-character
// file type followed by the nine rwx permission characters. Unlike ls, it
// doesn't yet render setuid/setgid/sticky as the s/S/t/T variants in the
// execute-bit position — a known simplification.
func permString(mode os.FileMode) string {
	typeChar := byte('-')
	switch {
	case mode&os.ModeDir != 0:
		typeChar = 'd'
	case mode&os.ModeSymlink != 0:
		typeChar = 'l'
	case mode&os.ModeNamedPipe != 0:
		typeChar = 'p'
	case mode&os.ModeSocket != 0:
		typeChar = 's'
	case mode&os.ModeDevice != 0 && mode&os.ModeCharDevice != 0:
		typeChar = 'c'
	case mode&os.ModeDevice != 0:
		typeChar = 'b'
	}

	const rwx = "rwxrwxrwx"
	perm := mode.Perm()
	buf := make([]byte, 0, 10)
	buf = append(buf, typeChar)
	for i, c := range rwx {
		if perm&(1<<uint(9-1-i)) != 0 {
			buf = append(buf, byte(c))
		} else {
			buf = append(buf, '-')
		}
	}
	return string(buf)
}

// humanSize renders size the way `ls -h` does: 1024-based, one decimal
// once it's above the smallest unit (e.g. "4.0K", "1.2M").
func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(size)/float64(div), "KMGTPE"[exp])
}

// textSize returns the width (the longest line, plus 1-char left/right
// padding — matching listSize) and height (line count) of a block of
// text, for sizing a no-border overlay to fit it exactly.
func textSize(text string) (width, height int) {
	lines := strings.Split(text, "\n")
	height = len(lines)
	for _, l := range lines {
		if w := len([]rune(l)); w > width {
			width = w
		}
	}
	return width + 2, height
}
