package batchrename

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CaseMode is the "Case" step's own choice of transform — see Rules.
type CaseMode int

const (
	// CaseNone leaves the base name's letters exactly as they are.
	CaseNone CaseMode = iota
	CaseUpper
	CaseLower
	// CaseTitle uppercases the first letter of every word (a "word" is
	// a maximal run of letters/digits) and lowercases the rest of it —
	// "my_report-v2" becomes "My_Report-V2".
	CaseTitle
	// CaseSentence uppercases only the first letter of the whole base
	// name and lowercases everything else.
	CaseSentence
)

// NumberPosition is the "Numbering" step's own choice of where the
// counter goes — see Rules.
type NumberPosition int

const (
	// NumberNone inserts no counter at all.
	NumberNone NumberPosition = iota
	NumberPrefix
	NumberSuffix
)

// ExtensionMode is the "Extension" step's own choice of transform —
// see Rules.
type ExtensionMode int

const (
	// ExtensionKeep leaves the extension exactly as it is.
	ExtensionKeep ExtensionMode = iota
	ExtensionLower
	ExtensionUpper
	// ExtensionRemove drops the extension entirely.
	ExtensionRemove
	// ExtensionSetTo replaces the extension with Rules.ExtensionValue.
	ExtensionSetTo
)

// Rules is one complete set of batch-rename settings — every step's
// own fields, all at once, since the pipeline's order is fixed rather
// than something the user assembles themselves (see the package doc).
// The zero Rules is a complete no-op: Rename returns every name
// unchanged.
type Rules struct {
	// Find/Replace/Regex back step 1 — Search & replace. Find == ""
	// means "do nothing"; Replace is only ever consulted when Find
	// isn't empty.
	Find    string
	Replace string
	Regex   bool

	// Case backs step 2.
	Case CaseMode

	// TrimFront/TrimBack back step 3 — how many characters (runes, not
	// bytes) to drop from the front and back of the base name.
	TrimFront int
	TrimBack  int

	// NumberPosition/NumberStart/NumberStep/NumberDigits back step 4.
	// NumberStart is the first counter value handed out (to the file at
	// index 0 — see Rename); NumberStep is added per index after that.
	// NumberDigits is the minimum width the counter is zero-padded to.
	NumberPosition NumberPosition
	NumberStart    int
	NumberStep     int
	NumberDigits   int

	// ExtensionMode/ExtensionValue back step 5. ExtensionValue is only
	// consulted when ExtensionMode is ExtensionSetTo — a leading "."
	// on it is optional, Rename accepts either.
	ExtensionMode  ExtensionMode
	ExtensionValue string
}

// numberSeparator joins an inserted counter to the rest of the base
// name — a fixed choice for this first version rather than a field of
// its own to configure, the same "groundwork first" scope as the rest
// of this package (see the package doc).
const numberSeparator = "-"

// splitName separates name into its base and extension the way this
// package treats them throughout — filepath.Ext would call the whole
// of ".bashrc" its own extension (there is no dot elsewhere in the
// name to find instead), which is wrong for a dotfile: ".bashrc" is a
// hidden file called "bashrc", not a nameless file whose extension is
// ".bashrc". Any leading dots are treated as part of the hidden-file
// marker, not as ending the base name; only a dot found after that
// counts as the start of an extension.
//
// Only ever splits off the single, final extension (an
// "archive.tar.gz" is base "archive.tar", extension ".gz") — the same
// single-extension convention most rename tools use, including Total
// Commander's own default.
func splitName(name string) (base, ext string) {
	trimmed := strings.TrimLeft(name, ".")
	leading := len(name) - len(trimmed)

	if i := strings.LastIndexByte(trimmed, '.'); i >= 0 {
		return name[:leading+i], name[leading+i:]
	}
	return name, ""
}

// applyFindReplace is step 1. An empty Find is a no-op (see Rules'
// own doc comment); otherwise it's a plain, case-sensitive substring
// replace, or a Go regexp.Regexp match/replace when Regex is set.
func applyFindReplace(base string, rules Rules) (string, error) {
	if rules.Find == "" {
		return base, nil
	}
	if !rules.Regex {
		return strings.ReplaceAll(base, rules.Find, rules.Replace), nil
	}
	re, err := regexp.Compile(rules.Find)
	if err != nil {
		return "", fmt.Errorf("search pattern: %w", err)
	}
	return re.ReplaceAllString(base, rules.Replace), nil
}

// applyCase is step 2.
func applyCase(base string, mode CaseMode) string {
	switch mode {
	case CaseUpper:
		return strings.ToUpper(base)
	case CaseLower:
		return strings.ToLower(base)
	case CaseTitle:
		return toTitleCase(base)
	case CaseSentence:
		return toSentenceCase(base)
	default:
		return base
	}
}

// toTitleCase implements CaseTitle — deliberately not the standard
// library's own strings.Title (long deprecated, and it only ever looks
// at Unicode word boundaries, not the "letters/digits are one word"
// rule a filename actually wants: "v2" should stay one word, not
// become "V2" -> "V2" split into "v" and "2").
func toTitleCase(s string) string {
	var b strings.Builder
	startOfWord := true
	for _, r := range s {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			b.WriteRune(r)
			startOfWord = true
		case startOfWord:
			b.WriteRune(unicode.ToUpper(r))
			startOfWord = false
		default:
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// toSentenceCase implements CaseSentence: lowercase throughout, except
// the first letter found, which is uppercased. A name with no letters
// at all (all digits/punctuation) comes back merely lowercased, which
// for such a name is already a no-op.
func toSentenceCase(s string) string {
	lower := strings.ToLower(s)
	for i, r := range lower {
		if unicode.IsLetter(r) {
			return lower[:i] + strings.ToUpper(string(r)) + lower[i+utf8.RuneLen(r):]
		}
	}
	return lower
}

// applyTrim is step 3. Rune-aware (not byte-aware) so a multi-byte
// character at either end is dropped whole rather than corrupted; a
// count larger than the name simply empties it rather than panicking.
// A negative count (not reachable through the UI's own input field, but
// not this package's job to assume) is treated as 0, the same "an
// unreasonable value degrades to a no-op" rule NumberDigits below
// already follows.
func applyTrim(base string, front, back int) string {
	if front < 0 {
		front = 0
	}
	if back < 0 {
		back = 0
	}
	runes := []rune(base)
	if front > 0 {
		if front > len(runes) {
			front = len(runes)
		}
		runes = runes[front:]
	}
	if back > 0 {
		if back > len(runes) {
			back = len(runes)
		}
		runes = runes[:len(runes)-back]
	}
	return string(runes)
}

// applyNumbering is step 4. index is the file's own position within
// the batch (see Rename) — NumberStart plus index*NumberStep, zero-
// padded to NumberDigits and joined with numberSeparator. NumberStep
// of 0 counts as 1 (an unset field, not a deliberate "every file gets
// the same number"); NumberDigits below 1 counts as 1.
func applyNumbering(base string, rules Rules, index int) string {
	if rules.NumberPosition == NumberNone {
		return base
	}

	step := rules.NumberStep
	if step == 0 {
		step = 1
	}
	digits := rules.NumberDigits
	if digits < 1 {
		digits = 1
	}

	counter := fmt.Sprintf("%0*d", digits, rules.NumberStart+index*step)
	switch rules.NumberPosition {
	case NumberPrefix:
		return counter + numberSeparator + base
	case NumberSuffix:
		return base + numberSeparator + counter
	default:
		return base
	}
}

// applyExtension is step 5 — acts on the extension split off by
// splitName, never on the base name the other four steps work on.
func applyExtension(ext string, rules Rules) string {
	switch rules.ExtensionMode {
	case ExtensionLower:
		return strings.ToLower(ext)
	case ExtensionUpper:
		return strings.ToUpper(ext)
	case ExtensionRemove:
		return ""
	case ExtensionSetTo:
		value := strings.TrimPrefix(rules.ExtensionValue, ".")
		if value == "" {
			return ""
		}
		return "." + value
	default:
		return ext
	}
}

// Rename computes the new base+extension name is renamed to, applying
// every step in Rules' own fixed order (see the package doc). index is
// this file's own position within the batch it's part of (0-based, in
// whatever order the caller is iterating — see Plan), consulted only
// by the numbering step.
//
// Returns an error only when Regex is set and Find isn't a valid Go
// regexp — every other step always succeeds.
func Rename(rules Rules, name string, index int) (string, error) {
	base, ext := splitName(name)

	base, err := applyFindReplace(base, rules)
	if err != nil {
		return "", err
	}
	base = applyCase(base, rules.Case)
	base = applyTrim(base, rules.TrimFront, rules.TrimBack)
	base = applyNumbering(base, rules, index)
	ext = applyExtension(ext, rules)

	return base + ext, nil
}
