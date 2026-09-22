package ui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
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
// instead of a per-file stat block right now — the currently selected
// row being "/" itself, exactly the way it would be any other single
// entry: Panel.load synthesizes a selectable "/" row in place of the
// usual ".." at the real filesystem root (there's no parent to go "up"
// to there — see its own doc comment), and detailsTarget is always
// whatever that selection's own path is (see loadDetailsTarget).
//
// Deliberately not "the browsed directory is '/'" any more — an
// earlier version of this worked that way, which made every other
// top-level entry (etc, home, usr, ...) permanently unreachable from
// Details while at "/", since System Info always won regardless of
// the cursor; per the user's own explicit correction, only selecting
// "/" itself should. Never true while browsing inside a virtual
// archive listing (see Panel.archivePath) — that path is always
// nested under a real directory, never literally "/", so no special-
// casing is needed there.
func (r *Root) showingSystemInfo() bool {
	return r.detailsTarget == "/"
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

	if remote := r.panel.remote; remote != nil {
		groups = append(groups, remoteSystemInfoIdentityLines(remote))
		groups = append(groups, r.remoteSystemInfoHealthLines(remote))
		groups = append(groups, remoteSystemInfoCountLines(remote))
	} else {
		groups = append(groups, systemInfoIdentityLines())
		groups = append(groups, r.systemInfoHealthLines())
		groups = append(groups, systemInfoCountLines())
	}

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
	if u, ok := diskUsageFor(r.panel); ok {
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

// remoteReadFile reads the whole of a small remote file through a
// connected Client — every remote System Info source below is a
// single /proc or /etc file, exactly the same kind of thing every
// local source here already reads via os.ReadFile, just over the
// connection instead. "" and ok=false for absence, permission, or
// anything else, matching the "quietly one less line" convention
// every local source here already follows for a platform that simply
// doesn't have a given file.
func remoteReadFile(client remotefs.Client, path string) (string, bool) {
	f, err := client.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// parseProcVersionKernel extracts the kernel release token from
// /proc/version's own "Linux version X.Y.Z-foo (builder@host) (gcc
// ...) #1 SMP ..." line — the remote counterpart to kernelVersionText's
// own `uname -r` (bottombar.go): there's no command-execution channel
// to a remote host here, but the exact same information already sits
// in this one file on every real Linux kernel, so no exec channel is
// needed for this particular fact.
func parseProcVersionKernel(data string) string {
	fields := strings.Fields(data)
	for i, f := range fields {
		if f == "version" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

// remoteSystemInfoIdentityLines is systemInfoIdentityLines' own remote
// counterpart — every fact it shows turns out to already be a plain
// file on any real Linux host (/proc/sys/kernel/hostname, /etc/os-
// release, /proc/version, /proc/cpuinfo), so a connected Client's own
// Open is all this needs; no remote command-execution channel required.
// Architecture (uname -m locally) has no equally reliable file-based
// source and is left out here entirely, the same "one less line"
// treatment every other genuinely unavailable local source already
// gets.
func remoteSystemInfoIdentityLines(client remotefs.Client) []string {
	var lines []string
	add := func(label, value string) {
		if value != "" {
			lines = append(lines, coloredStatLine(systemInfoIdentityColor, label, value))
		}
	}

	if data, ok := remoteReadFile(client, "/proc/sys/kernel/hostname"); ok {
		add("Host", strings.TrimSpace(data))
	}
	if data, ok := remoteReadFile(client, "/etc/os-release"); ok {
		add("OS", parseOSReleasePrettyName(data))
	}
	if data, ok := remoteReadFile(client, "/proc/version"); ok {
		if k := parseProcVersionKernel(data); k != "" {
			// See systemInfoIdentityLines' own doc comment on why
			// Kernel keeps statusKernelColor rather than this block's
			// own identity color.
			lines = append(lines, coloredStatLine(statusKernelColor, "Kernel", k))
		}
	}
	if data, ok := remoteReadFile(client, "/proc/cpuinfo"); ok {
		cores := countCPUInfoProcessors(data)
		if model := parseCPUModelName(data); model != "" || cores > 0 {
			add("CPU", formatCPUText(model, cores))
		}
	}
	return lines
}

// remoteSystemInfoHealthLines is systemInfoHealthLines' own remote
// counterpart — Uptime/Load/Memory/Swap all come from the exact same
// /proc files their local versions already parse (see
// parseMemInfoKB/memoryUsageBytesFrom/swapUsageBytesFrom), just read
// over the connection; Disk/Inodes go through diskUsageFor, which
// already dispatches to the connected Client's own DiskUsage. Open
// files (locally /proc/sys/fs/file-nr) is included too — it's just as
// much a plain file remotely as it is locally.
func (r *Root) remoteSystemInfoHealthLines(client remotefs.Client) []string {
	theme := r.theme
	var lines []string

	if data, ok := remoteReadFile(client, "/proc/uptime"); ok {
		if fields := strings.Fields(data); len(fields) > 0 {
			if seconds, err := strconv.ParseFloat(fields[0], 64); err == nil {
				lines = append(lines, coloredStatLine(statusUptimeColor, "Uptime", formatUptime(time.Duration(seconds*float64(time.Second)))))
			}
		}
	}

	cores := 0
	if cpuinfo, ok := remoteReadFile(client, "/proc/cpuinfo"); ok {
		cores = countCPUInfoProcessors(cpuinfo)
	}
	if data, ok := remoteReadFile(client, "/proc/loadavg"); ok {
		if fields := strings.Fields(data); len(fields) >= 3 {
			numbers := make([]string, 3)
			allParsed := true
			for i := 0; i < 3; i++ {
				v, err := strconv.ParseFloat(fields[i], 64)
				if err != nil {
					allParsed = false
					break
				}
				numbers[i] = wrapColor(loadNumberColor(v, cores, theme), fields[i])
			}
			if allParsed {
				lines = append(lines, coloredStatLine(statusLoadColor, "Load", strings.Join(numbers, " ")))
			}
		}
	}

	if data, ok := remoteReadFile(client, "/proc/meminfo"); ok {
		m := parseMemInfoKB(data)
		if total, used, ok := memoryUsageBytesFrom(m); ok {
			lines = append(lines, coloredStatLine(systemInfoMemoryColor, "Memory", usagePhrase("used", used, total, theme, systemInfoMemoryColor)))
		}
		if total, used, ok := swapUsageBytesFrom(m); ok {
			lines = append(lines, swapLine(theme, total, used))
		}
	}

	if u, ok := diskUsageFor(r.panel); ok {
		lines = append(lines, coloredStatLine(statusDiskColor, "Disk", usagePhrase("free", u.AvailBytes, u.UsedBytes+u.AvailBytes, theme, statusDiskColor)))
		lines = append(lines, coloredStatLine(statusInodeColor, "Inodes", usageCountPhrase("used", u.UsedInodes, u.UsedInodes+u.AvailInodes, theme, statusInodeColor)))
	}

	if data, ok := remoteReadFile(client, "/proc/sys/fs/file-nr"); ok {
		if allocated, max, ok := parseOpenFileHandles(data); ok {
			lines = append(lines, openFilesLine(theme, allocated, max))
		}
	}

	return lines
}

// remoteSystemInfoCountLines is systemInfoCountLines' own remote
// counterpart. Sessions (locally the `who` command) is left out
// entirely: unlike everything else in this block, it has no file-
// based source at all — it genuinely needs a command-execution
// channel to the remote host, which this project doesn't have (the
// same limitation Edit/Compare/Sed Replace already refuse remotely
// over — see remoteops.go's own package doc comment).
func remoteSystemInfoCountLines(client remotefs.Client) []string {
	var lines []string

	if data, ok := remoteReadFile(client, "/proc/mounts"); ok {
		lines = append(lines, coloredStatLine(systemInfoCountColor, "Mounted fs", strconv.Itoa(countNonEmptyLines(data))))
	}

	if entries, err := client.ListDir("/proc"); err == nil {
		n := 0
		for _, e := range entries {
			if e.Type != fsops.TypeDir {
				continue
			}
			if _, err := strconv.Atoi(e.Name); err == nil {
				n++
			}
		}
		lines = append(lines, coloredStatLine(systemInfoCountColor, "Processes", strconv.Itoa(n)))
	}

	if data, ok := remoteReadFile(client, "/proc/net/dev"); ok {
		ifaces := parseNetworkInterfaces(data)
		value := strconv.Itoa(len(ifaces))
		if len(ifaces) > 0 {
			value = fmt.Sprintf("%d (%s)", len(ifaces), strings.Join(ifaces, ", "))
		}
		lines = append(lines, coloredStatLine(systemInfoCountColor, "Network", value))
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
	return parseOSReleasePrettyName(string(data))
}

// parseOSReleasePrettyName extracts os-release's own PRETTY_NAME field
// — split out of osReleaseText so the identical parsing also serves a
// remote connection's own /etc/os-release (see
// remoteSystemInfoIdentityLines): the file itself is read differently
// (Client.Open vs os.ReadFile), but its content means the same thing
// either way.
func parseOSReleasePrettyName(data string) string {
	for _, line := range strings.Split(data, "\n") {
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
	return formatCPUText(cpuModelName(), runtime.NumCPU())
}

// formatCPUText renders a CPU model name plus its logical core count —
// split out of cpuText so the identical formatting also serves a
// remote host's own core count (see remoteSystemInfoIdentityLines),
// which has to come from counting /proc/cpuinfo's own "processor"
// lines (see countCPUInfoProcessors) rather than runtime.NumCPU(),
// which only ever answers for the machine breakthrough itself is
// running on.
func formatCPUText(model string, cores int) string {
	unit := "cores"
	if cores == 1 {
		unit = "core"
	}
	if model != "" {
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
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	return parseCPUModelName(string(data))
}

// parseCPUModelName is cpuModelName's own parsing half, split out for
// the same remote-reuse reason parseOSReleasePrettyName is.
func parseCPUModelName(data string) string {
	for _, line := range strings.Split(data, "\n") {
		if v, ok := strings.CutPrefix(line, "model name"); ok {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), ":"))
		}
	}
	return ""
}

// countCPUInfoProcessors counts /proc/cpuinfo's own "processor" lines
// — one per logical core, the standard portable way to count them from
// this file alone — the remote counterpart to runtime.NumCPU(), which
// only ever answers for this machine, never the one on the other end
// of a connection.
func countCPUInfoProcessors(data string) int {
	n := 0
	for _, line := range strings.Split(data, "\n") {
		if strings.HasPrefix(line, "processor") {
			n++
		}
	}
	return n
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
	return parseMemInfoKB(string(data)), nil
}

// parseMemInfoKB is memInfoKB's own parsing half, split out so the
// identical field-name → kB map also serves a remote /proc/meminfo
// (see remoteSystemInfoHealthLines) — the same remote-reuse reason
// parseOSReleasePrettyName/parseCPUModelName already give.
func parseMemInfoKB(data string) map[string]int64 {
	m := make(map[string]int64)
	for _, line := range strings.Split(data, "\n") {
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
	return m
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
	return memoryUsageBytesFrom(m)
}

// memoryUsageBytesFrom is memoryUsageBytes' own computation half,
// split out so it also serves a remote /proc/meminfo's own already-
// parsed map (see remoteSystemInfoHealthLines).
func memoryUsageBytesFrom(m map[string]int64) (total, used int64, ok bool) {
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
	return swapUsageBytesFrom(m)
}

// swapUsageBytesFrom is swapUsageBytes' own computation half, split
// out for the identical remote-reuse reason memoryUsageBytesFrom is.
func swapUsageBytesFrom(m map[string]int64) (total, used int64, ok bool) {
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
	return parseOpenFileHandles(string(data))
}

// parseOpenFileHandles is openFileHandles' own parsing half, split out
// for the identical remote-reuse reason parseOSReleasePrettyName is
// (see remoteSystemInfoHealthLines).
func parseOpenFileHandles(data string) (allocated, max int64, ok bool) {
	fields := strings.Fields(data)
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
	return parseNetworkInterfaces(string(data)), true
}

// parseNetworkInterfaces is networkInterfaces' own parsing half, split
// out for the identical remote-reuse reason parseOSReleasePrettyName
// is (see remoteSystemInfoCountLines).
func parseNetworkInterfaces(data string) []string {
	lines := strings.Split(data, "\n")
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
	return ifaces
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
