// Package logview turns a directory's own log files — plain, rotated,
// or gzip-compressed — into one chronologically merged stream of
// LogEntry values, independent of any UI.
//
// The pipeline is Discover (find candidate files, group logrotate
// families) → Open (transparently decompress) → Detect (sniff the
// format from a small sample) → Parse (turn lines into LogEntry) →
// Merge (combine several files' own entries into one chronological
// stream). internal/ui's own Log Audit screen ("jL") is the only
// caller; everything here stays free of tcell/tview so the format/
// merge logic can be tested without a terminal.
package logview
