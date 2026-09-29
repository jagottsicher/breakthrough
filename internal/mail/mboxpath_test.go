package mail

import (
	"os"
	"testing"
	"time"
)

// isolateMailboxDetection fakes currentUsername/statPath for
// DefaultMboxPath — files maps a path to whether it exists as a
// regular file, so a test never depends on this machine's own real
// /var/mail.
func isolateMailboxDetection(t *testing.T, username string, files map[string]bool) {
	t.Helper()
	origUser, origStat := currentUsername, statPath
	t.Cleanup(func() { currentUsername, statPath = origUser, origStat })

	currentUsername = func() (string, error) { return username, nil }
	statPath = func(path string) (os.FileInfo, error) {
		if files[path] {
			return fakeRegularFile{}, nil
		}
		return nil, os.ErrNotExist
	}
}

// fakeRegularFile is the minimal os.FileInfo isolateMailboxDetection
// needs — Mode() reporting a plain regular file (no bits set at all,
// which IsRegular() already reports true for).
type fakeRegularFile struct{ os.FileInfo }

func (fakeRegularFile) Mode() os.FileMode  { return 0o644 }
func (fakeRegularFile) ModTime() time.Time { return time.Time{} }
func (fakeRegularFile) IsDir() bool        { return false }

func TestDefaultMboxPathPrefersVarMail(t *testing.T) {
	isolateMailboxDetection(t, "jens", map[string]bool{
		"/var/mail/jens":       true,
		"/var/spool/mail/jens": true,
	})

	if got := DefaultMboxPath(); got != "/var/mail/jens" {
		t.Errorf("DefaultMboxPath() = %q, want \"/var/mail/jens\"", got)
	}
}

func TestDefaultMboxPathFallsBackToVarSpoolMail(t *testing.T) {
	isolateMailboxDetection(t, "jens", map[string]bool{
		"/var/spool/mail/jens": true,
	})

	if got := DefaultMboxPath(); got != "/var/spool/mail/jens" {
		t.Errorf("DefaultMboxPath() = %q, want \"/var/spool/mail/jens\"", got)
	}
}

func TestDefaultMboxPathEmptyWhenNeitherExists(t *testing.T) {
	isolateMailboxDetection(t, "jens", map[string]bool{})

	if got := DefaultMboxPath(); got != "" {
		t.Errorf("DefaultMboxPath() = %q, want \"\"", got)
	}
}

func TestDefaultMboxPathEmptyWhenUsernameUnavailable(t *testing.T) {
	orig := currentUsername
	t.Cleanup(func() { currentUsername = orig })
	currentUsername = func() (string, error) { return "", os.ErrNotExist }

	if got := DefaultMboxPath(); got != "" {
		t.Errorf("DefaultMboxPath() = %q, want \"\"", got)
	}
}
