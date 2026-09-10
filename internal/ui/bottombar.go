package ui

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// buttonBarSpan is one clickable region within the button bar's text —
// the same half-open [start,end) column-range idea as headerSpan, for a
// single-line display so no row is needed.
//
// Runs the same closure a keypress would (see keymap.go's own
// plainCommand.action/chordFamily), rather than switching on a separate,
// hand-maintained action enum: the button bar is a view onto the same
// registry the keyboard layer already reads, so a button and its key
// can never drift apart from each other the way two independently
// maintained lists eventually do. key identifies which registry entry a
// span came from — 0 for one that isn't tied to a single plain key
// (there is currently no such span, but the field costs nothing to keep
// available) — for the rare caller that needs to recognize one
// particular button rather than just click it (see
// Root.captureOutsideClick's own Details carve-out).
type buttonBarSpan struct {
	startCol, endCol int
	key              rune
	run              func(r *Root)
}

// newBottomBar builds the three rows below the panel: bashConsole (see
// newBashConsole, in bashconsole.go — bashLine, a multi-line shell
// command/script editor, plus bashHistoryView, its scrollable output
// transcript), buttonBar, a hand-built single line of quick-action
// buttons (see buildButtonBar), and statusBar, a purely informational
// line (see refreshStatusBar/buildStatusBar). NewRoot adds all three to
// mainLayout beneath the panel.
//
// buttonBar's initial text is built here (see buildButtonBar), but —
// unlike when this was first written — it's no longer fixed for the
// run of the program: refreshButtonBar rebuilds it on the same
// Panel.onLoad wiring statusBar already uses below, since which
// buttons even appear now depends on the panel's current directory
// (see buildButtonBar's own doc comment). statusBar starts blank;
// NewRoot's caller is expected to call refreshStatusBar once real data
// (the panel's own directory) is available, the same as it always has.
func (r *Root) newBottomBar() {
	r.newBashConsole()

	r.buttonBar = tview.NewTextView()
	r.buttonBar.SetDynamicColors(true)
	r.buttonBar.SetMouseCapture(r.captureButtonBarMouse)
	text, spans := r.buildButtonBar()
	r.buttonBarSpans = spans
	r.buttonBar.SetText(text)

	r.statusBar = tview.NewTextView()
	r.statusBar.SetDynamicColors(true)
	// Deliberately no SetMouseCapture: statusBar is purely informational
	// now, nothing in it is clickable — see buttonBar above for that.

	r.currentUser = currentUsername()
}

// currentUsername resolves the running process's own username, the same
// way fsops.Stat's Owner field would report it for a file this user
// owns — falls back to $USER if user.Current() itself fails for some
// reason (e.g. no matching /etc/passwd entry), rather than showing
// nothing at all.
func currentUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// refreshStatusBar rebuilds and redraws the (purely informational, no
// buttons — see buildButtonBar for those) status bar's text — called
// whenever anything it shows might have changed: the panel navigating
// (disk usage depends on the current directory — see Panel.onLoad,
// wired in NewRoot), and once a second from StartClock's ticker (the
// clock itself).
func (r *Root) refreshStatusBar() {
	r.statusBar.SetText(r.buildStatusBar())
}

// refreshButtonBar rebuilds and redraws the button bar's text — called
// from the same Panel.onLoad wiring refreshStatusBar uses (see NewRoot),
// since buildButtonBar's own output now depends on the panel's current
// directory (in/out of the trash — see inTrash) as well as its
// showHidden flag, both of which a navigation, toggleHidden's own
// reload, or a trash operation's reloadPanel can change.
func (r *Root) refreshButtonBar() {
	text, spans := r.buildButtonBar()
	r.buttonBarSpans = spans
	r.buttonBar.SetText(text)
}

// buildButtonBar renders the button bar's text: the always-visible
// legend for the keyboard layer's own "quick" subset (see
// plainCommand.quick/chordFamily.quick in keymap.go) — every entry a
// registry lookup away from the key that also runs it, so this can
// never say something the keyboard itself no longer does. A chord
// family renders as "prefix… name" (the same ellipsis
// resolveChord/chordHintBar use for "there's more here"), marking it as
// a cascade rather than a single keystroke.
//
// Two labels are computed rather than read straight from the registry,
// since they name what pressing the key does *next*, which depends on
// state buildButtonBar's own caller already knows to refresh on: hidden
// files reads "Hide" or "Unhide" depending on r.panel.showHidden (see
// hideUnhideLabel), and split reads "Split" or "Unsplit" depending on
// r.splitActive (see splitButtonLabel) — refreshButtonBar re-renders
// this whole bar whenever either one changes.
func (r *Root) buildButtonBar() (text string, spans []buttonBarSpan) {
	type buttonSpec struct {
		label string
		key   rune
		run   func(r *Root)
		chord bool // a chord-family cascade cell, not a plain command — see the block separator below
	}

	// keyBG highlights the actual key to press within a cell's own label
	// — one space either side of the letter, colored with the same
	// ButtonBackground every real button in this app already uses (see
	// styleButton) — the same treatment chordHintBar gives a chord's own
	// second key, applied here to this row's own top-level keys too, per
	// the user's own explicit request that both read the same way. The
	// label immediately follows highlightKey's own trailing space with
	// no space of its own added — per the user's own later explicit
	// request to close the gap between key and label to exactly one
	// space, not two, while keeping the highlight itself untouched.
	keyBG := colorTag(r.theme.ButtonBackground)
	highlightKey := func(key rune) string {
		return fmt.Sprintf("[:%s:] %c [-:-:-]", keyBG, key)
	}

	var buttons []buttonSpec
	for _, c := range plainCommands() {
		if !c.quick {
			continue
		}
		label := c.short
		switch c.key {
		case '.':
			label = hideUnhideLabel(r.panel.showHidden)
		case 's':
			label = splitButtonLabel(r.splitActive)
		}
		buttons = append(buttons, buttonSpec{
			label: highlightKey(c.key) + label,
			key:   c.key,
			run:   c.action,
		})
	}
	for _, f := range chordFamilies() {
		if !f.quick {
			continue
		}
		family := f // capture per iteration, not the loop variable
		buttons = append(buttons, buttonSpec{
			label: highlightKey(family.prefix) + "… " + family.name,
			key:   family.prefix,
			run:   func(r *Root) { r.startChord(family) },
			chord: true,
		})
	}

	// A single plain space between two ordinary buttons — per the user's
	// own explicit clarification, this is deliberately its own separator,
	// not the same thing as highlightKey's own leading space (which the
	// user considers part of that next button's own highlight, not
	// inter-button spacing at all): each button gets one space after it,
	// full stop, regardless of what the next button's own label happens
	// to start with. " │ " (U+2502, the same box-drawing vertical bar
	// buildStatusBar's own sep already uses one row below this) replaces
	// that plain space for one specific transition: right before the
	// chord-family cascades (g/p/z/o — always last, appended as their own
	// block just above) start, setting that whole block apart from the
	// plain commands before it — unlike an ordinary button, one of these
	// doesn't run its own action directly, it starts a whole second
	// keystroke instead, and the heavier separator marks that "this group
	// behaves differently" the same way chordHintBar's own trailing
	// " │ " sets "Esccancel" apart from its members below.
	const sep = " "
	const blockSep = " │ "

	var b strings.Builder
	col := 0

	// col advances by s's display width (tview.TaggedStringWidth), not a
	// plain rune count — a button label could in principle contain
	// double-width (e.g. CJK) characters some day, and a rune count
	// would misalign every buttonBarSpan after it (see buildHeaderSpans/
	// propertiesBuilder.text for the same fix elsewhere).
	write := func(s string) {
		b.WriteString(s)
		col += tview.TaggedStringWidth(s)
	}
	for i, bt := range buttons {
		if i > 0 {
			if bt.chord && !buttons[i-1].chord {
				write(blockSep)
			} else {
				write(sep)
			}
		}
		start := col
		write(bt.label)
		spans = append(spans, buttonBarSpan{startCol: start, endCol: col, key: bt.key, run: bt.run})
	}

	return b.String(), spans
}

// hideUnhideLabel is buildButtonBar's own computed label for the "."
// entry — see that func's doc comment for why this one specifically
// can't be a static field on the registry entry.
func hideUnhideLabel(showHidden bool) string {
	if showHidden {
		return "Hide"
	}
	return "Unhide"
}

// buildStatusBar renders the status bar's text: the current user,
// whether mouse reporting is currently on or off (see mouseStatusText/
// toggleMouseReporting), disk and inode usage for the panel's
// current directory (see fsops.FetchDiskUsage), the running kernel
// release (see kernelVersionText), uptime and load average where
// available (see uptimeText/loadAverageText — Linux only, gracefully
// omitted elsewhere, the same "just show one less segment" degradation
// fsops.FetchDiskUsage itself already has), and the clock. No buttons
// here any more — see buildButtonBar.
func (r *Root) buildStatusBar() string {
	var b strings.Builder
	write := func(s string) { b.WriteString(s) }
	sep := func() { write(" │ ") }

	// A pending chord's own countdown (see chordIndicatorText), leading
	// rather than trailing: it needs to be seen immediately, and the
	// segments after it (disk usage, uptime, load) are each already
	// optional on their own platform, so anything placed after them
	// would shift around depending on what happened to be available —
	// exactly the instability a fixed leading position avoids. Still
	// purely informational, same as every other segment here: it has no
	// click target of its own, matching this bar's own long-standing
	// "no buttons at all" rule.
	if chord := r.chordIndicatorText(); chord != "" {
		write(chord)
		sep()
	}

	// A running Paste's own progress takes this same leading spot instead
	// of the clipboard indicator below while one is actually in flight —
	// "what's copying right now" is more specific and more time-sensitive
	// than "what's staged to paste", so it wins for however long there's
	// something to say. Reverts to the clipboard indicator the moment
	// r.pasteJob clears (see finishPasteJob), the same instant the two
	// would otherwise have said contradictory things (a Cut's own
	// clipboard normally empties out right as its Paste finishes).
	switch {
	case r.pasteJob != nil:
		write(pasteProgressText(r.pasteJob, len(r.pasteQueue)))
		sep()
	default:
		// The clipboard's own contents, if anything — right after the
		// chord indicator and before the username, the same leading,
		// fixed position and the same reasoning: it needs to be seen
		// without hunting for it, and everything after the username is
		// each already optional on its own platform, so a fixed spot
		// ahead of all of that is the one place adding or removing this
		// segment never shifts something else around. Empty (no leading
		// text, no separator) once the clipboard itself is empty again,
		// the same "just show one less segment" shape as disk usage/
		// uptime/load below.
		if clip := clipboardIndicatorText(r.clipboardCut, r.clipboardDirs, r.clipboardFiles); clip != "" {
			write(clip)
			sep()
		}
	}

	write(r.currentUser)
	sep()
	write(mouseStatusText(r.mouseEnabled))
	sep()
	if u, ok := fsops.FetchDiskUsage(r.panel.path); ok {
		write(diskUsageText(u, r.theme))
		sep()
		write(inodeUsageText(u, r.theme))
		sep()
	}
	if k := kernelVersionText(); k != "" {
		write(k)
		sep()
	}
	if up, ok := uptimeText(); ok {
		write(up)
		sep()
	}
	if load, ok := loadAverageText(); ok {
		write(load)
		sep()
	}
	write(clockText())

	return b.String()
}

// clipboardIndicatorText renders buildStatusBar's own clipboard segment
// — "" once dirs and files are both 0 (nothing on the clipboard, the
// overwhelmingly common case), otherwise "Copy: N files, M dirs" or
// "Cut: N files, M dirs" (whichever of dirs/files is 0 dropped
// entirely, rather than shown as "0 dirs" — noise, not information).
// "Copy"/"Cut" name the pending operation itself, not "Copying"/
// "Cutting": nothing is actually in flight yet at this point — Paste
// hasn't been pressed — and this same text keeps showing, unchanged,
// for as long as the clipboard holds these paths, including through a
// Copy+Paste that leaves them there for a possible second Paste
// elsewhere. See config.Theme.ClipboardCopyBackground/
// ClipboardCutBackground for this same information's other half — the
// row highlighting a real file's own line gets while it's held.
func clipboardIndicatorText(cut bool, dirs, files int) string {
	if dirs == 0 && files == 0 {
		return ""
	}
	verb := "Copy"
	if cut {
		verb = "Cut"
	}
	var parts []string
	if files > 0 {
		parts = append(parts, pluralCount(files, "file", "files"))
	}
	if dirs > 0 {
		parts = append(parts, pluralCount(dirs, "dir", "dirs"))
	}
	return fmt.Sprintf("%s: %s", verb, strings.Join(parts, ", "))
}

// pluralCount renders n paired with singular or plural, whichever n
// itself calls for ("1 file", "2 files") — used wherever this bar
// counts something instead of just naming it, starting with
// clipboardIndicatorText above.
func pluralCount(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// pasteProgressBarWidth is how many characters wide pasteProgressText's
// own dual bar (see pasteDualBar) is — narrow enough to leave room for
// the segments after it (username, disk usage, uptime, ...), wide
// enough to actually read as a bar rather than a handful of ambiguous
// pixels.
const pasteProgressBarWidth = 10

// pasteDualBarLit/pasteDualBarDim are the two colors pasteDualBar fills
// each half-block character with — this bar's own fixed look,
// deliberately not part of the theme system config.Theme's other
// colors go through: a status-bar progress indicator, like the chord
// countdown's own block glyphs (see chordCountdownBlocks) or the
// spinner (see hashAnimationFrames), has never been a themed element
// in this app.
const (
	pasteDualBarLit = "green"
	pasteDualBarDim = "gray"
)

// pasteDualBar renders a width-wide row of upper-half-block characters
// (▀), each one's own foreground painting that column's top half and
// background painting its bottom half — a real terminal rendering
// behavior of that specific glyph, not a tview trick, which is what
// lets a single row of characters carry two independent fractions at
// once, per the user's own explicit request: topFrac (the same
// item-level "how much of the clipboard has a final outcome" fraction
// the old single bar showed) above, fileFrac (the file currently being
// streamed's own byte progress) below. Both clamped to [0,1] first — a
// fraction exceeding 1 (shouldn't happen, but cheap to guard, the same
// reasoning the old bar's own done>total clamp already followed) would
// otherwise overfill past width. Ends with a reset tag so whatever
// pasteProgressText appends after it (the ETA, the current file's own
// name) isn't left drawn in this bar's own last column's colors.
func pasteDualBar(topFrac, fileFrac float64, width int) string {
	topLit := int(clampFrac(topFrac) * float64(width))
	fileLit := int(clampFrac(fileFrac) * float64(width))

	var b strings.Builder
	for i := 0; i < width; i++ {
		fg, bg := pasteDualBarDim, pasteDualBarDim
		if i < topLit {
			fg = pasteDualBarLit
		}
		if i < fileLit {
			bg = pasteDualBarLit
		}
		fmt.Fprintf(&b, "[%s:%s]▀", fg, bg)
	}
	b.WriteString("[-:-]")
	return b.String()
}

// clampFrac clamps f to [0,1] — shared by every fraction this file
// turns into a glyph or a bar column, so a value that briefly strays
// outside that range (a size read mid-write growing past what an
// earlier stat reported, say) can never overfill or index out of
// bounds anywhere that happens.
func clampFrac(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// pasteBytesColumn renders bytesDone/bytesTotal as a single character
// from chordCountdownBlocks' own eight-level glyph set — the same "one
// lone character stands in for a fraction, no color needed" idea the
// chord countdown (see chordIndicatorText) already uses, per the
// user's own explicit request to style this the same way, just filling
// upward (0% is the thinnest sliver, 100% is a full block) instead of
// that one's own draining-downward direction, since this represents
// progress accumulating rather than time running out. Callers check
// bytesTotal > 0 themselves before calling this at all (see
// pasteProgressText and pasteJob.bytesTotal's own doc comment on what
// <= 0 means), so this only ever has to handle an already-known,
// positive total.
func pasteBytesColumn(bytesDone, bytesTotal int64) string {
	frac := clampFrac(float64(bytesDone) / float64(bytesTotal))
	idx := int(frac * float64(len(chordCountdownBlocks)-1))
	return string(chordCountdownBlocks[len(chordCountdownBlocks)-1-idx])
}

// pasteETA estimates a paste job's own remaining time from the average
// throughput observed since it started (bytesDone/elapsed) rather than
// an instantaneous rate sampled between two ticks — inherently
// smoother, since the denominator only ever grows, and needs no state
// of its own beyond the job's own start time (see pasteJob.startedAt),
// already recorded for exactly this. ok is false whenever an estimate
// wouldn't mean anything yet — no time has passed, nothing has copied
// yet, or the total is already reached — so the caller
// (pasteProgressText) simply omits the segment rather than showing a
// division-by-zero result or a stale "0s left" once the job is already
// wrapping up.
func pasteETA(startedAt time.Time, bytesDone, bytesTotal int64) (string, bool) {
	elapsed := time.Since(startedAt)
	if bytesDone <= 0 || elapsed <= 0 || bytesTotal <= bytesDone {
		return "", false
	}
	rate := float64(bytesDone) / elapsed.Seconds()
	remainingSeconds := float64(bytesTotal-bytesDone) / rate
	return formatETA(time.Duration(remainingSeconds * float64(time.Second))), true
}

// formatETA renders d as a compact "~Ns"/"~Mm Ns"/"~Hh Mm" — never more
// than two units, rounded to the nearest whole second. Prefixed with
// "~" throughout: this is always an extrapolation from an average
// rate observed so far, never a guarantee.
func formatETA(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d.Round(time.Second).Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds / 60) % 60
	seconds := totalSeconds % 60
	switch {
	case hours > 0:
		return fmt.Sprintf("~%dh %dm left", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("~%dm %ds left", minutes, seconds)
	default:
		return fmt.Sprintf("~%ds left", seconds)
	}
}

// pasteProgressText renders buildStatusBar's own "a Paste is running"
// segment, replacing the clipboard indicator for as long as job is
// non-nil (see buildStatusBar's own doc comment on why one wins over
// the other): a spinner (reusing hashAnimationFrames — the same visual
// language as every other "in progress" indicator this app already
// has, driven by animatePasteProgress's own ticker), "Copying"/"Moving"
// naming which of the two this actually is, how many of the clipboard's
// own top-level items have a final outcome so far, a leading
// byte-percentage column and a dual progress bar once the job's own
// byte total is known (see pasteBytesColumn/pasteDualBar and
// pasteJob.bytesTotal's own doc comment for what "known" means and why
// it isn't known from the very first tick), an estimated remaining
// duration once that same total makes one possible (see pasteETA), and
// the real file fsCopy/fsMove most recently reported touching (see
// pasteJob.currentFile) — its bare name, not the full path: the path is
// wherever the paste's own destination already says it's going, and a
// long one would crowd out every segment after it.
//
// The N/total count and the dual bar's own top half are per top-level
// clipboard item, not per file: a directory only advances either one
// once, when the whole thing finishes, no matter how many files it
// contains — currentFile (and the bar's own bottom half) is what
// actually moves during that stretch, updating per real file
// underneath it even while the top half sits still.
//
// queued is len(r.pasteQueue) at render time — a trailing "(+N
// queued)" once a further Paste is waiting behind this one (see
// startPaste/advancePasteQueue), omitted entirely at zero rather than
// shown as "(+0 queued)": the whole point is to say something is
// waiting, not to always report a count that's usually zero.
func pasteProgressText(job *pasteJob, queued int) string {
	verb := "Copying"
	if job.cut {
		verb = "Moving"
	}
	done := job.total - job.remaining
	if done < 0 {
		done = 0
	}
	spinner := hashAnimationFrames[job.animFrame%len(hashAnimationFrames)]

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %d/%d ", spinner, verb, done, job.total)

	bytesTotal := job.bytesTotal.Load()
	bytesDone := job.bytesBase.Load() + job.currentFileBytes.Load()
	if bytesTotal > 0 {
		b.WriteString(pasteBytesColumn(bytesDone, bytesTotal))
		b.WriteByte(' ')
	}

	itemFrac := 0.0
	if job.total > 0 {
		itemFrac = float64(done) / float64(job.total)
	}
	fileFrac := 0.0
	if size := job.currentFileSize.Load(); size > 0 {
		fileFrac = float64(job.currentFileBytes.Load()) / float64(size)
	}
	b.WriteString(pasteDualBar(itemFrac, fileFrac, pasteProgressBarWidth))

	if bytesTotal > 0 {
		if eta, ok := pasteETA(job.startedAt, bytesDone, bytesTotal); ok {
			b.WriteByte(' ')
			b.WriteString(eta)
		}
	}

	if current := job.currentFile.Load(); current != nil && *current != "" {
		b.WriteByte(' ')
		b.WriteString(filepath.Base(*current))
	}

	if queued > 0 {
		fmt.Fprintf(&b, " (+%d queued)", queued)
	}
	return b.String()
}

// mouseStatusText renders buildStatusBar's own "Mouse on"/"Mouse off"
// segment — per a real user report: enabling mouse reporting at all
// (needed for this app's own clicks/drags) hands every mouse event to
// breakthrough instead of the terminal emulator, which breaks that
// terminal's own native text selection/copy for anyone who doesn't
// already know its own override gesture (Shift-drag, on most
// xterm-derived emulators). The "om" chord (see toggleMouseReporting)
// is a plain, memorable way to turn reporting off (and back on) without
// needing to know that gesture at all — this is what lets the status
// bar answer "is it currently on" at a glance, the same way the
// Hide/Unhide button already names what its own next click will do
// rather than leaving the current state to be inferred.
func mouseStatusText(enabled bool) string {
	if enabled {
		return "Mouse on"
	}
	return "Mouse off"
}

// kernelVersionText returns `uname -r`'s own output, trimmed — the same
// "shell out to a real system tool" approach fsops.FetchDiskUsage's own
// df already uses, rather than a syscall wrapper needing per-platform
// struct handling for what's ultimately just one string. Returns "" if
// uname isn't available (e.g. some minimal containers) — the status bar
// just shows one less segment then.
func kernelVersionText() string {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// uptimeText and loadAverageText read Linux's /proc/uptime and
// /proc/loadavg directly rather than shelling out to and parsing
// `uptime`'s own output, whose format differs enough between GNU/Linux
// and BSD/macOS (singular/plural "load average(s)", comma- vs.
// space-separated numbers, different time formats — verified by actually
// comparing sample output from both, not guessed) to make reliable
// cross-platform parsing more fragile than it's worth for a status line.
// Both simply return ok=false where /proc/... doesn't exist at all (e.g.
// macOS, most BSDs) — no build tag needed, os.ReadFile's own error
// already tells the two cases apart.
func uptimeText() (string, bool) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "", false
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "", false
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "", false
	}
	return "up " + formatUptime(time.Duration(seconds*float64(time.Second))), true
}

// formatUptime renders d as "NdHH:MM" once it's reached a full day, or
// just "HH:MM" before that — the same shape `uptime`'s own "N days,
// HH:MM" takes, just compact enough for a status line already showing
// several other segments.
func formatUptime(d time.Duration) string {
	totalMinutes := int(d.Minutes())
	days := totalMinutes / (24 * 60)
	hours := (totalMinutes / 60) % 24
	minutes := totalMinutes % 60
	if days > 0 {
		return fmt.Sprintf("%dd %02d:%02d", days, hours, minutes)
	}
	return fmt.Sprintf("%02d:%02d", hours, minutes)
}

func loadAverageText() (string, bool) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return "", false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return "", false
	}
	return fmt.Sprintf("load %s %s %s", fields[0], fields[1], fields[2]), true
}

// diskUsageWarnColor is the color a usage percentage should stand out
// in — warn.CriticalText at 90% or more, warn.WarningText at 80% or
// more, tcell.ColorDefault (no warning, leave the surrounding text's
// own color alone) otherwise — the two thresholds the user asked for,
// shared by both the disk-space and the inode percentage. Reads its two
// "stand out" colors from the active theme (see
// config.Theme.WarningText/CriticalText's own doc comment) rather than
// a hardcoded tcell.ColorOrange/tcell.ColorRed, so a scheme that already
// leans orange or red elsewhere can still make this specific warning
// legible against it.
func diskUsageWarnColor(percent int, warn config.ResolvedTheme) tcell.Color {
	switch {
	case percent >= 90:
		return warn.CriticalText
	case percent >= 80:
		return warn.WarningText
	default:
		return tcell.ColorDefault
	}
}

// formatUsagePercent renders percent as "N%", wrapped in a foreground-
// only tview color tag (see colorTag — the same "#rrggbb", not a color
// name, so it round-trips exactly through tview's own tag parser) once
// diskUsageWarnColor says it should stand out — "[-]" resets just the
// foreground back to the status bar's own configured text color
// afterward, not a hardcoded one, so this still looks right under
// every color scheme (see Root.applyTheme).
func formatUsagePercent(percent int, theme config.ResolvedTheme) string {
	color := diskUsageWarnColor(percent, theme)
	if color == tcell.ColorDefault {
		return fmt.Sprintf("%d%%", percent)
	}
	return fmt.Sprintf("[%s]%d%%[-]", colorTag(color), percent)
}

// diskUsageText and inodeUsageText render one labeled "Label X used, Y
// free (Z%)" status-bar segment each — explicit "used"/"free" labels
// (not just two bare numbers) precisely because the user reported the
// previous, unlabeled df dump as unreadable ("man weiß gar nicht was
// die heißen sollen"), and explicit used *and* free numbers for
// inodes specifically, per the user's own request, rather than just a
// percentage.
func diskUsageText(u fsops.DiskUsage, theme config.ResolvedTheme) string {
	return fmt.Sprintf("Disk %s used, %s free (%s)", humanSize(u.UsedBytes), humanSize(u.AvailBytes), formatUsagePercent(u.UsePercent, theme))
}

func inodeUsageText(u fsops.DiskUsage, theme config.ResolvedTheme) string {
	return fmt.Sprintf("Inodes %s used, %s free (%s)", humanCount(u.UsedInodes), humanCount(u.AvailInodes), formatUsagePercent(u.InodePercent, theme))
}

// humanCount renders n the same way humanSize renders a byte count
// (1024-based grouping, one decimal above the smallest unit) but
// without humanSize's own "B" suffix — appropriate for a plain count
// (inodes) rather than a size in bytes, which "512B" would misleadingly
// suggest this was.
func humanCount(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(n)/float64(div), "KMGTPE"[exp])
}

// clockText renders the current date, time, and the local timezone's
// abbreviation (e.g. "CEST", "UTC") — whatever the OS's own timezone
// database reports, via Go's "MST" format verb.
func clockText() string {
	return time.Now().Format("2006-01-02 15:04:05 MST")
}

// buttonBarActionAt returns the buttonBarAction at screen position
// (x, y), if any — the column lookup captureButtonBarMouse itself needs
// to actually run one, factored out so captureOutsideClick can also ask
// "what would a click here do" without running it (see its own
// buttonActionDetails carve-out, letting a Details click through while
// Properties is open instead of treating it as a click outside the
// overlay).
func (r *Root) buttonBarActionAt(x, y int) (buttonBarSpan, bool) {
	if !r.buttonBar.InRect(x, y) {
		return buttonBarSpan{}, false
	}
	rectX, _, _, _ := r.buttonBar.GetInnerRect()
	col := x - rectX
	for _, s := range r.buttonBarSpans {
		if col >= s.startCol && col < s.endCol {
			return s, true
		}
	}
	return buttonBarSpan{}, false
}

// captureButtonBarMouse routes a click on one of the button bar's
// buttons (see buildButtonBar/buttonBarSpan) to its action. A click
// elsewhere on the row (the gaps between buttons, or empty space) just
// does nothing.
//
// InRect is checked before the action-type gate, not folded into the
// same condition — a real, user-reported regression otherwise:
// combining them (as this used to) let a MouseLeftDown that landed
// inside buttonBar's own rect fall through to its default TextView
// MouseHandler unsuppressed (verified directly against tview's own
// textview.go — its MouseLeftDown case unconditionally calls setFocus),
// stealing real keyboard focus onto the button bar itself and leaving
// c/x/v/d and every other plain-key shortcut dead afterward, exactly
// the same class of bug captureColumnHeaderMouse's own doc comment
// describes. Checking InRect first and unconditionally suppressing
// anything that isn't MouseLeftClick — the same shape
// captureHeaderMouse/captureTabStripMouse already use — closes it: a
// button bar click is never anything more than a one-shot trigger, so
// there is nothing for it to hold focus for afterward.
func (r *Root) captureButtonBarMouse(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse) {
	if !r.buttonBar.InRect(event.Position()) {
		return action, event
	}
	if action != tview.MouseLeftClick {
		return tview.MouseConsumed, nil
	}

	x, y := event.Position()
	if span, ok := r.buttonBarActionAt(x, y); ok && span.run != nil {
		// Called directly, unguarded, unlike the keyboard equivalent of
		// most of these (see acceptsPlainKeyCommand): a click is always a
		// deliberate, explicit action on whatever it landed on, with none
		// of the "is something else currently typing" ambiguity a global
		// keystroke has to rule out first.
		span.run(r)
	}
	return tview.MouseConsumed, nil
}

// editCurrentEntry is the Edit button/'e' key's actual action, also
// reused directly as the context menu's own "Edit" item (see NewRoot):
// a right-click already moves the table's cursor to the clicked row
// before the menu opens (see captureMouse's MouseRightClick case), so
// reading it here targets the same entry either way. Runs the
// configured editor (see editorCommand) on whichever entry the table's
// cursor is currently on. A no-op on the ".." row or an empty panel
// (Panel.CurrentRowPath's ok=false).
func (r *Root) editCurrentEntry() {
	_, path, ok := r.panel.CurrentRowPath()
	if !ok {
		return
	}
	r.runEditor(path, 0)
}

// renameCurrentEntry is the "r" key's actual action — the
// keyboard/status-bar equivalent of the context menu's "Rename" (see
// Root.openRename), targeting whichever entry the table's cursor is
// currently on instead of a right-clicked one.
func (r *Root) renameCurrentEntry() {
	row, path, ok := r.panel.CurrentRowPath()
	if !ok {
		return
	}
	r.target = path
	r.targetRow = row
	r.openRename()
}

// renameRow is the click-pause-click rename gesture's own action (see
// Panel.onRenameGesture/handleNameClick) — renameCurrentEntry's own
// shape, for a row given directly rather than read from the panel's
// current cursor, since the gesture already knows exactly which row it
// fired on. Excludes ".." (rowRef.checkable is false for it) the same
// way CurrentRowPath already does for the keyboard path — not a real
// rename target either way.
func (r *Root) renameRow(row int) {
	ref, ok := r.panel.rowRef(row)
	if !ok || !ref.checkable {
		return
	}
	r.target = ref.path
	r.targetRow = row
	r.openRename()
}

// acceptsGlobalShortcut reports whether one of this group's Shortcut
// methods (EditShortcut/ToggleHiddenShortcut/OptionsShortcut/
// SearchShortcut/PurgeShortcut) should act right now: no overlay is
// open, and the bash command line doesn't have keyboard focus. Of the
// five, only PurgeShortcut (Ctrl+Delete — see its own doc comment) is
// still wired up in cmd/breakthrough today; Edit, ToggleHidden, Search,
// and (most recently) Options all moved to the primary keyboard layer
// instead ('e'/'.'/'f', and the "o" chord's own "oo" — see keymap.go),
// which need no guard of their own (acceptsPlainKeyCommand already
// covers the same ground more precisely — see RenameShortcut's own doc
// comment for another Shortcut method in exactly that boat already).
// Their own Shortcut wrappers are kept anyway, not deleted, as the same
// exported-building-block shape RenameShortcut already established.
//
// Unlike RequestQuit/RequestCancel (Ctrl+Q/Ctrl+C), which are meant to
// work from literally anywhere, this group operates on "the currently
// selected file", the hidden-files display, or opens an overlay of its
// own — actions that only make sense while the panel itself is what's
// focused, or (Options, Search) that would otherwise layer confusingly
// on top of whatever's already open. Critically, this also keeps them
// out of the bash line's way: tview's TextArea already implements
// several readline-style keybindings of its own (Ctrl+A/Home,
// Ctrl+E/End, Ctrl+B/PgUp, Ctrl+F/PgDn) — a global, Application-level
// capture (see cmd/breakthrough) would otherwise reach and consume the
// keystroke before bashLine's own InputCapture or TextArea's own
// default handling ever saw it, silently defeating both that and the
// muscle memory this line is explicitly meant to feel like bash — hence
// checking this first and no-op'ing instead.
func (r *Root) acceptsGlobalShortcut() bool {
	return r.activePage == "" && !r.bashLine.HasFocus()
}

// AcceptsGlobalShortcut is acceptsGlobalShortcut, exported for
// cmd/breakthrough: Entf/Ctrl+Delete (see TrashShortcut/PurgeShortcut in
// trash.go) and Ctrl+T (see TabSwitcherShortcut in tabs.go) need to
// decide, before even calling their own Shortcut method, whether to
// consume the key at all — unlike Ctrl+Q/Ctrl+C, which always return nil
// regardless, each of these collides with a real, explicit feature of
// this same codebase: bashLine's own captureBashLineKey binds Ctrl+P to
// command-history recall, for instance, one reason Properties moved off
// Ctrl+P entirely onto the plain-letter layer's own 'i' instead of
// joining this group. Consuming one of them unconditionally at the
// Application level would silently break that native behavior every
// time the bash line has focus, not just fail to fire the intended
// action — so cmd/breakthrough falls through to bashLine's own handling
// (returns the event, not nil) whenever this reports false, rather than
// swallowing it either way.
func (r *Root) AcceptsGlobalShortcut() bool {
	return r.acceptsGlobalShortcut()
}

// BashLineHasFocus is acceptsGlobalShortcut's own bashLine.HasFocus()
// half, exported on its own for cmd/breakthrough's Ctrl+T case: unlike
// every AcceptsGlobalShortcut-gated shortcut above, walking to the next
// tab while the switcher itself is already the open overlay (see
// TabSwitcherShortcut's own doc comment) must NOT also require
// activePage == "" — the switcher being open is exactly the state it
// needs to keep working in, which AcceptsGlobalShortcut's coarser,
// combined check would otherwise block outright. Still needs to fall
// through instead of consuming the key while bashLine has focus, same
// as everything else in this file, which is the one piece of
// acceptsGlobalShortcut this still needs on its own.
func (r *Root) BashLineHasFocus() bool {
	return r.bashLine.HasFocus()
}

// EditShortcut, ToggleHiddenShortcut, OptionsShortcut, RenameShortcut,
// and SearchShortcut are acceptsGlobalShortcut's own remaining group —
// none of which cmd/breakthrough calls any more (see acceptsGlobalShortcut's
// own doc comment on where each one's real keyboard path lives instead).
// Kept rather than deleted as exported building blocks regardless, the
// shape RenameShortcut — the first to end up in this position — already
// established, for whatever future caller wants the guarded form.
// LookShortcut and PurgeShortcut (Remove) are the same shape, defined
// alongside the rest of Look/Trash in viewer.go/trash.go instead of
// here (PurgeShortcut is the one exception still actually wired up —
// see its own doc comment).
func (r *Root) EditShortcut() {
	if r.acceptsGlobalShortcut() {
		r.editCurrentEntry()
	}
}

func (r *Root) RenameShortcut() {
	if r.acceptsGlobalShortcut() {
		r.renameCurrentEntry()
	}
}

func (r *Root) ToggleHiddenShortcut() {
	if r.acceptsGlobalShortcut() {
		r.toggleHidden()
	}
}

func (r *Root) OptionsShortcut() {
	if r.acceptsGlobalShortcut() {
		r.openOptions()
	}
}

func (r *Root) SearchShortcut() {
	if r.acceptsGlobalShortcut() {
		r.openSearch()
	}
}

// editorCommand returns the editor to run for Edit — $VISUAL first (the
// POSIX convention for a screen-oriented editor, which is exactly this
// context: a full-screen program taking over the whole terminal), then
// $EDITOR, then Debian/Ubuntu's own select-editor(1) preference (see
// selectedEditor — a real system-level "which editor should command-line
// tools use" mechanism, unrelated to any particular tool, not this
// project's own convention), then "vi" as a last resort (POSIX-
// guaranteed to exist). This is select-editor(1)'s own documented
// precedence exactly: "Die Variable SELECTED_EDITOR wird durch die
// Umgebungsvariablen VISUAL und EDITOR außer Kraft gesetzt" (verified
// against the installed man page, not guessed).
func editorCommand() string {
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if s := selectedEditor(); s != "" {
		return s
	}
	return "vi"
}

// selectedEditor reads ~/.selected_editor — select-editor(1)'s own
// preference file (part of Debian/Ubuntu's sensible-utils package,
// installed independently of any particular editor or file manager;
// crontab -e and many other tools already honor it the same way this
// does) — and returns its SELECTED_EDITOR value, or "" if the file
// doesn't exist, can't be read, or doesn't contain a recognizable
// assignment. select-editor's own real, observed output (see its own
// source, and a live example file) is exactly:
//
//	# Generated by /usr/bin/select-editor
//	SELECTED_EDITOR="/usr/bin/vim.basic"
//
// This mechanism is Debian/Ubuntu-specific — simply absent on macOS and
// FreeBSD, which this app also targets — so a missing file is never an
// error, the same as an unset environment variable.
func selectedEditor() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".selected_editor"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "SELECTED_EDITOR=")
		if !ok {
			continue
		}
		return strings.Trim(rest, `"`)
	}
	return ""
}

// runEditor suspends the TUI (see runShellCommand's own doc comment for
// why) and runs editorCommand on path, at line if it's > 0 (see
// Panel.activateRow's own searchMode branch — a content-search match's
// own line number, 0 for every other caller, including
// editCurrentEntry). Run through the shell (via "$@", not a literal
// exec argument) rather than exec'd directly: $VISUAL/$EDITOR can
// legitimately be more than one word (e.g. "emacsclient -t"), and only
// the shell can be trusted to split that the way the user intended
// while still passing each of its own remaining arguments through
// exactly as given, spaces and all.
//
// A line is passed as a leading "+N" argument, vi/vim/nvim/nano/
// emacs' own shared convention for "open already positioned at line
// N" — the overwhelming majority of terminal $EDITOR values in this
// app's own POSIX-focused audience already understand it; there's no
// attempt at a per-editor
// lookup table for anything fancier (e.g. VS Code's own "-g file:N")
// — an editor that doesn't recognize "+N" is no worse off than not
// jumping to a line at all, just a leading argument it happens to
// ignore or, at worst, visibly complain about once, on-screen, exactly
// where the user would see and understand why.
//
// Skips its own usual post-edit reload if search results are currently
// showing (see Panel.searchMode): r.panel.path stays whatever real
// directory was current before the search that produced them ran (see
// Panel.showSearchResults' own doc comment), completely unrelated to
// path here, so reloading it would be both useless (refreshing a
// directory the file being edited isn't even in) and would silently
// discard the results themselves (Panel.load always exits search mode
// — see its own doc comment) the moment the editor closes — the
// opposite of the "stay in the results, jump straight back into the
// editor for the next match" flow this exists for. Editing a real row
// still refreshes the real directory afterward, unchanged.
func (r *Root) runEditor(path string, line int) {
	var runErr error
	r.app.Suspend(func() {
		script := editorCommand() + ` "$@"`
		args := []string{"-c", script, "sh"}
		if line > 0 {
			args = append(args, fmt.Sprintf("+%d", line))
		}
		args = append(args, path)
		cmd := exec.Command(userShell(), args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
	})

	if runErr != nil {
		r.showError(fmt.Errorf("edit %s: %w", path, runErr))
		return
	}
	if r.panel.searchMode {
		return
	}
	r.showError(r.panel.load(r.panel.path))
}

// StartClock begins refreshing the status bar's clock display once a
// second, via a background goroutine and Application.QueueUpdateDraw.
// Deliberately not started automatically by NewRoot: many tests
// construct a Root directly without ever calling Application.Run, and
// QueueUpdateDraw blocks forever if nothing's actually running the event
// loop to drain it — cmd/breakthrough calls this itself, once, right
// before Run. Returns a function that stops the ticker.
//
// Wrapped in safeGo (see its own doc comment) like every other
// background goroutine in this app: a panic in this one specifically
// would just stop the clock ticking (no "in progress" state of its own
// to reset — onPanic is nil), not the whole process.
func (r *Root) StartClock() (stop func()) {
	ticker := time.NewTicker(time.Second)
	done := make(chan struct{})
	r.safeGo("status clock", nil, func() {
		for {
			select {
			case <-ticker.C:
				r.app.QueueUpdateDraw(func() {
					r.refreshStatusBar()
				})
			case <-done:
				ticker.Stop()
				return
			}
		}
	})
	return func() { close(done) }
}
