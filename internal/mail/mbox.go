package mail

import (
	"bufio"
	"os"
	"strings"
)

// mboxScanBufferInitial/mboxScanBufferMax size bufio.Scanner's own
// buffer generously — a real header line (a long Subject/References/
// Received chain) can run well past bufio.Scanner's own 64KB default
// max, which would otherwise abort the whole scan with
// bufio.ErrTooLong partway through a real mailbox.
const (
	mboxScanBufferInitial = 64 * 1024
	mboxScanBufferMax     = 1 << 20 // 1MB
)

// MboxSource is one local mbox mailbox — the classic Unix mail spool
// format (see feature_ideas.txt's own "0f. E-Mail-Client-Integration"
// entry for the format's own shape: one flat file, messages separated
// by a "From " envelope line, no separate "new" folder the way Maildir
// has).
//
// Unlike MaildirSource, UnreadCount here means reading the whole file
// on every call — there's no cheap directory listing to lean on for
// this format. A large, long-lived mailbox (a personal /var/mail spool
// can genuinely grow into the tens of MB over months/years, verified
// directly against a real one, not assumed) makes this measurably more
// expensive than Maildir's own single syscall. No caching here yet
// (see internal/ui's own mailUnreadCount, called fresh every status-bar
// refresh, once a second) — accepted for now, the same "recompute the
// live figure every tick" cost this app's own disk-usage segment
// already pays via a real `df` subprocess; revisit if this turns out
// to matter in practice for a real, large mailbox.
type MboxSource struct {
	// Path is the mbox file itself, e.g. "/var/mail/jens".
	Path string
}

// NewMboxSource returns an MboxSource for path.
func NewMboxSource(path string) MboxSource {
	return MboxSource{Path: path}
}

// Name returns m.Path verbatim.
func (m MboxSource) Name() string {
	return m.Path
}

// UnreadCount counts messages with no "Status:" header at all — the
// classic BSD mail semantics real mail/mailx/procmail/exim already
// follow (verified directly against a real, live mailbox's own
// content, not assumed): a message with no Status header has never
// been seen by any mail reader at all ("new"); "Status: O" ("old")
// means a mail session has already listed it at least once, even if
// no one actually opened it — the overwhelming majority of a
// long-lived mailbox's own messages read this way, confirmed against
// real data — and only "Status: R" ("read") means it was actually
// opened. Only the true "never seen at all" state counts here, the
// same distinction the "N" flag in mail(1)'s own message listing
// already draws, matching what a user actually means by "new mail".
func (m MboxSource) UnreadCount() (int, error) {
	f, err := os.Open(m.Path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, mboxScanBufferInitial), mboxScanBufferMax)

	unread := 0
	inMessage := false
	inHeaders := false
	hasStatusHeader := false

	finishMessage := func() {
		if inMessage && !hasStatusHeader {
			unread++
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "From ") {
			finishMessage()
			inMessage, inHeaders, hasStatusHeader = true, true, false
			continue
		}
		if !inMessage || !inHeaders {
			continue
		}
		if line == "" {
			inHeaders = false
			continue
		}
		if len(line) >= 7 && strings.EqualFold(line[:7], "status:") {
			hasStatusHeader = true
		}
	}
	finishMessage()

	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return unread, nil
}
