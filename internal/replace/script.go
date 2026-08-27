package replace

import (
	"fmt"
	"strings"
)

// delimiterCandidates are tried in order for BuildScript's s<delim>...
// command — chosen to be characters unlikely to appear in an everyday
// find/replace pair, but any occurrence in either string is still
// detected and skipped rather than assumed absent.
const delimiterCandidates = "/#|~^@%"

// pickDelimiter returns the first candidate present in neither find nor
// replace, so it can safely delimit a sed s/// command built from them
// (sed lets s use any character as its delimiter, not just "/" — this is
// what makes a find/replace pair containing literal slashes, e.g. a
// path, still work without the caller needing to escape every one).
func pickDelimiter(find, replace string) (byte, error) {
	for i := 0; i < len(delimiterCandidates); i++ {
		d := delimiterCandidates[i]
		if !strings.ContainsRune(find, rune(d)) && !strings.ContainsRune(replace, rune(d)) {
			return d, nil
		}
	}
	return 0, fmt.Errorf("replace: no delimiter candidate (%s) is absent from both Find and Replace", delimiterCandidates)
}

// escapeSedPattern escapes s so it matches literally inside a sed
// pattern, honoring BRE vs ERE's different metacharacter sets: BRE's
// (sed's default, no -E) only special characters are . * [ ] ^ $ — ( )
// { } + ? | are already literal there unless backslash-escaped the
// *other* way, so escaping them here would change what they match, not
// keep it literal (verified against sed's own regex(7), not assumed to
// match Go's regexp.QuoteMeta). ERE (-E) uses the more familiar "these
// are all special" set that QuoteMeta itself targets.
func escapeSedPattern(s string, delim byte, extendedRegex bool) string {
	special := ".*[]^$\\"
	if extendedRegex {
		special = ".*[]^$\\(){}+?|"
	}
	special += string(delim)

	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeSedReplacement escapes s so it's inserted literally into a sed
// replacement: & (whole match), \ (backreference/escape prefix), and the
// chosen delimiter all need escaping there; nothing else does.
func escapeSedReplacement(s string, delim byte) string {
	special := "&\\" + string(delim)

	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// BuildScript assembles a sed s/// substitution from a plain Find/
// Replace pair — the guided mode's own script builder (see
// internal/ui's Sed Replace dialog). The advanced/raw mode bypasses this
// entirely and uses the user's own sed script unchanged.
//
// useRegex=false treats find and replace as literal text, escaped for
// sed's own basic (BRE) or extended (ERE, extendedRegex=true) regex
// dialect — see escapeSedPattern/escapeSedReplacement. useRegex=true
// passes both through unescaped, so the user's own regex/backreferences
// (\1, &, character classes, ...) work directly, the same trade-off any
// regex-aware find/replace UI makes: you gain regex power and lose
// literal-by-default safety on the same field.
//
// caseInsensitive appends GNU sed's own s///I modifier — verified
// present in GNU sed; not confirmed against BSD/macOS sed (see the
// package doc's portability note).
func BuildScript(find, replace string, useRegex, extendedRegex, caseInsensitive, global bool) (string, error) {
	delim, err := pickDelimiter(find, replace)
	if err != nil {
		return "", err
	}

	if !useRegex {
		find = escapeSedPattern(find, delim, extendedRegex)
		replace = escapeSedReplacement(replace, delim)
	}

	flags := ""
	if global {
		flags += "g"
	}
	if caseInsensitive {
		flags += "I"
	}

	return fmt.Sprintf("s%c%s%c%s%c%s", delim, find, delim, replace, delim, flags), nil
}
