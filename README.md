[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-brightgreen.svg)](https://opensource.org/licenses/Apache-2.0)
[![CI (Linux)](https://github.com/jagottsicher/breakthrough/actions/workflows/test-linux.yml/badge.svg)](https://github.com/jagottsicher/breakthrough/actions/workflows/test-linux.yml)
[![CI (Linux ARM64)](https://github.com/jagottsicher/breakthrough/actions/workflows/test-linux-arm.yml/badge.svg)](https://github.com/jagottsicher/breakthrough/actions/workflows/test-linux-arm.yml)
[![CI (macOS)](https://github.com/jagottsicher/breakthrough/actions/workflows/test-macos.yml/badge.svg)](https://github.com/jagottsicher/breakthrough/actions/workflows/test-macos.yml)

# breakthrough
## A window/menu-based TUI file manager for your POSIX-compliant terminal, written in Go.

breakthrough puts a quasi-windows-like layer — a panel, floating overlays,
context menus, a bottom bar — over your terminal session, without leaving
the shell. It's built with [tcell](https://github.com/gdamore/tcell) and
[tview](https://github.com/rivo/tview); it is not integrated into Bash
itself. The shell connection stays a thin wrapper (a command line and a
handful of quick actions), never code integration into Bash.

It is explicitly not an attempt to rebuild Midnight Commander — the goal is
its own UX philosophy, closer to classic GUI file managers, just in the
terminal.

## Features

- Panel-based directory browsing: arrow-key and mouse navigation, a
  clickable path breadcrumb bar with Start/Home/Back/Forward, sortable
  Name/Size/Modified columns, and file-type indicators (directory,
  symlink — including broken and multi-hop chains, socket, FIFO, device,
  mount point, hard link) matching Midnight Commander's own glyph scheme.
- A live filter, right in the top row: type to narrow the listing on
  every keystroke, with a Glob/Regex toggle for how the pattern is
  interpreted.
- A right-click context menu: an editable Properties view (name,
  permissions — click a bit or type the octal value directly, owner and
  group via a scrollable picker of every local user/group, modified
  date and time), Rename, checkbox-based multi-selection (including
  glob-pattern Select +/-), Copy/Cut/Paste, chmod, and chown.
- A bottom bar: a real shell command line (with its own history, shared
  with your regular shell's `$HISTFILE`, and `cd` handled directly
  rather than uselessly changing a subshell's own directory) plus quick
  actions — Edit (`^E`, opens `$VISUAL`/`$EDITOR` on the selected file),
  Rename (`^R`), toggle hidden files (`^G`), Settings (`^X`) — alongside
  the current user, disk usage for the directory on screen, and a clock.
- Color schemes: JSON files under `colorschemes/` in either config tier
  (see below), switchable live from the Settings overlay (`^X` or the
  bottom bar's own button) — no restart needed, and the pick is
  remembered for next time.

## Status

Actively developed. The single-panel core above is functional and
tested; a second panel, a session-scoped trash, and the rest of the
settings layer beyond color schemes are planned next — see
[docs/whitepaper.md](docs/whitepaper.md) for the full concept and
vision, and follow along or join in on
[Discussions](https://github.com/jagottsicher/breakthrough/discussions).

## Color schemes

breakthrough ships with one built-in scheme ("Default") and reads
further ones from `colorschemes/*.json` in either config tier —
`/etc/breakthrough/colorschemes/` for system-wide schemes,
`~/.config/breakthrough/colorschemes/` (or `$XDG_CONFIG_HOME/breakthrough/colorschemes/`
if set) for your own; a user file with the same name replaces a system
one. Switch between whatever's found via the Settings overlay (`^X`, or
its button in the bottom bar) — the pick applies immediately and is
saved to your own `~/.config/breakthrough/config`.

Two ready-made examples ship in [`examples/colorschemes/`](examples/colorschemes/)
— a dark Solarized-based scheme and a light one:

```sh
mkdir -p ~/.config/breakthrough/colorschemes
cp examples/colorschemes/*.json ~/.config/breakthrough/colorschemes/
```

A scheme file only needs to set the fields it actually wants to change —
anything left out falls back to the Default scheme's own value. Each
color is either a `#rrggbb` hex value or a
[W3C color name](https://pkg.go.dev/github.com/gdamore/tcell/v2#pkg-variables)
(e.g. `"darkslategray"`). See
[`examples/colorschemes/solarized.json`](examples/colorschemes/solarized.json)
for every field a scheme can set.

## Installing

Download a release for your platform from the
[Releases page](https://github.com/jagottsicher/breakthrough/releases) —
a `.tar.gz` archive for Linux (x86_64, arm64), macOS (Intel, Apple
Silicon), and FreeBSD (x86_64), or a `.deb`/`.rpm` package on Linux.
Verify a download against the release's `checksums.txt` with
`sha256sum -c`.

Or build it yourself — see [CONTRIBUTING.md](CONTRIBUTING.md) for the
full local setup.

## Contributing

Contributions are very welcome — this is an ambitious project for one
person alone. See [CONTRIBUTING.md](CONTRIBUTING.md) for local setup, the
branch workflow, and code style expectations.

## Support

The best way to support breakthrough is to participate and contribute. If
you'd like to leave a tip instead, you can send a few Satoshis via the
Lightning Network ⚡️ using the sponsor button above.
