package multiplex

import (
	"reflect"
	"testing"
)

// realZellijListTranscript is `zellij list-sessions -n -s`'s own real
// output (zellij 0.45.1, downloaded and run directly against a real
// background session, not assumed) — a single plain session name, no
// attach status of any kind (see Status's own doc comment for why
// parseZellijList never sets anything but StatusUnknown).
const realZellijListTranscript = "bt-test-zellij2\n"

func TestParseZellijListParsesARealTranscript(t *testing.T) {
	got := parseZellijList(realZellijListTranscript)
	want := []Session{{Name: "bt-test-zellij2", Backend: BackendZellij, Status: StatusUnknown}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseZellijList() = %+v, want %+v", got, want)
	}
}

func TestParseZellijListParsesMultipleSessions(t *testing.T) {
	got := parseZellijList("alpha\nbeta\n")
	want := []Session{
		{Name: "alpha", Backend: BackendZellij, Status: StatusUnknown},
		{Name: "beta", Backend: BackendZellij, Status: StatusUnknown},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseZellijList() = %+v, want %+v", got, want)
	}
}

func TestParseZellijListOnNoSessionsReturnsNil(t *testing.T) {
	const transcript = "No active zellij sessions found.\n"
	if got := parseZellijList(transcript); got != nil {
		t.Errorf("parseZellijList(no sessions) = %+v, want nil", got)
	}
}

func TestParseZellijListOnEmptyTextReturnsNil(t *testing.T) {
	if got := parseZellijList(""); got != nil {
		t.Errorf("parseZellijList(\"\") = %+v, want nil", got)
	}
}

func TestZellijAttachCommandHasNoForceFlag(t *testing.T) {
	got := zellijAttachCommand(Session{Name: "foo", Backend: BackendZellij})
	want := []string{"zellij", "attach", "foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("zellijAttachCommand() = %v, want %v", got, want)
	}
}

func TestZellijCloseCommandUsesKillSession(t *testing.T) {
	got := zellijCloseCommand(Session{Name: "foo", Backend: BackendZellij})
	want := []string{"zellij", "kill-session", "foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("zellijCloseCommand() = %v, want %v", got, want)
	}
}

func TestListZellijSessionsReadsThroughRunZellijList(t *testing.T) {
	orig := runZellijList
	t.Cleanup(func() { runZellijList = orig })
	runZellijList = func() (string, error) { return realZellijListTranscript, nil }

	got, err := listZellijSessions()
	if err != nil {
		t.Fatalf("listZellijSessions: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("len(got) = %d, want 1", len(got))
	}
}
