package mail

import (
	"os"
	"path/filepath"
	"strings"
)

// MaildirSource is one local Maildir mailbox — the first, simplest
// Source implementation (see source.go's own doc comment): the
// Maildir specification itself already guarantees "new/" holds
// exactly the unseen messages and nothing else, so counting unread is
// a single directory listing, no message parsing, no locking (Maildir
// was explicitly designed to need none), no credentials.
type MaildirSource struct {
	// Path is the Maildir's own root — the directory containing
	// "new"/"cur"/"tmp", e.g. "~/Mail/INBOX" (already expanded, never
	// a literal "~").
	Path string
}

// NewMaildirSource returns a MaildirSource for path.
func NewMaildirSource(path string) MaildirSource {
	return MaildirSource{Path: path}
}

// Name returns m.Path verbatim.
func (m MaildirSource) Name() string {
	return m.Path
}

// UnreadCount lists m.Path's own "new" subdirectory and counts real
// entries — a hidden dotfile (a stray ".lock" or similar some tools
// leave behind, though Maildir itself needs none) is skipped rather
// than counted as a message, the same defensiveness a real MUA's own
// Maildir reader would apply, even though the specification itself
// promises "new" holds nothing else.
func (m MaildirSource) UnreadCount() (int, error) {
	entries, err := os.ReadDir(filepath.Join(m.Path, "new"))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		n++
	}
	return n, nil
}
