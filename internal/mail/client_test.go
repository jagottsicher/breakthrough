package mail

import (
	"os/exec"
	"reflect"
	"testing"
)

// isolateLookPath fakes lookPath to report only the binaries named in
// installed as found — the same isolation shape internal/multiplex's
// own detect_test.go already establishes, so a test never depends on
// what's actually installed on the machine running it.
func isolateLookPath(t *testing.T, installed ...string) {
	t.Helper()
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	set := make(map[string]bool, len(installed))
	for _, c := range installed {
		set[c] = true
	}
	lookPath = func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
}

func TestDetectReturnsNothingWhenNoneInstalled(t *testing.T) {
	isolateLookPath(t)
	if got := Detect(); len(got) != 0 {
		t.Errorf("Detect() = %v, want none", got)
	}
}

func TestDetectReturnsExactlyWhatsInstalled(t *testing.T) {
	isolateLookPath(t, "aerc")
	if got := Detect(); !reflect.DeepEqual(got, []string{"aerc"}) {
		t.Errorf("Detect() = %v, want [aerc]", got)
	}
}

// TestDetectOrdersByCandidatesNotInstallOrder pins that Detect's own
// output always follows Candidates' own priority order (neomutt, aerc,
// himalaya, mail, mailx), regardless of what order lookPath happens to
// report them found in.
func TestDetectOrdersByCandidatesNotInstallOrder(t *testing.T) {
	isolateLookPath(t, "mailx", "himalaya", "mail", "aerc", "neomutt")
	want := []string{"neomutt", "aerc", "himalaya", "mail", "mailx"}
	if got := Detect(); !reflect.DeepEqual(got, want) {
		t.Errorf("Detect() = %v, want %v", got, want)
	}
}

func TestInstalledReflectsLookPath(t *testing.T) {
	isolateLookPath(t, "neomutt")
	if !Installed("neomutt") {
		t.Error("Installed(\"neomutt\") = false, want true")
	}
	if Installed("aerc") {
		t.Error("Installed(\"aerc\") = true, want false")
	}
}

func TestCommandIsJustTheBinaryNameItself(t *testing.T) {
	got := Command("neomutt")
	want := []string{"neomutt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Command(\"neomutt\") = %v, want %v", got, want)
	}
}

func TestCommandWithEmptyClientIsNil(t *testing.T) {
	if got := Command(""); got != nil {
		t.Errorf("Command(\"\") = %v, want nil", got)
	}
}
