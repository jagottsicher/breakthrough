package multiplex

import (
	"reflect"
	"testing"
)

// realTmuxListTranscript is a real `tmux list-sessions -F ...`
// transcript (tmux 3.7c), captured directly against two real sessions,
// one of them attached.
const realTmuxListTranscript = "bt-test-a\t0\nbt-test-b\t1\n"

func TestParseTmuxListParsesARealTranscript(t *testing.T) {
	got := parseTmuxList(realTmuxListTranscript)
	want := []Session{
		{Name: "bt-test-a", Backend: BackendTmux, Attached: false},
		{Name: "bt-test-b", Backend: BackendTmux, Attached: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseTmuxList() = %+v, want %+v", got, want)
	}
}

func TestParseTmuxListOnNoServerRunningReturnsNil(t *testing.T) {
	const transcript = "no server running on /tmp/tmux-1000/default\n"
	if got := parseTmuxList(transcript); got != nil {
		t.Errorf("parseTmuxList(no server) = %+v, want nil", got)
	}
}

func TestParseTmuxListOnEmptyTextReturnsNil(t *testing.T) {
	if got := parseTmuxList(""); got != nil {
		t.Errorf("parseTmuxList(\"\") = %+v, want nil", got)
	}
}

func TestTmuxAttachCommandUsesDashDDashT(t *testing.T) {
	got := tmuxAttachCommand(Session{Name: "foo", Backend: BackendTmux})
	want := []string{"tmux", "attach", "-d", "-t", "foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tmuxAttachCommand() = %v, want %v", got, want)
	}
}

func TestTmuxCloseCommandUsesKillSession(t *testing.T) {
	got := tmuxCloseCommand(Session{Name: "foo", Backend: BackendTmux})
	want := []string{"tmux", "kill-session", "-t", "foo"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tmuxCloseCommand() = %v, want %v", got, want)
	}
}

func TestListTmuxSessionsReadsThroughRunTmuxList(t *testing.T) {
	orig := runTmuxList
	t.Cleanup(func() { runTmuxList = orig })
	runTmuxList = func() (string, error) { return realTmuxListTranscript, nil }

	got, err := listTmuxSessions()
	if err != nil {
		t.Fatalf("listTmuxSessions: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len(got) = %d, want 2", len(got))
	}
}
