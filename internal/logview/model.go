package logview

import "time"

// Level is a log entry's own severity, normalized across every format
// Detect recognizes — so the UI can color/filter/navigate
// ("next error", "next warning") by one shared scale regardless of
// whether the source line said "ERROR", "error", "err", or a syslog
// numeric facility/severity code.
type Level int

const (
	LevelUnknown Level = iota
	LevelTrace
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// String renders Level the same fixed-width-friendly way
// activitylog.Level.String() already does elsewhere in this app.
func (l Level) String() string {
	switch l {
	case LevelTrace:
		return "TRACE"
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return ""
	}
}

// ParseLevel maps a format's own level token (any case) to Level —
// "" or anything unrecognized is LevelUnknown, not an error: a line
// with no level token at all is still a perfectly good LogEntry, just
// one the error/warning navigation will skip over.
func ParseLevel(token string) Level {
	switch normalizeLevelToken(token) {
	case "trace":
		return LevelTrace
	case "debug", "dbg":
		return LevelDebug
	case "info", "information", "notice":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error", "err", "critical", "crit", "alert", "emerg", "emergency":
		return LevelError
	case "fatal", "panic":
		return LevelFatal
	default:
		return LevelUnknown
	}
}

func normalizeLevelToken(token string) string {
	out := make([]byte, 0, len(token))
	for _, c := range token {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c >= 'a' && c <= 'z' {
			out = append(out, byte(c))
		}
	}
	return string(out)
}

// Entry is one parsed log line — the shared shape every format (JSON
// Lines, syslog, generic timestamp+level, or the plain fallback)
// reduces to, so the UI never needs to know which format a line came
// from.
type Entry struct {
	Time    time.Time // zero if the line carried no recognizable timestamp
	Level   Level
	Source  string // e.g. a JSON "service"/"logger" field, a syslog tag, or "" if none
	Message string
	File    string // base name of the file this line came from
	Line    int    // 1-based line number within File — tie-breaker for Merge
}
