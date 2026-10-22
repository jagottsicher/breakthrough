package mail

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// netrcCredentials is one "machine" entry's login/password pair, as
// read from ~/.netrc (or $NETRC, see netrcPath) — the same file
// curl/ftp/wget already read for exactly this purpose. breakthrough
// only ever reads it, never writes to it or caches its own copy on
// disk: per the user's own explicit decision (see feature_ideas.txt's
// own "0f. E-Mail-Client-Integration" — IMAP's credential-storage
// question), this keeps the project's existing "no secret of its own,
// ever" invariant (SSH uses the agent/real keys, cloud mounts leave
// rclone to own its config) intact for IMAP too, with no new storage
// mechanism of breakthrough's own to get wrong.
type netrcCredentials struct {
	Login    string
	Password string
}

// netrcPath is ~/.netrc's own location — $NETRC overrides it first,
// the same override curl itself honors, useful for pointing at a
// non-default file (or a test fixture) without touching the real one.
func netrcPath() (string, error) {
	if p := os.Getenv("NETRC"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".netrc"), nil
}

// lookupNetrc reads ~/.netrc (see netrcPath) and returns the
// credentials for machine — an exact, case-sensitive match against
// each entry's own "machine" token, the same matching rule curl's own
// netrc parser uses, falling back to a "default" entry (one with no
// "machine" token of its own, meant to apply to anything not matched
// above it) if the file has one. Returns an error naming the file
// outright — "no entry for this host" or "file doesn't exist" are both
// surfaced the same way the mbox/Maildir Sources already surface a
// missing path, left for the caller (see IMAPSource.UnreadCount) to
// decide how that renders (no error overlay, same badge-just-isn't-
// shown convention every other Source failure already has — see
// mailBadgeCount's own doc comment).
func lookupNetrc(machine string) (netrcCredentials, error) {
	path, err := netrcPath()
	if err != nil {
		return netrcCredentials{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return netrcCredentials{}, err
	}
	defer func() { _ = f.Close() }() // read-only; nothing to do if this fails

	tokens, err := tokenizeNetrc(f)
	if err != nil {
		return netrcCredentials{}, err
	}

	var def *netrcCredentials
	i := 0
	for i < len(tokens) {
		switch tokens[i] {
		case "machine":
			if i+1 >= len(tokens) {
				return netrcCredentials{}, fmt.Errorf("netrc: %s: \"machine\" with no value", path)
			}
			name := tokens[i+1]
			creds, consumed := parseNetrcEntry(tokens[i+2:])
			if name == machine {
				return creds, nil
			}
			i += 2 + consumed
		case "default":
			creds, consumed := parseNetrcEntry(tokens[i+1:])
			def = &creds
			i += 1 + consumed
		case "macdef":
			// Not a credential entry at all — a named shell macro,
			// whose body is everything up to the next blank line,
			// never parsed as machine/login/password tokens. Already
			// excluded from tokens by tokenizeNetrc itself (see its
			// own doc comment), so finding "macdef" here at all means
			// the name token right after it is the only thing left to
			// skip.
			i += 2
		default:
			// Stray token outside any entry — skip it rather than
			// erroring: a real-world .netrc occasionally has comments
			// or tokens this minimal parser doesn't know about, and
			// refusing to find an otherwise-present "machine" entry
			// over that would be worse than ignoring it.
			i++
		}
	}
	if def != nil {
		return *def, nil
	}
	return netrcCredentials{}, fmt.Errorf("netrc: %s: no entry for %q", path, machine)
}

// parseNetrcEntry reads login/password/account tokens following a
// "machine"/"default" token until the next "machine"/"default"/
// "macdef" keyword (or the end of the token stream), returning the
// credentials found and how many tokens it consumed — account is
// read (to skip its own value) but never stored, since IMAP login
// here only ever needs login+password.
func parseNetrcEntry(tokens []string) (netrcCredentials, int) {
	var creds netrcCredentials
	i := 0
	for i < len(tokens) {
		switch tokens[i] {
		case "machine", "default", "macdef":
			return creds, i
		case "login":
			if i+1 < len(tokens) {
				creds.Login = tokens[i+1]
			}
			i += 2
		case "password":
			if i+1 < len(tokens) {
				creds.Password = tokens[i+1]
			}
			i += 2
		case "account":
			i += 2
		default:
			i++
		}
	}
	return creds, i
}

// tokenizeNetrc splits r's content on whitespace, the same simple
// shell-word-ish lexing a real .netrc expects (no quoting support —
// neither does curl's own parser for this file), while skipping every
// "macdef <name>" block's own body (everything up to the next blank
// line) wholesale: that body is shell script text for a long-obsolete
// FTP auto-login feature, never credential tokens, and could easily
// contain words ("machine", "login", ...) that would otherwise be
// misread as real entries.
func tokenizeNetrc(r *os.File) ([]string, error) {
	var tokens []string
	scanner := bufio.NewScanner(r)
	inMacdef := false
	for scanner.Scan() {
		line := scanner.Text()
		if inMacdef {
			if strings.TrimSpace(line) == "" {
				inMacdef = false
			}
			continue
		}
		fields := strings.Fields(line)
		for i, tok := range fields {
			tokens = append(tokens, tok)
			if tok == "macdef" {
				inMacdef = true
				// The macro's own name is the very next field on this
				// same line — still a real token (parseNetrcEntry's
				// own "macdef" case in lookupNetrc skips exactly one
				// token past it), not part of the body being skipped.
				if i+1 < len(fields) {
					tokens = append(tokens, fields[i+1])
				}
				break
			}
		}
	}
	return tokens, scanner.Err()
}
