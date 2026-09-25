package multiplex

import (
	"errors"
	"os/exec"
	"testing"
)

// isolateBinaries points lookPath at a fake resolver for the duration
// of one test, restoring the original via t.Cleanup — the same "swap
// the package-level var, restore in Cleanup" idiom internal/firewall's
// own detect_test.go already establishes.
func isolateBinaries(t *testing.T, screenPresent, tmuxPresent, zellijPresent bool) {
	t.Helper()
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(name string) (string, error) {
		switch name {
		case "screen":
			if screenPresent {
				return "/usr/bin/screen", nil
			}
		case "tmux":
			if tmuxPresent {
				return "/usr/bin/tmux", nil
			}
		case "zellij":
			if zellijPresent {
				return "/usr/bin/zellij", nil
			}
		}
		return "", exec.ErrNotFound
	}
}

func isolateListers(t *testing.T, screenOut, tmuxOut, zellijOut string) {
	t.Helper()
	origScreen, origTmux, origZellij := runScreenList, runTmuxList, runZellijList
	t.Cleanup(func() { runScreenList, runTmuxList, runZellijList = origScreen, origTmux, origZellij })
	runScreenList = func() (string, error) { return screenOut, nil }
	runTmuxList = func() (string, error) { return tmuxOut, nil }
	runZellijList = func() (string, error) { return zellijOut, nil }
}

func TestListSessionsRefusesWhenNoBackendIsInstalled(t *testing.T) {
	isolateBinaries(t, false, false, false)

	_, err := ListSessions()
	if err == nil {
		t.Fatal("ListSessions() = nil error, want a refusal when none of screen/tmux/zellij is installed")
	}
}

func TestListSessionsCombinesAllThreeBackendsWhenAllAreInstalled(t *testing.T) {
	isolateBinaries(t, true, true, true)
	isolateListers(t, realScreenListTranscript, realTmuxListTranscript, realZellijListTranscript)

	got, err := ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got) != 6 { // 3 screen + 2 tmux + 1 zellij
		t.Errorf("len(got) = %d, want 6", len(got))
	}
}

func TestListSessionsUsesOnlyTheInstalledBackend(t *testing.T) {
	isolateBinaries(t, true, false, false)
	isolateListers(t, realScreenListTranscript, realTmuxListTranscript, realZellijListTranscript)

	got, err := ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	for _, s := range got {
		if s.Backend != BackendScreen {
			t.Errorf("got a %s session with only screen installed: %+v", s.Backend, s)
		}
	}
	if len(got) != 3 {
		t.Errorf("len(got) = %d, want 3 (screen only)", len(got))
	}
}

func TestListSessionsUsesOnlyZellijWhenOnlyZellijIsInstalled(t *testing.T) {
	isolateBinaries(t, false, false, true)
	isolateListers(t, realScreenListTranscript, realTmuxListTranscript, realZellijListTranscript)

	got, err := ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got) != 1 || got[0].Backend != BackendZellij {
		t.Errorf("got %+v, want exactly one BackendZellij session", got)
	}
}

func TestListSessionsPropagatesARealScreenFailure(t *testing.T) {
	isolateBinaries(t, true, false, false)
	origScreen := runScreenList
	t.Cleanup(func() { runScreenList = origScreen })
	runScreenList = func() (string, error) { return "", errors.New("exec: \"screen\": permission denied") }

	if _, err := ListSessions(); err == nil {
		t.Error("ListSessions() = nil error, want the real screen failure surfaced")
	}
}

func TestAttachCommandDispatchesByBackend(t *testing.T) {
	if got := AttachCommand(Session{Name: "1.foo", Backend: BackendScreen}); got[0] != "screen" {
		t.Errorf("AttachCommand(screen) = %v, want it to start with \"screen\"", got)
	}
	if got := AttachCommand(Session{Name: "foo", Backend: BackendTmux}); got[0] != "tmux" {
		t.Errorf("AttachCommand(tmux) = %v, want it to start with \"tmux\"", got)
	}
	if got := AttachCommand(Session{Name: "foo", Backend: BackendZellij}); got[0] != "zellij" {
		t.Errorf("AttachCommand(zellij) = %v, want it to start with \"zellij\"", got)
	}
}

func TestCloseCommandDispatchesByBackend(t *testing.T) {
	if got := CloseCommand(Session{Name: "1.foo", Backend: BackendScreen}); got[0] != "screen" {
		t.Errorf("CloseCommand(screen) = %v, want it to start with \"screen\"", got)
	}
	if got := CloseCommand(Session{Name: "foo", Backend: BackendTmux}); got[0] != "tmux" {
		t.Errorf("CloseCommand(tmux) = %v, want it to start with \"tmux\"", got)
	}
	if got := CloseCommand(Session{Name: "foo", Backend: BackendZellij}); got[0] != "zellij" {
		t.Errorf("CloseCommand(zellij) = %v, want it to start with \"zellij\"", got)
	}
}
