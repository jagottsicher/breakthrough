package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIDStableWithinProcess(t *testing.T) {
	first := ID()
	second := ID()
	if first != second {
		t.Fatalf("ID() changed within the same process: %q vs %q", first, second)
	}
	if first == "" {
		t.Fatal("ID() returned an empty string")
	}
}

func TestTrashDirSessionUsesRuntimeDirAndID(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/xdgruntime-test")

	dir, err := TrashDir(false)
	if err != nil {
		t.Fatalf("TrashDir(false): %v", err)
	}
	if !strings.HasPrefix(dir, "/tmp/xdgruntime-test/breakthrough/trash/") {
		t.Fatalf("session trash dir = %q, want prefix under XDG_RUNTIME_DIR", dir)
	}
	if !strings.HasSuffix(dir, ID()) {
		t.Fatalf("session trash dir = %q, want suffix = session ID %q", dir, ID())
	}
}

func TestTrashDirSessionFallsBackToTempDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")

	dir, err := TrashDir(false)
	if err != nil {
		t.Fatalf("TrashDir(false): %v", err)
	}
	wantPrefix := filepath.Clean(os.TempDir())
	if !strings.HasPrefix(dir, wantPrefix) {
		t.Fatalf("session trash dir = %q, want prefix under os.TempDir() %q", dir, wantPrefix)
	}
}

func TestTrashDirRootAlwaysUsesPersistentPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/xdgdata-test")
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/xdgruntime-test")

	old := isRoot
	isRoot = func() bool { return true }
	defer func() { isRoot = old }()

	// persistent=false is what the config's default (trash_persistent)
	// would pass - root must still get the persistent path regardless.
	dir, err := TrashDir(false)
	if err != nil {
		t.Fatalf("TrashDir(false) as root: %v", err)
	}
	want := filepath.Join("/tmp/xdgdata-test", "breakthrough", "trash", username())
	if dir != want {
		t.Fatalf("root trash dir = %q, want %q (persistent, ignoring persistent=false)", dir, want)
	}
	if strings.Contains(dir, ID()) {
		t.Fatalf("root trash dir = %q should not contain a session ID", dir)
	}
}

func TestTrashDirPersistentUsesDataHomeNoSessionID(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/tmp/xdgdata-test")

	dir, err := TrashDir(true)
	if err != nil {
		t.Fatalf("TrashDir(true): %v", err)
	}
	want := filepath.Join("/tmp/xdgdata-test", "breakthrough", "trash", username())
	if dir != want {
		t.Fatalf("persistent trash dir = %q, want %q", dir, want)
	}
	if strings.Contains(dir, ID()) {
		t.Fatalf("persistent trash dir = %q should not contain the session ID", dir)
	}
}
