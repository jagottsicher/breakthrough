package fsops

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DuplicateStrategy selects how ComputeDuplicateName builds the suffix
// it appends to a duplicated file's own basename.
type DuplicateStrategy int

const (
	// DuplicateSuffixText appends a fixed piece of text (e.g. "BAK"),
	// verbatim, exactly once — see ComputeDuplicateName's own doc
	// comment for what happens if the result already exists.
	DuplicateSuffixText DuplicateStrategy = iota
	// DuplicateNumbered appends an incrementing number, starting at 1,
	// scanning upward until a free name is found.
	DuplicateNumbered
	// DuplicateDateTime appends a timestamp — either formatted (see
	// DuplicateOptions.DateTimeFormat/DateTimeStrftime) or, with
	// DateTimeUseUnix, a raw Unix timestamp.
	DuplicateDateTime
)

// duplicateNumberedScanLimit bounds DuplicateNumbered's own upward scan
// — not a designed ceiling on how many duplicates someone can
// reasonably have (100000 numbered duplicates of the same file is not
// a real scenario this needs to serve), purely a safety net against an
// unbounded loop if something else about a directory's own state makes
// every single candidate this ever tries come back "already exists". A
// var, not a const, so a test can lower it temporarily rather than
// actually creating 100000 files on disk to exercise the limit itself.
var duplicateNumberedScanLimit = 100000

// DuplicateOptions configures ComputeDuplicateName — see its own field
// comments for what each one means, and the function's own doc comment
// for how they combine per strategy.
type DuplicateOptions struct {
	// Separator sits between the original basename (its extension, if
	// any, held aside — see ComputeDuplicateName) and whatever the
	// chosen strategy produces. Free-form: "_", "-", "." are the
	// obvious choices, but nothing here requires any particular one.
	Separator string

	Strategy DuplicateStrategy

	// SuffixText is DuplicateSuffixText's own literal suffix.
	SuffixText string

	// NumberPadding is DuplicateNumbered's own zero-padding width — 0
	// means no padding at all ("_1", "_2", ... "_10"), a positive value
	// pads every number to at least that many digits ("_001", "_002",
	// ... "_010" for NumberPadding 3).
	NumberPadding int

	// DateTimeFormat is a format string for DuplicateDateTime,
	// interpreted as a Go reference-time layout (the default) or, with
	// DateTimeStrftime, as a strftime-style format — see
	// strftimeToGoLayout's own doc comment for exactly which
	// specifiers that second mode supports. Ignored entirely when
	// DateTimeUseUnix is set.
	DateTimeFormat   string
	DateTimeStrftime bool

	// DateTimeUseUnix, when true, appends a raw Unix timestamp instead
	// of a formatted one — the escape hatch for anyone who'd rather not
	// deal with a format string at all. Takes priority over
	// DateTimeFormat/DateTimeStrftime when set.
	DateTimeUseUnix bool

	// Now overrides time.Now() for DuplicateDateTime — a test hook
	// only; the zero value means "use the real current time", which is
	// what every real caller wants.
	Now time.Time
}

// ComputeDuplicateName returns the path a duplicate of src should be
// created at — the same directory and extension, with a new basename
// built from opts' own separator and strategy. This never touches the
// filesystem beyond checking whether a candidate path already exists
// (DuplicateNumbered's own scan); it never creates, moves, or copies
// anything itself — the caller runs an entirely ordinary Copy(src,
// result, ...) afterward. fsops.Overlaps never has to be taught
// anything new for this: src and the computed result are always
// different paths by construction, so the existing "destination is the
// same as, or inside, the source" refusal in Copy/Move is neither
// bypassed nor even relevant here.
//
// Each strategy computes exactly one candidate and stops there, except
// DuplicateNumbered, which is the one shape with an obvious "next" step
// to fall back on:
//
//   - DuplicateSuffixText and DuplicateDateTime never retry. If the one
//     candidate each computes already exists, ComputeDuplicateName
//     returns it anyway — the caller's own subsequent Copy call is what
//     actually reports "already exists", the same ordinary error any
//     other conflicting Copy already produces, no special-cased message
//     needed here. Deliberately no auto-retry loop for either: for
//     DuplicateSuffixText, running Duplicate a second time on the
//     result ("xyz_BAK") produces "xyz_BAK_BAK" by applying the exact
//     same one-shot rule again to the new name, which is the intended
//     way repeated suffixes ever arise — not an internal loop within a
//     single call. For DuplicateDateTime, there's no natural "next"
//     timestamp to fall back on the way there's an obvious next number.
//   - DuplicateNumbered scans upward from 1, returning the first
//     candidate that doesn't already exist — the ordinary, unsurprising
//     behavior a "duplicate" feature needs so it keeps working once a
//     few numbered duplicates already exist, capped at
//     duplicateNumberedScanLimit purely as a runaway-loop safety net.
func ComputeDuplicateName(src string, opts DuplicateOptions) (string, error) {
	dir := filepath.Dir(src)
	base := filepath.Base(src)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	if opts.Strategy != DuplicateNumbered {
		suffix, err := duplicateSuffix(opts)
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, stem+opts.Separator+suffix+ext), nil
	}

	for n := 1; n <= duplicateNumberedScanLimit; n++ {
		numStr := strconv.Itoa(n)
		if opts.NumberPadding > 0 {
			numStr = fmt.Sprintf("%0*d", opts.NumberPadding, n)
		}
		candidate := filepath.Join(dir, stem+opts.Separator+numStr+ext)
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("fsops: no free numbered name found for %s after %d attempts", src, duplicateNumberedScanLimit)
}

// duplicateSuffix computes the non-numbered strategies' own one-shot
// suffix text — split out of ComputeDuplicateName since
// DuplicateNumbered's own suffix depends on the candidate number being
// tried, not a single fixed value the way the other two are.
func duplicateSuffix(opts DuplicateOptions) (string, error) {
	switch opts.Strategy {
	case DuplicateSuffixText:
		return opts.SuffixText, nil
	case DuplicateNumbered:
		return "", fmt.Errorf("fsops: duplicateSuffix does not handle DuplicateNumbered — see ComputeDuplicateName's own scan loop")
	case DuplicateDateTime:
		now := opts.Now
		if now.IsZero() {
			now = time.Now()
		}
		if opts.DateTimeUseUnix {
			return strconv.FormatInt(now.Unix(), 10), nil
		}
		layout := opts.DateTimeFormat
		if opts.DateTimeStrftime {
			var err error
			layout, err = strftimeToGoLayout(opts.DateTimeFormat)
			if err != nil {
				return "", err
			}
		}
		return now.Format(layout), nil
	default:
		return "", fmt.Errorf("fsops: unknown DuplicateStrategy %d", opts.Strategy)
	}
}

// strftimeToGoLayout translates a commonly-used subset of strftime's
// own %-directives into Go's reference-time layout equivalent — just
// enough for a duplicate's own date/time suffix, not the full strftime
// specification: no locale-dependent %c/%x/%X, no week-number fields,
// nothing beyond the directives below.
//
// GNU's "%-" no-padding variant (e.g. "%-d" instead of "%d") is
// supported for every field where Go itself has a distinct unpadded
// layout token to translate it to — every one of them except the
// 24-hour hour, which Go's own layout vocabulary has no unpadded token
// for at all (only "15", always zero-padded); "%-H"/"%H" both fall back
// to it, so an early-morning hour prints with its leading zero
// regardless ("05", never "5") — a cosmetic gap in Go's own layout
// system, not something worth writing custom formatting code to work
// around for one digit.
func strftimeToGoLayout(format string) (string, error) {
	var b strings.Builder
	i := 0
	for i < len(format) {
		c := format[i]
		if c != '%' {
			b.WriteByte(c)
			i++
			continue
		}
		i++
		if i >= len(format) {
			return "", fmt.Errorf("fsops: dangling %%%% at the end of the format string %q", format)
		}
		noPad := false
		if format[i] == '-' {
			noPad = true
			i++
			if i >= len(format) {
				return "", fmt.Errorf("fsops: dangling %%%%- at the end of the format string %q", format)
			}
		}
		spec := format[i]
		i++
		layout, ok := strftimeLayout(spec, noPad)
		if !ok {
			return "", fmt.Errorf("fsops: unsupported strftime specifier %%%c in format string %q", spec, format)
		}
		b.WriteString(layout)
	}
	return b.String(), nil
}

// strftimeLayout is strftimeToGoLayout's own per-specifier lookup,
// split out so its own table is easy to scan and extend on its own.
func strftimeLayout(spec byte, noPad bool) (string, bool) {
	switch spec {
	case 'Y':
		return "2006", true
	case 'y':
		return "06", true
	case 'm':
		if noPad {
			return "1", true
		}
		return "01", true
	case 'd':
		if noPad {
			return "2", true
		}
		return "02", true
	case 'H':
		return "15", true // see strftimeToGoLayout's own doc comment: no unpadded 24h token exists in Go
	case 'I':
		if noPad {
			return "3", true
		}
		return "03", true
	case 'M':
		return "04", true
	case 'S':
		return "05", true
	case 'p':
		return "PM", true
	case 'B':
		return "January", true
	case 'b':
		return "Jan", true
	case 'A':
		return "Monday", true
	case 'a':
		return "Mon", true
	case 'j':
		return "002", true
	case 'Z':
		return "MST", true
	case 'z':
		return "-0700", true
	case '%':
		return "%", true
	default:
		return "", false
	}
}
