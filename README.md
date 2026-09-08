[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-2db94d.svg)](https://opensource.org/licenses/Apache-2.0) [![CI (Linux)](https://github.com/jagottsicher/breakthrough/actions/workflows/test-linux.yml/badge.svg)](https://github.com/jagottsicher/breakthrough/actions/workflows/test-linux.yml)
[![CI (Linux ARM64)](https://github.com/jagottsicher/breakthrough/actions/workflows/test-linux-arm.yml/badge.svg)](https://github.com/jagottsicher/breakthrough/actions/workflows/test-linux-arm.yml)
[![CI (macOS)](https://github.com/jagottsicher/breakthrough/actions/workflows/test-macos.yml/badge.svg)](https://github.com/jagottsicher/breakthrough/actions/workflows/test-macos.yml)

# breakthrough
## A mouse-and-menu-driven TUI file manager for your POSIX-compliant terminal, built around a real embedded bash shell — written in Go.

![Hero Picture](docs/images/20260830_000058_01B543A0-5FB0-4374-B58D-31F07F11333F.jpg)

breakthrough puts classic GUI file-manager idioms — a browsable panel,
context menus, floating dialogs, clickable breadcrumbs — over a real bash
session, without ever leaving the shell. It's built with
[tcell](https://github.com/gdamore/tcell) and
[tview](https://github.com/rivo/tview); it is not integrated into Bash
itself — no fork, no patch, no hook into Bash's own internals. Its console
is a first-class citizen all the same: its own history, path completion
against the current directory, multi-line scripting, and every command
running through your actual `$SHELL`, started the same way an
interactive login shell would be — so it sources that shell's own
startup files and expands its aliases, whichever shell that actually is
— not a reimplementation of one.

![screenshot](https://github.com/jagottsicher/breakthrough/blob/develop/docs/images/Bildschirmfoto%20vom%202026-08-30%2022-46-12.png)

It is explicitly not an attempt to rebuild Midnight Commander — the goal is
its own UX philosophy, closer to classic GUI file managers, just in the
terminal.

## Features

- Panel-based directory browsing: arrow-key and mouse navigation, a
  clickable path breadcrumb bar with Start/Home/Back/Forward — its
  history covers a trip into search results or the trash exactly the
  same as a real directory, and Back/Forward into any of them restores
  the cursor row it was left on, a search's own results included, shown
  again exactly as they were rather than a live re-run — plus Reload
  (`⭯`, right before the path itself), re-reading the current directory
  straight from disk for anything this app has no other way to notice
  on its own: another process changing files underneath it, a
  network/mounted filesystem's own content changing, and so on —
  sortable
  Name/Size/Modified columns, and file-type indicators (directory,
  symlink — including broken and multi-hop chains, socket, FIFO, device,
  mount point, hard link) matching Midnight Commander's own glyph scheme.
  Every entry Enter can navigate into — a directory, a symlink to one, a
  mount point, or `..` itself — gets its own name highlighted dark
  yellow (just the name, not the trailing `/` or a symlink's `-> target`
  arrow), so folders stand out from plain files at a glance. Beyond
  that, a name's own text color also tells them apart: green for
  executable, red for a broken symlink, a darker red for anything the
  current user can't actually read (checked with a real permission
  check, not just Mode's bits — a `/proc` entry included), cyan for a
  working symlink to a file, orange for a socket/FIFO/device, magenta
  for a recognized archive extension, and a dim gray for a dotfile/
  dotdir (unless one of the other cases above already applies).
- Columns that never lie: Size and Modified take exactly the width their
  content needs — six columns for human-readable sizes, ten for a Unix
  timestamp, nineteen for a full date — and the name column absorbs the
  difference. Switching either format reflows them immediately, and the
  column header is simply "mtime", short enough never to constrain the
  column. When something has to give
  it is the filename, shortened in the middle (`annual-re…final.pdf`) so
  both the identifying start and the extension survive. This matters most
  in split view, where each pane is half as wide: before, Size collapsed
  to `…` and Modified disappeared off the right edge entirely.
- A live filter, right in the top row: type to narrow the listing on
  every keystroke, with a Glob/Regex toggle for how the pattern is
  interpreted.
- Tabs: several directories open at once in one window, each keeping its
  own history, filter, sort order, selection and cursor position, so
  switching away and back leaves everything exactly as it was. `^1`
  through `^0` (or `Alt+1`...`Alt+0` — bound to the same tabs, for
  terminals that can't report one or the other) jump straight to a tab,
  `^Tab`/`^Shift+Tab` step through a switcher showing every tab's full
  path. `t`/`^T` opens the same switcher without moving, and works
  everywhere the others might not. A
  compact numbered strip sits beside the filter — numbers rather than paths, so
  the header row's width stays put as you navigate — and shrinks to just
  a "+" while only one tab is open. The layout is saved on exit and
  reopened next time, unless a directory was named on the command line
  or `restore_tabs = false` turns it off.
- Split view (`s`): two of those tabs on screen at once, side by side
  or stacked (`z` then `o` flips it, or set `split_stacked` once and
  forget it). With a single tab open, `s` opens a second one on the same
  directory — the usual starting point for copying between two places in
  one tree; otherwise it pairs you with the tab you last split with. To
  choose the other pane yourself, the tab list (`t`) gives every row a `◫` button,
  including its "+ New tab" row, which creates a tab and splits with it
  in one step. `Tab`, or a click, moves between the panes, and the one
  you are in keeps the keyboard, the context menu and every shortcut;
  the highlighted row shows which that is. Switching tabs while split
  replaces only the pane you're in — the panes never swap sides. The
  split is saved and reopened alongside the tabs. See
  [docs/user-guide.md](docs/user-guide.md#split-view) for the details.
- A primary keyboard layer of plain letters — `c`/`x`/`v` copy/cut/paste,
  `d`/`D` Trash/remove permanently, `r` rename, `i`/`I` Properties/
  Details, `s`/`S` split/swap panes, and more — active only while the
  file listing itself has keyboard focus, never while typing in the
  filter box, the path editor, or the command line. No modifier key
  means no terminal/layout/multiplexer to trip over — the same approach
  ranger, nnn, lf and vifm all settled on. Related, rarer actions sit
  behind a chord (a letter, then one more within about four seconds,
  with a countdown in the status bar and a clickable legend in the
  button bar): `g` to jump somewhere (`gg` top,
  `gh` home, `gr` `/`, `gb` Trash), `p` for permissions (`pm` chmod,
  `po` chown), `z` for display toggles (`zs` size format, `zt` time
  format, `zo` split orientation, `zw` swap panes), `o` for Options
  (`oo` the screen itself, `om` mouse reporting on/off — quick, direct
  toggles without opening the screen at all). See
  [docs/user-guide.md](docs/user-guide.md#the-keyboard-layer).
- No function keys anywhere in the application, and — as of the latest
  round — no Ctrl-letter bindings either, apart from the two things a
  plain letter genuinely cannot do (quit/cancel from literally anywhere,
  even mid-edit) and one narrow extra reach for the tab switcher
  (`^T` alongside `t`, for walking to the next tab while the switcher
  itself is already open). Mouse reporting's own former `^_` — a
  single, narrow toggle that fired even with a dialog open or a text
  field focused, since a Ctrl combination safely can where a plain
  letter never can — was deliberately retired anyway in favor of `om`
  above, a considered trade-off rather than an oversight.
- A context menu on `m` or right-click, showing only what actually
  applies right now rather than a fixed list of everything it can ever
  do: Look, Edit (dropped for a directory), Rename, Copy/Cut/Paste
  (Paste only once the clipboard has something in it), Move to Trash,
  Properties (editable — name, permissions, click a bit or type the
  octal value directly, owner and group via a scrollable picker of
  every local user/group, modified date and time), plus three `▸`
  submenus that replace the list in place when chosen (Windows
  Explorer's own cascading-menu idea, without needing room to open
  beside it): "More actions" (`tail -f`, chown, chmod, Sed Replace,
  Batch rename, Undo last rename, Remove), "Selection" (Select
  all/Deselect all/glob-pattern Select +/-), and "Tabs & Split" (New/
  close tab, Switch tab..., Split on/off, orientation and Swap panes —
  the last two only once a split actually exists). `◂ Back`, `Escape`,
  or Left arrow step back out one level at a time. Browsing the Trash
  itself replaces the whole menu with just Restore/Empty Trash/
  Properties, since almost nothing else still applies there.
- Copy/Cut/Paste (`c`/`x`/`v`, or the context menu): works on the whole
  current selection, not just one file. Paste runs in the background —
  a file that already exists at the destination opens a small dialog
  (Overwrite, Skip, an "all" variant of each for the rest of this
  Paste, or apply "only if the source is newer"/"only if the source
  isn't empty" to every conflict it still runs into) without blocking
  anything else in the same Paste: whatever doesn't conflict keeps
  copying/moving while that dialog is up, and a second conflict found
  before the first is answered queues behind it — shown as "(N more
  waiting)" right in the dialog's own message — rather than stacking a
  second dialog on top. Any real failure (permission, a full disk, ...)
  is collected rather than stopping at the first one, and reported once
  the whole Paste is done. Whatever's currently on the clipboard shows
  two ways: every row it holds gets a full-row grey tint (a lighter
  shade for Cut than Copy, since Cut is the one where the original
  actually disappears), across every open tab showing that row, not
  just the one Copy/Cut was pressed in; and the status bar names it —
  "Copy: 3 files, 1 dir" or "Cut: ..." — right after the button-bar
  chord countdown's own spot, for as long as there's something to
  Paste.
- Move to Trash / Remove: `d` or Entf moves the current selection to
  your own trash — recursively for a directory, no confirmation, since
  that's the reversible action by design. `D`, Ctrl+Entf (best-effort —
  terminal-dependent; `D` is always the reliable one), or the context
  menu's "Remove" permanently deletes instead (a file like `rm`, a
  directory recursively like `rm -rf`, empty or not), always behind a
  confirmation dialog with Cancel preselected — a single stray keypress
  can never confirm it by itself. "Go to Trash" (the `g` chord's own
  `gb`) jumps straight into it without needing to
  know its path; "Restore from Trash" (`r`, while browsing it) and
  "Empty Trash" (`D`, same confirmation) round
  it out. Persistent by default — lives under
  `~/.local/share/breakthrough/trash`, so it's still there tomorrow, even
  across a login session boundary — or session-scoped via
  `trash_persistent = false` in your config, under `$XDG_RUNTIME_DIR`
  instead, gone once the session ends; running as root (e.g. via `sudo`)
  always gets the persistent path regardless, since root has no real
  session of its own for a session-scoped trash to mean anything for.
  Kept from growing forever by two settings checked once at startup, not
  on every single trash operation: `trash_max_age_days` (30 by default —
  anything older is removed unconditionally) and `trash_quota_percent`
  (10 by default — a backstop, oldest item first, only if age alone
  didn't already bring the trash back under that share of the
  filesystem it lives on); either one is `0` to disable it. Anything
  actually removed this way is reported once, on the next start.
  Browsing the trash itself ("Go to Trash") shows each item's
  own original path in place of its real on-disk name (a collision-
  avoidance hash you'd otherwise have to squint past — two files
  trashed from the very same location, more than once, still stay
  distinguishable this way) and labels the Modified column "Deletion
  time" instead — both, like the Modified column always has, respecting
  the Options overlay's timestamp-vs-formatted toggle and the column's
  own sort.
- Sed Replace (`E`, or the context menu): runs a real `sed(1)`
  substitution against the current selection — one file or several, not
  a directory tree. A guided Find/Replace pair (Regex, Extended regex
  `-E`, Case-insensitive, and Replace-all-per-line toggles) builds the
  script for you, or drop straight into the advanced field and write the
  sed script yourself for anything real sed can do — address ranges,
  multiple commands, backreferences. Always previews first, as a
  Name/Line/Excerpt table — one row per changed line, skipped files
  listed with why — computed in the background with a live "Checking N
  of M" status; only Apply, behind the same Cancel-preselected
  confirmation, actually writes, optionally keeping a `.bak` of each
  original first. Never uses sed's own `-i`: GNU and BSD/macOS sed take
  incompatible arguments for it, so this always runs sed as a plain
  filter and writes the result back itself.
- Batch rename (context menu): renames a whole selection through a fixed
  pipeline of steps — Search & Replace (literal or regex), Case
  (UPPER/lower/Title/Sentence), Trim (drop N characters off either end),
  Numbering (a zero-padded counter as prefix or suffix), and Extension
  (lower/upper/remove/replace) — with the steps listed down the left and
  the selected one's own settings on the right. A step left alone does
  nothing; there is no separate on/off switch to also remember. Search &
  Replace and Case only ever touch the name, never the extension, and
  Extension only ever touches the extension, so a case transform can't
  quietly rewrite `.JPG` behind your back. The whole selection is
  previewed live, old name beside new, updated on every keystroke rather
  than behind a "Preview" button — unchanged rows dimmed, and any
  collision (two files landing on the same new name, or a name already
  taken on disk) shown in red with the reason, right where it would
  happen. Nothing is written until Rename is confirmed, and "Undo last
  rename" reverses the whole batch afterwards. See
  [docs/user-guide.md](docs/user-guide.md#batch-rename) for the step
  reference.
- Three rows below the panel, each with its own job. First, a real
  shell command line (with its own history — shared with `$HISTFILE` if
  you've set it, `~/.bash_history` otherwise regardless of your actual
  shell, a deliberate choice so this line's own history always behaves
  like a bash one's — and `cd` handled directly rather than uselessly
  changing a subshell's own directory), which expands when clicked
  into — full width, no prompt, growing upward toward mid-screen with a
  "Bash Prompt Editor" legend above it — for
  multi-line bash scripting (Enter runs the buffer, Ctrl+J or Alt+Enter
  inserts a newline instead — Ctrl+J always works, Alt+Enter is
  intercepted by some terminal emulators for their own use; Up/Down
  recall history too, not just Ctrl+P/Ctrl+N, except at a line a
  multi-line script is still being composed on; Tab completes the
  filename/directory at the cursor against the panel's own current
  directory, or, when several matches agree on nothing further, opens a
  scrollable pick list of them instead of doing nothing). Every command
  runs through your actual `$SHELL`, started the same way an
  interactive login shell would be — that shell's own startup files,
  aliases and functions sourced first, so something like `ll` works
  exactly as it does in a real terminal — the same way Midnight
  Commander's own command line handles every command — no attempt to
  guess which programs need one and which don't. Its own output stays on
  screen until you press Escape to return, so it doesn't just flash by.
- A middle row, always visible right below the command line, showing a
  curated subset of the keyboard layer's own letters as a quick legend —
  each key set off in its own petrol background, one space either side,
  so the letter-to-action mapping reads at a glance: Copy (`c`), Cut
  (`x`), Paste (`v`), Move to Trash (`d`), toggle hidden files (`.` —
  labeled Hide or Unhide, whichever it would do next, not whichever
  state you're currently in), Properties (`i`), Details sidebar (`I`),
  context menu (`m`), Split view (`s`), the tab switcher (`t`), Look
  (`l`), and Help (`?`), plus the three chord families marked with an
  ellipsis to show they lead to more keys (`g…` go, `p…` permissions,
  `z…` display toggles) — every member of an open chord's own legend is
  clickable too, the same highlighted-key treatment, so pointing at one
  works as well as typing its second letter. A few of these change
  meaning while actually browsing the
  trash itself: `d` asks to remove permanently instead of moving
  something already-trashed to the trash again, `r` (not shown in this
  row, but still fully live) restores instead of renaming, and `D`
  empties the whole trash instead of removing just the selection — the
  button labels themselves stay put either way, since a bar that changed
  shape underfoot would be its own kind of confusing. Every entry here
  is also reachable from the context menu
  and fully documented (including everything that doesn't fit this one
  row) in the in-app help (`?`), and each still works the same way
  whichever panel or field currently has focus, except while the command
  line itself is expanded and needs those same keys for its own editing.
  Hidden-files/size-format/mtime-format toggles are remembered across
  restarts.
- A bottom row that's purely informational, no buttons on it at all: the
  current user, disk and inode usage for the directory on screen, the
  running kernel (`uname -r`), uptime and load average where the
  platform exposes them (Linux's own `/proc/uptime` and
  `/proc/loadavg` — quietly omitted elsewhere rather than shown wrong),
  and a clock.
- Color schemes: JSON files under `colorschemes/` in either config tier
  (see below), switchable live from the Options screen (the `o` chord's
  own `oo`) — no restart needed, and the pick is remembered for next
  time.
- Search (`f`): by file name (glob, a
  plain keyword, or regex — via `find`, or `locate` where its own index
  is available) or by file content (`grep`, and — where installed —
  `zgrep`/`zipgrep` for gzip/zip contents), scoped to any directory,
  with real-time streamed results you can jump straight to. Runs the
  real system tools rather than a reimplemented search, so it inherits
  whatever's already indexed by `locate`'s own `updatedb`. The results
  view isn't a dead end either: its own header carries a real,
  fully clickable/editable path breadcrumb — starting at the search's
  own scope — right alongside the status line, so you can keep
  browsing normally without first jumping to a specific hit or backing
  all the way out with Escape.
- Look (`l`, the bottom bar's own button, or the context menu): a
  read-only, full-screen preview of the selected file's content.
  Plain text, source code, config files, diffs/patches, and logs get
  built-in syntax coloring (~200 languages, no external dependency —
  large files show their own first 8 MiB rather than being fully
  loaded). PNG, JPEG, GIF, BMP, TIFF, and WebP images render right in
  the terminal — decoded and scaled entirely in Go, no external tool
  needed there either. A format Look doesn't have a decoder for at all
  (HEIC, AVIF, RAW, ...) still opens the same overlay, with a
  recommendation for an external tool that can (`chafa`, or `pixterm`
  if you'd rather stick to Go-only tooling) — matched to whichever
  package manager your system actually has. Set `pager = external` in
  your config instead (see [Color schemes](#color-schemes) below for
  the file itself) to open `bat`/`batcat`, `$PAGER`, or `less`/`more`
  in your real terminal for text. "Tail -f", right next to Look in the
  context menu, follows a growing log live via the real `tail -f`.
  PDFs open page by page — as a real rendered page image where
  [poppler-utils](https://poppler.freedesktop.org/)'s `pdftoppm` is
  installed (feeding straight into the same image renderer above), or
  as extracted plain text otherwise, entirely in Go, no external tool
  required. `PageUp`/`PageDown` move between pages either way; `g`/`t`
  switch a given page between rendered-image and extracted-text view
  on demand — handy since a text-heavy page rendered as an image
  downsamples into illegible mush at any realistic terminal size.

## Status

Actively developed and usable day to day. Everything described above is
built and tested: browsing, tabs, split view, the trash, Search, Look,
Sed Replace, Batch rename, and a full Options screen covering every
setting breakthrough recognizes. Progress bars for long-running file
operations, archive handling, and a set of built-in networking/hardware
tool windows are what's planned next — see
[docs/whitepaper.md](docs/whitepaper.md) for the full concept and
vision, and follow along or join in on
[Discussions](https://github.com/jagottsicher/breakthrough/discussions).

## Color schemes

breakthrough ships with one built-in scheme ("Default") and reads
further ones from `colorschemes/*.json` in either config tier —
`/etc/breakthrough/colorschemes/` for system-wide schemes,
`~/.config/breakthrough/colorschemes/` (or `$XDG_CONFIG_HOME/breakthrough/colorschemes/`
if set) for your own; a user file with the same name replaces a system
one. Switch between whatever's found via the Options screen (the `o`
chord's own `oo`) — the pick applies immediately and is saved to your
own `~/.config/breakthrough/config`.

The `.deb`/`.rpm` packages (see [Installing](#installing)) create
`/etc/breakthrough/config` and `/etc/breakthrough/colorschemes/` for
you — the config file already fully documented and commented out, ready
for a system administrator to uncomment and edit — so a package install
gives you a real, discoverable starting point rather than a path that
simply doesn't exist yet. A plain `.tar.gz` install has no installer to
do that for it; the Options screen's own "Edit config file" button
creates your *user* config the same way, on first use, if you'd rather
start from that tier instead.

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

Every release ships prebuilt binaries for all supported platforms on the
[Releases page](https://github.com/jagottsicher/breakthrough/releases).
No runtime dependencies: breakthrough is a single static Go binary built
with `CGO_ENABLED=0`, so there is nothing to install alongside it and
nothing to break on a libc upgrade.

### Which build do I need?

| Your system | Architecture | Download |
|---|---|---|
| Linux, ordinary PC/server | x86_64 / amd64 | `breakthrough_<version>_linux_amd64.*` |
| Linux, Raspberry Pi 4/5 (64-bit), ARM servers, AWS Graviton | aarch64 / arm64 | `breakthrough_<version>_linux_arm64.*` |
| macOS, Intel | x86_64 | `breakthrough_<version>_darwin_amd64.tar.gz` |
| macOS, Apple Silicon (M1–M4) | arm64 | `breakthrough_<version>_darwin_arm64.tar.gz` |
| FreeBSD | x86_64 / amd64 | `breakthrough_<version>_freebsd_amd64.tar.gz` |

Not sure which one you're on? `uname -sm` answers both questions at
once — `Linux x86_64` means linux/amd64, `Linux aarch64` means
linux/arm64, `Darwin arm64` means Apple Silicon.

32-bit builds (i386, armv6/armv7) are deliberately not published. If you
need one, it cross-compiles from source in one command — see
[Building from source](#building-from-source) below.

### Debian, Ubuntu, Linux Mint, Raspberry Pi OS (`.deb`)

```sh
VERSION=0.18.0                     # or whatever the latest release is
ARCH=$(dpkg --print-architecture)  # amd64 or arm64
curl -LO "https://github.com/jagottsicher/breakthrough/releases/download/v${VERSION}/breakthrough_${VERSION}_linux_${ARCH}.deb"
sudo apt install "./breakthrough_${VERSION}_linux_${ARCH}.deb"
```

`apt install ./file.deb` rather than `dpkg -i` so any dependency
resolution is handled for you. Upgrade by installing a newer `.deb` the
same way; remove with `sudo apt remove breakthrough`.

### Fedora, RHEL, AlmaLinux, Rocky, openSUSE (`.rpm`)

```sh
VERSION=0.18.0
ARCH=$(uname -m)                   # x86_64 or aarch64
case "$ARCH" in x86_64) PKG=amd64 ;; aarch64) PKG=arm64 ;; esac
curl -LO "https://github.com/jagottsicher/breakthrough/releases/download/v${VERSION}/breakthrough_${VERSION}_linux_${PKG}.rpm"
sudo dnf install "./breakthrough_${VERSION}_linux_${PKG}.rpm"   # or: sudo zypper install ./...
```

Both packages also create `/etc/breakthrough/config` — fully documented,
every setting listed and commented out — and `/etc/breakthrough/colorschemes/`,
so a system administrator has a real starting point for machine-wide
defaults (see [Color schemes](#color-schemes)). Your own edits to that
file survive a package upgrade: it's registered as a conffile on Debian
and `%config(noreplace)` on RPM.

### Any Linux, macOS, or FreeBSD (`.tar.gz`)

Works on any distribution, with or without root:

```sh
VERSION=0.18.0
OS=$(uname -s | tr '[:upper:]' '[:lower:]')          # linux, darwin, freebsd
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
esac

curl -LO "https://github.com/jagottsicher/breakthrough/releases/download/v${VERSION}/breakthrough_${VERSION}_${OS}_${ARCH}.tar.gz"
tar xzf "breakthrough_${VERSION}_${OS}_${ARCH}.tar.gz"
sudo install -m 0755 breakthrough /usr/local/bin/breakthrough
```

Without root, drop it somewhere on your own `PATH` instead:

```sh
mkdir -p ~/.local/bin && install -m 0755 breakthrough ~/.local/bin/
```

On macOS, Gatekeeper quarantines anything downloaded with a browser. If
you get "cannot be opened because the developer cannot be verified",
clear the quarantine flag once:

```sh
xattr -d com.apple.quarantine /usr/local/bin/breakthrough
```

Downloading with `curl`, as above, avoids that entirely.

### Verifying a download

Every release includes a `checksums.txt` covering all its artifacts:

```sh
curl -LO "https://github.com/jagottsicher/breakthrough/releases/download/v${VERSION}/checksums.txt"
sha256sum --ignore-missing -c checksums.txt      # shasum -a 256 on macOS/FreeBSD
```

### Building from source

Needs only a current Go toolchain — no C compiler, no system libraries:

```sh
git clone https://github.com/jagottsicher/breakthrough.git
cd breakthrough
go build ./cmd/breakthrough
```

Cross-compiling for another machine is a matter of two environment
variables, which is also how to get an architecture the releases don't
cover:

```sh
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build ./cmd/breakthrough   # 32-bit Raspberry Pi
GOOS=linux GOARCH=386        CGO_ENABLED=0 go build ./cmd/breakthrough   # 32-bit x86
GOOS=openbsd GOARCH=amd64    CGO_ENABLED=0 go build ./cmd/breakthrough   # OpenBSD
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full development setup,
and [docs/installation.md](docs/installation.md) for terminal
requirements, the files breakthrough creates on your system, and
uninstall instructions.

## Documentation

- **[docs/user-guide.md](docs/user-guide.md)** — every feature in
  detail, the complete settings reference, and a full keyboard map.
  `?` inside the application shows a condensed version of the same
  thing, always matching the build you're running.
- **[docs/installation.md](docs/installation.md)** — terminal
  requirements, exactly which files land where, optional external
  tools, system-wide rollout for administrators, upgrading,
  uninstalling, and troubleshooting.
- **[docs/whitepaper.md](docs/whitepaper.md)** — the concept and the
  reasoning behind the project.
- **[CONTRIBUTING.md](CONTRIBUTING.md)** — local setup and the branch
  workflow.

## Contributing

Contributions are very welcome — this is an ambitious project for one
person alone. See [CONTRIBUTING.md](CONTRIBUTING.md) for local setup, the
branch workflow, and code style expectations.

## Support

The best way to support breakthrough is to participate and contribute. If
you'd like to leave a tip instead, you can send a few Satoshis via the
Lightning Network ⚡️ using the sponsor button above.
