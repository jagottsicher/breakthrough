# breakthrough user guide

A reference for everything breakthrough can do. The
[README](../README.md) is the overview; this is the detail behind it.
`?` inside the application shows a condensed version of the same
material, always matching the version you are actually running.

## Contents

- [The keyboard layer](#the-keyboard-layer)
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
- [Copy, Cut and Paste](#copy-cut-and-paste)
- [Trash, Remove and Restore](#trash-remove-and-restore)
- [The command line](#the-command-line)
- [Options and configuration](#options-and-configuration)
- [Settings reference](#settings-reference)
- [Keyboard reference](#keyboard-reference)

---

## The keyboard layer

breakthrough's primary way to reach every everyday action is a plain
letter, typed while the file listing has keyboard focus — no modifier
key, no terminal/layout/multiplexer risk, the same approach ranger,
nnn, lf and vifm all settled on. It never fires while you're typing in
the filter box, the path editor, or the command line, or with any
dialog open.

| Key | Action | Key | Action | Key | Action |
|---|---|---|---|---|---|
| `c` | Copy | `d` | Move to Trash | `i` | Properties |
| `x` | Cut | `D` | Remove permanently | `I` | Details sidebar |
| `v` | Paste | `u` | Undo last rename | `l` | Look |
| `r` | Rename | `e` | Edit | `/` | Filter |
| `m` | Context menu | `f` | Find | `.` | Toggle hidden files |
| `n` | New tab | `w` | Close tab | `t` | Tab switcher |
| `s` | Split view on/off | | | `a` | Select all |
| `*` | Invert selection | `+`/`-` | Select/deselect by pattern | `B` | Batch rename |
| `E` | Sed Replace | `G` | Go to the last row | `q` | Quit |
| `h` | Compute hashes | `k` | Directory size | `M` | Image metadata |
| `?` | This help | `:` | Bash command line | | |

A capital letter is the bigger sibling of its own lowercase one
wherever both exist: `d` is reversible (the Trash), `D` asks first and
isn't. Browsing the Trash itself flips two of these to their
trash-specific meaning: `r` restores instead of renaming, `D` empties
the whole Trash instead of removing one file.

`h`/`k`/`M` target whichever of Properties/Details is relevant right
now (Properties first if both are open on the same file), opening the
Details sidebar first if neither is — see [The Details
sidebar](#the-details-sidebar). They, along with `l` and `I`, are also
the one handful of plain letters that keep working while Properties
itself is open, rather than being blocked the way every other plain
letter correctly is once an overlay has focus.

**Chords** cover the rarer, related actions — one letter, then within
about four seconds one more. The status bar shows a small countdown
(`g▆`, shrinking) while one is pending, and the button bar becomes that
chord's own legend:

| Chord | Members |
|---|---|
| `g` — go to | `gg` top · `gh` home · `gp` back · `gn` forward · `gu` up · `gr` `/` (filesystem root) · `gb` Trash |
| `p` — permissions | `pm` chmod · `po` chown |
| `z` — display | `zs` size format · `zt` time format · `zo` split orientation · `zw` swap panes · `zr` reload |
| `y` — yank | reserved for a future system-clipboard feature (copy path/name); each member says so rather than doing nothing |

`Escape` cancels a pending chord, and so does any key that isn't one of
its members — which says so, the same as an unrecognized second key
would. Letting it simply run out is treated as "changed my mind" and
cancels without comment. Every member in that legend is also clickable
with the mouse, the same as an ordinary button — no need to type the
second letter if you'd rather point at it.

**The button bar** below the command line always shows a curated subset
of these keys as a quick legend — Copy/Cut/Paste, Move to Trash,
Properties, Details, the context menu, Split, the tab switcher, Look,
Help, and the three chord families — with the actual key to press set
off by its own background color, one space either side, so the
letter-to-action mapping is easy to scan at a glance. Every other key
still works exactly the same whether or not it's shown there; `?`
documents all of them.

**No function keys anywhere in the application.** Every F-key is free
for your terminal or window manager to use however it likes. A handful
of Ctrl-letter shortcuts remain, documented section by section below,
for the few things this layer doesn't reach on its own (Quit, Cancel,
Options, the mouse-reporting toggle, ...) — everything else has exactly
one keyboard path, the plain letter above.

---

## Getting around

Arrow keys move the cursor, `Enter` opens a directory (or tries Look on
a plain file), and `..` goes up. The header row above the listing is a
clickable breadcrumb: click any path segment to jump straight there, or
click the path itself to type a new one — `Tab` completes it, `Enter`
goes.

Entering a directory always lands at the top of it — Enter, a
breadcrumb click, `..`, or Back/Forward, regardless of whatever row you
last left it scrolled to. The trash is loaded as an ordinary directory
path, so the same applies there too. A trip into search results is the
one exception: Back/Forward there restores the exact cursor row you
left it on, since a frozen result list isn't something to reset to the
top of the way a real, re-listable directory is — and the results
themselves come back as they were, rather than being re-run.

`⭯`, right before the path itself, is Reload — the `z` chord's own `r`
member (`zr`) does the same from the keyboard. Re-reads the current
directory straight from disk, for anything this app has no other way
to notice on its own: another process changing files underneath it, a
network/mounted filesystem's own content changing, and so on. While
search results are showing, this exits back to the plain directory
listing rather than re-running the search.

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

Sort by clicking a column heading (Name, Size, Modified).

### The path bar

The five buttons at the very start of it — Start, Home, Back, Forward,
Up — are real, clickable buttons: a background-colored square on
either side of the glyph, with its own plain-background column between
one button and the next, so each reads as its own separate control
rather than a run of characters. Clicking anywhere in a button's own
colored square activates it, not just the glyph's own single column.

### Filtering

Click the "Y" button near the right edge of the path bar (an "Nx"
count appears right before it once one or more filters are actually
narrowing the listing — omitted while none are) to open a small
dropdown with three rows, all combinable:

- **Glob/regex filter** — the same live, type-to-narrow filter this app
  has always had, now living in the dropdown instead of always taking
  up its own space in the path bar: its own button still switches
  between glob and regular-expression matching, and its own checkbox
  switches the filter off without clearing whatever pattern is already
  typed — handy for temporarily seeing everything again without losing
  your place.
- **Size filter** — on/off for now; the comparison operators and
  ranges (`size >= 1m`, `size < 1m AND size > 5m`, and so on) this is
  meant to grow into aren't built yet.
- **Modified-time filter** — on/off for now too; absolute and relative
  date/time ranges (`before`, `after`, `between`, or "last N days") are
  the planned next step.

The dropdown stays open while you tick more than one of these — closing
it only ever takes clicking elsewhere or `Ctrl`+`C`, the same as
cancelling anything else. Navigating to a different directory resets
all three back to their own defaults (the glob/regex filter cleared and
re-enabled, size/modified-time switched off), the same "scoped to
what's on screen, not carried across navigation" rule this filter has
always followed.

A name's color tells you what it is at a glance: dark-yellow highlight
for anything `Enter` navigates into, green for executable, red for a
broken symlink, darker red for something you can't read, cyan for a
symlink to a file, orange for a socket/FIFO/device, magenta for a
recognized archive, dim gray for a dotfile.

## Selecting files

`Space` checks or unchecks the row under the cursor. Right-drag over
several rows toggles all of them. The context menu's own "▸ Selection"
submenu adds Select all, Deselect all, and glob-pattern Select +/− for
things like `*.log`.

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
| `t` or `Ctrl`+`T` | open the switcher on the current tab; press again to walk down it |

The switcher lists every tab's full directory — the numbered strip
beside the filter button deliberately shows numbers only, so the header
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
| `s` | split / unsplit |
| `z` then `o` | flip between side-by-side and stacked |
| `Tab` | move to the other pane |
| click | move to the pane you clicked |
| `S` | swap the panes — left and right trade places |

**Choosing the second pane.** `s` on its own picks for you, in this
order: the tab you last split with, then the next tab along, and — if
only one tab is open — a brand-new tab on the same directory. That last
case is the useful default: one keypress gives you the same directory in
two panes, which is where copying between two places in one tree
usually starts.

To choose deliberately, open the tab list (`t`) and use a row's `◫`
button: on a real tab to show that one beside the current one, or on the
`+ New tab` row to create a tab and split with it in one step.

**Which pane is which.** The pane you are in keeps the keyboard, the
context menu, and every shortcut — everything acts on it, and only on
it. Its selected row carries the "focused" highlight (petrol); the other
pane's is dimmed. Each pane's own number strip marks the tab it holds,
so you can always see which two tabs you have up.

**Layout rules.** The panes never swap sides on their own: moving focus
across the divider changes nothing about the layout, and the `z` chord's
own `zw` (or the context menu's "Swap panes") is the only thing that
exchanges them.
Swapping moves the pane you are in to the other side and leaves the
keyboard with it, rather than handing focus over — moving between panes
is its own action. Switching to a different tab
while split replaces what the pane you are in shows and leaves the other
alone. Closing either pane's own tab ends the split; closing an
unrelated tab doesn't.

Whether panes sit side by side or stacked is the `split_stacked`
setting, so it survives a restart — `z` then `o` and the Options screen
both write to it. Which one works better depends on your terminal: side by
side keeps every row of both listings visible, stacking keeps the full
column width for long filenames.

The split itself is saved and restored along with the tabs. If one
pane's directory has disappeared by the next start, breakthrough opens
single-pane rather than restoring half a layout.

## The context menu

`m`, or right-click anywhere in the listing.

Only what actually applies right now is shown — not a fixed list of
everything the menu can ever do. On a plain file, that's:

```
Look
Edit
Rename
Copy
Cut
Move to Trash
Properties
▸ More actions
▸ Selection
▸ Tabs & Split
```

A few of these come and go on their own: **Edit** (and, one level into
"More actions", **tail -f**) drop out entirely for a directory — neither
means anything there. **Paste** only appears once Copy or Cut has
actually put something in the clipboard. Inside "Tabs & Split",
**Split orientation** and **Swap panes** only show up once a split is
actually active — there's nothing to orient or swap before that.

**While browsing the Trash itself**, the whole menu is replaced by a
much shorter one — almost nothing about the ordinary list still applies
to something already trashed:

```
Restore from Trash
Empty Trash
Properties
```

**Submenus** (marked with `▸`) replace the current list with just that
group's own entries, led by a `◂ Back` row — the same "drill in, one
level at a time" shape a settings app on a phone already uses, chosen
over a flyout beside the menu since it needs no horizontal room a
narrow terminal might not have. `Escape` backs out one level at a time
(a second press closes the menu once you're back at the top); Left
arrow does the same, alongside clicking or selecting `◂ Back` itself.
The menu's own title bar names where you are — "Menu" at the top,
"Menu › Selection" one level in.

- **▸ More actions** — `tail -f` (files only), `chown`, `chmod`, `sed`,
  Batch rename, Undo last rename, Remove (the permanent, asks-first
  sibling of Move to Trash above).
- **▸ Selection** — Select all, Deselect all, Select +, Select -
  (checkbox-based, the same these already reach on their own keys).
- **▸ Tabs & Split** — New tab, Close tab, Switch tab..., Split on/off,
  Split orientation, Swap panes.

Everything in the menu is also reachable from the keyboard or the
button bar — the menu is a discovery aid, never the only path to a
feature. The hidden-files/size-format/time-format toggles that used to
live at the bottom of this menu are Options-screen and keyboard-only
now (`.` and the `z` chord) — they're a view setting for the whole
panel, not an action on whatever the menu was opened for.

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

`E`, or the context menu's "sed". Runs a real `sed(1)`
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

`f`. Two modes:

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

`l`, `Enter` on a file, a double-click, or the context menu.
Read-only, full-screen.

- **Text, source, configs, diffs, logs** get syntax coloring for around
  200 languages, with no external dependency. Files larger than 8 MiB
  show their first 8 MiB rather than loading entirely.
- **Images** (PNG, JPEG, GIF, BMP, TIFF, WebP) render in the terminal,
  decoded and scaled in pure Go. Anything past 50 megapixels is refused
  with a note saying so, rather than spending seconds and hundreds of
  megabytes decoding a picture the terminal shows a few hundred
  characters of — the size is read from the header, so the refusal is
  free.
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

`I`, or the `<` button at the far end of the path bar, right after the
tab strip. A live, read-only panel on the right that follows the
cursor.

It shows the full stat block (type, permissions, owner, group, size,
timestamps, path), and on demand:

- `h` — SHA-256, SHA-1, MD5, SHA-512 and BLAKE2b-512.
- `k` — a directory's total size via `du -hs`. On a symlink or
  mount point it resolves the whole link chain first and reports the
  target's real size, naming what it actually measured.
- `M` — an image's metadata (EXIF and the like) — not implemented yet;
  pressing it shows a placeholder rather than doing nothing.

Each of `h`/`k`/`M` opens the sidebar first if it isn't already showing,
so "select something, press the key" works straight from plain
browsing. All three, along with `l` (Look) and `I` itself, also keep
working while Properties is open on the same file, rather than needing
it closed first — pressing `h` there fills in Properties' own hash
section instead of Details', so it never fills in a window you can't
see.

Images and PDFs get an inline preview with its own click zone for
fullscreen. Previews load in the background and only once the cursor has
rested briefly, so holding an arrow key through a directory costs
nothing: each row passed over cancels the one before it, and for a PDF
that cancellation kills the `pdftoppm` subprocess rather than leaving it
running. Only the stat block is read synchronously — one syscall, and
it is what the sidebar shows first anyway. `Tab` moves keyboard focus into the sidebar so its own
scrolling works; `Tab` again comes back. The `>` button in its corner
closes it.

## Properties

`i`, or the context menu. Editable: name, permission bits
(click a bit, press `r`/`w`/`x`, or type the octal value directly),
owner and group through a scrollable picker of every local user and
group, and the modified date and time.

`Tab` moves between fields, `Enter` or `Space` activates the focused
one, `Escape` cancels.

## Copy, Cut and Paste

`c`/`x` copy or cut the current selection — the whole selection, not
just the file under the cursor — onto an internal clipboard; `v` pastes
it into whatever directory the panel is showing. The context menu
offers all three too, with Paste only appearing once the clipboard
actually has something in it.

Pasting into the very directory a file is already in, or a directory
into one of its own subdirectories, is refused outright rather than
started at all — the first would destroy the only copy of the file
there ever was, the second would recurse into itself without any
bound.

Paste runs in the background rather than one file at a time in a
blocking loop. A file that copies or moves cleanly just lands at its
destination with no interruption. One that already exists there opens
a small dialog instead, without stopping anything else in the same
Paste:

| Option | Effect |
| --- | --- |
| Overwrite | Replace this one entry; the next conflict (if any) gets its own dialog |
| Overwrite all | Same, and apply it to every conflict the rest of this Paste runs into |
| Merge into existing folder | For a directory conflict specifically: copy the source's own files over it, keeping whatever's already there that the source doesn't have; the next conflict gets its own dialog |
| Merge all into existing folders | Same, for every conflict the rest of this Paste runs into |
| Skip | Leave the existing entry untouched; the next conflict gets its own dialog |
| Skip all | Same, for every conflict the rest of this Paste runs into |
| Overwrite all if source is newer | Overwrite only where the copied file's modified time is newer than the existing one; skip the rest — applies to every remaining conflict |
| Overwrite all if source is not empty | Overwrite only where the copied file actually has content, so a zero-byte source never replaces something real; skip the rest — applies to every remaining conflict |

For a plain file conflict, Overwrite and Merge behave identically —
there's nothing to merge, only a whole file's content to replace
either way. The distinction is real for a directory: **Overwrite makes
the destination identical to the source**, removing anything already
there that the source doesn't have, while **Merge keeps it**. Replacing
a compromised directory from a known-clean copy (a WordPress install's
own core files, say) needs Overwrite specifically — a merge would
leave anything an attacker planted there, that the clean source never
had to begin with, completely untouched.

`Up`/`Down` move between the options, `Enter`/`Space` applies the
highlighted one, `Escape` is the same as the preselected "Skip" — a
stray keypress can never overwrite anything by accident. Clicking
anywhere outside the dialog does nothing at all, on purpose — unlike
every other dialog in breakthrough, this one can't be dismissed by an
outside click: one of its own options, or `Escape`, is the only way
past it, so a conflict can never be left half-answered by an accidental
click elsewhere.

Everything that doesn't conflict keeps copying or moving in the
background while this dialog is open. Paste doesn't scan the whole
selection for conflicts before starting either — it checks each item
in order and starts copying/moving it immediately if nothing's in the
way, so most of a large selection is often already done, or well under
way, before you've even answered the first conflict. If Paste runs into
a second conflict before the first is answered, it doesn't stack a
second dialog on top — it queues behind the one already showing,
reflected right in that dialog's own message as "(N more waiting)" the
moment it's found (which, since checking whether something's in the
way is quick, usually means well before you've answered the one
currently shown), and gets its own dialog (or resolves automatically,
if an "all" option was already chosen) once the current one is
answered.

Starting a further Paste while one is already running doesn't run it
alongside the first, and doesn't replace it either — it queues behind
it, shown as "(+N queued)" right in the status bar's own progress line,
and starts automatically, in the order each was asked for, the moment
the one ahead of it finishes.

`Ctrl+C` stops a running Paste outright, whether or not its own
conflict dialog happens to be open at the time. Whatever's already
mid-write finishes normally — on disk, exactly where it was already
headed — rather than being interrupted mid-write; anything not yet
started simply never starts, including a whole further Paste still
queued behind this one. A *different* dialog (Properties, say)
happening to be open while a Paste merely continues in the background
is unaffected — `Ctrl+C` there closes that dialog as it always has,
since it's what you're actually looking at.

Any real failure along the way — a permission error, a full disk, and
so on, never a conflict, which always has a decision — is collected
rather than stopping the whole Paste at the first one, and reported
together once every item has a final outcome.

Every open tab showing the destination reloads automatically as items
actually land — not just once the whole Paste is fully done — in every
tab it's open in, not just wherever Paste was pressed, and not gated on
answering a conflict dialog that's still sitting open: whatever doesn't
conflict keeps landing in the background regardless (see above), and
now shows up there too, live, while the dialog waits. No manual
`zr`/`⭯` needed to see what's already landed. Cut gets the same
treatment on the other side: every open tab showing one of the moved
items' own source directories reloads too, as items actually leave, so
a tab you cut something from never keeps listing a file that's already
gone — which, within the same filesystem, can happen almost the
instant Paste is pressed, moves being close to instant there. Copy
leaves its own source list alone, since nothing there was ever removed.

### What's on the clipboard right now

Two indicators, both live for as long as there's actually something to
Paste:

- **Every row the clipboard holds** gets a full-row background tint —
  not just its checkbox, the whole row — so it stays visible while
  scrolling past it or browsing elsewhere. Cut gets a distinct,
  slightly pinkish-tinted gray rather than just a lighter shade of
  Copy's own neutral gray (`clipboard_cut_background`/
  `clipboard_copy_background` in the active color scheme): Cut is the
  more consequential of the two operations, since the original
  disappears once Paste actually succeeds, so it's worth being able to
  tell the two apart at a glance rather than needing a brightness
  comparison. A directory that's also on the
  clipboard shows this tint across its whole row instead of its usual
  gold name highlight — the two would otherwise compete for the same
  characters, so the clipboard tint wins outright rather than the two
  blending. This applies across every open tab currently showing that
  row, not only the tab Copy/Cut was pressed in, since the clipboard
  itself is shared by the whole application, not scoped to one tab.
- **The status bar** names what's held — "Copy: 3 files, 1 dir" or
  "Cut: 2 files" (a zero count is dropped rather than shown as "0
  dirs") — right after the chord countdown's own leading spot, ahead
  of the username. Disappears the moment the clipboard is empty again,
  the same "just show one less segment" shape as the disk-usage/
  uptime/load segments further along the same line.

### Watching a Paste while it runs

The moment a Paste actually starts, that same status bar spot switches
from the clipboard indicator to its own live progress instead, for
example:

```
● Copying 2/5 ▅ ▀▀▀▀▀▄▄▄▄▄ ~14s left holiday-photo.jpg
```

- A spinner (cycling dots, the same one Properties' own hash
  computation already uses) — a "still working" cue even during a
  single very large file, where the rest of this line might otherwise
  sit still for a while.
- "Copying"/"Moving", naming which of the two this is.
- How many of the selection's own top-level items have a final outcome
  so far, out of the total — a directory only advances this once, when
  the whole thing finishes, not per file inside it.
- A single character showing what percentage of the *entire
  selection's own byte size* has copied so far, filling up from a thin
  sliver to a solid block — the same glyph style the chord countdown
  uses to drain, just running the other way. This (and the estimated
  time below) only appears once a one-time background scan of the
  whole selection has measured its total size — started the moment
  Paste is pressed, running alongside the copy itself rather than
  delaying it, so a very large selection still starts copying
  immediately even though this one character and the estimate after
  the bar take a moment longer to show up.
- A two-row progress bar packed into a single line of half-block
  characters: the *top* half of each character is the same item-count
  fraction the count above already shows; the *bottom* half is the
  file currently being written's own byte progress. Both halves fill
  left to right independently, so a bar can show (for example) its top
  half half-full while its bottom half is already nearly done with the
  one file currently in flight.
- An estimated remaining duration, once the background scan above has
  a total to measure against and at least some progress to extrapolate
  from — based on the average throughput since the Paste started, so
  it settles down after the first moment rather than jumping around.
- The real file currently being written — its bare name, not the full
  path, so a long one doesn't crowd out everything after it. Inside a
  large directory, this keeps changing file by file even while the
  count/top bar above sit still waiting for that one directory to
  finish.

Only one file actually copies or moves at a time, in whatever order
each one happens to start, regardless of how large the selection is —
so this line's own "current file" is always a single, unambiguous
answer, and a very large Paste never launches more than one real disk
operation at once.

A same-filesystem move is atomic regardless of size — `mv` on the same
disk doesn't copy bytes at all, it just relinks a name — so cutting and
pasting within one filesystem usually finishes before this ever has a
chance to show anything at all. That's correct, not a missed update:
there is no meaningful "progress" to report for an operation that's
already done by the time it started.

## Trash, Remove and Restore

`Delete` moves the selection to your trash — recursively for a
directory, without a confirmation, because it is the reversible action
by design.

`D` (or `Ctrl`+`Delete`, terminal permitting) permanently deletes
instead, always behind a confirmation with Cancel preselected, so a
stray `Enter` can never trigger it.

The `g` chord's own `gb` browses the trash directly. While you're in it, `Delete`
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

The `o` chord's own `oo` (`o` then `o` again — see [The keyboard
layer](#the-keyboard-layer)), or the button bar's own `o…` cascade
followed by `o`. Categories down the left, that category's settings on
the right, action buttons underneath.

| Key | Action |
|---|---|
| `←` / `→` | move between the two panes |
| `↑` / `↓` | move within one |
| `Enter` / `Space` | change the selected setting |
| `?` | explain the selected setting in its own window |
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
| `mouse_enabled` | `true` | Mouse reporting on at startup (clicks/drags work, but blocks the terminal's own native text selection) |
| `pager` | `builtin` | How Look renders a file: `builtin` or `external` |
| `trash_persistent` | `true` | Keep trashed files across login sessions |
| `trash_max_age_days` | `30` | Remove trashed items older than this at startup; `0` disables |
| `trash_quota_percent` | `10` | Keep the trash at or under this share of its filesystem; `0` disables |
| `language` | `en` | Reserved for future translations — parsed, no effect yet |

## Keyboard reference

### The keyboard layer (primary)

The plain letters and `g`/`p`/`z` chords covered in
[The keyboard layer](#the-keyboard-layer) above are the primary route
to everyday actions — see that section for the full table. Everything
below this point is still fully working, kept for muscle memory built
on it before that layer existed.

### Anywhere

| Key | Action |
|---|---|
| `Ctrl`+`Q` | Quit (asks first) |
| `Ctrl`+`C` | Back out of whatever is open — never quits |

### File panel

Almost everything that used to live here as its own Ctrl-letter binding
now has exactly one keyboard path — the plain letter or chord on [The
keyboard layer](#the-keyboard-layer) above (Options and mouse reporting
most recently, via the `o` chord's own `oo`/`om`) — with no Ctrl
equivalent left at all. What remains below reaches something the plain
letter genuinely can't (`Ctrl`+`T` while the tab switcher itself is
already open; `Ctrl`+`Delete` regardless of terminal support).

| Key | Action | also on |
|---|---|---|
| `Enter` | Open a directory, or Look at a file | |
| `Space` | Select / deselect | |
| `Tab` | Cycle focus: panes, Details sidebar, tool windows | |
| `Delete` | Move to Trash | `d` |
| `Ctrl`+`Delete` | Remove permanently (asks first), best-effort depending on terminal | `D` (always reliable) |
| `Ctrl`+`1`…`0`, `Alt`+`1`…`0` | Jump to a tab | |
| `Ctrl`+`T` | Tab switcher; also walks to the next tab while the switcher is already open, which `t` alone can't | `t` |
| `Ctrl`+`Tab` / `Ctrl`+`Shift`+`Tab` | Step through tabs | |

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

### Terminal recovery over a dropped connection

breakthrough responds to SIGHUP, SIGTERM and SIGINT by restoring the
terminal (exiting the alternate screen buffer, turning off mouse
reporting) before the process actually exits — the same reason
vim/htop/less and most other full-screen terminal programs install a
handler like this. A dropped SSH connection delivers exactly one of
these (SIGHUP — literally "hang up") once the session tears down, and
without a handler, an unhandled signal terminates a Go process
immediately, skipping every bit of cleanup: whichever raw modes were
on stay on at the terminal emulator itself, showing up afterward as
garbled output and mouse movements arriving as stray character
sequences.

This can only help, not guarantee a fix in every case: a connection
that's already fully, physically severed leaves no channel left to
send a reset sequence over, so nothing running remotely can undo that
after the fact. For that situation — or simply to keep breakthrough
(and whatever it's running in its own embedded shell) alive across a
dropped connection at all, rather than relying on a signal handler to
merely leave a clean terminal behind — running it inside a remote
`tmux` or `screen` session is the robust fix: the session keeps
running, completely undisturbed, independent of any one SSH
connection to it, and reattaching afterward needs no recovery of any
kind because the new connection's own terminal was never touched by
breakthrough in the first place.
