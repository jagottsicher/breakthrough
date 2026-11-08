package systemd

import (
	"os/exec"
	"testing"
)

// requireCommand skips t unless name is on $PATH — the same small
// helper internal/mounts/internal/ui already keep their own copy of.
func requireCommand(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available in this environment: %v", name, err)
	}
}

func TestParseUnitsJSONDecodesEveryField(t *testing.T) {
	const data = `[
		{"unit":"accounts-daemon.service","load":"loaded","active":"active","sub":"running","description":"Accounts Service"},
		{"unit":"apparmor.service","load":"loaded","active":"inactive","sub":"dead","description":"Load AppArmor profiles"}
	]`

	got, err := parseUnitsJSON([]byte(data))
	if err != nil {
		t.Fatalf("parseUnitsJSON: %v", err)
	}
	want := []Unit{
		{Name: "accounts-daemon.service", Load: "loaded", Active: "active", Sub: "running", Description: "Accounts Service"},
		{Name: "apparmor.service", Load: "loaded", Active: "inactive", Sub: "dead", Description: "Load AppArmor profiles"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d units, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("unit %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestParseUnitsJSONPassesThroughUnrecognizedLoadAndActiveValues pins
// that a load/active value this package has never special-cased (a
// real "not-found" unit and a real "failed" one, both confirmed
// against a live systemctl before writing this package — see
// services.go's own doc comment on Unit) still comes through verbatim,
// not dropped or mapped to some other value by a switch that only
// knew a handful of states.
func TestParseUnitsJSONPassesThroughUnrecognizedLoadAndActiveValues(t *testing.T) {
	const data = `[
		{"unit":"gone.service","load":"not-found","active":"inactive","sub":"dead","description":"gone.service"},
		{"unit":"broken.service","load":"loaded","active":"failed","sub":"failed","description":"A service that failed"}
	]`

	got, err := parseUnitsJSON([]byte(data))
	if err != nil {
		t.Fatalf("parseUnitsJSON: %v", err)
	}
	if got[0].Load != "not-found" {
		t.Errorf("Load = %q, want %q", got[0].Load, "not-found")
	}
	if got[1].Active != "failed" || got[1].Sub != "failed" {
		t.Errorf("Active/Sub = %q/%q, want %q/%q", got[1].Active, got[1].Sub, "failed", "failed")
	}
}

// TestParseUnitsJSONPassesThroughEscapedUnitNames pins that a unit name
// systemctl itself renders with a "\x.." escape (confirmed against a
// real scope/service unit on a real system, not guessed) is shown
// exactly as systemctl prints it — the same "don't half-do a decision"
// reasoning as the Active/Load pass-through above, just for Name.
func TestParseUnitsJSONPassesThroughEscapedUnitNames(t *testing.T) {
	const data = `[{"unit":"app-gnome-firefox\\x2desr-123.scope","load":"loaded","active":"active","sub":"running","description":"Firefox"}]`

	got, err := parseUnitsJSON([]byte(data))
	if err != nil {
		t.Fatalf("parseUnitsJSON: %v", err)
	}
	want := `app-gnome-firefox\x2desr-123.scope`
	if got[0].Name != want {
		t.Errorf("Name = %q, want %q", got[0].Name, want)
	}
}

func TestParseUnitsJSONInvalidReturnsError(t *testing.T) {
	if _, err := parseUnitsJSON([]byte("not json")); err == nil {
		t.Error("parseUnitsJSON should report an error for invalid JSON")
	}
}

func TestParseUnitsJSONEmptyArrayReturnsEmptySlice(t *testing.T) {
	got, err := parseUnitsJSON([]byte("[]"))
	if err != nil {
		t.Fatalf("parseUnitsJSON: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d units, want 0", len(got))
	}
}

// TestReadReturnsServiceUnits is an integration test against the real
// systemctl binary — skipped, not failed, wherever it's unavailable,
// per CLAUDE.md's own "document platform/sandbox limits, don't present
// them as a code bug" rule (systemd/systemctl doesn't exist at all on
// macOS/BSD).
func TestReadReturnsServiceUnits(t *testing.T) {
	requireCommand(t, "systemctl")
	units, err := Read()
	if err != nil {
		t.Skipf("systemctl present but unusable in this sandbox: %v", err)
	}
	if len(units) == 0 {
		t.Skip("no service units reported in this sandbox")
	}
	for _, u := range units {
		if u.Name == "" {
			t.Error("Unit.Name is empty")
		}
	}
}
