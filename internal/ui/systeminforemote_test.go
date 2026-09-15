package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/fsops"
	"github.com/jagottsicher/breakthrough/internal/remotefs"
)

func TestParseProcVersionKernelExtractsTheReleaseToken(t *testing.T) {
	data := "Linux version 6.1.0-13-amd64 (debian-kernel@lists.debian.org) (gcc-12) #1 SMP Debian 6.1.55-1\n"
	if got, want := parseProcVersionKernel(data), "6.1.0-13-amd64"; got != want {
		t.Errorf("parseProcVersionKernel(%q) = %q, want %q", data, got, want)
	}
}

func TestParseProcVersionKernelOnUnrecognizedContentReturnsEmpty(t *testing.T) {
	if got := parseProcVersionKernel("not a real /proc/version line"); got != "" {
		t.Errorf("parseProcVersionKernel = %q, want empty", got)
	}
}

// remoteSystemFile is a small helper: newTestRemoteRoot's own fake
// client only ever gets a "/remote" tree populated by default (see its
// own doc comment) — System Info's remote sources all live at
// unrelated absolute paths like /proc/..., so every test here adds
// them itself via the fake's own content map, exactly the way a real
// SFTP server would serve them regardless of where the connection
// happens to be browsing right now.
func remoteSystemFile(client *fakeRemoteClient, path, content string) {
	if client.content == nil {
		client.content = map[string][]byte{}
	}
	client.content[path] = []byte(content)
}

func TestRemoteSystemInfoIdentityLinesReadsHostnameOSReleaseKernelAndCPU(t *testing.T) {
	client := &fakeRemoteClient{}
	remoteSystemFile(client, "/proc/sys/kernel/hostname", "faraway-host\n")
	remoteSystemFile(client, "/etc/os-release", `PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"`+"\n")
	remoteSystemFile(client, "/proc/version", "Linux version 6.1.0-13-amd64 (debian-kernel@lists.debian.org) (gcc-12) #1 SMP\n")
	remoteSystemFile(client, "/proc/cpuinfo", "processor\t: 0\nmodel name\t: AMD EPYC 7302P\nprocessor\t: 1\nmodel name\t: AMD EPYC 7302P\n")

	lines := remoteSystemInfoIdentityLines(client)
	text := strings.Join(lines, "\n")

	for _, want := range []string{"faraway-host", "Debian GNU/Linux 12 (bookworm)", "6.1.0-13-amd64", "AMD EPYC 7302P (2 cores)"} {
		if !strings.Contains(text, want) {
			t.Errorf("remoteSystemInfoIdentityLines = %q, want it to contain %q", text, want)
		}
	}
}

func TestRemoteSystemInfoIdentityLinesOmitsWhateverSourceIsMissing(t *testing.T) {
	client := &fakeRemoteClient{} // nothing populated at all
	lines := remoteSystemInfoIdentityLines(client)
	if len(lines) != 0 {
		t.Errorf("lines = %v, want none when every remote source is missing", lines)
	}
}

func TestRemoteSystemInfoHealthLinesReadsUptimeLoadMemorySwapAndDisk(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	client := fakeConnectedClient()
	if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	remoteSystemFile(client, "/proc/uptime", "12345.67 54321.00\n")
	remoteSystemFile(client, "/proc/loadavg", "1.00 0.50 0.25 1/200 12345\n")
	remoteSystemFile(client, "/proc/meminfo", "MemTotal:       1000 kB\nMemAvailable:    400 kB\nSwapTotal:       500 kB\nSwapFree:        500 kB\n")
	client.diskUsage = fsops.DiskUsage{UsedBytes: 100, AvailBytes: 900, UsePercent: 10}

	lines := r.remoteSystemInfoHealthLines(client)
	text := strings.Join(lines, "\n")

	for _, want := range []string{"Uptime", "Load", "1.00", "Memory", "Swap"} {
		if !strings.Contains(text, want) {
			t.Errorf("remoteSystemInfoHealthLines = %q, want it to contain %q", text, want)
		}
	}
}

// TestSystemInfoTextOnARemoteRootUsesTheRemoteHostnameNotThisMachines
// pins the actual, previously-real bug: before systemInfoText learned
// to dispatch on r.panel.remote, browsing a remote connection's own
// "/" showed *this* machine's hostname/OS/kernel/etc., mislabeled as
// if it described the remote one — actively wrong information, not
// merely a missing feature.
func TestSystemInfoTextOnARemoteRootUsesTheRemoteHostnameNotThisMachines(t *testing.T) {
	r := newTestRootForConnectionMenu(t)
	client := fakeConnectedClient()
	if err := r.panel.connectRemote(client, remotefs.Connection{Host: "example.com"}); err != nil {
		t.Fatalf("connectRemote: %v", err)
	}
	remoteSystemFile(client, "/proc/sys/kernel/hostname", "totally-different-remote-hostname\n")
	r.detailsTarget = "/" // showingSystemInfo compares against this literally, regardless of local/remote

	if !r.showingSystemInfo() {
		t.Fatal("showingSystemInfo() = false, want true once detailsTarget is \"/\"")
	}

	got := r.systemInfoText()

	if !strings.Contains(got, "totally-different-remote-hostname") {
		t.Errorf("systemInfoText() = %q, want it to contain the remote client's own hostname", got)
	}
	if localHostname, err := os.Hostname(); err == nil && localHostname != "" && strings.Contains(got, localHostname) {
		t.Errorf("systemInfoText() = %q, still contains this machine's own hostname %q", got, localHostname)
	}
}

func TestRemoteSystemInfoCountLinesOmitsSessionsEntirely(t *testing.T) {
	client := &fakeRemoteClient{entries: map[string][]fsops.Entry{
		"/proc": {
			{Name: "1", Type: fsops.TypeDir, IsDir: true},
			{Name: "42", Type: fsops.TypeDir, IsDir: true},
			{Name: "cpuinfo", Type: fsops.TypeFile}, // not numeric — must not be counted as a process
		},
	}}
	remoteSystemFile(client, "/proc/mounts", "/dev/sda1 / ext4 rw 0 0\ntmpfs /tmp tmpfs rw 0 0\n")
	remoteSystemFile(client, "/proc/net/dev", "Inter-|   Receive\n face |bytes\n  lo:    0    0\n  eth0:  0    0\n")

	lines := remoteSystemInfoCountLines(client)
	text := strings.Join(lines, "\n")

	if !strings.Contains(text, "Mounted fs") || !strings.Contains(text, "2") {
		t.Errorf("lines = %q, want Mounted fs: 2", text)
	}
	if !strings.Contains(text, "Processes") {
		t.Errorf("lines = %q, want a Processes line", text)
	}
	// Exactly the two numeric entries (1, 42) count as processes —
	// "cpuinfo" must not.
	if strings.Contains(text, "Processes") {
		var procLine string
		for _, l := range lines {
			if strings.Contains(l, "Processes") {
				procLine = l
			}
		}
		if !strings.Contains(procLine, "2") {
			t.Errorf("Processes line = %q, want exactly 2 numeric /proc entries counted", procLine)
		}
	}
	if !strings.Contains(text, "Network") || !strings.Contains(text, "eth0") {
		t.Errorf("lines = %q, want a Network line naming eth0, not lo", text)
	}
	if strings.Contains(text, "Sessions") {
		t.Error("remoteSystemInfoCountLines includes a Sessions line — it needs a command-execution channel this project doesn't have")
	}
}
