//go:build darwin

package fsops

import "testing"

// TestListUsersFailsOnDarwin/TestListGroupsFailsOnDarwin pin the
// documented contract: both always fail on macOS, rather than silently
// returning an incomplete list from /etc/passwd's legacy-accounts-only
// content.
func TestListUsersFailsOnDarwin(t *testing.T) {
	if _, err := ListUsers(); err == nil {
		t.Error("ListUsers should fail on macOS, got nil error")
	}
}

func TestListGroupsFailsOnDarwin(t *testing.T) {
	if _, err := ListGroups(); err == nil {
		t.Error("ListGroups should fail on macOS, got nil error")
	}
}
