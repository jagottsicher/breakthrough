package fsops

import (
	"os/exec"
	"testing"
)

// requireCommand skips t unless name is on $PATH.
func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available in this environment: %v", name, err)
	}
}

// TestFetchDiskUsageRealFilesystem runs the real df binary against a
// real, if throwaway, directory — sanity-checking shape (non-negative
// values, a 0-100 percent) rather than exact numbers, which depend on
// whatever this machine's own disk state happens to be.
func TestFetchDiskUsageRealFilesystem(t *testing.T) {
	requireCommand(t, "df")
	u, ok := FetchDiskUsage(t.TempDir())
	if !ok {
		t.Fatal("FetchDiskUsage should succeed against a real, existing directory")
	}
	if u.UsedBytes < 0 || u.AvailBytes < 0 || u.UsedInodes < 0 || u.AvailInodes < 0 {
		t.Errorf("negative usage: %+v", u)
	}
	if u.UsePercent < 0 || u.UsePercent > 100 || u.InodePercent < 0 || u.InodePercent > 100 {
		t.Errorf("percent out of [0,100]: %+v", u)
	}
}

// TestParseDfDataLine pins the field layout against real df output
// captured on this machine (GNU df, df -k and df -i) plus a simulated
// BSD-style wrapped line (Filesystem name on its own line, so the data
// line itself starts one field short) — parseDfDataLine indexes from
// the end specifically so that second case still works.
func TestParseDfDataLine(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantUsed    int64
		wantAvail   int64
		wantPercent int
	}{
		{
			name:        "GNU df -k",
			line:        "/dev/md0       480149504 454345216  1782272  97% /",
			wantUsed:    454345216,
			wantAvail:   1782272,
			wantPercent: 97,
		},
		{
			name:        "GNU df -i",
			line:        "/dev/md0        30515200 1316665 29198535    5% /",
			wantUsed:    1316665,
			wantAvail:   29198535,
			wantPercent: 5,
		},
		{
			name:        "wrapped Filesystem name (BSD-style, own line above)",
			line:        "        480149504 454345216  1782272  97% /",
			wantUsed:    454345216,
			wantAvail:   1782272,
			wantPercent: 97,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			used, avail, percent, ok := parseDfDataLine(tt.line)
			if !ok {
				t.Fatalf("parseDfDataLine(%q) ok = false, want true", tt.line)
			}
			if used != tt.wantUsed || avail != tt.wantAvail || percent != tt.wantPercent {
				t.Errorf("parseDfDataLine(%q) = (%d, %d, %d), want (%d, %d, %d)",
					tt.line, used, avail, percent, tt.wantUsed, tt.wantAvail, tt.wantPercent)
			}
		})
	}
}

func TestParseDfDataLineMalformed(t *testing.T) {
	for _, line := range []string{"", "too few fields", "not numbers at all here really"} {
		if _, _, _, ok := parseDfDataLine(line); ok {
			t.Errorf("parseDfDataLine(%q) ok = true, want false", line)
		}
	}
}
