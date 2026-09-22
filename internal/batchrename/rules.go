package batchrename

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jagottsicher/breakthrough/internal/fsops"
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

// NumberOrder is the "Numbering" step's own choice of which file gets
// which number — see Rules.NumberOrder.
type NumberOrder int

const (
	// OrderAsListed counts in the order the caller hands the paths over
	// (see Ordered) — the preview's own order, which the user can
	// rearrange by hand.
	OrderAsListed NumberOrder = iota
	// OrderByName counts in case-insensitive name order.
	OrderByName
	// OrderByModTime counts oldest-first by modification time.
	OrderByModTime
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
// unchanged. The json tags are the on-disk shape of a preset (see
// preset.go) — snake_case keys, enums by their spelling (see enums.go).
type Rules struct {
	// Find/Replace/Regex back step 1 — Search & replace. Find == ""
	// means "do nothing"; Replace is only ever consulted when Find
	// isn't empty.
	Find    string `json:"find"`
	Replace string `json:"replace"`
	Regex   bool   `json:"regex"`

	// Case backs step 2.
	Case CaseMode `json:"case"`

	// TrimFront/TrimBack back step 3 — how many characters (runes, not
	// bytes) to drop from the front and back of the base name.
	TrimFront int `json:"trim_front"`
	TrimBack  int `json:"trim_back"`

	// Template backs step 4 — an optional pattern the base name is
	// rebuilt from, with {name}, {ext}, {counter}, {parent} and {date}
	// tokens (see applyTemplate for what each expands to). "" means "do
	// nothing"; a template without any token replaces every name with
	// the same literal text, which Plan then reports as the collisions
	// it is. DateFormat is what {date} prints the modification time as
	// — a Go reference-time layout, or with DateStrftime a strftime
	// format (the same choice Duplicate/Multiply offer, and the same
	// translator: fsops.StrftimeToGoLayout); "" means templateDateDefault.
	Template     string `json:"template"`
	DateFormat   string `json:"date_format"`
	DateStrftime bool   `json:"date_strftime"`

	// NumberPosition/NumberStart/NumberStep/NumberDigits back step 5.
	// NumberStart is the first counter value handed out (to the file at
	// index 0 — see Rename); NumberStep is added per index after that.
	// NumberDigits is the minimum width the counter is zero-padded to.
	// The Template step's {counter} token prints this same counter, so
	// Start/Step/Digits apply to it too, whatever NumberPosition says.
	NumberPosition NumberPosition `json:"number_position"`
	NumberStart    int            `json:"number_start"`
	NumberStep     int            `json:"number_step"`
	NumberDigits   int            `json:"number_digits"`

	// NumberOrder/NumberReversed decide which file is index 0, 1, 2...
	// — see Ordered, which is where they take effect; Rename itself only
	// ever sees the resulting Input.Index. Reversed flips whichever
	// order NumberOrder picked, so "by name, reversed" is Z to A.
	NumberOrder    NumberOrder `json:"number_order"`
	NumberReversed bool        `json:"number_reversed"`

	// ExtensionMode/ExtensionValue back step 6. ExtensionValue is only
	// consulted when ExtensionMode is ExtensionSetTo — a leading "."
	// on it is optional, Rename accepts either.
	ExtensionMode  ExtensionMode `json:"extension_mode"`
	ExtensionValue string        `json:"extension_value"`

	// ExtensionOnDirs decides whether a *directory* in the batch is
	// split into base + extension at all. Off (the default), a
	// directory's whole name is its base name — "my.project" is a folder
	// called "my.project", not a nameless folder with a ".project"
	// extension — so neither the Extension step nor anything else ever
	// touches what looks like one. The same default Total Commander's
	// own Multi-Rename Tool uses for directories. On, a directory is
	// treated exactly like a file.
	ExtensionOnDirs bool `json:"extension_on_dirs"`
}

// Input is everything Rename needs to know about one entry of the
// batch beyond Rules itself — its current name, whether it's a
// directory (see Rules.ExtensionOnDirs), its own position within the
// batch (0-based, in whatever order the caller is iterating — see
// Plan), consulted by the numbering step and the {counter} token, and
// the two things only the Template step's tokens ever look at: the
// name of the directory it sits in ({parent}) and its modification
// time ({date}).
type Input struct {
	Name    string
	IsDir   bool
	Index   int
	Parent  string
	ModTime time.Time
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
//
// In Regex mode, Replace may refer back to capture groups: Go's own
// "$1"/"${name}" forms work as-is (regexp.ReplaceAllString expands
// them), and the "\1" form sed, Perl, and Total Commander's own tool
// all use is accepted too — see backrefsToGo — so whichever habit
// someone brings along just works, rather than one of the two silently
// inserting literal text.
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
	return re.ReplaceAllString(base, backrefsToGo(rules.Replace)), nil
}

// backrefsToGo rewrites sed-style "\1".."\9" back-references in a
// replacement string into the "${1}" form Go's regexp expands —
// braced, not bare "$1", so a digit right after the group number
// ("\1" followed by "0" in the text) can't be misread as group 10.
// Everything else, including a literal "$" (which Go would otherwise
// also treat as the start of a reference — "$$" is its escape), is
// left exactly as typed: this only ever adds the one spelling Go
// lacks, it doesn't try to second-guess the rest.
func backrefsToGo(replace string) string {
	if !strings.Contains(replace, `\`) {
		return replace
	}
	var b strings.Builder
	for i := 0; i < len(replace); i++ {
		c := replace[i]
		if c == '\\' && i+1 < len(replace) && replace[i+1] >= '1' && replace[i+1] <= '9' {
			b.WriteString("${")
			b.WriteByte(replace[i+1])
			b.WriteString("}")
			i++
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
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

// templateDateDefault is what {date} prints when Rules.DateFormat is
// empty: ISO 8601's own date, which sorts correctly as text and has no
// characters a filename would mind.
const templateDateDefault = "2006-01-02"

// TemplateTokens lists every token applyTemplate expands, with a short
// description each — the UI's own cheat sheet reads from this rather
// than keeping a second copy that would drift.
var TemplateTokens = []struct{ Token, Meaning string }{
	{"{name}", "the name after the steps above"},
	{"{ext}", "the extension, without its dot"},
	{"{counter}", "Numbering's counter (Start/Step/Digits apply)"},
	{"{parent}", "the folder's name"},
	{"{date}", "the modification date, per \"Date format\""},
}

// applyTemplate is step 4. An empty Template is a no-op; otherwise the
// base name is replaced by Template with each token expanded (see
// TemplateTokens). Anything that isn't a known token — including an
// unknown "{something}" — is copied through literally, so a name that
// legitimately contains braces survives. Returns an error only when
// DateFormat is a strftime format that can't be translated (see
// fsops.StrftimeToGoLayout); a Go layout can't fail, it just prints
// whatever it says.
func applyTemplate(base, ext string, rules Rules, in Input) (string, error) {
	if rules.Template == "" {
		return base, nil
	}
	date := ""
	if strings.Contains(rules.Template, "{date}") {
		layout := rules.DateFormat
		if layout == "" {
			layout = templateDateDefault
			if rules.DateStrftime {
				layout = "%Y-%m-%d"
			}
		}
		if rules.DateStrftime {
			var err error
			if layout, err = fsops.StrftimeToGoLayout(layout); err != nil {
				return "", fmt.Errorf("date format: %w", err)
			}
		}
		date = in.ModTime.Format(layout)
	}
	replacer := strings.NewReplacer(
		"{name}", base,
		"{ext}", strings.TrimPrefix(ext, "."),
		"{counter}", counterText(rules, in.Index),
		"{parent}", in.Parent,
		"{date}", date,
	)
	return replacer.Replace(rules.Template), nil
}

// counterText is the numbering counter for the file at index —
// NumberStart plus index*NumberStep, zero-padded to NumberDigits —
// shared by the Numbering step and the Template step's {counter}
// token, so the two never disagree about what number a file has.
// NumberStep of 0 counts as 1 (an unset field, not a deliberate "every
// file gets the same number"); NumberDigits below 1 counts as 1.
func counterText(rules Rules, index int) string {
	step := rules.NumberStep
	if step == 0 {
		step = 1
	}
	digits := rules.NumberDigits
	if digits < 1 {
		digits = 1
	}
	return fmt.Sprintf("%0*d", digits, rules.NumberStart+index*step)
}

// applyNumbering is step 5. index is the file's own position within
// the batch (see Rename) — the counter (see counterText) joined with
// numberSeparator on whichever side NumberPosition says.
func applyNumbering(base string, rules Rules, index int) string {
	if rules.NumberPosition == NumberNone {
		return base
	}

	counter := counterText(rules, index)
	switch rules.NumberPosition {
	case NumberPrefix:
		return counter + numberSeparator + base
	case NumberSuffix:
		return base + numberSeparator + counter
	default:
		return base
	}
}

// applyExtension is step 6 — acts on the extension split off by
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

// Rename computes the new name in.Name is renamed to, applying every
// step in Rules' own fixed order (see the package doc). See Input for
// what else about the entry it consults.
//
// A directory is never split into base + extension unless
// Rules.ExtensionOnDirs asks for it (see its own doc comment) — its
// whole name goes through the base-name steps as one piece, and the
// Extension step has nothing to act on.
//
// Returns an error only when Regex is set and Find isn't a valid Go
// regexp, or the Template step's strftime date format can't be
// translated — every other step always succeeds.
func Rename(rules Rules, in Input) (string, error) {
	base, ext := in.Name, ""
	if !in.IsDir || rules.ExtensionOnDirs {
		base, ext = splitName(in.Name)
	}

	base, err := applyFindReplace(base, rules)
	if err != nil {
		return "", err
	}
	base = applyCase(base, rules.Case)
	base = applyTrim(base, rules.TrimFront, rules.TrimBack)
	base, err = applyTemplate(base, ext, rules, in)
	if err != nil {
		return "", err
	}
	base = applyNumbering(base, rules, in.Index)
	if !in.IsDir || rules.ExtensionOnDirs {
		ext = applyExtension(ext, rules)
	}

	return base + ext, nil
}
