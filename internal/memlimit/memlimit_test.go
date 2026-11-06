package memlimit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTotalMemBytesParsesMemTotalLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meminfo")
	content := "MemFree:         123456 kB\nMemTotal:       16384000 kB\nSwapTotal:             0 kB\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := totalMemBytes(path)
	if !ok {
		t.Fatal("totalMemBytes reported not-ok for a well-formed file")
	}
	want := int64(16384000 * 1024)
	if got != want {
		t.Errorf("totalMemBytes() = %d, want %d", got, want)
	}
}

func TestTotalMemBytesMissingFileReportsNotOk(t *testing.T) {
	if _, ok := totalMemBytes(filepath.Join(t.TempDir(), "does-not-exist")); ok {
		t.Error("totalMemBytes reported ok for a nonexistent file — the macOS/BSD no-op path depends on this")
	}
}

func TestTotalMemBytesMissingMemTotalLineReportsNotOk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meminfo")
	if err := os.WriteFile(path, []byte("MemFree: 123 kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := totalMemBytes(path); ok {
		t.Error("totalMemBytes reported ok for a file with no MemTotal line")
	}
}

func TestTotalMemBytesUnparsableValueReportsNotOk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meminfo")
	if err := os.WriteFile(path, []byte("MemTotal:       not-a-number kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := totalMemBytes(path); ok {
		t.Error("totalMemBytes reported ok for an unparsable MemTotal value")
	}
}

func TestApplyUsesEightyPercentOfRealMemTotalOnLinux(t *testing.T) {
	total, ok := totalMemBytes("/proc/meminfo")
	if !ok {
		t.Skip("no /proc/meminfo on this platform — Apply's own no-op path, covered by TestApplyIsANoOpWhenMeminfoIsUnavailable instead")
	}

	limit, applied := Apply()
	if !applied {
		t.Fatal("Apply reported not-applied despite a readable /proc/meminfo")
	}
	want := int64(float64(total) * fraction)
	if limit != want {
		t.Errorf("Apply() limit = %d, want %d (80%% of %d)", limit, want, total)
	}
}
