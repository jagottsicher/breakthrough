package config

import (
	"os"
	"path/filepath"
)

// appDirName is this project's own subdirectory name under both the
// system config root and the user's XDG config home.
const appDirName = "breakthrough"

// systemDir is the system-wide defaults root, fixed per the project's
// own convention (see the package doc) — unlike UserDir, this isn't
// configurable via an environment variable.
const systemDir = "/etc/" + appDirName

// UserDir returns the user's own config directory: $XDG_CONFIG_HOME/
// breakthrough if XDG_CONFIG_HOME is set (the XDG Base Directory
// convention this project follows — see the package doc), otherwise
// ~/.config/breakthrough. Returns "" if neither is available (e.g.
// os.UserHomeDir fails) — callers treat that as "no user tier", not a
// fatal error, the same way a missing config file is treated elsewhere
// in this package.
func UserDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, appDirName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", appDirName)
}

// SystemDir returns the system-wide defaults root, /etc/breakthrough.
func SystemDir() string {
	return systemDir
}

// SystemConfigFile and UserConfigFile are the two tiers' own flat
// "key = value" files (see ParseFile/Load). UserConfigFile returns ""
// when UserDir does (see its own doc comment) — callers must check for
// that before using it as a path.
func SystemConfigFile() string {
	return filepath.Join(SystemDir(), "config")
}

func UserConfigFile() string {
	dir := UserDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config")
}

// SystemColorSchemeDir and UserColorSchemeDir are the two tiers'
// colorschemes/ subdirectories (see LoadColorSchemes). Same "" contract
// as UserConfigFile for the user tier.
func SystemColorSchemeDir() string {
	return filepath.Join(SystemDir(), "colorschemes")
}

func UserColorSchemeDir() string {
	dir := UserDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "colorschemes")
}
