// Package mounts reads the live, currently mounted filesystem table via
// the real findmnt binary and cross-references it against /etc/fstab, the
// same "shell out to the real tool instead of reimplementing it" principle
// already used by internal/rsync and internal/replace for their own
// external tools. Kept as its own package, like internal/gitstatus, rather
// than inline in internal/ui, so the UI layer only ever renders an already
// assembled []Entry.
package mounts

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Entry is one currently mounted filesystem, ready for display.
type Entry struct {
	Target     string
	Source     string
	Fstype     string
	Options    string
	Bind       bool
	Persistent bool // also listed in /etc/fstab under the same target — see Build
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
// pseudo filesystems are already excluded (see Read's own --real
// flag): a handful of genuinely pseudo entries (tmpfs/nsfs namespace
// bind-equivalents) use the exact same bracket syntax for an unrelated
// reason and would otherwise be misread as real bind mounts too.
func isBindMountSource(source string) bool {
	return strings.Contains(source, "[") && strings.HasSuffix(source, "]")
}

// buildEntries cross-references live's own currently mounted
// filesystems against fstab's own configured ones, purely by mount
// target: findmnt's own "--fstab" mode already resolves whatever
// UUID=/LABEL= form a real /etc/fstab entry uses, but not necessarily
// into the same literal source string a live mount reports, so the
// mountpoint itself — not the device string — is the one field both
// sides always agree on for an ordinary, non-overlapping mount.
// "none" is fstab's own target for a swap entry, never a real
// mountpoint, and is skipped so it can never spuriously match anything.
func buildEntries(live, fstab []findmntNode) []Entry {
	inFstab := make(map[string]bool, len(fstab))
	for _, f := range fstab {
		if f.Target != "" && f.Target != "none" {
			inFstab[f.Target] = true
		}
	}

	entries := make([]Entry, 0, len(live))
	for _, m := range live {
		entries = append(entries, Entry{
			Target:     m.Target,
			Source:     m.Source,
			Fstype:     m.Fstype,
			Options:    m.Options,
			Bind:       isBindMountSource(m.Source),
			Persistent: inFstab[m.Target],
		})
	}
	return entries
}

// Read reads the live mount table, cross-referenced against /etc/fstab —
// two things findmnt's own live view can't answer by itself, resolved
// here: whether a mount is a bind mount (see isBindMountSource) and
// whether the same mountpoint is also configured in /etc/fstab, i.e.
// whether it would come back on its own after a reboot, or was mounted by
// hand (or by something other than the boot-time fstab pass) at some
// point since (see buildEntries).
func Read() ([]Entry, error) {
	live, err := runFindmnt("--real")
	if err != nil {
		return nil, err
	}
	fstab, err := runFindmnt("--fstab")
	if err != nil {
		return nil, err
	}
	return buildEntries(live, fstab), nil
}

// runFindmnt runs "findmnt -J <extraArg>" and parses its JSON output.
func runFindmnt(extraArg string) ([]findmntNode, error) {
	out, err := exec.Command("findmnt", "-J", extraArg).Output()
	if err != nil {
		return nil, fmt.Errorf("findmnt %s: %w", extraArg, err)
	}
	return parseFindmntJSON(out)
}
