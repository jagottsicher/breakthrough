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
)

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
