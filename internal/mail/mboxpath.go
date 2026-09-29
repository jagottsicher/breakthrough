package mail

import (
	"os"
	"os/user"
	"path/filepath"
)

// systemMailboxDirs are the two POSIX-conventional locations a local
// MTA delivers a user's own system mailbox to — "/var/spool/mail" is
// the traditional, "/var/mail" the newer name, commonly the same
// directory via a symlink (verified directly against a real system:
// /var/spool/mail -> ../mail), but not guaranteed to be on every
// distribution, so both are checked. Unlike Maildir's own several
// genuinely ambiguous candidate locations (~/Mail, ~/.maildir, ...),
// this is a single, well-defined system convention per user, not a
// guess among many — the same "real, unambiguous detection" class as
// finding a binary on $PATH, just for a file instead.
var systemMailboxDirs = []string{"/var/mail", "/var/spool/mail"}

// currentUsername is a package-level swappable var — the same
// mockable idiom this package's own lookPath (client.go) already
// uses — so a test can pin a username without depending on who's
// actually running it.
var currentUsername = func() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

// statPath is os.Stat, indirected the same way currentUsername is, so
// a test can simulate a mailbox existing (or not) without a real file
// on disk.
var statPath = os.Stat

// DefaultMboxPath returns the current user's own system mailbox —
// the first of systemMailboxDirs/<username> that exists as a regular
// file — or "" if neither does, or the current user can't be
// determined at all. Never returns a directory or anything else
// unusual sitting at that path: a real mailbox is always a plain
// file, and something else there (a stray directory, e.g.) is not
// this user's mailbox, not something to guess about.
func DefaultMboxPath() string {
	username, err := currentUsername()
	if err != nil || username == "" {
		return ""
	}
	for _, dir := range systemMailboxDirs {
		path := filepath.Join(dir, username)
		info, err := statPath(path)
		if err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}
