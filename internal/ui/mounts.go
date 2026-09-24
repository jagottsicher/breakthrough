package ui

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// The Mounts screen: a full-screen, read-only view of every currently
// mounted filesystem — a third full-screen catalog alongside Options
// and the Toolbox, per the user's own explicit request to keep it
// separate rather than folding it into the Toolbox's own command list,
// since this one shows live, structured data rather than a catalog of
// commands to run.
//
// Two things findmnt's own live view can't answer by itself are
// resolved here: whether a mount is a bind mount (see
// isBindMountSource) and whether the same mountpoint is also
// configured in /etc/fstab, i.e. whether it would come back on its own
// after a reboot, or was mounted by hand (or by something other than
// the boot-time fstab pass) at some point since (see buildMountEntries).

const mountsPage = "mounts"

// Column indices within mountsTable. Bind/Persistent/Type — this
// screen's own short, fixed-width, at-a-glance columns — deliberately
// come right after Target, before Source and Options: a live check
// against a real system with a mounted CIFS share showed a single long
// network-share path in Source pushing every column after it well past
// a normal terminal's own width, exactly the class of problem
// optionsColInfo's own doc comment already describes for a too-narrow
// row ("the rightmost column is the one that gets clipped"). Source and
// Options — both unbounded (see mountsColumnWidth) and the two least
// essential for a quick scan — are what ends up needing a horizontal
// scroll on a narrow terminal instead, never Bind/Persistent.
const (
	mountsColTarget = iota
	mountsColBind
	mountsColPersistent
	mountsColFstype
	mountsColSource
	mountsColOptions
)

// mountsColumnWidth is each column's own padding target — a floor, not
// a ceiling: padRight (see renderMounts) never truncates a value longer
// than it, only pads a shorter one out to it, so a long value simply
// pushes whatever comes after it rather than being clipped, the same
// reasoning panel.go's own Size/Modified columns already follow.
// Source's own floor is deliberately modest (just enough gap before
// Options for a short local device path like "/dev/md0") rather than a
// real column width, since a CIFS/NFS share's own path routinely runs
// far longer than any fixed width would help with anyway. Options has
// no floor at all: nothing follows it, so there's nothing to keep a gap
// before.
func mountsColumnWidth(col int) int {
	switch col {
	case mountsColTarget:
		return 32
	case mountsColBind:
		return 6
	case mountsColPersistent:
		return 12
	case mountsColFstype:
		return 8
	case mountsColSource:
		return 20
	default:
		return 0
	}
}

// mountEntry is one currently mounted filesystem, ready for display.
type mountEntry struct {
	target     string
	source     string
	fstype     string
	options    string
	bind       bool
	persistent bool // also listed in /etc/fstab under the same target — see buildMountEntries
}

// findmntNode mirrors one entry of findmnt's own JSON output (both
// "findmnt -J --real" and "findmnt -J --fstab" use the same shape) —
// Children nests every mount under whatever it's physically mounted on
// top of, arbitrarily deep, which parseFindmntJSON flattens.
type findmntNode struct {
	Target   string        `json:"target"`
	Source   string        `json:"source"`
	Fstype   string        `json:"fstype"`
	Options  string        `json:"options"`
	Children []findmntNode `json:"children"`
}

type findmntOutput struct {
	Filesystems []findmntNode `json:"filesystems"`
}

// parseFindmntJSON flattens findmnt's own nested JSON tree into a
// plain, depth-first list, dropping each node's own Children slice
// once it's been walked — verified against real findmnt output, not
// guessed: every mount nested under the one it's physically mounted on
// top of comes back as a "children" array.
func parseFindmntJSON(data []byte) ([]findmntNode, error) {
	var out findmntOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}

	var flat []findmntNode
	var walk func([]findmntNode)
	walk = func(nodes []findmntNode) {
		for _, n := range nodes {
			children := n.Children
			n.Children = nil
			flat = append(flat, n)
			walk(children)
		}
	}
	walk(out.Filesystems)
	return flat, nil
}

// isBindMountSource reports whether source is findmnt's own notation
// for a bind mount: the underlying device or share, followed by the
// bound sub-path in brackets — e.g. "/dev/sdd1[/.bitcoin]" for `mount
// --bind /some/dir /dev/sdd1-mountpoint/.bitcoin`, confirmed against a
// real bind mount on a real system, not guessed. Only meaningful once
// pseudo filesystems are already excluded (see readMounts' own --real
// flag): a handful of genuinely pseudo entries (tmpfs/nsfs namespace
// bind-equivalents) use the exact same bracket syntax for an unrelated
// reason and would otherwise be misread as real bind mounts too.
func isBindMountSource(source string) bool {
	return strings.Contains(source, "[") && strings.HasSuffix(source, "]")
}

// buildMountEntries cross-references live's own currently mounted
// filesystems against fstab's own configured ones, purely by mount
// target: findmnt's own "--fstab" mode already resolves whatever
// UUID=/LABEL= form a real /etc/fstab entry uses, but not necessarily
// into the same literal source string a live mount reports, so the
// mountpoint itself — not the device string — is the one field both
// sides always agree on for an ordinary, non-overlapping mount.
// "none" is fstab's own target for a swap entry, never a real
// mountpoint, and is skipped so it can never spuriously match anything.
func buildMountEntries(live, fstab []findmntNode) []mountEntry {
	inFstab := make(map[string]bool, len(fstab))
	for _, f := range fstab {
		if f.Target != "" && f.Target != "none" {
			inFstab[f.Target] = true
		}
	}

	entries := make([]mountEntry, 0, len(live))
	for _, m := range live {
		entries = append(entries, mountEntry{
			target:     m.Target,
			source:     m.Source,
			fstype:     m.Fstype,
			options:    m.Options,
			bind:       isBindMountSource(m.Source),
			persistent: inFstab[m.Target],
		})
	}
	return entries
}

// readMounts shells out to findmnt twice — once for what's actually
// mounted right now (--real: skips proc/sysfs/tmpfs/cgroup/... pseudo
// filesystems, keeping only genuine storage, confirmed by hand against
// a real system), once for what /etc/fstab itself configures — and
// cross-references the two (see buildMountEntries). No /etc/fstab
// parsing of our own: findmnt already resolves its own UUID=/LABEL=
// entries and comment/whitespace handling, the same "shell out to the
// real tool instead of reimplementing it" principle already used for
// Rsync and Sed Replace.
func readMounts() ([]mountEntry, error) {
	live, err := runFindmnt("--real")
	if err != nil {
		return nil, err
	}
	fstab, err := runFindmnt("--fstab")
	if err != nil {
		return nil, err
	}
	return buildMountEntries(live, fstab), nil
}

// runFindmnt runs "findmnt -J <extraArg>" and parses its JSON output.
func runFindmnt(extraArg string) ([]findmntNode, error) {
	out, err := exec.Command("findmnt", "-J", extraArg).Output()
	if err != nil {
		return nil, fmt.Errorf("findmnt %s: %w", extraArg, err)
	}
	return parseFindmntJSON(out)
}

// newMountsScreen builds the whole screen once, at startup — the same
// build-once/repopulate-on-open shape newToolboxScreen already
// establishes.
func (r *Root) newMountsScreen() {
	r.mountsTable = tview.NewTable()
	r.mountsTable.SetBorders(false)
	r.mountsTable.SetBorderPadding(1, 0, 2, 1)
	r.mountsTable.SetSelectable(true, false)
	// The header row (see renderMounts) never scrolls out of view, the
	// same fixed-header shape renderCompareTree already establishes for
	// its own table.
	r.mountsTable.SetFixed(1, 0)
	r.mountsTable.SetInputCapture(r.captureMountsKey)

	r.mountsTitleBar = newPlainTitleBar("Mounts")

	r.mountsHint = tview.NewTextView()
	r.mountsHint.SetWrap(false)
	r.mountsHint.SetText(" ↑/↓: move · r: refresh · Esc: close ")

	r.mountsLayout = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(r.mountsTitleBar, 1, 0, false).
		AddItem(r.mountsTable, 0, 1, true).
		AddItem(r.mountsHint, 1, 0, false)
}

// openMounts shows the Mounts screen, freshly read every time — there
// is nothing here worth remembering across opens, since the whole point
// is to reflect whatever is mounted right now.
func (r *Root) openMounts() {
	r.reloadMounts()
	r.showOverlay(mountsPage, r.mountsLayout)
}

// closeMounts hides the Mounts screen. Nothing to save — this is a pure
// read-only view.
func (r *Root) closeMounts() {
	r.hideOverlay()
}

// reloadMounts re-reads the live mount table and re-renders — the
// screen's own initial population, and "r"'s own manual refresh, since
// a mount can appear or disappear (a USB stick, a network share) while
// this screen is open and nothing here otherwise notices on its own.
func (r *Root) reloadMounts() {
	entries, err := readMounts()
	r.mountsEntries = entries
	r.mountsErr = err
	r.renderMounts()
}

// renderMounts fills the table: a bold header row (see newMountsScreen's
// own SetFixed), then one row per currently mounted filesystem. A
// failed read (findmnt missing, or erroring outright) shows in place of
// any rows rather than silently leaving the screen empty — the same
// "report it, don't swallow it" principle every other action in this
// app already follows.
func (r *Root) renderMounts() {
	r.mountsTable.Clear()

	header := func(col int, text string) {
		r.mountsTable.SetCell(0, col,
			tview.NewTableCell(padRight(text, mountsColumnWidth(col))).
				SetTextColor(r.theme.Text).
				SetAttributes(tcell.AttrBold).
				SetSelectable(false))
	}
	header(mountsColTarget, "Target")
	header(mountsColBind, "Bind")
	header(mountsColPersistent, "Persistent")
	header(mountsColFstype, "Type")
	header(mountsColSource, "Source")
	header(mountsColOptions, "Options")

	// See showTablePlaceholder's own doc comment for why this — and the
	// zero-entries case below — go through it rather than a bare
	// SetCell: it's the fix for a real, reported freeze, not just a
	// message.
	if r.mountsErr != nil {
		showTablePlaceholder(r.mountsTable, r.mountsErr.Error(), r.theme.EntryError)
		return
	}

	// Real, selectable rows may exist below — reset again just below if
	// this directory turns out to have none.
	enableTableSelection(r.mountsTable)

	for i, m := range r.mountsEntries {
		row := i + 1

		// WarningText, not plain Text, for a mount that is NOT in
		// fstab: that's the one state worth noticing at a glance — a
		// mount that will silently be gone after the next reboot,
		// unlike the plain, unremarkable default of "this comes back on
		// its own" — the same "color the exception, not the norm"
		// convention optionDefaultHint's own doc comment already
		// follows.
		persistentColor := r.theme.Text
		if !m.persistent {
			persistentColor = r.theme.WarningText
		}

		r.mountsTable.SetCell(row, mountsColTarget,
			tview.NewTableCell(padRight(m.target, mountsColumnWidth(mountsColTarget))).
				SetTextColor(r.theme.Text).SetSelectable(true))
		r.mountsTable.SetCell(row, mountsColBind,
			tview.NewTableCell(padRight(checkboxText(m.bind), mountsColumnWidth(mountsColBind))).
				SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
		r.mountsTable.SetCell(row, mountsColPersistent,
			tview.NewTableCell(padRight(checkboxText(m.persistent), mountsColumnWidth(mountsColPersistent))).
				SetTextColor(persistentColor).SetSelectable(true))
		r.mountsTable.SetCell(row, mountsColFstype,
			tview.NewTableCell(padRight(m.fstype, mountsColumnWidth(mountsColFstype))).
				SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
		r.mountsTable.SetCell(row, mountsColSource,
			tview.NewTableCell(padRight(m.source, mountsColumnWidth(mountsColSource))).
				SetTextColor(r.theme.Text).SetSelectable(true))
		r.mountsTable.SetCell(row, mountsColOptions,
			tview.NewTableCell(m.options).
				SetTextColor(r.theme.PlaceholderText).SetSelectable(true))
	}

	// Keep the cursor in range (and off the header row) after a refresh
	// changed how many rows there are — landing on the first real row
	// the very first time this ever renders, left alone otherwise, the
	// same restraint renderToolbox's own tail already shows. The
	// zero-entries case (no real storage mounted at all — vanishingly
	// unlikely in practice, but not impossible) is just as vulnerable to
	// the freeze showTablePlaceholder's own doc comment describes as the
	// error branch above, so it goes through the same helper — and,
	// same as renderFirewall's "No rules configured.", reports the
	// empty read explicitly instead of just looking like one.
	if len(r.mountsEntries) == 0 {
		showTablePlaceholder(r.mountsTable, "No real storage mounted.", r.theme.PlaceholderText)
		return
	}
	if row, _ := r.mountsTable.GetSelection(); row < 1 || row > len(r.mountsEntries) {
		r.mountsTable.Select(1, 0)
	}
}

// captureMountsKey is the Mounts screen's own key handling: Escape
// closes it, "r" re-reads the live mount table.
func (r *Root) captureMountsKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		r.closeMounts()
		return nil
	}
	if event.Key() == tcell.KeyRune && event.Rune() == 'r' {
		r.reloadMounts()
		return nil
	}
	return event
}
