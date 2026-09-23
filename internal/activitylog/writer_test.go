package activitylog

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateResolvePath points SystemLogDir at a fresh subdirectory of a
// temp dir instead of the real /var/log/breakthrough, and isolates
// $XDG_STATE_HOME so session.StateDir() resolves to a temp dir too —
// neither ResolvePath test below should ever touch anything on the
// real filesystem outside t.TempDir().
func isolateResolvePath(t *testing.T) (systemDir, userStateDir string) {
	t.Helper()
	orig := SystemLogDir
	t.Cleanup(func() { SystemLogDir = orig })

	systemDir = filepath.Join(t.TempDir(), "system-log")
	SystemLogDir = systemDir

	userStateDir = t.TempDir()
	t.Setenv("XDG_STATE_HOME", userStateDir)
	return systemDir, userStateDir
}

func TestResolvePathPrefersTheSystemLogDirWhenWritable(t *testing.T) {
	systemDir, _ := isolateResolvePath(t)

	path, fellBack, err := ResolvePath()

	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if fellBack {
		t.Error("fellBack = true, want false: the system log dir is writable")
	}
	if want := filepath.Join(systemDir, logFileName); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestResolvePathFallsBackWhenTheSystemLogDirIsNotWritable(t *testing.T) {
	_, userStateDir := isolateResolvePath(t)
	// A file, not a directory, at the system log dir's own path: MkdirAll
	// fails outright on it, the simplest reliable way to force "not
	// writable" without relying on real permission bits (see
	// tryLogDir's own doc comment on why bits alone aren't checked
	// either).
	if err := os.WriteFile(SystemLogDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, fellBack, err := ResolvePath()

	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if !fellBack {
		t.Error("fellBack = false, want true: the system log dir could not be used")
	}
	if want := filepath.Join(userStateDir, "breakthrough", logFileName); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestResolvePathFailsWhenNeitherLocationIsAvailable(t *testing.T) {
	isolateResolvePath(t)
	if err := os.WriteFile(SystemLogDir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")

	if _, _, err := ResolvePath(); err == nil {
		t.Error("ResolvePath should fail once neither location is available")
	}
}

func TestOpenWriterAndWriteEntryAppendsARealLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "breakthrough.log")
	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	entry := Entry{Time: timeNow(), Level: LevelActions, Category: CategoryFileOps, Message: "test message"}
	if err := w.WriteEntry(entry); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != entry.Format() {
		t.Errorf("file content = %q, want %q", got, entry.Format())
	}
}

func TestOpenWriterAppendsRatherThanTruncates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "breakthrough.log")
	if err := os.WriteFile(path, []byte("existing line\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	w, err := OpenWriter(path)
	if err != nil {
		t.Fatalf("OpenWriter: %v", err)
	}
	if err := w.WriteEntry(Entry{Time: timeNow(), Level: LevelErrors, Category: CategoryRsync, Message: "new line"}); err != nil {
		t.Fatalf("WriteEntry: %v", err)
	}
	_ = w.Close()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(got); got[:len("existing line\n")] != "existing line\n" {
		t.Errorf("file content = %q, want the existing line preserved at the start", got)
	}
}

func TestWriterCloseIsNilSafe(t *testing.T) {
	var w *Writer
	if err := w.Close(); err != nil {
		t.Errorf("(*Writer)(nil).Close() = %v, want nil", err)
	}
}
