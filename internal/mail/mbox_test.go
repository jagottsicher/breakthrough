package mail

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeMbox builds a real mbox file from raw message bodies (each
// already including its own leading "From " envelope line) and
// returns its path.
func writeMbox(t *testing.T, messages ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mbox")
	content := strings.Join(messages, "")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// newMessage builds one real mbox message: the envelope line, headers
// (status, if non-empty, appended as its own "Status:" header), a
// blank line, then a one-line body.
func newMessage(status string) string {
	var b strings.Builder
	b.WriteString("From sender@example.com Mon Jan  2 15:04:05 2006\n")
	b.WriteString("From: sender@example.com\n")
	b.WriteString("Subject: test\n")
	if status != "" {
		b.WriteString("Status: " + status + "\n")
	}
	b.WriteString("\nbody\n\n")
	return b.String()
}

// TestMboxSourceUnreadCountCountsEverythingWithoutAnRStatus pins the
// user's own real-world correction: "Status: O" (old — already seen
// by a mail session, but never individually opened) still counts as
// unread, same as no Status header at all — only "R" (read) excludes
// a message.
func TestMboxSourceUnreadCountCountsEverythingWithoutAnRStatus(t *testing.T) {
	path := writeMbox(t,
		newMessage(""),   // no Status header — unread
		newMessage("O"),  // old, never actually opened — still unread
		newMessage("RO"), // read
		newMessage("R"),  // read
		newMessage(""),   // unread again
	)
	s := NewMboxSource(path)

	got, err := s.UnreadCount()
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if got != 3 {
		t.Errorf("UnreadCount() = %d, want 3", got)
	}
}

func TestMboxSourceUnreadCountZeroWhenEveryMessageWasRead(t *testing.T) {
	path := writeMbox(t, newMessage("RO"), newMessage("R"))
	s := NewMboxSource(path)

	got, err := s.UnreadCount()
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if got != 0 {
		t.Errorf("UnreadCount() = %d, want 0", got)
	}
}

func TestMboxSourceUnreadCountForAnEmptyFile(t *testing.T) {
	path := writeMbox(t)
	s := NewMboxSource(path)

	got, err := s.UnreadCount()
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if got != 0 {
		t.Errorf("UnreadCount() = %d, want 0", got)
	}
}

func TestMboxSourceUnreadCountErrorsWhenPathDoesNotExist(t *testing.T) {
	s := NewMboxSource(filepath.Join(t.TempDir(), "does-not-exist"))

	if _, err := s.UnreadCount(); err == nil {
		t.Error("UnreadCount() = nil error, want one for a nonexistent mbox")
	}
}

func TestMboxSourceUnreadCountIsCaseInsensitiveForTheStatusHeaderName(t *testing.T) {
	path := writeMbox(t, strings.Replace(newMessage("R"), "Status:", "status:", 1))
	s := NewMboxSource(path)

	got, err := s.UnreadCount()
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if got != 0 {
		t.Errorf("UnreadCount() = %d, want 0 (lowercase \"status:\" must still count as a real header)", got)
	}
}

func TestMboxSourceNameReturnsPathVerbatim(t *testing.T) {
	s := NewMboxSource("/var/mail/jens")
	if got := s.Name(); got != "/var/mail/jens" {
		t.Errorf("Name() = %q, want the path verbatim", got)
	}
}

// TestMboxSourceUnreadCountMatchesRealWorldShape pins the exact
// proportions observed against a real, live /var/mail mailbox while
// building this feature (826 messages, 825 with "Status: O", 1 with no
// Status header at all — none carrying "R") — a regression guard
// against ever going back to the narrower "no header at all" rule an
// earlier draft of this feature used, which counted only 1 of these
// 826 as unread; the person actually reading that mailbox considered
// all 826 of them still unread.
func TestMboxSourceUnreadCountMatchesRealWorldShape(t *testing.T) {
	var messages []string
	for i := 0; i < 825; i++ {
		messages = append(messages, newMessage("O"))
	}
	messages = append(messages, newMessage(""))
	path := writeMbox(t, messages...)
	s := NewMboxSource(path)

	got, err := s.UnreadCount()
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if got != 826 {
		t.Errorf("UnreadCount() = %d, want 826", got)
	}
}
