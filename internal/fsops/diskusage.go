package fsops

import (
	"os/exec"
	"strconv"
	"strings"
)

// DiskUsage is one filesystem's block and inode usage — shared by the
// status bar's own display (see internal/ui's diskUsageText/
// inodeUsageText) and the trash's own age/quota pruning (see
// PruneTrash), which needs UsedBytes+AvailBytes to know how big the
// filesystem the trash lives on actually is.
type DiskUsage struct {
	UsedBytes, AvailBytes    int64
	UsedInodes, AvailInodes  int64
	UsePercent, InodePercent int
}

// FetchDiskUsage runs `df -k` (block usage) and `df -i` (inode usage)
// on dir and parses each one's own data line into a DiskUsage.
//
// Deliberately not `df -h`: -h's own human-readable formatting is
// locale-dependent (this app's own target audience includes non-
// English locales — a German one, for instance, renders
// "1.7G" as "1,7G", a comma this app would then have no reliable way
// to tell apart from a field separator when parsing it back out). Also
// deliberately not `df -P`, which on GNU df guarantees a single,
// portably-parseable data line — but means something else entirely on
// BSD df (512-byte blocks, not "portable output format" — verified
// against the FreeBSD/macOS df(1) man pages, not guessed: using it
// cross-platform for parseability, the way a straight port of GNU df's
// own convention would, is actually wrong here). Requesting raw block/
// inode counts via -k/-i and formatting them with this app's own
// humanSize/humanCount instead sidesteps both problems, and gets
// exact UsedBytes/AvailBytes/UsedInodes/AvailInodes for free rather
// than needing to reverse a rounded, unit-suffixed string.
func FetchDiskUsage(dir string) (DiskUsage, bool) {
	blockLine, ok := dfLastLine("df", "-k", dir)
	if !ok {
		return DiskUsage{}, false
	}
	inodeLine, ok := dfLastLine("df", "-i", dir)
	if !ok {
		return DiskUsage{}, false
	}

	usedBlocks, availBlocks, usePercent, ok := parseDfDataLine(blockLine)
	if !ok {
		return DiskUsage{}, false
	}
	usedInodes, availInodes, inodePercent, ok := parseDfDataLine(inodeLine)
	if !ok {
		return DiskUsage{}, false
	}

	return DiskUsage{
		UsedBytes:    usedBlocks * 1024,
		AvailBytes:   availBlocks * 1024,
		UsedInodes:   usedInodes,
		AvailInodes:  availInodes,
		UsePercent:   usePercent,
		InodePercent: inodePercent,
	}, true
}

// dfLastLine runs name(args...) (df, with whatever flags the caller
// chose) and returns its own data line — the last line of output,
// skipping the header row df always prints first. A single given path
// always produces exactly one data line, on every platform this
// project targets.
func dfLastLine(name string, args ...string) (string, bool) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return "", false
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 2 {
		return "", false
	}
	return lines[len(lines)-1], true
}

// parseDfDataLine extracts Used, Available, and Capacity (Use%/IUse%)
// from one df data line, indexed from the END of its whitespace-
// separated fields — Mounted-on last, Capacity/Use% just before it,
// Available before that, Used before that — rather than from the
// start. That's what makes this robust to a wrapped Filesystem name (a
// real, if rare, BSD df quirk for a very long device name, splitting
// it onto its own line and shifting how many fields precede the data
// that actually matters here) without needing to detect the wrap
// itself. Not robust to a mount point that itself contains a space
// (e.g. "/Volumes/My Drive") — an accepted, rare limitation, the same
// class dfSummary's own predecessor already accepted before this.
func parseDfDataLine(line string) (used, avail int64, percent int, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return 0, 0, 0, false
	}
	percent, err := strconv.Atoi(strings.TrimSuffix(fields[len(fields)-2], "%"))
	if err != nil {
		return 0, 0, 0, false
	}
	avail, err = strconv.ParseInt(fields[len(fields)-3], 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	used, err = strconv.ParseInt(fields[len(fields)-4], 10, 64)
	if err != nil {
		return 0, 0, 0, false
	}
	return used, avail, percent, true
}
