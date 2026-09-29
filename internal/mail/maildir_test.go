package mail

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func newTestMaildir(t *testing.T, unreadFiles, readFiles, hiddenFiles int) string {
	t.Helper()
	root := t.TempDir()
	newDir := filepath.Join(root, "new")
	curDir := filepath.Join(root, "cur")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(curDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < unreadFiles; i++ {
		write(t, filepath.Join(newDir, "msg"+strconv.Itoa(i)))
	}
	for i := 0; i < hiddenFiles; i++ {
		write(t, filepath.Join(newDir, ".hidden"+strconv.Itoa(i)))
	}
	for i := 0; i < readFiles; i++ {
		write(t, filepath.Join(curDir, "msg"+strconv.Itoa(i)+":2,S"))
	}
	return root
}

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMaildirSourceUnreadCountCountsOnlyNew(t *testing.T) {
	root := newTestMaildir(t, 3, 5, 0)
	s := NewMaildirSource(root)

	got, err := s.UnreadCount()
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if got != 3 {
		t.Errorf("UnreadCount() = %d, want 3 (cur/ own read messages must not count)", got)
	}
}

func TestMaildirSourceUnreadCountSkipsHiddenEntries(t *testing.T) {
	root := newTestMaildir(t, 2, 0, 3)
	s := NewMaildirSource(root)

	got, err := s.UnreadCount()
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if got != 2 {
		t.Errorf("UnreadCount() = %d, want 2 (hidden entries must not count)", got)
	}
}

func TestMaildirSourceUnreadCountZeroForAnEmptyInbox(t *testing.T) {
	root := newTestMaildir(t, 0, 0, 0)
	s := NewMaildirSource(root)

	got, err := s.UnreadCount()
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if got != 0 {
		t.Errorf("UnreadCount() = %d, want 0", got)
	}
}

func TestMaildirSourceUnreadCountErrorsWhenPathDoesNotExist(t *testing.T) {
	s := NewMaildirSource(filepath.Join(t.TempDir(), "does-not-exist"))

	if _, err := s.UnreadCount(); err == nil {
		t.Error("UnreadCount() = nil error, want one for a nonexistent Maildir")
	}
}

func TestMaildirSourceNameReturnsPathVerbatim(t *testing.T) {
	s := NewMaildirSource("/home/user/Mail/INBOX")
	if got := s.Name(); got != "/home/user/Mail/INBOX" {
		t.Errorf("Name() = %q, want the path verbatim", got)
	}
}
