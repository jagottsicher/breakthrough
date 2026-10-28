package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jagottsicher/breakthrough/internal/config"
)

// withFakeConsigliereSystemPromptPath installs path as
// consigliereSystemPromptPath for the duration of the test, restoring
// TestMain's own "" default afterwards.
func withFakeConsigliereSystemPromptPath(t *testing.T, path string) {
	t.Helper()
	previous := consigliereSystemPromptPath
	consigliereSystemPromptPath = func() string { return path }
	t.Cleanup(func() { consigliereSystemPromptPath = previous })
}

// Calling runEditor directly in a test never actually spawns an
// editor: app.Suspend is a no-op without a real screen behind r.app
// (see TestRunEditorSkipsReloadWhileSearchResultsShowing's own doc
// comment in bottombar_test.go for the identical reasoning) — so
// editConsigliereSystemPrompt is safe to call for real here, same as
// every other runEditor-reaching test in this package already does.

func TestEditConsigliereSystemPromptCreatesDefaultFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consigliere-system-prompt.txt")
	withFakeConsigliereSystemPromptPath(t, path)
	root := newPlainKeyRoot(t)

	root.editConsigliereSystemPrompt()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading created file: %v", err)
	}
	if string(data) != config.DefaultConsigliereSystemPrompt+"\n" {
		t.Errorf("file content = %q, want the default prompt", data)
	}
}

func TestEditConsigliereSystemPromptNeverTouchesAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consigliere-system-prompt.txt")
	const custom = "My own custom instructions."
	if err := os.WriteFile(path, []byte(custom), 0o600); err != nil {
		t.Fatalf("writing custom prompt: %v", err)
	}
	withFakeConsigliereSystemPromptPath(t, path)
	root := newPlainKeyRoot(t)

	root.editConsigliereSystemPrompt()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if string(data) != custom {
		t.Errorf("file content = %q, want the existing custom prompt untouched", data)
	}
}

func TestResetConsigliereSystemPromptOverwritesACustomPromptAfterConfirm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consigliere-system-prompt.txt")
	if err := os.WriteFile(path, []byte("My own custom instructions."), 0o600); err != nil {
		t.Fatalf("writing custom prompt: %v", err)
	}
	withFakeConsigliereSystemPromptPath(t, path)
	root := newPlainKeyRoot(t)

	root.resetConsigliereSystemPrompt()
	if root.pendingConfirm == nil {
		t.Fatal("resetConsigliereSystemPrompt should have opened a confirm dialog")
	}
	root.pendingConfirm()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if string(data) != config.DefaultConsigliereSystemPrompt+"\n" {
		t.Errorf("file content = %q, want it reset to the default", data)
	}
}
