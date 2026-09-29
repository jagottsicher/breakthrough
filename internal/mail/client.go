package mail

import "os/exec"

// lookPath is a package-level swappable var — the same mockable-exec
// idiom internal/multiplex's own detect.go already uses — so a test
// can simulate "none of these are installed" (or exactly one, or all
// three) without needing a real machine with any particular mail
// client actually present.
var lookPath = exec.LookPath

// Candidates is the curated, ordered list of terminal mail clients
// this package knows how to launch — per the user's own explicit
// choice of which ones to support, modern TUI clients first (neomutt,
// aerc, himalaya), the POSIX-standard mail/mailx last as the always-
// available fallback (see Detect's own doc comment for what the order
// is used for). A fixed, hand-picked list, not "whatever looks like a
// mail command on $PATH": the same restraint internal/multiplex's own
// three backends already apply. mail and mailx are two different
// binary names for the same classic tool depending on the
// distribution (Debian/Ubuntu's mailutils package installs "mail",
// others ship "mailx" instead, occasionally both) — both are listed
// so either one is found regardless of which name the system uses.
var Candidates = []string{"neomutt", "aerc", "himalaya", "mail", "mailx"}

// Detect returns every one of Candidates actually found on $PATH,
// still in Candidates' own order — the priority order Root.openMail
// falls back to when settings.MailClient hasn't picked one explicitly,
// and the order the Options screen's own choices list them in.
func Detect() []string {
	var found []string
	for _, c := range Candidates {
		if _, err := lookPath(c); err == nil {
			found = append(found, c)
		}
	}
	return found
}

// Installed reports whether client itself is currently on $PATH —
// used to fall back gracefully when settings.MailClient names one
// that used to be installed but no longer is, rather than trying to
// exec a binary that's already known to be missing.
func Installed(client string) bool {
	_, err := lookPath(client)
	return err == nil
}

// Command returns the argv to launch client with — just the binary
// name itself, no arguments: every one of Candidates, invoked with
// nothing else, opens straight to its own default inbox/account view,
// exactly the "select something, press the key" entry point this
// integration wants. nil for an empty client name, so a caller can
// pass whatever
// settings.MailClient holds (possibly "") straight through without a
// separate emptiness check of its own.
func Command(client string) []string {
	if client == "" {
		return nil
	}
	return []string{client}
}
