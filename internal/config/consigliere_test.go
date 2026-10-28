package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureConsigliereSystemPromptFileCreatesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consigliere-system-prompt.txt")

	if err := EnsureConsigliereSystemPromptFile(path); err != nil {
		t.Fatalf("EnsureConsigliereSystemPromptFile: %v", err)
	}

	got := LoadConsigliereSystemPrompt(path)
	if got != DefaultConsigliereSystemPrompt {
		t.Errorf("LoadConsigliereSystemPrompt = %q, want the default", got)
	}
}

func TestEnsureConsigliereSystemPromptFileNeverTouchesAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "consigliere-system-prompt.txt")
	const custom = "Be extremely terse."
	if err := os.WriteFile(path, []byte(custom), 0o600); err != nil {
		t.Fatalf("writing custom prompt: %v", err)
	}

	if err := EnsureConsigliereSystemPromptFile(path); err != nil {
		t.Fatalf("EnsureConsigliereSystemPromptFile: %v", err)
	}

	got := LoadConsigliereSystemPrompt(path)
	if got != custom {
		t.Errorf("LoadConsigliereSystemPrompt = %q, want the existing custom prompt untouched", got)
	}
}

func TestLoadConsigliereSystemPromptFallsBackToDefault(t *testing.T) {
	if got := LoadConsigliereSystemPrompt(""); got != DefaultConsigliereSystemPrompt {
		t.Errorf("LoadConsigliereSystemPrompt(\"\") = %q, want the default", got)
	}

	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")
	if got := LoadConsigliereSystemPrompt(missing); got != DefaultConsigliereSystemPrompt {
		t.Errorf("LoadConsigliereSystemPrompt(missing) = %q, want the default", got)
	}

	blank := filepath.Join(t.TempDir(), "blank.txt")
	if err := os.WriteFile(blank, []byte("   \n\n"), 0o600); err != nil {
		t.Fatalf("writing blank file: %v", err)
	}
	if got := LoadConsigliereSystemPrompt(blank); got != DefaultConsigliereSystemPrompt {
		t.Errorf("LoadConsigliereSystemPrompt(blank) = %q, want the default", got)
	}
}
