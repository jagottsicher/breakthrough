package multiplex

import (
	"os/exec"
	"testing"
)

func TestScreenInstalledReflectsLookPath(t *testing.T) {
	isolateBinaries(t, true, false)
	if !ScreenInstalled() {
		t.Error("ScreenInstalled() = false, want true")
	}
	if TmuxInstalled() {
		t.Error("TmuxInstalled() = true, want false")
	}
}

func TestScreenInstalledFalseWhenLookPathFails(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	if ScreenInstalled() {
		t.Error("ScreenInstalled() = true, want false")
	}
	if TmuxInstalled() {
		t.Error("TmuxInstalled() = true, want false")
	}
}
