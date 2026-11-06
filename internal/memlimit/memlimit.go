// Package memlimit sets a soft process-wide memory ceiling
// (runtime/debug.SetMemoryLimit, "GOMEMLIMIT") from the host's own
// total RAM — a safety net for a real, user-reported case: opening a
// multi-million-entry log family (internal/ui's Log Audit screen)
// under WSL2 can legitimately need several GB of live heap while
// loading, and without a limit the Go pacer lets the heap grow to
// roughly 2x whatever it last measured as live (GOGC=100's default
// behavior) — a goal that can land past the host's own real ceiling,
// killing the process outright rather than ever giving the GC a
// chance to collect harder first (see feature_ideas.txt 19e for the
// full gctrace analysis this is based on).
//
// This does not reduce how much memory that screen actually needs —
// a live-set measurement (19e) found the peak during loading is
// transient and the process recovers on its own once loading
// finishes, which is exactly what a soft limit is for: forcing the
// pacer to collect earlier and more often during that window instead
// of chasing an unreachable goal. It is not a fix for a live set that
// is already close to the limit — the GC can end up running almost
// continuously in that case ("death spiral"), trading an outright
// kill for a process that is merely very slow. That tradeoff is the
// whole point here: slow-but-alive over killed-outright.
package memlimit

import (
	"bufio"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// fraction is how much of the host's own total RAM becomes the soft
// ceiling — left enough headroom (the other 20%) for everything else
// sharing the same memory (the OS itself, a WSL2 VM's own overhead, a
// terminal multiplexer, whatever else is running) that this process
// alone has no visibility into and must not claim all of.
const fraction = 0.8

// Apply reads the host's own total RAM from /proc/meminfo and calls
// debug.SetMemoryLimit with fraction of it, returning the limit it
// set (in bytes) and whether it actually did anything. A missing or
// unparsable /proc/meminfo — every non-Linux platform, or a Linux
// sandbox without it — is treated as "cannot determine total RAM",
// not an error: Apply simply does nothing and returns false, leaving
// this process's own memory behavior exactly as it was before this
// package existed. That covers macOS/BSD (CLAUDE.md: must be
// considered, not necessarily matched feature-for-feature) without
// any platform-specific code of its own.
func Apply() (limitBytes int64, applied bool) {
	total, ok := totalMemBytes("/proc/meminfo")
	if !ok || total <= 0 {
		return 0, false
	}
	limitBytes = int64(float64(total) * fraction)
	debug.SetMemoryLimit(limitBytes)
	return limitBytes, true
}

// totalMemBytes parses path's own "MemTotal: NNNN kB" line — the one
// line /proc/meminfo documents (proc(5)) as always present whenever
// the file itself is. Deliberately tolerant of anything else in the
// file: every other line, and any line this doesn't recognize, is
// simply skipped rather than treated as a parse failure.
func totalMemBytes(path string) (int64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		rest, ok := strings.CutPrefix(line, "MemTotal:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0, false
		}
		kb, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0, false
		}
		return kb * 1024, true
	}
	if err := scanner.Err(); err != nil {
		return 0, false
	}
	return 0, false
}
