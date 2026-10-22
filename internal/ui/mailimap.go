// mailimap.go is the "0f. E-Mail-Client-Integration" IMAP stage
// (feature_ideas.txt) — the mail badge's own third Source
// (internal/mail.IMAPSource), alongside Maildir and mbox (mail.go).
// Unlike those two, this one is a real network round-trip, so it's
// polled on its own background interval (StartMailImapPoll) rather
// than on the once-a-second status-bar tick every other mail-badge
// Source shares — see its own doc comment for why.
package ui

import (
	"time"

	"github.com/jagottsicher/breakthrough/internal/mail"
)

// mailImapPollInterval is how often StartMailImapPoll refreshes the
// cached IMAP unread count. Long enough that a real network round-trip
// (dial, login, STATUS) every tick — the cadence Maildir/mbox share,
// cheap local reads either way — would be unreasonable; short enough
// that the badge still reads as current in practice. Not
// user-configurable yet — nothing in feature_ideas.txt's own plan
// asked for that, and there's no real need for it until someone
// actually wants a different cadence.
const mailImapPollInterval = 2 * time.Minute

// mailImapSourceUnreadCount is mail.NewIMAPSource(cfg).UnreadCount,
// indirected through a package-level var the same way
// mailUnreadCount/mailMboxUnreadCount already are (see mail.go) — a
// test swaps this to simulate any count, or a failure, for whatever
// exercises the polling/caching behavior here without needing a real
// IMAP server.
var mailImapSourceUnreadCount = func(cfg mail.IMAPConfig) (int, error) {
	return mail.NewIMAPSource(cfg).UnreadCount()
}

// mailImapConfig builds a mail.IMAPConfig from settings — the one
// place this file reads them, so StartMailImapPoll/mailBadgeCount
// don't each have to know the field names individually.
func (r *Root) mailImapConfig() mail.IMAPConfig {
	return mail.IMAPConfig{
		Host:     r.settings.MailImapHost,
		Port:     r.settings.MailImapPort,
		Mailbox:  r.settings.MailImapMailbox,
		StartTLS: r.settings.MailImapStartTLS,
	}
}

// fetchMailImapCount runs mailImapSourceUnreadCount once against the
// currently configured server — pure in the sense that it touches no
// Root field, the one piece of StartMailImapPoll's own background
// poll that's safe to call directly, with no goroutine, ticker, or
// Application.QueueUpdateDraw involved at all (that would block
// forever without a running event loop to drain it — see
// StartMailImapPoll's own doc comment), specifically so a test can
// exercise it that way.
func (r *Root) fetchMailImapCount() (count int, ok bool) {
	n, err := mailImapSourceUnreadCount(r.mailImapConfig())
	return n, err == nil
}

// StartMailImapPoll begins refreshing mailImapCount/mailImapOK in the
// background on mailImapPollInterval, via a background goroutine and
// Application.QueueUpdateDraw — the exact shape StartClock's own doc
// comment already establishes for the identical "not started
// automatically by NewRoot" reason (many tests construct a Root
// directly without ever running the event loop QueueUpdateDraw needs
// to drain). cmd/breakthrough calls this once, right before Run, same
// as StartClock.
//
// The network round-trip itself (mailImapSourceUnreadCount, dial +
// login + STATUS) runs directly on this background goroutine, never
// inside QueueUpdateDraw's own callback — only the finished result is
// handed back that way. A slow or unreachable IMAP server then delays
// this goroutine's own next check, never the UI, the same split
// search.go's own streamSearchResults already keeps between running a
// real search and posting its results.
//
// A no-op (an already-true stop func, no ticker ever started) while
// settings.MailImapHost is empty: nothing to poll, same "the badge
// simply isn't shown" degradation every other mail-badge Source
// already has, and no reason to spin up a goroutine that would only
// ever find nothing to do.
//
// Wrapped in safeGo (see its own doc comment) like every other
// background goroutine in this app: a panic in this one specifically
// would just stop the IMAP badge from ever updating again (no
// "in progress" state of its own to reset — onPanic is nil), not the
// whole process.
func (r *Root) StartMailImapPoll() (stop func()) {
	if r.settings.MailImapHost == "" {
		return func() {}
	}

	ticker := time.NewTicker(mailImapPollInterval)
	done := make(chan struct{})
	poll := func() {
		n, ok := r.fetchMailImapCount()
		r.app.QueueUpdateDraw(func() {
			r.mailImapCount, r.mailImapOK = n, ok
			r.refreshStatusBar()
		})
	}
	r.safeGo("mail imap poll", nil, func() {
		poll() // the first count arrives right away, not after the first full interval
		for {
			select {
			case <-ticker.C:
				poll()
			case <-done:
				ticker.Stop()
				return
			}
		}
	})
	return func() { close(done) }
}
