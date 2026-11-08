package logview

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// journalPseudoFile is what every Entry.File from ReadJournal carries —
// there is no real file backing a systemd-journal entry, but Merge's
// own tie-breaker and the UI's "current file" column (internal/ui's
// refreshLogAuditCurrentFile/refreshLogAuditJournal) both expect a
// non-empty name to show.
const journalPseudoFile = "systemd-journal"

// journalSourceKeys are, in priority order, the journald fields that
// best identify which service wrote an entry — SYSLOG_IDENTIFIER is
// what a service names itself (closest match to Entry.Source's own
// concept elsewhere), _COMM/_SYSTEMD_UNIT are systemd's own fallbacks
// for a process that never set one.
var journalSourceKeys = []string{"SYSLOG_IDENTIFIER", "_COMM", "_SYSTEMD_UNIT"}

// JournalAvailable reports whether journalctl exists on $PATH — the
// gate internal/ui uses to decide whether to offer a systemd-journal
// row at all (see logauditjournal.go's own doc comment). journald is
// Linux-only; this is simply false, not an error, on any other
// platform.
func JournalAvailable() bool {
	_, err := exec.LookPath("journalctl")
	return err == nil
}

// JournalOptions bounds a ReadJournal call — Lines is the window size
// ("-n"), the only knob this first version exposes. Unit/boot
// selection, priority pre-filtering, and "-f" streaming are
// deliberately out of scope (see feature_ideas.txt) — the caller
// re-runs ReadJournal on its own follow tick instead of journalctl's
// own "-f" staying resident.
type JournalOptions struct {
	Lines int
}

// defaultJournalLines is Lines' own fallback when a caller leaves it
// at (or below) zero.
const defaultJournalLines = 1000

// ReadJournal runs "journalctl -o json -n <Lines>" and decodes its
// output into Entry values — the systemd-journal counterpart to
// ParseAll, but for a binary store only journalctl itself knows how to
// read, not a FileSource (see internal/ui's remoteLogFileSource/
// FileSource doc comments for why: there is no path, size, or mtime
// for a live, append-only journal to report, and Discover's own
// logrotate-family grouping has nothing to group here). A malformed
// individual line is skipped, not fatal — the same "a few corrupt
// entries don't lose the rest" tolerance ParseAll already has for an
// unparseable line elsewhere.
func ReadJournal(ctx context.Context, opts JournalOptions) ([]Entry, error) {
	lines := opts.Lines
	if lines <= 0 {
		lines = defaultJournalLines
	}

	cmd := exec.CommandContext(ctx, "journalctl", "-o", "json", "--no-pager", "-n", strconv.Itoa(lines))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var entries []Entry
	n := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		n++
		if e, ok := decodeJournalLine(line, n); ok {
			entries = append(entries, e)
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if waitErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return nil, fmt.Errorf("journalctl: %s", msg)
	}
	if scanErr != nil {
		return nil, scanErr
	}
	return entries, nil
}

// decodeJournalLine parses one line of journalctl's own "-o json"
// output into an Entry — ok is false only for a line that isn't valid
// JSON at all (ReadJournal skips it rather than failing the whole
// read).
func decodeJournalLine(line string, n int) (Entry, bool) {
	var fields map[string]any
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		return Entry{}, false
	}

	e := Entry{
		Raw:  line,
		File: journalPseudoFile,
		Line: n,
	}
	if ts, ok := fields["__REALTIME_TIMESTAMP"].(string); ok {
		if usec, err := strconv.ParseInt(ts, 10, 64); err == nil {
			e.Time = time.UnixMicro(usec)
		}
	}
	if pri, ok := fields["PRIORITY"].(string); ok {
		if sev, err := strconv.Atoi(pri); err == nil {
			// PRIORITY is already the declared syslog severity journald
			// itself assigned, not a scanned guess — LevelInferred stays
			// false (its own zero value), unlike a level ParseAll's
			// generic formats sometimes have to infer from free text.
			e.Level = rfc5424SeverityLevel(sev)
		}
	}
	e.Source = firstStringField(fields, journalSourceKeys)
	e.Message = journalMessageField(fields["MESSAGE"])
	if e.Message == "" {
		e.Message = line
	}
	return e, true
}

// journalMessageField extracts MESSAGE's own text. Ordinarily a plain
// string, but journalctl's JSON output renders a field containing
// non-UTF8 bytes or repeated values as a JSON array instead (systemd's
// own documented behavior, not a parse error): an array of numbers is
// the field's raw bytes, individually; an array of strings is the same
// key logged more than once in a single entry, joined so nothing is
// silently dropped.
func journalMessageField(v any) string {
	switch m := v.(type) {
	case string:
		return m
	case []any:
		if len(m) == 0 {
			return ""
		}
		if _, ok := m[0].(string); ok {
			parts := make([]string, 0, len(m))
			for _, p := range m {
				if s, ok := p.(string); ok {
					parts = append(parts, s)
				}
			}
			return strings.Join(parts, "\n")
		}
		buf := make([]byte, 0, len(m))
		for _, b := range m {
			if f, ok := b.(float64); ok {
				buf = append(buf, byte(int(f)))
			}
		}
		return string(buf)
	default:
		return ""
	}
}
