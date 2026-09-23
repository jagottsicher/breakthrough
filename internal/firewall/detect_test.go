package firewall

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// isolateDetection swaps every one of DetectBackend/ReadSnapshot's own
// exec wrappers for the duration of one test, restoring the originals via
// t.Cleanup — the same "swap the package-level var, restore in Cleanup"
// idiom internal/ui's own dirSize tests already use, kept here rather than
// introducing a mockable CommandRunner interface this package doesn't
// otherwise need.
func isolateDetection(t *testing.T, ufwPath, nftPath, iptablesSavePath error, ufwOut string, ufwErr error, nftOut []byte, nftErr error, iptablesOut string, iptablesErr error) {
	t.Helper()
	origLookPath, origUFW, origNFT, origIPTables := lookPath, runUFWStatus, runNFTRuleset, runIPTablesSave
	t.Cleanup(func() {
		lookPath, runUFWStatus, runNFTRuleset, runIPTablesSave = origLookPath, origUFW, origNFT, origIPTables
	})
	lookPath = func(file string) (string, error) {
		switch file {
		case "ufw":
			if ufwPath != nil {
				return "", ufwPath
			}
			return "/usr/sbin/ufw", nil
		case "nft":
			if nftPath != nil {
				return "", nftPath
			}
			return "/usr/sbin/nft", nil
		case "iptables-save":
			if iptablesSavePath != nil {
				return "", iptablesSavePath
			}
			return "/usr/sbin/iptables-save", nil
		}
		return "", exec.ErrNotFound
	}
	runUFWStatus = func() (string, error) { return ufwOut, ufwErr }
	runNFTRuleset = func() ([]byte, error) { return nftOut, nftErr }
	runIPTablesSave = func() (string, error) { return iptablesOut, iptablesErr }
}

var errNotFound = errors.New("executable file not found in $PATH")

func TestDetectBackendPrefersActiveUFW(t *testing.T) {
	isolateDetection(t, nil, nil, nil, "Status: active\n", nil, []byte(`{"nftables":[{"table":{}}]}`), nil, "", nil)
	if got := DetectBackend(); got != BackendUFW {
		t.Errorf("DetectBackend() = %q, want %q", got, BackendUFW)
	}
}

func TestDetectBackendFallsBackWhenUFWInactive(t *testing.T) {
	isolateDetection(t, nil, nil, nil, "Status: inactive\n", nil, []byte(`{"nftables":[{"table":{}}]}`), nil, "", nil)
	if got := DetectBackend(); got != BackendNFTables {
		t.Errorf("DetectBackend() = %q, want %q", got, BackendNFTables)
	}
}

func TestDetectBackendFallsBackWhenUFWMissing(t *testing.T) {
	isolateDetection(t, errNotFound, nil, nil, "", nil, []byte(`{"nftables":[{"table":{}}]}`), nil, "", nil)
	if got := DetectBackend(); got != BackendNFTables {
		t.Errorf("DetectBackend() = %q, want %q", got, BackendNFTables)
	}
}

func TestDetectBackendSkipsEmptyNFTRuleset(t *testing.T) {
	isolateDetection(t, errNotFound, nil, nil, "", nil, []byte(`{"nftables":[{"metainfo":{}}]}`), nil, "", nil)
	if got := DetectBackend(); got != BackendIPTables {
		t.Errorf("DetectBackend() = %q, want %q (empty nft ruleset should fall through)", got, BackendIPTables)
	}
}

func TestDetectBackendFallsBackToIPTables(t *testing.T) {
	isolateDetection(t, errNotFound, errNotFound, nil, "", nil, nil, nil, "", nil)
	if got := DetectBackend(); got != BackendIPTables {
		t.Errorf("DetectBackend() = %q, want %q", got, BackendIPTables)
	}
}

func TestDetectBackendNoneWhenNothingIsInstalled(t *testing.T) {
	isolateDetection(t, errNotFound, errNotFound, errNotFound, "", nil, nil, nil, "", nil)
	if got := DetectBackend(); got != BackendNone {
		t.Errorf("DetectBackend() = %q, want %q", got, BackendNone)
	}
}

// TestWrapExitErrorAppendsCapturedStderr pins the real, reported gap:
// a bare *exec.ExitError says only "exit status N" on its own — this
// has to surface the command's own real stderr text too, the part that
// actually explains why (most commonly a permission error, since
// several of these commands need root).
func TestWrapExitErrorAppendsCapturedStderr(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo 'permission denied' >&2; exit 4")
	_, err := cmd.Output()
	if err == nil {
		t.Fatal("setup: expected the command to fail")
	}

	got := wrapExitError(err)

	if !strings.Contains(got.Error(), "exit status 4") {
		t.Errorf("wrapExitError(%v) = %q, want it to still mention the exit status", err, got)
	}
	if !strings.Contains(got.Error(), "permission denied") {
		t.Errorf("wrapExitError(%v) = %q, want it to include the captured stderr", err, got)
	}
}

// TestWrapExitErrorLeavesOtherErrorsUnchanged pins the other half: an
// error with no stderr to add (the binary itself failing to start,
// say) is returned exactly as given.
func TestWrapExitErrorLeavesOtherErrorsUnchanged(t *testing.T) {
	err := exec.ErrNotFound

	if got := wrapExitError(err); got != err {
		t.Errorf("wrapExitError(%v) = %v, want the same error unchanged", err, got)
	}
}
