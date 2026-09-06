# Installing breakthrough

The [README's own Installing section](../README.md#installing) has the
short version — the copy-paste commands for each package format. This
page covers the rest: what your terminal needs, exactly which files end
up where, how to run breakthrough without installing it at all, and how
to remove it again.

## Contents

- [Supported platforms](#supported-platforms)
- [Terminal requirements](#terminal-requirements)
- [What gets installed where](#what-gets-installed-where)
- [Files breakthrough creates while running](#files-breakthrough-creates-while-running)
- [System-wide defaults for administrators](#system-wide-defaults-for-administrators)
- [Optional external tools](#optional-external-tools)
- [Running without installing](#running-without-installing)
- [Upgrading](#upgrading)
- [Uninstalling](#uninstalling)
- [Troubleshooting](#troubleshooting)

## Supported platforms

Prebuilt binaries are published for these five targets:

| `GOOS`/`GOARCH` | Typical hardware |
|---|---|
| `linux/amd64` | Ordinary PCs and servers (x86_64) |
| `linux/arm64` | Raspberry Pi 4/5 in 64-bit mode, ARM servers, AWS Graviton, Apple Silicon under Linux |
| `darwin/amd64` | Intel Macs |
| `darwin/arm64` | Apple Silicon Macs (M1 and later) |
| `freebsd/amd64` | FreeBSD on x86_64 |

Every one of them is a single statically linked binary built with
`CGO_ENABLED=0`. There are no shared-library dependencies, no runtime,
and nothing to install alongside it — the binary that works today keeps
working across a libc or distribution upgrade.

Anything else Go itself can target builds from source in one command
(see [Building from source](../README.md#building-from-source)):
32-bit x86 and ARM, OpenBSD, NetBSD, and so on. These are not published
as releases only because they are not regularly tested, not because
anything in breakthrough is specific to the five above.

Windows is not supported. breakthrough is built around POSIX filesystem
semantics (permission bits, owner/group, symlinks) and shells out to
real POSIX tools; WSL is the way to run it on a Windows machine.

## Terminal requirements

breakthrough asks very little of a terminal, and degrades rather than
breaking when something is missing:

- **256 colors or true color** for the color schemes to look as
  intended. On a 16-color terminal it still runs, with the nearest
  available colors.
- **UTF-8** for the file-type glyphs, box-drawing characters and the
  `◫`/`✕`/`●` control glyphs. Check with `locale` — you want a `UTF-8`
  suffix on `LC_CTYPE`.
- **Mouse reporting** (SGR extended mode) for clicking, the context
  menu and drag-selection. Practically every terminal from the last
  decade has it. It is entirely optional: every mouse action has a
  keyboard equivalent, by design, because a lot of this program's use
  happens over SSH. `F3` toggles mouse reporting off when you want your
  terminal's own text selection back.
- **An enhanced keyboard protocol** (kitty's CSI-u or xterm's
  `modifyOtherKeys`) *only* for `Ctrl`+digit and `Ctrl`+`Tab`. Terminals
  that don't implement either — several VTE-based ones among them —
  simply never deliver those combinations. Every feature they reach has
  a second path that needs no such support: `Alt`+digit for the same
  tabs, and `F4` for the tab switcher.

`tmux` and `screen` are fully supported, including mouse reporting
(`set -g mouse on` in tmux).

## What gets installed where

### `.deb` / `.rpm`

| Path | Purpose |
|---|---|
| `/usr/bin/breakthrough` | the binary |
| `/etc/breakthrough/config` | system-wide settings, fully commented, all values inactive |
| `/etc/breakthrough/colorschemes/` | empty directory for system-wide color schemes |

`/etc/breakthrough/config` is registered as a conffile (Debian) and
`%config(noreplace)` (RPM), so an administrator's edits survive package
upgrades instead of being overwritten by a newer template.

### `.tar.gz`

The archive contains the binary plus `LICENSE`, `NOTICE` and
`README.md`. Nothing is placed anywhere for you — where the binary goes
is your choice (`/usr/local/bin` and `~/.local/bin` are the usual two).
No `/etc/breakthrough` is created; breakthrough runs perfectly well
without one, falling back to its built-in defaults, and the Options
screen's own "Edit config file" button creates your personal config on
first use.

## Files breakthrough creates while running

All of them follow the XDG Base Directory specification, and all are
created lazily — on first actual use, not at startup — regardless of how
breakthrough was installed:

| Path | Created when | Contents |
|---|---|---|
| `~/.config/breakthrough/config` | you change a setting, or use "Edit config file" | your own settings, overriding the system tier |
| `~/.config/breakthrough/colorschemes/` | you use "New color scheme" | your own color schemes |
| `~/.local/state/breakthrough/tabs` | you quit cleanly | which tabs and split were open, for the next start (mode `0600`) |
| `~/.local/state/breakthrough/crash.log` | a recovered panic occurs | the panic and its traceback, for a bug report |
| `~/.local/state/breakthrough/debug.log` | you pass `--debug` | everything the program writes to stderr that run |
| `~/.local/share/breakthrough/trash/` | you first move something to the trash | the persistent trash, with `files/` and `info/` subdirectories |
| `$XDG_RUNTIME_DIR/breakthrough/trash/` | as above, with `trash_persistent = false` | the session-scoped trash, discarded when your login session ends |

`$XDG_CONFIG_HOME`, `$XDG_STATE_HOME` and `$XDG_DATA_HOME` are honored
where set, and the paths above are what those default to when they
aren't.

breakthrough also appends to your shell history file — `$HISTFILE` where
set, `~/.bash_history` otherwise — for commands run in its own command
line, so they show up in your normal shell history too.

Nothing is written outside these paths, and nothing phones home.

## System-wide defaults for administrators

The two config tiers are merged at startup, key by key: a value in
`~/.config/breakthrough/config` beats the same key in
`/etc/breakthrough/config`, which beats breakthrough's own built-in
default. A key that appears in neither file simply uses the default —
there is no need to list every setting in either.

That makes rolling breakthrough out across a fleet straightforward: ship
one `/etc/breakthrough/config` with your house defaults via Ansible,
Puppet, or whatever you already use, and leave every user free to
override individual keys for themselves. The Options screen's own
"Reset" removes a user's override rather than writing a default over it,
so a reset falls back to *your* system-wide value, not to breakthrough's.

The packaged `/etc/breakthrough/config` is generated from the same code
that documents the settings inside the application, so it can never
list a key breakthrough doesn't actually recognize. See the
[settings reference](user-guide.md#settings-reference) for every key.

## Optional external tools

breakthrough deliberately calls real POSIX tools rather than
reimplementing them — a sysadmin already knows exactly how these behave.
None of them is required to start; each only affects one feature:

| Tool | Used for | Without it |
|---|---|---|
| `find`, `grep` | Search | Search is unusable — these are effectively required in practice |
| `locate` | faster indexed filename search | Search falls back to `find` |
| `zgrep`, `zipgrep` | searching inside gzip/zip files | those archives are skipped |
| `sed` | Sed Replace | that dialog can't run |
| `du`, `df` | directory sizes, status-bar disk usage | those readings are omitted |
| `tail` | "Tail -f" | that menu entry can't run |
| `pdftoppm` ([poppler-utils](https://poppler.freedesktop.org/)) | rendering PDF pages as images | PDFs open as extracted text instead, still fully in Go |
| `bat`, `less`, `$PAGER` | `pager = external` | only relevant if you set that; the built-in viewer needs nothing |

Image viewing, syntax highlighting, PDF text extraction and hashing are
all pure Go and need nothing installed.

## Running without installing

The binary is self-contained, so it runs from wherever it happens to be:

```sh
tar xzf breakthrough_0.14.0_linux_amd64.tar.gz
./breakthrough /some/directory
```

Useful for trying it on a machine you'd rather not install anything on,
or for keeping a copy on a USB stick. It still reads and writes the
config and trash paths above, under the invoking user's own home
directory.

Starting with an explicit directory argument, as above, also suppresses
the saved-tab restore for that run — an explicit path is an unambiguous
instruction about where to open.

## Upgrading

- **`.deb` / `.rpm`**: install the newer package the same way you
  installed the first one. Your `/etc/breakthrough/config` is kept.
- **`.tar.gz`**: overwrite the binary. Nothing else changes.

Your settings, color schemes, saved tabs and trash all live outside the
installed files and are untouched by an upgrade.

Check what you're actually running with:

```sh
breakthrough --version
# breakthrough 0.14.0 (commit 1a2b3c4, built 2026-08-28T20:11:03Z by goreleaser)
```

## Uninstalling

```sh
sudo apt remove breakthrough        # Debian/Ubuntu
sudo dnf remove breakthrough        # Fedora/RHEL
sudo rm /usr/local/bin/breakthrough # tar.gz install
```

That leaves your own files alone. To remove those too:

```sh
rm -rf ~/.config/breakthrough ~/.local/state/breakthrough ~/.local/share/breakthrough
```

The middle one is only the saved tab layout; the last one contains your
trash, so check it first if you might still want something out of it.

## Troubleshooting

**Colors look wrong or washed out.** Your terminal is probably not in
256-color mode. Check `echo $TERM` — `xterm-256color` or better is what
you want, and inside tmux, `set -g default-terminal "tmux-256color"`.

**Glyphs show as boxes or question marks.** The locale isn't UTF-8, or
the font lacks the box-drawing/geometric ranges. `locale` will tell you
the first; most monospace fonts intended for terminals cover the second.

**Clicking does nothing.** Mouse reporting is off or unsupported. `F3`
toggles it; the status bar shows "Mouse on"/"Mouse off". Inside tmux,
`set -g mouse on`.

**`Ctrl`+digit or `Ctrl`+`Tab` do nothing.** Your terminal can't report
those combinations — see [Terminal requirements](#terminal-requirements).
Use `Alt`+digit or `F4` instead; both are bound to the same features
precisely for this case.

**Nothing happens on a key that should work.** The command line at the
bottom takes priority over global shortcuts while it has focus, since it
needs those same keys for readline-style editing. Press `Escape` or
click the panel to get out of it.

**A setting won't stick.** breakthrough writes to
`~/.config/breakthrough/config`; if `$XDG_CONFIG_HOME` points somewhere
unwritable, the save is reported as an error but the value still applies
for the session. Check the Options screen's own info window (`?` on a
setting) — it names which tier the current value came from.
