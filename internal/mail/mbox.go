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

// UnreadCount counts every message whose "Status:" header — if it has
// one at all — doesn't contain "R": real mail/mailx/procmail/exim all
// write "Status: O" ("old") the moment any mail session has so much as
// listed a message, whether or not anyone actually opened it, and only
// ever add "R" ("read") once it was — verified directly against a
// real, live mailbox's own content, not assumed: 825 of 826 real
// messages there carried "Status: O" with no "R", and the person
// reading that mailbox considered every one of those 825 still unread.
// A message with no Status header at all (truly never seen by
// anything) counts the same way, for the same reason. An earlier draft
// of this method counted only that last, narrower case ("no header at
// all") — technically closer to the classic BSD "N" (new) flag as
// mail(1) itself defines it, but not what "unread mail" actually means
// to a real reader of a real mailbox; corrected after checking against
// the real data instead of the terminology alone.
func (m MboxSource) UnreadCount() (int, error) {
	f, err := os.Open(m.Path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }() // read-only; nothing to do if this fails

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, mboxScanBufferInitial), mboxScanBufferMax)

	unread := 0
	inMessage := false
	inHeaders := false
	hasReadStatus := false

	finishMessage := func() {
		if inMessage && !hasReadStatus {
			unread++
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "From ") {
			finishMessage()
			inMessage, inHeaders, hasReadStatus = true, true, false
			continue
		}
		if !inMessage || !inHeaders {
			continue
		}
		if line == "" {
			inHeaders = false
			continue
		}
		if len(line) >= 7 && strings.EqualFold(line[:7], "status:") && strings.Contains(line[7:], "R") {
			hasReadStatus = true
		}
	}
	finishMessage()

	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return unread, nil
}
