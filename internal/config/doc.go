// Package config implements breakthrough's two-tier settings layer: a
// system-wide default file (/etc/breakthrough/config) merged with a
// per-user override (XDG_CONFIG_HOME or ~/.config/breakthrough/config).
// The on-disk format is a flat "key = value" file with "#" comments,
// intentionally simple and dependency-free. If nested structures (e.g.
// keybinding groups) become necessary, that's a signal to reconsider the
// format (e.g. TOML) explicitly, rather than growing this parser further.
//
// Color schemes are the one deliberate exception to that flat format:
// each scheme is its own JSON file under a colorschemes/ subdirectory of
// either tier (see LoadColorSchemes), selected by the flat file's own
// color_scheme key (see Settings). JSON was chosen specifically for
// schemes — a genuinely nested, multi-field structure, exactly the case
// the flat format's own doc comment above already anticipates — not as a
// silent departure from the flat-file decision for settings in general.
package config
