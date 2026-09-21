package firewall

import (
	"errors"
	"testing"
)

func TestReadSnapshotUFW(t *testing.T) {
	isolateDetection(t, nil, nil, nil, realUFWStatusVerbose, nil, nil, nil, "", nil)
	snap, err := ReadSnapshot()
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	if snap.Backend != BackendUFW {
		t.Errorf("Backend = %q, want %q", snap.Backend, BackendUFW)
	}
	if len(snap.Rules) != 7 {
		t.Errorf("got %d rules, want 7", len(snap.Rules))
	}
}

func TestReadSnapshotNoBackendIsNotAnError(t *testing.T) {
	isolateDetection(t, errNotFound, errNotFound, errNotFound, "", nil, nil, nil, "", nil)
	snap, err := ReadSnapshot()
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	if snap.Backend != BackendNone {
		t.Errorf("Backend = %q, want %q", snap.Backend, BackendNone)
	}
	if snap.Rules != nil {
		t.Errorf("Rules = %v, want nil", snap.Rules)
	}
}

func TestReadSnapshotSurfacesAParseError(t *testing.T) {
	isolateDetection(t, nil, nil, nil, "Status: active\nTo  Action  From\n--  ------  ----\nnot a real rule line\n", nil, nil, nil, "", nil)
	if _, err := ReadSnapshot(); err == nil {
		t.Error("expected an error for a rule line ParseUFWStatusVerbose cannot understand")
	}
}

func TestReadSnapshotSurfacesAShellError(t *testing.T) {
	// ufw and nft both missing so DetectBackend falls all the way through
	// to iptables-save, whose own detection (unlike ufw/nft) is a plain
	// lookPath check that never actually runs the command — the one
	// backend where a "detected but fails when actually read" scenario
	// can be simulated without a stateful mock.
	isolateDetection(t, errNotFound, errNotFound, nil, "", nil, nil, nil, "", errors.New("permission denied"))
	if _, err := ReadSnapshot(); err == nil {
		t.Error("expected an error when iptables-save itself fails")
	}
}

func TestReadSnapshotAnnotatesShadows(t *testing.T) {
	const fixture = `Status: active
Logging: on (low)
Default: deny (incoming), allow (outgoing), disabled (routed)
New profiles: skip

To                         Action      From
--                         ------      ----
Anywhere                   ALLOW IN    Anywhere
22/tcp                     DENY IN     Anywhere
`
	isolateDetection(t, nil, nil, nil, fixture, nil, nil, nil, "", nil)
	snap, err := ReadSnapshot()
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	if len(snap.Rules) != 2 {
		t.Fatalf("got %d rules, want 2", len(snap.Rules))
	}
	if snap.Rules[1].ShadowedByOrder != snap.Rules[0].Order {
		t.Errorf("the second rule should be shadowed by the first (any/any/any) rule, got ShadowedByOrder=%d", snap.Rules[1].ShadowedByOrder)
	}
}
