package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rivo/tview"
)

func TestShowTransientErrorOpensTheErrorOverlay(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.showTransientError(fmt.Errorf("gx is not a command — see \"?\" for help"))

	if r.activePage != errorPage {
		t.Fatalf("activePage = %q, want the error overlay", r.activePage)
	}
}

func TestShowTransientErrorWithANilErrorIsANoOp(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}

	r.showTransientError(nil)

	if r.activePage == errorPage {
		t.Error("showTransientError(nil) opened the error overlay, want a no-op")
	}
}

// TestAutoHideErrorClosesTheNoticeItWasArmedFor pins the timer callback
// showTransientError arms (see autoHideError's own doc comment) —
// called directly rather than waiting on a real errorAutoHideDelay, the
// same "call the timer's own body, don't wait on it" shape
// rollbackFirewallRule's own tests already use.
func TestAutoHideErrorClosesTheNoticeItWasArmedFor(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.showTransientError(fmt.Errorf("gx is not a command — see \"?\" for help"))
	generation := r.errorGeneration

	r.autoHideError(generation)

	if r.activePage == errorPage {
		t.Error("autoHideError left the notice open, want it closed")
	}
}

// TestAutoHideErrorLeavesANewerUnrelatedNoticeAlone guards the race
// showTransientError's own doc comment describes: a stale timer must
// never close a second, unrelated notice that happens to already be
// showing on errorPage by the time it fires.
func TestAutoHideErrorLeavesANewerUnrelatedNoticeAlone(t *testing.T) {
	r, err := NewRoot(tview.NewApplication(), fixtureDir(t))
	if err != nil {
		t.Fatalf("NewRoot: %v", err)
	}
	r.showTransientError(fmt.Errorf("gx is not a command — see \"?\" for help"))
	staleGeneration := r.errorGeneration
	r.hideOverlay() // dismissed early, e.g. via Escape/Ctrl+C/a click outside
	r.showError(fmt.Errorf("a real, unrelated failure"))

	r.autoHideError(staleGeneration)

	if r.activePage != errorPage {
		t.Error("autoHideError closed a newer, unrelated notice using a stale generation")
	}
}

func TestWrapText(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{
			name:  "short text stays on one line",
			text:  "permission denied",
			width: 40,
			want:  []string{"permission denied"},
		},
		{
			name:  "breaks at spaces",
			text:  "rename failed because the target exists",
			width: 20,
			want:  []string{"rename failed", "because the target", "exists"},
		},
		{
			name:  "existing newlines are kept",
			text:  "line one\nline two",
			width: 40,
			want:  []string{"line one", "line two"},
		},
		{
			name:  "empty text yields one empty line",
			text:  "",
			width: 10,
			want:  []string{""},
		},
	}

	for _, tt := range tests {
		got := wrapText(tt.text, tt.width)
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("%s: wrapText(%q, %d) = %q, want %q", tt.name, tt.text, tt.width, got, tt.want)
		}
	}
}

// TestWrapTextHardSplitsLongWords covers the case error messages actually
// hit: a path with no spaces in it, longer than the whole overlay.
func TestWrapTextHardSplitsLongWords(t *testing.T) {
	path := "/home/jens/development/chatgpthelps/breakthrough/internal/fsops/list.go"

	const width = 20
	got := wrapText("open "+path+": permission denied", width)

	for i, line := range got {
		if n := len([]rune(line)); n > width {
			t.Errorf("line %d is %d columns wide, want at most %d: %q", i, n, width, line)
		}
	}

	// Nothing may be lost: joining the lines back must contain the path.
	if joined := strings.Join(got, ""); !strings.Contains(joined, path) {
		t.Errorf("wrapped text lost part of the path:\n%q", joined)
	}
}

// TestWrapTextNeverSplitsRunes guards the rune-counting: a hard split at
// a byte boundary would corrupt multi-byte characters.
func TestWrapTextNeverSplitsRunes(t *testing.T) {
	const width = 4
	got := wrapText(strings.Repeat("ä", 10), width)

	for i, line := range got {
		if n := len([]rune(line)); n > width {
			t.Errorf("line %d is %d runes wide, want at most %d", i, n, width)
		}
		if strings.ContainsRune(line, '�') {
			t.Errorf("line %d contains a replacement character, a rune was split: %q", i, line)
		}
	}

	if joined := strings.Join(got, ""); joined != strings.Repeat("ä", 10) {
		t.Errorf("joined = %q, want the original text back", joined)
	}
}
