package ui

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// System Info: what Details (see detailssidebar.go, showingSystemInfo)
// shows instead of a single file's own stat block while the current
// tab is browsing the real filesystem root ("/") — there's no more
// useful single "selected file" to describe there, so this shows an
// overview of the machine itself instead, per the user's own explicit
// request for "so much as is possible with what's already on the
// system" — deliberately nothing that needs a package installed
// beyond what this app already assumes elsewhere (uname, who — the
// same "no extra tools" rule kernelVersionText's own doc comment
// already states), which is exactly why there's no CPU temperature
// here: unlike everything below, it has no such universal built-in
// source (lm-sensors or a vendor tool, neither ever assumed present).
// A figure whose source doesn't exist on this platform (anything
// without /proc, or without a given command) is quietly left out of
// its own line entirely, the same "one less segment" rule the status
// bar's own kernel/uptime/load segments already follow.
//
// Reuses the exact same colors and green/orange/red percent scale the
// status bar already established (statusDiskColor/statusInodeColor/
// percentStatusColor/coloredPercentIn/wrapColor, all in bottombar.go)
// for the fields that are genuinely the same concept (kernel, uptime,
// load, disk/inode usage for "/"), and adds one small palette of its
// own, same conventions, for what's new here.

// systemInfoIdentityColor/systemInfoMemoryColor/systemInfoSwapColor/
// systemInfoOpenFilesColor/systemInfoCountColor are System Info's own
// fixed colors, chosen the same "distinguishable, not configurable"
// way the status bar's own statusDiskColor and friends already are
// (see their own doc comment) — literals, not new theme roles, since
// nothing here needs to vary by color scheme.
var (
	systemInfoIdentityColor  = tcell.GetColor("#e0a458") // amber: static machine facts (host/OS/kernel/arch/CPU)
	systemInfoMemoryColor    = tcell.GetColor("#e8829e") // rose: RAM
	systemInfoSwapColor      = tcell.GetColor("#c97b53") // terracotta: swap, memory's own overflow
	systemInfoOpenFilesColor = tcell.GetColor("#5ec8d8") // cyan: open file handles
	systemInfoCountColor     = tcell.GetColor("#9aa8b8") // muted blue-grey: plain counts with no percentage of their own
)

// showingSystemInfo reports whether Details should show System Info
// instead of a per-file stat block right now — exactly the current
// tab's own browsed directory being the real filesystem root, per the
// user's own explicit trigger ("wenn man mit I das Infofenster
// aufmacht und mit dem aktuellen Tab im Root Verzeichnis ist"),
// regardless of which row happens to be highlighted there: the point
// is an overview of the machine itself, not of whichever top-level
// entry the cursor landed on. Never true while browsing inside a
// virtual archive listing (see Panel.archivePath) — that path is
// always nested under a real directory, never literally "/", so no
// special-casing is needed there.
func (r *Root) showingSystemInfo() bool {
	return r.panel.path == "/"
}

// coloredStatLine renders one "Label: value" line the same fixed
// 13-column width infoField already uses elsewhere in this sidebar,
// with the whole line — label included — in color, resetting to the
// widget's own default only at the very end. value is expected to
// already be a complete, self-contained piece of markup: either plain
// text (for a field with no percentage of its own) or something built
// via coloredPercentIn (which itself switches back to color partway
// through, exactly the nesting this needs to still look right).
func coloredStatLine(color tcell.Color, label, value string) string {
	return fmt.Sprintf("[%s]%-13s%s[-]", colorTag(color), label+":", value)
}

// systemInfoText assembles the whole System Info block — three
// paragraphs (static identity, live health figures, plain counts),
// blank-line separated the same way renderDetailsSidebar's own
// writeSection groups its sections — from whatever of the sources
// below this platform actually has.
func (r *Root) systemInfoText() string {
	var groups [][]string

	groups = append(groups, systemInfoIdentityLines())
	groups = append(groups, r.systemInfoHealthLines())
	groups = append(groups, systemInfoCountLines())

	var paragraphs []string
	for _, g := range groups {
		if len(g) > 0 {
			paragraphs = append(paragraphs, strings.Join(g, "\n"))
		}
	}
	if len(paragraphs) == 0 {
		return "(no system information available on this platform)"
	}
	return strings.Join(paragraphs, "\n\n")
}

// systemInfoIdentityLines is every static fact about the machine
// itself — nothing here changes from one render to the next, unlike
// systemInfoHealthLines' own figures.
func systemInfoIdentityLines() []string {
	var lines []string
	add := func(label, value string) {
		if value != "" {
			lines = append(lines, coloredStatLine(systemInfoIdentityColor, label, value))
		}
	}
	add("Host", hostnameText())
	add("OS", osReleaseText())
	if k := kernelVersionText(); k != "" {
		// Kernel keeps its own established status-bar color rather
		// than the identity block's — it's the one fact here someone
		// already sees colored gold on every other screen, and giving
		// it a second, different color here would just make the two
		// look like they meant something different.
		lines = append(lines, coloredStatLine(statusKernelColor, "Kernel", k))
	}
	add("Architecture", unameField("-m"))
	add("CPU", cpuText())
	return lines
}

// systemInfoHealthLines is every figure that can actually change while
// you're looking at it — the same figures StartClock's own ticker
// re-renders this panel for once a second, exactly like the status
// bar's own uptime/load/disk segments.
func (r *Root) systemInfoHealthLines() []string {
	theme := r.theme
	var lines []string

	if up, ok := uptimeText(); ok {
		lines = append(lines, coloredStatLine(statusUptimeColor, "Uptime", strings.TrimPrefix(up, "up ")))
	}
	if numbers, ok := coloredLoadNumbers(theme); ok {
		lines = append(lines, coloredStatLine(statusLoadColor, "Load", numbers))
	}
	if total, used, ok := memoryUsageBytes(); ok {
		lines = append(lines, coloredStatLine(systemInfoMemoryColor, "Memory", usagePhrase("used", used, total, theme, systemInfoMemoryColor)))
	}
	if total, used, ok := swapUsageBytes(); ok {
		lines = append(lines, swapLine(theme, total, used))
	}
	if u, ok := fsops.FetchDiskUsage("/"); ok {
		lines = append(lines, coloredStatLine(statusDiskColor, "Disk", usagePhrase("free", u.AvailBytes, u.UsedBytes+u.AvailBytes, theme, statusDiskColor)))
		lines = append(lines, coloredStatLine(statusInodeColor, "Inodes", usageCountPhrase("used", u.UsedInodes, u.UsedInodes+u.AvailInodes, theme, statusInodeColor)))
	}
	if allocated, max, ok := openFileHandles(); ok {
		lines = append(lines, openFilesLine(theme, allocated, max))
	}
	return lines
}

// systemInfoCountLines is everything that's just a plain count, with
// no percentage or threshold of its own to color — one shared color
// throughout, deliberately muted: none of these is a "watch this"
// figure the way the health block's percentages are.
func systemInfoCountLines() []string {
	var lines []string
	if n, ok := mountCount(); ok {
		lines = append(lines, coloredStatLine(systemInfoCountColor, "Mounted fs", strconv.Itoa(n)))
	}
	if n, ok := processCount(); ok {
		lines = append(lines, coloredStatLine(systemInfoCountColor, "Processes", strconv.Itoa(n)))
	}
	if ifaces, ok := networkInterfaces(); ok {
		value := strconv.Itoa(len(ifaces))
		if len(ifaces) > 0 {
			value = fmt.Sprintf("%d (%s)", len(ifaces), strings.Join(ifaces, ", "))
		}
		lines = append(lines, coloredStatLine(systemInfoCountColor, "Network", value))
	}
	if n, ok := loggedInSessions(); ok {
		lines = append(lines, coloredStatLine(systemInfoCountColor, "Sessions", strconv.Itoa(n)))
	}
	return lines
}

// usagePhrase renders "<verb> <a>/<b> (<percent>%)" for a byte
// quantity (see humanSize), with the percentage colored via
// percentStatusColor/coloredPercentIn on top of base — the same shape
// diskUsageText/inodeUsageText already use, generalized here for
// Memory/Swap/Disk to share instead of three near-identical one-off
// formatters.
func usagePhrase(verb string, part, total int64, theme config.ResolvedTheme, base tcell.Color) string {
	percent := percentOf(part, total)
	return fmt.Sprintf("%s %s/%s (%s)", verb, humanSize(part), humanSize(total), coloredPercentIn(percent, percentStatusColor(percent, theme), base))
}

// usageCountPhrase is usagePhrase for a plain count (inodes, open file
// handles) rather than a byte size — humanCount instead of humanSize,
// otherwise identical.
func usageCountPhrase(verb string, part, total int64, theme config.ResolvedTheme, base tcell.Color) string {
	percent := percentOf(part, total)
	return fmt.Sprintf("%s %s/%s (%s)", verb, humanCount(part), humanCount(total), coloredPercentIn(percent, percentStatusColor(percent, theme), base))
}

// swapLine is the Swap line's own two cases, split out of
// systemInfoHealthLines for the same reason openFilesLine is: a
// standalone function a test can call directly with contrived numbers,
// rather than only ever through the real /proc/meminfo this machine
// happens to have. A machine with no swap configured at all
// (SwapTotal itself 0) is a legitimate, worth-knowing answer on its
// own — not colored or percentaged, since there's no meaningful
// "used/total" to show for a resource that doesn't exist.
func swapLine(theme config.ResolvedTheme, total, used int64) string {
	if total == 0 {
		return coloredStatLine(systemInfoSwapColor, "Swap", "none configured")
	}
	return coloredStatLine(systemInfoSwapColor, "Swap", usagePhrase("used", used, total, theme, systemInfoSwapColor))
}

// openFilesLine is the Open files line's own two cases, split out of
// systemInfoHealthLines for the same testability reason swapLine is —
// see openFileHandlesSaneMax's own doc comment for why max isn't
// always a real ceiling worth dividing against.
func openFilesLine(theme config.ResolvedTheme, allocated, max int64) string {
	value := fmt.Sprintf("%s allocated (no practical limit)", humanCount(allocated))
	if max > 0 && max <= openFileHandlesSaneMax {
		value = usageCountPhrase("used", allocated, max, theme, systemInfoOpenFilesColor)
	}
	return coloredStatLine(systemInfoOpenFilesColor, "Open files", value)
}

// percentOf is part as a percentage of total, rounded to the nearest
// whole number — 0 for a zero or negative total rather than dividing
// by it, since "0 of 0" is a degenerate case no real filesystem/
// memory/handle-table figure should ever actually produce, but a
// stray platform quirk shouldn't be allowed to panic over.
func percentOf(part, total int64) int {
	if total <= 0 {
		return 0
	}
	return int(part * 100 / total)
}

// hostnameText is os.Hostname(), or "" if it fails — vanishingly rare
// (it can only fail if the syscall itself does), but treated the same
// "quietly show one less line" way as every other source here rather
// than surfacing the error.
func hostnameText() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

// osReleaseText reads /etc/os-release's own PRETTY_NAME — the one
// field every major distribution's own file sets specifically to be
// shown to a human (freedesktop.org's os-release(5) spec), quoted with
// literal double quotes in the file itself, stripped here. "" if the
// file doesn't exist (non-Linux, or a minimal image without one).
func osReleaseText() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// cpuText is the CPU's own model name (from /proc/cpuinfo, "" if
// unavailable) plus this machine's own logical core count
// (runtime.NumCPU, always available on every platform Go itself
// targets) — shown even without a model name, since the core count
// alone is still useful and needs no /proc access at all.
func cpuText() string {
	cores := runtime.NumCPU()
	unit := "cores"
	if cores == 1 {
		unit = "core"
	}
	if model := cpuModelName(); model != "" {
		return fmt.Sprintf("%s (%d %s)", model, cores, unit)
	}
	return fmt.Sprintf("%d %s", cores, unit)
}

// cpuModelName reads /proc/cpuinfo's own first "model name" line — one
// per logical core, all identical on every real machine, so only the
// first is read. "" if the file doesn't exist (non-Linux) or the
// field itself doesn't (some ARM/RISC-V listings use different field
// names entirely — quietly omitted rather than guessed at).
func cpuModelName() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if v, ok := strings.CutPrefix(scanner.Text(), "model name"); ok {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), ":"))
		}
	}
	return ""
}

// memInfoKB reads /proc/meminfo into a field-name → kB map — every
// value in that file is already in kB except a handful this package
// never reads (HugePages_Total et al.), so no per-field unit handling
// is needed.
func memInfoKB() (map[string]int64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	m := make(map[string]int64)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		m[name] = value
	}
	return m, nil
}

// memoryUsageBytes is RAM total/used, in bytes, from /proc/meminfo —
// used is Total minus Available, the same figure free(1) itself
// reports as "used" on a modern kernel. MemAvailable itself dates to
// Linux 3.14; on an older kernel without it, Free+Buffers+Cached is
// the same estimate free(1) used to compute before MemAvailable
// existed, close enough for a status display. ok is false only if
// /proc/meminfo itself doesn't exist (non-Linux) or lacks MemTotal
// entirely, which shouldn't happen on any real Linux system.
func memoryUsageBytes() (total, used int64, ok bool) {
	m, err := memInfoKB()
	if err != nil {
		return 0, 0, false
	}
	totalKB, hasTotal := m["MemTotal"]
	if !hasTotal {
		return 0, 0, false
	}
	availKB, hasAvail := m["MemAvailable"]
	if !hasAvail {
		availKB = m["MemFree"] + m["Buffers"] + m["Cached"]
	}
	usedKB := totalKB - availKB
	if usedKB < 0 {
		usedKB = 0
	}
	return totalKB * 1024, usedKB * 1024, true
}

// swapUsageBytes is swap total/used, in bytes — total 0 (not ok=false)
// when SwapTotal itself is 0: a machine with no swap configured at all
// is a legitimate, worth-knowing answer (see systemInfoHealthLines'
// own "none configured" line for it), not the same as this platform
// having no swap concept at all.
func swapUsageBytes() (total, used int64, ok bool) {
	m, err := memInfoKB()
	if err != nil {
		return 0, 0, false
	}
	totalKB, hasTotal := m["SwapTotal"]
	if !hasTotal {
		return 0, 0, false
	}
	usedKB := totalKB - m["SwapFree"]
	if usedKB < 0 {
		usedKB = 0
	}
	return totalKB * 1024, usedKB * 1024, true
}

// openFileHandlesSaneMax is the largest fs.file-max this treats as a
// real, meaningful ceiling — some kernels (observed directly: a Kali
// VM under certain hypervisor/cgroup configurations) report
// math.MaxInt64 itself as file-max, the kernel's own way of saying
// "no limit is actually enforced", not a real number of handles ever
// achievable. Dividing against that literally gives "16137/8.0E
// (0%)", technically correct and completely useless — well below any
// real-world configured limit (even a heavily tuned server rarely
// exceeds a few million), so this is a generous ceiling, not a tight
// one.
const openFileHandlesSaneMax = 1 << 32

// openFileHandles reads /proc/sys/fs/file-nr: "<allocated> <free>
// <max>" — the middle field is a historical, effectively-unused
// "free allocated-but-unused handles" count on a modern kernel, not
// read here; the number that actually matters is the first
// (currently allocated, i.e. in use) against the third (this system's
// own configured ceiling, fs.file-max) — see openFileHandlesSaneMax
// for what happens once that ceiling isn't a real one.
func openFileHandles() (allocated, max int64, ok bool) {
	data, err := os.ReadFile("/proc/sys/fs/file-nr")
	if err != nil {
		return 0, 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, false
	}
	a, err1 := strconv.ParseInt(fields[0], 10, 64)
	m, err2 := strconv.ParseInt(fields[2], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return a, m, true
}

// mountCount is the number of lines in /proc/mounts — every currently
// mounted filesystem, real and virtual (proc, sysfs, cgroup, tmpfs, ...)
// alike, the same complete count `mount | wc -l` would give.
func mountCount() (int, bool) {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return 0, false
	}
	return countNonEmptyLines(string(data)), true
}

// processCount counts /proc's own numerically-named entries — one per
// running process, on every Linux system, with no need to shell out to
// ps at all.
func processCount() (int, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, false
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err == nil {
			n++
		}
	}
	return n, true
}

// networkInterfaces lists every network interface's own name from
// /proc/net/dev, loopback excluded (always present, never itself
// interesting) — the file's own first two lines are a fixed header,
// never data.
func networkInterfaces() ([]string, bool) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil, false
	}
	lines := strings.Split(string(data), "\n")
	var ifaces []string
	for i, line := range lines {
		if i < 2 {
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, _, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || name == "lo" {
			continue
		}
		ifaces = append(ifaces, name)
	}
	return ifaces, true
}

// loggedInSessions shells out to who(1) (POSIX-standard, present on
// every platform this app targets, the same "real tool" precedent
// kernelVersionText's own uname already sets) and counts its lines,
// one per logged-in session.
func loggedInSessions() (int, bool) {
	out, err := exec.Command("who").Output()
	if err != nil {
		return 0, false
	}
	return countNonEmptyLines(string(out)), true
}

// countNonEmptyLines counts s's own lines, ignoring a trailing newline
// (which would otherwise count as one extra, empty line) and any
// blank line in the middle — shared by mountCount and
// loggedInSessions, the only two callers that just need "how many
// real lines does this output have".
func countNonEmptyLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
