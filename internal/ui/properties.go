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
// (TextView.SetDoneFunc fires for all four) — that's unchanged by the
// hash button (see computeHashes): captureInfoKey only intercepts 'h',
// leaving those four alone, and captureInfoMouse only intercepts a click
// that lands on the hash line specifically.
func (r *Root) newInfoView() *tview.TextView {
	v := tview.NewTextView().SetTextColor(tcell.ColorWhite)
	v.SetBackgroundColor(accentBackgroundColor)
	v.SetBorderPadding(0, 0, 1, 1)
	v.SetDoneFunc(func(tcell.Key) { r.closeInfo() })
	v.SetInputCapture(r.captureInfoKey)
	v.SetMouseCapture(r.captureInfoMouse)
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

	r.infoTarget = r.target
	r.infoStat = info
	r.renderInfo(nil) // nil: fresh open, nothing hashed yet even if this target was hashed before

	x, y, _, _ := r.menu.GetRect()
	width, height := textSize(r.info.GetText(true))
	x, y, width, height = r.clampToPanel(x, y, width, height)
	r.info.SetRect(x, y, width, height)

	r.showOverlay(infoPage, r.info)
}

// closeInfo hides the info overlay (Escape, Enter, or Tab).
func (r *Root) closeInfo() {
	r.hideOverlay()
}

// renderInfo rebuilds the Info overlay's text from whatever fsops.Stat
// found for the target it's currently showing (see openInfo) plus a hash
// section — a hint to compute them until hashes is non-nil, then the
// digests themselves (see hashLines) — appended for anything that isn't a
// directory. Also records where that section starts (hashSectionRow), so
// captureInfoMouse knows what counts as a click on it.
func (r *Root) renderInfo(hashes *fsops.Hashes) {
	text := formatInfo(r.infoStat)

	if r.infoStat.IsSymlink {
		if chain := fsops.ResolveChain(r.infoTarget); len(chain.Hops) > 1 {
			// Only shown once there's an actual chain (more than one
			// hop) — a simple single-target symlink is already fully
			// described by the "Link target" and "Type" fields above.
			text += "\n" + infoField("Chain", formatChain(chain))
		}
	}

	if !isDirish(r.infoStat) {
		r.hashSectionRow = strings.Count(text, "\n") + 2 // +1 past the fields' own last line, +1 for the blank separator
		text += "\n\n" + hashLines(hashes)
	}
	r.info.SetText(text)
}

// isDirish reports whether info should be treated as a directory for the
// hash button's purposes (see renderInfo/computeHashes/captureInfoMouse).
// info.IsDir alone isn't enough: it's Lstat-based, so it's always false
// for a symlink even when the symlink resolves to a directory — hashing
// that would just fail once fsops.Hash follows it there anyway, so
// there's no point offering the button in the first place.
func isDirish(info fsops.Info) bool {
	return info.IsDir || (info.IsSymlink && info.LinkIsDir)
}

// formatChain renders a multi-hop symlink chain as e.g.
// "/a -> /b -> /c (file)" (or "(directory)"/"(broken)"/
// "(cycle detected)"/"(too many hops)" instead) — see ResolveChain.
func formatChain(chain fsops.LinkChain) string {
	suffix := " (file)"
	switch {
	case chain.Broken:
		suffix = " (broken)"
	case chain.Cyclic:
		suffix = " (cycle detected)"
	case chain.TooDeep:
		suffix = " (too many hops)"
	case chain.FinalIsDir:
		suffix = " (directory)"
	}
	return strings.Join(chain.Hops, " -> ") + suffix
}

// computeHashes is the Info overlay's hash action (see hashLines and
// captureInfoKey/captureInfoMouse, its two triggers): hashes the entry
// Info is currently showing via fsops.Hash and re-renders the overlay
// with the results in place of the hint line. A no-op if Info isn't the
// open overlay, or its target is a directory, or resolves to one via
// isDirish (hashing isn't offered for those — see fsops.Hash's own doc
// comment on why).
func (r *Root) computeHashes() {
	if r.activePage != infoPage || isDirish(r.infoStat) {
		return
	}

	hashes, err := fsops.Hash(r.infoTarget)
	if err != nil {
		r.showError(err)
		return
	}

	r.renderInfo(&hashes)

	// The text just grew by two lines; keep the overlay sized to fit it,
	// same clamping openInfo's initial sizing uses.
	x, y, _, _ := r.info.GetRect()
	width, height := textSize(r.info.GetText(true))
	x, y, width, height = r.clampToPanel(x, y, width, height)
	r.info.SetRect(x, y, width, height)
}

// captureInfoKey adds "h computes hashes" to the Info overlay, alongside
// its existing Escape/Enter/Tab/Backtab close behavior (see newInfoView)
// — this only intercepts the one rune those don't already use.
func (r *Root) captureInfoKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyRune && event.Rune() == 'h' {
		r.computeHashes()
		return nil
	}
	return event
}

// captureInfoMouse makes the hash hint/result section at the bottom of
// the Info overlay (see hashLines) clickable — the same action the 'h'
// key triggers. Everything else in the overlay passes through unchanged;
// unlike Panel's header, there's no default TextView behavior here worth
// pre-empting, so a click that misses the hash section just does nothing.
func (r *Root) captureInfoMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if action != tview.MouseLeftClick || !r.info.InRect(event.Position()) {
		return action, event
	}

	_, y := event.Position()
	_, rectY, _, _ := r.info.GetInnerRect()
	if !isDirish(r.infoStat) && y-rectY >= r.hashSectionRow {
		r.computeHashes()
		return tview.MouseConsumed, nil
	}
	return action, event
}

// infoField renders one "Label: value" line in the Info overlay's fixed
// column layout — shared by formatInfo and hashLines so both stay
// visually aligned.
func infoField(label, value string) string {
	return fmt.Sprintf("%-13s%s", label+":", value)
}

// formatInfo renders info as labeled lines — the same facts `ls -halF`
// would show for one entry, but as a small properties list instead of a
// single packed line.
func formatInfo(info fsops.Info) string {
	lines := []string{
		infoField("Name", info.Name),
		infoField("Type", classifyKind(info)),
		infoField("Permissions", fmt.Sprintf("%s (%04o)", permString(info.Mode), info.Mode.Perm())),
	}
	if info.Nlink > 1 && !info.IsDir {
		// Not shown for directories: every directory has Nlink >= 2
		// trivially (its own "." entry, plus each subdirectory's ".."),
		// which isn't the "this content also exists under another name"
		// signal that's actually worth flagging.
		lines = append(lines, infoField("Links", fmt.Sprintf("%d (shared with other names)", info.Nlink)))
	}
	lines = append(lines,
		infoField("Owner", info.Owner),
		infoField("Group", info.Group),
		infoField("Size", sizeWithBytes(info.Size)),
		infoField("Modified", info.ModTime.Format("2006-01-02 15:04:05")),
		infoField("Path", info.Path),
	)
	if info.IsSymlink && info.LinkTarget != "" {
		lines = append(lines, infoField("Link target", info.LinkTarget))
	}
	if info.MountPoint {
		lines = append(lines, infoField("Mount point", "yes"))
	}

	return strings.Join(lines, "\n")
}

// classifyKind renders info's Type field with more detail than a plain
// file/directory/symlink split: a symlink additionally says what it
// resolves to (or that it's broken — info.LinkBroken), and the rarer
// special files (sockets, FIFOs, devices) get their own label instead of
// falling back to "file".
func classifyKind(info fsops.Info) string {
	switch {
	case info.IsSymlink && info.LinkBroken:
		return "broken symlink"
	case info.IsSymlink && info.LinkIsDir:
		return "symlink to directory"
	case info.IsSymlink:
		return "symlink to file"
	case info.IsDir:
		return "directory"
	case info.Mode&os.ModeSocket != 0:
		return "socket"
	case info.Mode&os.ModeNamedPipe != 0:
		return "FIFO"
	case info.Mode&os.ModeDevice != 0 && info.Mode&os.ModeCharDevice != 0:
		return "character device"
	case info.Mode&os.ModeDevice != 0:
		return "block device"
	default:
		return "file"
	}
}

// hashLines renders the Info overlay's hash section: a hint to compute
// them (see Root.computeHashes) until hashes is non-nil, then the three
// digests themselves.
func hashLines(hashes *fsops.Hashes) string {
	if hashes == nil {
		return "Press h or click here to compute MD5 / SHA-1 / SHA-256"
	}
	return strings.Join([]string{
		infoField("MD5", hashes.MD5),
		infoField("SHA-1", hashes.SHA1),
		infoField("SHA-256", hashes.SHA256),
	}, "\n")
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

// sizeWithBytes renders size as its human-readable form (see humanSize)
// followed by the exact byte count, e.g. "2.1K (2184 bytes)" — the
// shorthand's rounding hides the kind of precision that matters when
// comparing two similarly-sized files. Below 1024 bytes, humanSize is
// already exact (e.g. "512B"), so there's nothing to add.
func sizeWithBytes(size int64) string {
	human := humanSize(size)
	if size < 1024 {
		return human
	}
	return fmt.Sprintf("%s (%d bytes)", human, size)
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
