package multiplex

import (
	"reflect"
	"testing"
)

// realScreenListTranscript is a real `screen -ls` transcript (screen
// 5.0.2, German locale timestamps) — captured directly, not hand-
// written, so the parser is proven against what screen actually prints,
// timestamp column included, not just an idealized shape.
const realScreenListTranscript = `There are screens on:
	3343442.bt-test-2	(25.09.2026 14:38:56)	(Detached)
	3343440.bt-test-1	(25.09.2026 14:38:56)	(Detached)
	10065.breakthrough	(18.09.2026 17:33:08)	(Attached)
13 Sockets in /run/screen/S-jens.
`

func TestParseScreenListParsesARealTranscriptWithTimestamps(t *testing.T) {
	got := parseScreenList(realScreenListTranscript)
	want := []Session{
		{Name: "3343442.bt-test-2", Backend: BackendScreen, Attached: false},
		{Name: "3343440.bt-test-1", Backend: BackendScreen, Attached: false},
		{Name: "10065.breakthrough", Backend: BackendScreen, Attached: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseScreenList() = %+v, want %+v", got, want)
	}
}

// TestParseScreenListParsesATranscriptWithoutTimestamps covers an older
// screen build's own leaner line shape (no per-session timestamp column
// at all) — the same session line, minus the middle "(...)" group.
func TestParseScreenListParsesATranscriptWithoutTimestamps(t *testing.T) {
	const transcript = "There is a screen on:\n\t12345.mysession\t(Detached)\n1 Socket in /run/screen/S-user.\n"
	got := parseScreenList(transcript)
	want := []Session{{Name: "12345.mysession", Backend: BackendScreen, Attached: false}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseScreenList() = %+v, want %+v", got, want)
	}
}

func TestParseScreenListOnNoSessionsReturnsNil(t *testing.T) {
	const transcript = "No Sockets found in /run/screen/S-jens.\n\n"
	if got := parseScreenList(transcript); got != nil {
		t.Errorf("parseScreenList(no sessions) = %+v, want nil", got)
	}
}

func TestParseScreenListOnEmptyTextReturnsNil(t *testing.T) {
	if got := parseScreenList(""); got != nil {
		t.Errorf("parseScreenList(\"\") = %+v, want nil", got)
	}
}

func TestScreenAttachCommandUsesDashDDashR(t *testing.T) {
	got := screenAttachCommand(Session{Name: "123.foo", Backend: BackendScreen})
	want := []string{"screen", "-D", "-r", "123.foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("screenAttachCommand() = %v, want %v", got, want)
	}
}

func TestScreenCloseCommandUsesXQuit(t *testing.T) {
	got := screenCloseCommand(Session{Name: "123.foo", Backend: BackendScreen})
	want := []string{"screen", "-S", "123.foo", "-X", "quit"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("screenCloseCommand() = %v, want %v", got, want)
	}
}

func TestListScreenSessionsReadsThroughRunScreenList(t *testing.T) {
	orig := runScreenList
	t.Cleanup(func() { runScreenList = orig })
	runScreenList = func() (string, error) { return realScreenListTranscript, nil }

	got, err := listScreenSessions()
	if err != nil {
		t.Fatalf("listScreenSessions: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len(got) = %d, want 3", len(got))
	}
}
