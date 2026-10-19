package rclone

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jagottsicher/breakthrough/internal/config"
	"github.com/jagottsicher/breakthrough/internal/session"
)

// MountEntry is one configured cloud mount — deliberately just the
// rclone remote name and the local directory it mounts onto, plus the
// same optional Identifier/Tags remotefs.Connection already carries
// (see internal/ui's own connectdialog.go for the shared character
// set/normalization both now use). Carries nothing rclone's own config
// file doesn't already have — see this package's own doc comment on
// why no secret, and nothing else about the remote itself, ever lives
// here.
type MountEntry struct {
	RemoteName string
	Target     string

	Identifier string   `json:",omitempty"`
	Tags       []string `json:",omitempty"`
}

// cloudMountsFile is where LoadMountEntries/SaveMountEntries persist
// the configured mount list — alongside connections.json
// (internal/remotefs's own history.go), the same user config directory
// and the same reasoning for a dedicated JSON document rather than a
// key in the flat config file.
func cloudMountsFile() string {
	dir := config.UserDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "cloudmounts.json")
}

// LoadMountEntries reads the persisted cloud-mount list. A missing
// file is not an error — mirrors remotefs.LoadHistory's own "nothing
// configured yet" contract exactly.
func LoadMountEntries() ([]MountEntry, error) {
	path := cloudMountsFile()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []MountEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// SaveMountEntries writes entries back out — mirrors
// remotefs.SaveHistory's own permissions (0o600, directory 0o700).
func SaveMountEntries(entries []MountEntry) error {
	path := cloudMountsFile()
	if path == "" {
		return nil // no user config directory at all — nowhere safe to write
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// AddMountEntry appends entry, refusing a duplicate Target outright:
// two different cloud mounts can never share one local mountpoint, so
// this must never silently overwrite whichever one got there first —
// the same "no silent overwrite" rule this project applies to real
// file operations, here applied to its own configuration.
func AddMountEntry(entry MountEntry) error {
	entries, err := LoadMountEntries()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Target == entry.Target {
			return fmt.Errorf("rclone: %s is already configured as a cloud mount", entry.Target)
		}
	}
	return SaveMountEntries(append(entries, entry))
}

// RemoveMountEntry drops the entry for target — a no-op, not an
// error, if none exists, mirroring remotefs.RemoveFromHistory's own
// "absence isn't an error" contract.
func RemoveMountEntry(target string) error {
	entries, err := LoadMountEntries()
	if err != nil {
		return err
	}
	kept := entries[:0]
	for _, e := range entries {
		if e.Target != target {
			kept = append(kept, e)
		}
	}
	return SaveMountEntries(kept)
}

// DefaultMountBase is the computed base directory a cloud mount's own
// per-remote subdirectory is created under when
// config.Settings.CloudMountBaseDir is left blank: session.MountsDir()
// (XDG_DATA_HOME/breakthrough/mounts/<user>, mirroring where
// internal/fsops's own trash lives — see MountsDir's own doc comment
// on why "data," not "state" or "config," is the right tier for this)
// — a plain user-owned directory, never /mnt or /media, so creating it
// never needs root (see this package's own doc comment, and the
// user's own explicit decision that /mnt/media only get used when
// already writable, never force-created). Returns "" when MountsDir
// does (no home directory at all) — same "" sentinel contract
// StateDir/UserDir already follow.
func DefaultMountBase() string {
	dir, err := session.MountsDir()
	if err != nil {
		return ""
	}
	return dir
}

// MountBase resolves the actual base directory to use: settings' own
// CloudMountBaseDir if set, otherwise DefaultMountBase.
func MountBase(settings config.Settings) string {
	if settings.CloudMountBaseDir != "" {
		return settings.CloudMountBaseDir
	}
	return DefaultMountBase()
}

// MountDir joins base with name — the one place that decides a given
// remote's own mountpoint lives directly under the base directory, by
// name, so every caller (the Connect dialog building a brand-new
// entry, a later "reopen all configured mounts" pass) computes the
// exact same path for the same name.
func MountDir(base, name string) string {
	return filepath.Join(base, name)
}

// EnsureMountDir prepares target as a cloud mount's own mountpoint:
// creates it (0700) if it doesn't exist yet, or confirms it already
// exists as an empty directory. Deliberately refuses an existing,
// non-empty directory rather than mounting over it — a live cloud
// mount shadows whatever was already there underneath it for as long
// as it stays mounted, so silently doing that to a directory that
// already has real files in it is exactly the kind of silent
// data-hiding this project's own "keine stillen Fehler oder
// Datenverlust" rule exists to prevent. Also what makes a
// pre-created, root-owned directory under /mnt or /media safe to
// point at: if it already exists and is empty, this never touches its
// ownership/permissions at all, it's simply accepted as is.
func EnsureMountDir(target string) error {
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		return os.MkdirAll(target, 0o700)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("rclone: %s already exists and is not a directory", target)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("rclone: %s already exists and is not empty", target)
	}
	return nil
}
