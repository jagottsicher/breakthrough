package mounts

import (
	"os/exec"
	"testing"
)

// requireCommand skips t unless name is on $PATH — the same small helper
// internal/fsops and internal/ui already keep their own copy of.
func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available in this environment: %v", name, err)
	}
}

// TestParseFindmntJSONFlattensNestedChildren pins the flattening this
// package depends on: findmnt nests every mount under whatever it's
// physically mounted on top of, arbitrarily deep, and callers need a
// plain list, depth-first, in the same order findmnt itself reports.
func TestParseFindmntJSONFlattensNestedChildren(t *testing.T) {
	const data = `{
		"filesystems": [
			{
				"target": "/", "source": "/dev/md0", "fstype": "ext4", "options": "rw",
				"children": [
					{"target": "/boot", "source": "/dev/md1", "fstype": "ext4", "options": "rw"},
					{
						"target": "/home", "source": "/dev/md2", "fstype": "ext4", "options": "rw",
						"children": [
							{"target": "/home/jens/snap", "source": "/dev/sdd1", "fstype": "ext4", "options": "rw"}
						]
					}
				]
			}
		]
	}`

	got, err := parseFindmntJSON([]byte(data))
	if err != nil {
		t.Fatalf("parseFindmntJSON: %v", err)
	}

	wantTargets := []string{"/", "/boot", "/home", "/home/jens/snap"}
	if len(got) != len(wantTargets) {
		t.Fatalf("got %d nodes, want %d: %+v", len(got), len(wantTargets), got)
	}
	for i, want := range wantTargets {
		if got[i].Target != want {
			t.Errorf("node %d target = %q, want %q", i, got[i].Target, want)
		}
		if got[i].Children != nil {
			t.Errorf("node %d (%q) still carries its own Children after flattening", i, got[i].Target)
		}
	}
}

// TestParseFindmntJSONInvalidReturnsError pins that malformed input is
// reported rather than silently producing an empty (and therefore
// misleadingly "nothing is mounted") list.
func TestParseFindmntJSONInvalidReturnsError(t *testing.T) {
	if _, err := parseFindmntJSON([]byte("not json")); err == nil {
		t.Error("parseFindmntJSON should report an error for invalid JSON")
	}
}

// TestIsBindMountSourceDetectsBracketNotation pins the exact pattern
// confirmed by hand against a real bind mount on a real system:
// "<device>[<subpath>]" means a bind mount of that sub-path, and
// anything else — a plain device, a bare name with no brackets, an
// unterminated bracket — is not one.
func TestIsBindMountSourceDetectsBracketNotation(t *testing.T) {
	cases := []struct {
		source string
		want   bool
	}{
		{"/dev/sdd1[/.bitcoin]", true},
		{"//192.168.1.1/share[/sub/dir]", true},
		{"/dev/sda1", false},
		{"tmpfs", false},
		{"/dev/sda1[unterminated", false},
	}
	for _, c := range cases {
		if got := isBindMountSource(c.source); got != c.want {
			t.Errorf("isBindMountSource(%q) = %v, want %v", c.source, got, c.want)
		}
	}
}

// TestBuildEntriesMarksFstabTargetsAsPersistent pins the whole point of
// cross-referencing the two findmnt calls: a live mount whose target is
// also configured in fstab is persistent (survives a reboot); one that
// isn't was mounted some other way.
func TestBuildEntriesMarksFstabTargetsAsPersistent(t *testing.T) {
	live := []findmntNode{
		{Target: "/", Source: "/dev/md0", Fstype: "ext4", Options: "rw"},
		{Target: "/mnt/usb", Source: "/dev/sdb1", Fstype: "vfat", Options: "rw"},
	}
	fstab := []findmntNode{
		{Target: "/", Source: "UUID=abc", Fstype: "ext4", Options: "errors=remount-ro"},
		{Target: "none", Source: "/dev/mapper/cryptswap1", Fstype: "swap", Options: "sw"},
	}

	entries := buildEntries(live, fstab)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if !entries[0].Persistent {
		t.Errorf("%q is in fstab, should be marked persistent", entries[0].Target)
	}
	if entries[1].Persistent {
		t.Errorf("%q is not in fstab, should not be marked persistent", entries[1].Target)
	}
}

// TestBuildEntriesNeverMatchesTheSwapPlaceholderTarget pins a specific
// edge case: fstab's own swap entries use the literal target "none",
// which is never a real mountpoint — a naive implementation comparing
// target strings could accidentally treat a live mount that somehow also
// reported "/" as empty, or otherwise degenerate, as a false match. This
// pins that "none" itself is never eligible at all.
func TestBuildEntriesNeverMatchesTheSwapPlaceholderTarget(t *testing.T) {
	live := []findmntNode{{Target: "none", Source: "/dev/mapper/cryptswap1", Fstype: "swap", Options: "sw"}}
	fstab := []findmntNode{{Target: "none", Source: "/dev/mapper/cryptswap1", Fstype: "swap", Options: "sw"}}

	entries := buildEntries(live, fstab)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Persistent {
		t.Error("the swap placeholder target \"none\" should never be treated as a real, persistent mountpoint")
	}
}

// TestBuildEntriesDetectsBindMounts pins that bind-mount detection (see
// TestIsBindMountSourceDetectsBracketNotation) actually reaches the
// assembled Entry, not just the lower-level helper.
func TestBuildEntriesDetectsBindMounts(t *testing.T) {
	live := []findmntNode{
		{Target: "/home/jens/snap/bitcoin-core/common/.bitcoin", Source: "/dev/sdd1[/.bitcoin]", Fstype: "ext4", Options: "rw"},
		{Target: "/", Source: "/dev/md0", Fstype: "ext4", Options: "rw"},
	}

	entries := buildEntries(live, nil)
	if !entries[0].Bind {
		t.Error("a bracketed source should be detected as a bind mount")
	}
	if entries[1].Bind {
		t.Error("a plain device source should not be detected as a bind mount")
	}
}

// TestReadRealSystem exercises the real, live shelling-out path end to
// end — skipped where findmnt itself isn't installed. "/" is always
// mounted, and a real ext4/xfs/btrfs root is never excluded by --real's
// own pseudo-filesystem filter, so it's a safe thing to assert on any
// real Linux system.
func TestReadRealSystem(t *testing.T) {
	requireCommand(t, "findmnt")

	entries, err := Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.Target == "/" {
			found = true
		}
	}
	if !found {
		t.Error("Read should always include the real filesystem root \"/\"")
	}
}
