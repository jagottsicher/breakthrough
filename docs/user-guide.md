# breakthrough user guide

A reference for everything breakthrough can do. The
[README](../README.md) is the overview; this is the detail behind it.
`F1` inside the application shows a condensed version of the same
material, always matching the version you are actually running.

## Contents

- [The key prefix](#the-key-prefix)
- [Getting around](#getting-around)
- [Selecting files](#selecting-files)
- [Tabs](#tabs)
- [Split view](#split-view)
- [The context menu](#the-context-menu)
- [Batch rename](#batch-rename)
- [Sed Replace](#sed-replace)
- [Search](#search)
- [Look and Tail -f](#look-and-tail--f)
- [The Details sidebar](#the-details-sidebar)
- [Properties](#properties)
- [Trash, Remove and Restore](#trash-remove-and-restore)
- [The command line](#the-command-line)
- [Options and configuration](#options-and-configuration)
- [Settings reference](#settings-reference)
- [Keyboard reference](#keyboard-reference)

---

## The key prefix

`Ctrl+_`, then one letter. The button bar becomes the list of what's
available the moment you press it:

```
 ^_  s Split  o Orient  p otherPane  d Details │ t Tabs  n New tab
     w Close tab │ r Rename  m Mouse  , Options  ? Help │ 1-0 Tab N │ Esc cancel
```

| Verb | Action | also on |
|---|---|---|
| `s` | Split view on/off | `F5` |
| `o` | Flip split orientation | `F6` |
| `p` | Jump to the other pane | `Tab` |
| `d` | Details sidebar | `Ctrl+D` |
| `t` | Tab switcher | `Ctrl+T` |
| `n` | New tab | — |
| `w` | Close tab | — |
| `1`…`0` | Jump straight to tab 1–10 | `Ctrl`/`Alt`+digit |
| `r` | Rename | context menu, click-pause-click |
| `m` | Toggle mouse reporting | `F12` |
| `,` | Options | `Ctrl+O` |
| `?` | Help | `F1` |

**Why it exists.** On a MacBook, `F1`–`F6` are media keys unless you
change a system setting, so a feature reachable only by function key is
a feature a Mac user can't reach. Every entry above is also still on its
own key — the prefix is a second route, never a replacement.

It also fixes a smaller problem: `Ctrl`+digit needs an enhanced keyboard
protocol that several terminals don't implement, so jumping to a tab by
number silently does nothing there. `Ctrl+_ 1` works everywhere.

**Why `Ctrl+_`.** The single-key namespace is genuinely full: four
`Ctrl`+letter combinations are structurally unavailable in a terminal
(`Ctrl+I` is Tab, `Ctrl+M` is Enter, `Ctrl+H` is Backspace, `Ctrl+[` is
Escape — identical bytes, indistinguishable), most of the rest are
bound, and the remainder are readline keys the command line needs. More
importantly, no terminal multiplexer claims `Ctrl+_`: tmux takes
`Ctrl+B`, screen and byobu `Ctrl+A`, dtach and abduco `Ctrl+\`. A
multiplexer intercepts its own prefix before the application inside ever
sees the key, so any of those would have been dead weight for anyone
working inside one.

**How it behaves.**

- The prefix does **nothing on its own**, which is what makes a timeout
  unnecessary — there's no "did they mean the prefix, or the start of a
  chord" to resolve by waiting. Take as long as you like.
- `Escape` cancels, so does pressing `Ctrl+_` again, and so does any key
  that isn't a verb (which says so rather than failing silently).
- Every key is consumed while the prefix is waiting, so one keypress can
  never both pick a verb and trigger its own shortcut.
- While the command line has focus, `Ctrl+_` is left alone for the
  shell — the same as every other global shortcut there.
- Clicking `^_ More` in the button bar opens the same legend.

---

## Getting around

Arrow keys move the cursor, `Enter` opens a directory (or tries Look on
a plain file), and `..` goes up. The header row above the listing is a
clickable breadcrumb: click any path segment to jump straight there, or
click the path itself to type a new one — `Tab` completes it, `Enter`
goes.

Back and Forward treat a trip into search results or the trash exactly
like a real directory, and returning to one restores the cursor row you
left it on. A search's results come back as they were, rather than being
re-run.

### Column widths

Size and Modified are sized from what they actually contain, and never
truncated — a clipped number or half a timestamp tells you nothing, and
there is no way to tell one from a real value. Human-readable sizes need
six columns, exact byte counts as many as the largest file needs, a Unix
timestamp ten, a full date and time nineteen; switching either format
reflows the columns straight away and hands the difference to the name.

The modification column's header is just "mtime" — the word anyone
working at a shell already uses, and short enough to fit whichever
format the column is in, so it never costs the name column room. Where a
label genuinely is too long for its data (the trash's own "Deletion
time" beside a column of Unix timestamps), the *label* abbreviates
rather than the value, because a shortened label still says what the
column is where a cut-off timestamp would not.

The name column takes whatever is left, and is the one thing shortened
when there isn't enough: with a middle ellipsis, so both the start (which
distinguishes it from its neighbours) and the extension survive —
`annual-report-2026-final.pdf` becomes `annual-re…final.pdf`. A
directory's trailing `/` and a symlink's `-> target` are kept whole where
they can be, since they say what kind of entry it is.

This matters most in [split view](#split-view), where each pane is half
the width.

Sort by clicking a column heading (Name, Size, Modified). The filter box
in the top row narrows the listing as you type; its own button switches
between glob and regular-expression matching.

A name's color tells you what it is at a glance: dark-yellow highlight
for anything `Enter` navigates into, green for executable, red for a
broken symlink, darker red for something you can't read, cyan for a
symlink to a file, orange for a socket/FIFO/device, magenta for a
recognized archive, dim gray for a dotfile.

## Selecting files

`Space` checks or unchecks the row under the cursor. Right-drag over
several rows toggles all of them. The context menu adds Select all,
Deselect all, and glob-pattern Select +/− for things like `*.log`.

Every bulk action — Copy, Cut, Move to Trash, Remove, Sed Replace, Batch
rename — acts on the checked selection if there is one, and otherwise on
the single row under the cursor. There is no separate "nothing is
selected" state to worry about.

## Tabs

Each tab is a complete browsing context: its own directory, history,
filter, sort order, selection and cursor position. Switching away and
back leaves all of it exactly as it was.

| Key | Action |
|---|---|
| `Ctrl`+`1`…`Ctrl`+`0` | jump to tab 1–10 |
| `Alt`+`1`…`Alt`+`0` | the same tabs, for terminals that can't report `Ctrl`+digit |
| `Ctrl`+`Tab` / `Ctrl`+`Shift`+`Tab` | step through the switcher |
| `F4` or `Ctrl`+`T` | open the switcher on the current tab; press again to walk down it |

The switcher lists every tab's full directory — the numbered strip
beside the filter box deliberately shows numbers only, so the header
doesn't change width as you navigate. In the switcher, `Enter` or
`Space` goes to a tab, `Delete` closes one, `Escape` leaves things as
they are, and the last row opens a new tab. Each row also carries a `◫`
button ([split view](#split-view)) and, except the first tab, a `✕`.

The first tab can't be closed — `Ctrl`+`Q` is how you leave.

Your tabs are saved when you quit and reopened next time. Starting with
an explicit directory (`breakthrough /some/path`) opens just that
instead, and `restore_tabs = false` turns the whole thing off.

## Split view

Two of your open tabs on screen at once, side by side or stacked.

| Key | Action |
|---|---|
| `F5` | split / unsplit |
| `F6` | flip between side-by-side and stacked |
| `Tab` | move to the other pane |
| click | move to the pane you clicked |

**Choosing the second pane.** `F5` on its own picks for you, in this
order: the tab you last split with, then the next tab along, and — if
only one tab is open — a brand-new tab on the same directory. That last
case is the useful default: one keypress gives you the same directory in
two panes, which is where copying between two places in one tree
usually starts.

To choose deliberately, open the tab list (`F4`) and use a row's `◫`
button: on a real tab to show that one beside the current one, or on the
`+ New tab` row to create a tab and split with it in one step.

**Which pane is which.** The pane you are in keeps the keyboard, the
context menu, and every shortcut — everything acts on it, and only on
it. Its selected row carries the "focused" highlight (petrol); the other
pane's is dimmed. Each pane's own number strip marks the tab it holds,
so you can always see which two tabs you have up.

**Layout rules.** The panes never swap sides: moving focus across the
divider changes nothing about the layout. Switching to a different tab
while split replaces what the pane you are in shows and leaves the other
alone. Closing either pane's own tab ends the split; closing an
unrelated tab doesn't.

Whether panes sit side by side or stacked is the `split_stacked`
setting, so it survives a restart — `F6` and the Options screen both
write to it. Which one works better depends on your terminal: side by
side keeps every row of both listings visible, stacking keeps the full
column width for long filenames.

The split itself is saved and restored along with the tabs. If one
pane's directory has disappeared by the next start, breakthrough opens
single-pane rather than restoring half a layout.

## The context menu

`F2`, or right-click anywhere in the listing. The menu is grouped: the entry
under the cursor first (Look, Rename, Edit, `tail -f`, Properties), then
Selection, Commands, Delete, Tabs, Tools, and Globals.

Everything in it is also reachable from the keyboard or the button bar —
the menu is a discovery aid, never the only path to a feature.

## Batch rename

Renames a whole selection through a fixed pipeline, with a live preview
of the result. Reached from the context menu's "Batch rename".

The screen has the steps down the left, the selected step's own settings
on the right, and the preview underneath.

| Key | Action |
|---|---|
| `←` / `→` | move between the step list and its settings |
| `↑` / `↓` | move within whichever of the two you're in |
| `Enter` / `Space` | change the selected setting |
| `Tab` / `Shift`+`Tab` | cycle steps → settings → preview → buttons |
| `Escape` | close without renaming anything |

### The steps

They always run in this order, and a step left at its default does
nothing — there is no separate on/off switch to also set.

| Step | Settings | Notes |
|---|---|---|
| **Search & Replace** | Find, Replace with, Regex | Literal text by default. With Regex on, Find is a [Go regular expression](https://pkg.go.dev/regexp/syntax) and Replace can use `$1`, `$2`, … for captured groups |
| **Case** | Unchanged / UPPERCASE / lowercase / Title Case / Sentence case | Title Case treats a run of letters *and digits* as one word, so `v2` stays `V2` |
| **Trim** | characters off the front, off the back | Counted in characters, not bytes, so accented and non-Latin names are never cut mid-character |
| **Numbering** | Position (None/Prefix/Suffix), Start at, Step, Digits | Zero-padded to Digits, joined with `-`. Counts in the order the preview lists the files. A number wider than Digits is never truncated |
| **Extension** | Keep / lowercase / UPPERCASE / Remove / Set to… | "Set to…" uses the field below it; a leading dot is optional |

Steps 1–4 only ever touch the name; step 5 only ever touches the
extension. So a Case transform can't quietly rewrite `.JPG`, and a
Search & Replace for `jpg` won't reach into it either — use the
Extension step for that.

A dotfile's leading dot is part of its name, not an extension separator:
`.bashrc` is the name `.bashrc` with no extension, and only the *last*
dot after that counts, so `archive.tar.gz` is `archive.tar` + `.gz`.

### The preview

Every selected file is listed, changed or not, and it updates on every
keystroke — there is no "Preview" button to press first, because
renaming has no file contents to read and nothing to wait for.

- **Changed** rows show the new name in full color.
- **Unchanged** rows are dimmed and marked `(unchanged)`.
- **Conflicts** are red, with the reason: either two files in the batch
  would land on the same new name, or the new name is already taken on
  disk. Conflicting files are simply left out of the rename — the rest
  still goes through.

The status line underneath counts all three.

### Applying and undoing

Nothing is written until you press **Rename** and confirm. "Reset all
steps" clears the whole pipeline without closing the screen.

Afterwards, the context menu's **"Undo last rename"** reverses the
entire batch in one go — one level deep, cleared once used. Renaming
never crosses filesystems (a new name always lands in the same
directory), so there is no partial-move case to recover from.

## Sed Replace

`Ctrl`+`S`, or the context menu's "sed". Runs a real `sed(1)`
substitution against the selected file(s) — contents, not names.

Fill in Find and Replace with, and set any of the four toggles (Regex,
Extended regex `-E`, Case-insensitive, Replace all matches per line), or
ignore all of that and write a sed script yourself in the advanced
field — address ranges, multiple commands, backreferences, anything real
sed does.

Preview first, always: a Name/Line/Excerpt table with one row per
changed line, files that were skipped listed with why (a directory,
unreadable, or binary). Only Apply writes, behind a confirmation, and it
can keep a `.bak` of each original.

breakthrough never uses sed's own `-i`: GNU and BSD/macOS sed take
incompatible arguments for it, so this runs sed as a plain filter and
writes the result back itself, atomically.

## Search

`Ctrl`+`F`, or the button bar. Two modes:

- **By name** — glob, plain keyword, or regex, via `find`, or `locate`
  where its index is available.
- **By content** — via `grep`, plus `zgrep`/`zipgrep` for gzip and zip
  archives where those are installed.

Scope it to any directory. Results stream in live and you can jump
straight to any hit. A content match opens in your editor at the matched
line.

The results view is a real browsing context, not a dead end: its header
carries a full clickable breadcrumb starting at the search scope, so you
can keep navigating from there without backing out first.

## Look and Tail -f

`Ctrl`+`L`, `Enter` on a file, a double-click, or the context menu.
Read-only, full-screen.

- **Text, source, configs, diffs, logs** get syntax coloring for around
  200 languages, with no external dependency. Files larger than 8 MiB
  show their first 8 MiB rather than loading entirely.
- **Images** (PNG, JPEG, GIF, BMP, TIFF, WebP) render in the terminal,
  decoded and scaled in pure Go.
- **PDFs** open page by page: as real rendered images where
  [poppler-utils](https://poppler.freedesktop.org/)' `pdftoppm` is
  installed, as extracted text otherwise. `PageUp`/`PageDown` turn
  pages; `g` and `t` switch a single page between rendered and text
  view, which matters because a text-heavy page downsamples into mush
  as an image at terminal resolution.
- **Formats with no decoder** (HEIC, AVIF, RAW…) still open the
  overlay, with a suggested external tool and the install command for
  your actual package manager.

Set `pager = external` to hand text files to `bat`/`batcat`, `$PAGER`,
or `less`/`more` in your real terminal instead.

"Tail -f", beside Look in the context menu, follows a growing log
through the real `tail -f`.

## The Details sidebar

`Ctrl`+`D`, or the `<` button after the filter box. A live, read-only
panel on the right that follows the cursor.

It shows the full stat block (type, permissions, owner, group, size,
timestamps, path), and on demand:

- `Ctrl`+`K` — SHA-256, SHA-1, MD5, SHA-512 and BLAKE2b-512.
- `Ctrl`+`U` — a directory's total size via `du -hs`. On a symlink or
  mount point it resolves the whole link chain first and reports the
  target's real size, naming what it actually measured.

Images and PDFs get an inline preview with its own click zone for
fullscreen. `Tab` moves keyboard focus into the sidebar so its own
scrolling works; `Tab` again comes back. The `>` button in its corner
closes it.

With Properties open on the same file, `Ctrl`+`K` fills in both.

## Properties

`Ctrl`+`P`, or the context menu. Editable: name, permission bits
(click a bit, press `r`/`w`/`x`, or type the octal value directly),
owner and group through a scrollable picker of every local user and
group, and the modified date and time.

`Tab` moves between fields, `Enter` or `Space` activates the focused
one, `Escape` cancels.

## Trash, Remove and Restore

`Delete` moves the selection to your trash — recursively for a
directory, without a confirmation, because it is the reversible action
by design.

`Ctrl`+`R` (or `Ctrl`+`Delete`, terminal permitting) permanently deletes
instead, always behind a confirmation with Cancel preselected, so a
stray `Enter` can never trigger it.

`Ctrl`+`B` browses the trash directly. While you're in it, `Delete`
means Remove — there is nowhere left to move an already-trashed item to
— and the button bar swaps Trashbin for Restore.

Browsing the trash shows each item's **original path** in place of its
real on-disk name (which carries a collision-avoidance hash), and labels
the Modified column "Deletion time". Both respect the timestamp
formatting toggle and the column sort, so the same file trashed twice
from the same place stays distinguishable.

The trash is persistent by default, under
`~/.local/share/breakthrough/trash`. Set `trash_persistent = false` for
a session-scoped one under `$XDG_RUNTIME_DIR`, discarded when your login
session ends. Running as root always gets the persistent path — root has
no real session for the other mode to mean anything against.

It's kept from growing forever by two settings, both checked once at
startup rather than on every operation: `trash_max_age_days` (30 by
default; anything older goes unconditionally) and `trash_quota_percent`
(10 by default; a backstop that removes oldest-first only if the age
rule alone didn't bring the trash back under that share of its
filesystem). Either is `0` to disable. Anything removed this way is
reported on the next start — a trash that quietly empties itself would
be worse than no trash.

## The command line

The row below the panel is a real shell command line. Click into it and
it expands upward toward mid-screen as a multi-line editor.

- Every command runs through your actual `$SHELL`, started the way an
  interactive login shell is — so your startup files, aliases and
  functions are all in effect and `ll` works exactly as it does in a
  normal terminal.
- `cd` is handled directly, so it actually moves the panel instead of
  uselessly changing a subshell's directory.
- History is shared with `$HISTFILE`, or `~/.bash_history` otherwise.
  `Up`/`Down` or `Ctrl`+`P`/`Ctrl`+`N` recall it.
- `Tab` completes the filename at the cursor against the panel's current
  directory; where several matches agree on nothing further, it opens a
  pick list instead of doing nothing.
- `Enter` runs the buffer; `Ctrl`+`J` (always) or `Alt`+`Enter` (where
  your terminal doesn't intercept it) inserts a newline for multi-line
  scripting.
- Output stays on screen until you press `Escape`, so nothing flashes
  past.

While the command line has focus it keeps the keys it needs for
readline-style editing, so global shortcuts that would collide with them
fall through to it. `Escape` or a click on the panel gets you back out.

## Options and configuration

`Ctrl`+`O`, or the button bar. Categories down the left, that category's
settings on the right, action buttons underneath.

| Key | Action |
|---|---|
| `←` / `→` | move between the two panes |
| `↑` / `↓` | move within one |
| `Enter` / `Space` | change the selected setting |
| `?` or `F1` | explain the selected setting in its own window |
| `Tab` / `Shift`+`Tab` | cycle categories → settings → buttons |
| `Escape` | close |

**There is no save button.** Every change takes effect and is written to
your config file the moment you make it. Booleans toggle in place,
choices cycle, numbers open a one-line field where `Enter` commits and
`Escape` discards.

A setting whose value differs from the built-in default shows a dim
`default: …` hint beside it, so you can always see what you've changed.

**Reset removes, it doesn't overwrite.** "Reset category" and "Reset
all" delete your override from your own config file rather than writing
a default into it, so the value falls back through the tiers — to your
administrator's system-wide setting where there is one, and only to
breakthrough's built-in default where there isn't. Both ask first.

"Edit config file" opens your config in your editor, creating it — fully
commented, every setting listed — if you don't have one yet. "New color
scheme" copies the active scheme under a fresh name and opens that.
Either way, changes are picked up as soon as the editor closes.

### Config file format and locations

Plain `key = value` lines, `#` for comments — deliberately the
`sshd_config`/`nginx.conf` shape rather than TOML or YAML, because it is
what this program's audience already reads without thinking.

| Tier | Path |
|---|---|
| System-wide | `/etc/breakthrough/config` |
| Yours | `~/.config/breakthrough/config` (or `$XDG_CONFIG_HOME/breakthrough/config`) |

Both are read at startup and merged key by key, with yours winning. A
key listed in neither uses the built-in default; there is no need to
list settings you don't want to change. Booleans accept
`true`/`false`, `1`/`0` or `yes`/`no`.

A malformed line costs that one setting and is reported at startup — it
never stops breakthrough from starting.

## Settings reference

Every key breakthrough recognizes, with its default:

| Key | Default | Meaning |
|---|---|---|
| `color_scheme` | `default` | Active color scheme — the filename stem of a `colorschemes/*.json` file |
| `show_hidden` | `true` | Show dotfiles and dot-directories |
| `size_bytes` | `false` | Size column as exact bytes instead of human-readable |
| `mtime_unix` | `false` | Time column as a Unix timestamp instead of a formatted date |
| `restore_tabs` | `true` | Reopen the tabs (and split) that were open on last exit |
| `split_stacked` | `false` | Split view stacks its panes above each other instead of side by side |
| `pager` | `builtin` | How Look renders a file: `builtin` or `external` |
| `trash_persistent` | `true` | Keep trashed files across login sessions |
| `trash_max_age_days` | `30` | Remove trashed items older than this at startup; `0` disables |
| `trash_quota_percent` | `10` | Keep the trash at or under this share of its filesystem; `0` disables |
| `language` | `en` | Reserved for future translations — parsed, no effect yet |

## Keyboard reference

### Anywhere

The `F1`–`F4` row follows Midnight Commander's own layout, so muscle
memory carries over.

| Key | Action | also on |
|---|---|---|
| `F1` | Help | `Ctrl+_ ?` |
| `F2` | Context menu for the row under the cursor | right-click, `Ctrl+_` legend |
| `F3` | Look at the selected file | `Ctrl+L` |
| `F4` | Edit the selected file | `Ctrl+E` |
| `F5` | Split view on/off | `Ctrl+_ s` |
| `F6` | Flip split orientation | `Ctrl+_ o` |
| `F12` | Toggle mouse reporting | `Ctrl+_ m` |
| `Ctrl`+`_` | Key prefix — one letter reaches all of the above, without function keys (see [The key prefix](#the-key-prefix)) |
| `Ctrl`+`Q` | Quit (asks first) |
| `Ctrl`+`C` | Back out of whatever is open — never quits |

### File panel

| Key | Action |
|---|---|
| `Enter` | Open a directory, or Look at a file |
| `Space` | Select / deselect |
| `Tab` | Cycle focus: panes, Details sidebar, tool windows |
| `Ctrl`+`E` | Edit in `$VISUAL`/`$EDITOR` (also `F4`) |
| `Ctrl`+`L` | Look (also `F3`) |
| `Ctrl`+`P` | Properties |
| `Ctrl`+`D` | Details sidebar |
| `Ctrl`+`K` | Compute hashes |
| `Ctrl`+`U` | Directory size (`du -hs`) |
| `Ctrl`+`F` | Search |
| `Ctrl`+`S` | Sed Replace |
| `Ctrl`+`G` | Toggle hidden files |
| `Ctrl`+`O` | Options |
| `Ctrl`+`B` | Go to Trash |
| `Delete` | Move to Trash |
| `Ctrl`+`R` | Remove permanently (asks first) |
| `Ctrl`+`1`…`0`, `Alt`+`1`…`0` | Jump to a tab |
| `Ctrl`+`T` | Tab switcher (second path, alongside `F4`) |
| `Ctrl`+`Tab` / `Ctrl`+`Shift`+`Tab` | Step through tabs |

Click, pause, click again on an already-selected name renames it. The
pause is deliberately generous — about a second — so an unhurried second
click still counts.

### Command-line flags

| Flag | Effect |
|---|---|
| `breakthrough <path>` | Open that directory, and skip the saved-tab restore |
| `--version`, `-v` | Print version, commit, build date and builder, then exit |
| `--debug` | Redirect stderr — including a Go panic and its full traceback — to `~/.local/state/breakthrough/debug.log` instead of losing it behind the TUI |

`--debug` is what to use when reporting a crash: a TUI owns the screen,
so anything written to stderr would otherwise be overwritten or wiped
by the terminal reset before you could read it. The log is appended to,
with a timestamped header per session, and tracebacks are set to `all`
so every goroutine is captured. Recovered panics are additionally shown
in the error overlay and logged to `crash.log` in the same directory,
with or without this flag.
