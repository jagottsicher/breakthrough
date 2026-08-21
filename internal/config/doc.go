// Package config implements breakthrough's two-tier settings layer: a
// system-wide default file (/etc/breakthrough/config) merged with a
// per-user override (XDG_CONFIG_HOME or ~/.config/breakthrough/config).
// The on-disk format is a flat "key = value" file with "#" comments,
// intentionally simple and dependency-free. If nested structures (e.g.
// keybinding groups) become necessary, that's a signal to reconsider the
// format (e.g. TOML) explicitly, rather than growing this parser further.
package config
