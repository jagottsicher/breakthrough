package config

import (
	"path/filepath"
	"testing"
)

func TestUserDirPrefersXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg/home")

	got := UserDir()
	want := filepath.Join("/xdg/home", "breakthrough")
	if got != want {
		t.Errorf("UserDir() = %q, want %q", got, want)
	}
}

func TestUserDirFallsBackToDotConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/someone")

	got := UserDir()
	want := filepath.Join("/home/someone", ".config", "breakthrough")
	if got != want {
		t.Errorf("UserDir() = %q, want %q", got, want)
	}
}

func TestSystemPaths(t *testing.T) {
	if got, want := SystemDir(), "/etc/breakthrough"; got != want {
		t.Errorf("SystemDir() = %q, want %q", got, want)
	}
	if got, want := SystemConfigFile(), "/etc/breakthrough/config"; got != want {
		t.Errorf("SystemConfigFile() = %q, want %q", got, want)
	}
	if got, want := SystemColorSchemeDir(), "/etc/breakthrough/colorschemes"; got != want {
		t.Errorf("SystemColorSchemeDir() = %q, want %q", got, want)
	}
}

func TestUserPathsDeriveFromUserDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg/home")

	if got, want := UserConfigFile(), filepath.Join("/xdg/home", "breakthrough", "config"); got != want {
		t.Errorf("UserConfigFile() = %q, want %q", got, want)
	}
	if got, want := UserColorSchemeDir(), filepath.Join("/xdg/home", "breakthrough", "colorschemes"); got != want {
		t.Errorf("UserColorSchemeDir() = %q, want %q", got, want)
	}
}

// TestUserPathsEmptyWhenUserDirUnavailable pins the "" contract both
// UserConfigFile and UserColorSchemeDir document: with no XDG_CONFIG_HOME
// and no resolvable home directory, there's no user tier at all, and
// callers must be able to detect that rather than silently joining onto
// an empty base path.
func TestUserPathsEmptyWhenUserDirUnavailable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	// os.UserHomeDir on unix falls back to nothing further once HOME is
	// unset — matches what a locked-down service account might see.

	if got := UserDir(); got != "" {
		t.Skipf("UserDir() = %q, want \"\" — environment provides a home directory some other way, skipping", got)
	}
	if got := UserConfigFile(); got != "" {
		t.Errorf("UserConfigFile() = %q, want \"\" when UserDir() is unavailable", got)
	}
	if got := UserColorSchemeDir(); got != "" {
		t.Errorf("UserColorSchemeDir() = %q, want \"\" when UserDir() is unavailable", got)
	}
}
