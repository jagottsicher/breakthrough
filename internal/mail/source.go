package mail

// Source is one mailbox breakthrough can report an unread count for —
// feature_ideas.txt's own "0f. E-Mail-Client-Integration" sketch,
// abstracted just enough to let internal/ui's own status bar badge
// (bottombar.go) read a count without knowing which kind of mailbox
// it's actually looking at. MaildirSource is the only real
// implementation so far; mbox and IMAP are later, explicitly separate
// stages (see feature_ideas.txt — IMAP in particular needs its own
// credential-storage design first, not something to bolt onto this
// interface ahead of that decision).
//
// Deliberately no context.Context parameter yet, unlike the sketch in
// feature_ideas.txt: MaildirSource's own UnreadCount is a single local
// directory listing, nothing to cancel or time out. Add one once a
// real implementation (IMAP, most likely) actually needs it, rather
// than a parameter every current caller would just ignore.
type Source interface {
	// Name is this source's own identifying label — MaildirSource's
	// own path, verbatim.
	Name() string

	// UnreadCount reports how many unread messages this source
	// currently holds.
	UnreadCount() (int, error)
}
