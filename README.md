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
  clickable path breadcrumb bar with Start/Root/Home/Back/Forward — its
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
- A live filter, tucked behind a compact "Y" button in the top row (an
  "Nx" count appears right before it once one or more are actually
  narrowing the listing, turning bright red instead if one is currently
  hiding everything a directory would otherwise show): click it, or
  press `/`, to open a small dropdown with three independently
  combinable rows, each narrowing the listing live as you type:
  - **Glob/regex** — the original filter, with a Glob/Regex toggle for
    how the pattern is interpreted and its own checkbox to switch it
    off without losing what's typed.
  - **Size** — comparison expressions like `> 1m`, `>= 500k`, or a
    range by joining two with `and` (`> 1m and < 1g`). Units are
    `b`/`k`/`m`/`g`/`t`, binary (1024-based), the same convention the
    Size column's own human-readable mode already uses; a bare number
    means plain bytes.
  - **Modified time** — `before`/`after`/`between ... and ...`, each
    side either an absolute date (`2026-09-01`, optionally with a
    time) or a relative one (`7 days`, `2 hours ago`); a bare relative
    expression on its own (`last 7 days`) means "modified within
    that span".

  Typing into any field auto-activates its own row, the same way it
  already did for glob. `Tab`/`Shift+Tab` cycle through all seven of
  the dropdown's own pieces (checkbox + field for size and
  modified-time, checkbox + mode button + field for glob),
  `Space`/`Enter` toggles whichever checkbox has focus, `/` — once the
  dropdown is already open — jumps straight to the next of the three
  fields instead (the same "press it again to advance further" trick
  `Ctrl+T` already does for the tab switcher; safe to repurpose since a
  bare filename can never contain `/` in the first place), and `Escape`
  closes it from any of them.
  Carries over across a directory change by default (`filter_persistent`),
  so browsing a whole tree with the same filter switched on is the
  normal way to use it, not a special case; set it to `false` to go
  back to every new directory starting unfiltered instead.
- The seven nav buttons (Start/Root/Home/Up/Back/Forward/Reload) at the
  very start of the path bar are real, highlighted buttons now — a plain
  background-colored square either side of each glyph, with its own
  separator column between one button and the next. Root (`/`) sits right
  next to Start and jumps straight to the filesystem root — the same
  destination the path breadcrumb's own leading "/" already links to, just
  as a second, consistently styled, easier-to-spot target for it.
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
  behind a chord (a letter, then one more within about four seconds by
  default — "Chord timeout (ms)" under Options, Behavior's own
  Miscellaneous subsection — with a countdown in the status bar and a
  clickable legend in the
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
  current selection, not just one file. Pasting into the very directory
  a file is already in, or a directory into one of its own
  subdirectories, is refused outright rather than started at all — the
  first would have destroyed the only copy there ever was, the second
  would recurse into itself without any bound. Paste runs in the
  background — a file that already exists at the destination opens a
  small dialog (Overwrite, Skip, "Merge into existing folder", an "all"
  variant of each for the rest of this Paste, or apply "only if the
  source is newer"/"only if the source isn't empty" to every conflict
  it still runs into) without blocking anything else in the same
  Paste: whatever doesn't conflict keeps copying/moving while that
  dialog is up, and a second conflict found before the first is
  answered queues behind it — shown as "(N more waiting)" right in the
  dialog's own message — rather than stacking a second dialog on top.
  Unlike every other dialog here, clicking outside it does nothing —
  one of its own options, or Escape, is the only way past a conflict,
  so a stray click can never leave one stranded, half-answered forever.
  Overwriting a directory replaces it entirely (nothing left over from
  whatever was there before — the right choice when "overwrite" needs
  to mean "make this identical to the source", not "patch it"); Merge
  is the explicit alternative, keeping whatever the source doesn't
  also have. "Auto-merge directories" under Options (off by default,
  set separately for Copy and Move) skips this dialog entirely for a
  directory-vs-directory conflict and merges right away every time —
  a file-vs-file or file-vs-directory conflict always still asks,
  since merging has no meaning there. Starting a further Paste while one is already running
  queues it rather than running it alongside the first or replacing
  it outright — shown as "(+N queued)" right in the status bar's own
  progress line — and it starts automatically the moment the one ahead
  of it finishes, in the order each was asked for. Ctrl+C stops a
  running Paste outright — whatever's already mid-write finishes
  normally, on disk, right where it was headed; nothing still queued,
  whether a pending conflict or a whole further Paste behind this one,
  starts at all. Any real failure (permission, a full disk, ...) is
  collected rather than stopping at the first one, and reported once
  the whole Paste is done. Every open tab showing the destination
  reloads automatically as items actually land, not just once the
  whole Paste is fully done — including while an unrelated conflict's
  own dialog is still sitting open, since whatever doesn't conflict
  keeps landing regardless; Cut also reloads every open tab showing one
  of the moved items' own source directories, live, as items actually
  leave, so a tab something was cut from never keeps listing a file
  that's actually gone — Copy leaves its own source list alone, since
  nothing there was ever removed.
  Whatever's currently on the clipboard shows
  two ways: every row it holds gets a full-row tint — a slightly
  bluish-tinted grey for Copy, a slightly pinkish-tinted grey for Cut,
  a deliberately matched pair rather than two shades of one plain
  grey, so a Cut selection reads as visually different from a Copy one
  at a glance rather than needing a brightness comparison. This tint
  stays visible even on whichever row the cursor happens to land on:
  the ordinary focus highlight wins outright over it while this panel
  actually has keyboard focus, so the cursor's own position among
  several tinted rows is never ambiguous, but a dimmer variant of the
  same tint takes over once focus moves elsewhere, still clearly Cut-
  or Copy-colored rather than fading to a plain, indistinct gray —
  across every open tab showing that row, not just the one Copy/Cut
  was pressed in; and the status bar names it —
  "Copy: 3 files, 1 dir" or "Cut: ..." — right after the button-bar
  chord countdown's own spot, for as long as there's something to
  Paste. Once a Paste actually starts, that same spot switches to its
  own live progress instead — a spinner, "Copying"/"Moving" and how
  many of the selection's own top-level items are done, and a two-row
  progress bar packed into one line of half-block characters (the top
  half is that same item-count fraction, the bottom half is the file
  currently being written's own byte progress), plus whichever real
  file is being written right now (its bare name, e.g. inside a large
  directory this Paste is still working through). A one-time background
  scan of the whole selection's byte size (started alongside the Paste
  itself, never blocking it) adds two more things once it's done: a
  single character before the bar showing what percentage of the total
  bytes has copied so far (the same shrinking/filling block style the
  chord countdown uses), and an estimated remaining duration after the
  bar. A same-filesystem move is atomic regardless of size, so a Cut
  within one filesystem usually finishes too fast for any of this to
  show anything at all — expected, not a bug: there's nothing to report
  progress on.
  Plain `v` follows whichever "Follow symlinks" default is set under
  Options for Copy or Move (off by default, set separately for each):
  off recreates a symlink as a symlink at the destination, on replaces
  it — and any symlink nested inside a pasted folder — with a real,
  independent copy of whatever it points to (recursively, through a
  multi-hop chain too), instead. `V` (Shift+Paste, or the context
  menu's "Paste, following symlinks") flips that default for this one
  paste only, without changing the setting itself. Works for both a
  Copy- and a Cut-marked clipboard alike; for a Cut, only the original
  link itself is removed afterward — never whatever it pointed to,
  however far away that actually lives (a different filesystem, a
  network mount). `V` only asks for confirmation first when it's
  actually the thing turning dereferencing on (the configured default
  was off) — dereferencing can turn a small, instant symlink into an
  arbitrarily large copy, so activating it is never a single,
  undialogued keypress; flipping it back off for one paste needs no
  such confirmation, since that's the safer direction.
  Two more Copy/Move settings round this out, both under Options and
  set separately for Copy and Move: "Preserve attributes" (on by
  default) carries the source's own permissions, ownership, and
  modification time over to the destination — turning it off leaves
  the destination at whatever creating it just produced. "Stable
  symlinks" (off by default, matching Midnight Commander's own default
  for the equivalent option) rewrites a copied symlink's target to
  point at its new location if that target lives inside the tree being
  copied, rather than keeping the original target verbatim — without
  it, such a link can end up pointing back at the original source (an
  absolute target) or nowhere at all (a relative target that climbed
  out of the copied root and back in by its old name) once that source
  is later moved, renamed, or removed.
- Move to Trash / Remove: `d` or Entf moves the current selection to
  your own trash — recursively for a directory, no confirmation by
  default, since that's the reversible action by design ("Confirm
  before moving to Trash" under Options, off by default, asks first
  anyway for anyone who wants that extra safety net). `D`, Ctrl+Entf
  (best-effort — terminal-dependent; `D` is always the reliable one),
  or the context menu's "Remove" permanently deletes instead (a file
  like `rm`, a directory recursively like `rm -rf`, empty or not),
  always behind a confirmation dialog with Cancel preselected — a
  single stray keypress can never confirm it by itself. "Go to Trash"
  (the `g` chord's own `gb`) jumps straight into it without needing to
  know its path; "Restore from Trash" (`r`, while browsing it) and
  "Empty Trash" (`D`, same confirmation) round it out. Restoring
  something whose original path now has an unrelated
  file sitting on it — recreated after the original was trashed, say —
  opens the exact same conflict dialog a Paste collision already does
  (Overwrite/Skip and their "for all" and "if newer"/"if not empty"
  variants, Skip preselected as the safe default), rather than silently
  overwriting it or refusing outright with nothing but an error; a
  multi-item restore can pull items whose own original locations were
  entirely different folders, each resolved independently. Persistent by default — lives under
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
