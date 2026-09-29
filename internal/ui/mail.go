// mail.go is Stufe 1 of feature_ideas.txt's own "0f.
// E-Mail-Client-Integration" (chord "ge", "go email"): launches
// whichever terminal mail client is configured or detected
// (internal/mail) via Suspend + direct exec, the exact same mechanism
// Sessions' own attachSession (sessions.go) already uses — no mail
// client of its own here, no IMAP/SMTP, no PTY/ANSI terminal emulation.
// Control returns to breakthrough automatically the moment the real
// client exits, the same as Attach.
package ui

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/jagottsicher/breakthrough/internal/mail"
)

// mailDetect/mailInstalled are mail.Detect/mail.Installed, indirected
// through package-level vars the same way sessions.go's own
// listMultiplexSessions already is — so a test can simulate any
// combination of installed clients without depending on, or being
// broken by, whatever's actually on $PATH on the machine running it.
var (
	mailDetect    = mail.Detect
	mailInstalled = mail.Installed

	// mailUnreadCount is mail.NewMaildirSource(path).UnreadCount, the
	// same indirection shape as mailDetect/mailInstalled above — so a
	// test can simulate any unread count (or a read failure) for the
	// status bar's own mail badge (bottombar.go) without needing a
	// real Maildir tree on disk.
	mailUnreadCount = func(path string) (int, error) {
		return mail.NewMaildirSource(path).UnreadCount()
	}

	// mailMboxUnreadCount is mailUnreadCount's own mbox counterpart —
	// mail.NewMboxSource(path).UnreadCount, indirected the same way.
	mailMboxUnreadCount = func(path string) (int, error) {
		return mail.NewMboxSource(path).UnreadCount()
	}

	// mailDefaultMboxPath is mail.DefaultMboxPath, indirected the same
	// way — so a test can simulate a real (or absent) system mailbox
	// without depending on, or being broken by, whatever's actually at
	// /var/mail on the machine running it.
	mailDefaultMboxPath = mail.DefaultMboxPath
)

// mailBadgeCount is the status bar's own mail badge (bottombar.go)
// resolving which mailbox to read and reporting its unread count in
// one step: an explicit Maildir path (settings.MailMaildirPath) wins
// outright if set, then an explicit mbox override
// (settings.MailMboxPath), then the auto-detected system mailbox
// (mailDefaultMboxPath — see MailMboxPath's own doc comment on why
// this one case is auto-detected at all, unlike Maildir's own several
// genuinely ambiguous candidates). ok is false whenever none of those
// apply, or the one that does fails to read — the badge simply isn't
// shown either way, never an error overlay.
func (r *Root) mailBadgeCount() (count int, ok bool) {
	if r.settings.MailMaildirPath != "" {
		n, err := mailUnreadCount(r.settings.MailMaildirPath)
		return n, err == nil
	}
	path := r.settings.MailMboxPath
	if path == "" {
		path = mailDefaultMboxPath()
	}
	if path == "" {
		return 0, false
	}
	n, err := mailMboxUnreadCount(path)
	return n, err == nil
}

// resolveMailClient picks which client openMail actually launches:
// settings.MailClient if it names one that's still installed — a
// config naming a client since uninstalled degrades to auto-detection
// rather than trying to exec a binary already known to be missing —
// otherwise the first of mailDetect's own priority-ordered results. ""
// if nothing in mail.Candidates is installed at all.
func (r *Root) resolveMailClient() string {
	if r.settings.MailClient != "" && mailInstalled(r.settings.MailClient) {
		return r.settings.MailClient
	}
	if found := mailDetect(); len(found) > 0 {
		return found[0]
	}
	return ""
}

// openMail is the "ge" chord's own action. A clear, dismissible notice
// — never a crash or silent no-op — when nothing is installed at all,
// the same "detect the real binary, degrade clearly when it's missing"
// pattern screen/tmux/Zellij detection already establishes for
// Sessions.
func (r *Root) openMail() {
	client := r.resolveMailClient()
	if client == "" {
		r.showTransientError(fmt.Errorf("no mail client found (looked for %v)", mail.Candidates))
		return
	}
	argv := mail.Command(client)
	if len(argv) == 0 {
		return
	}

	var runErr error
	r.app.Suspend(func() {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		runErr = cmd.Run()
	})
	if runErr != nil {
		r.showError(fmt.Errorf("%s: %w", client, runErr))
	}
}
