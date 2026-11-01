---
name: docs-sync
description: Update breakthrough's documentation (README.md, built-in help internal/ui/help.go, docs/user-guide.md, docs/installation.md, docs/breakthrough.1 manpage) after a feature or behavior change, check for outdated info while there, and regenerate the architecture artifacts. Use whenever the user asks to update docs/readme/help/manpages, or says "Doku-Durchgang" / "doku aktualisieren".
---

# Documentation sync — breakthrough

breakthrough keeps the same facts stated in up to five places that never update themselves together: `docs/user-guide.md` (the online guide), `internal/ui/help.go` (the in-app "?" help, deliberately redundant with the guide, verified against source independently), `README.md`, `docs/installation.md`, and `docs/breakthrough.1` (the manpage — much lower detail, usually out of scope for a feature-level change; check whether it even mentions the area at all before touching it).

## Non-negotiable attribution rule

Never mention Claude, Anthropic, or Claude Code in any doc, comment, or commit message produced by this skill. See the `merge-release` skill's own section on the context-blind `commit-msg` hook — reword around literal "Claude"/"Anthropic" mentions rather than fighting it.

## Step 1 — find every place the changed behavior is already described

```sh
grep -rn "<old keyword/number/behavior>" README.md docs/*.md internal/ui/help.go
```
Don't assume only one file mentions it — the AI Consigliere / Machiavelli Mode area alone touches all of README.md, help.go, and user-guide.md simultaneously, each with its own independent wording (not shared templates).

## Step 2 — while already in a given section, scan for drift unrelated to the current change

This project's own workflow explicitly asks for this ("bei der Gelegenheit auch nach outdated Infos suchen"): read the surrounding paragraphs, not just the lines you're about to edit. Real examples caught this way: `F3`/`F4` key references for features that were rebound to `om`/`t`/`Ctrl+T` long ago; "progress bars planned" after they'd already shipped; a documented filter syntax (AND/OR/NAND/NOR/NOT) that existed in the built-in help but was missing from the online guide. Verify keybindings against `internal/ui/keymap.go` and the real capture functions, not against memory of an earlier doc pass.

## Step 3 — edit each file in its own voice

- `README.md`: feature-list prose, longer paragraphs, aimed at a GitHub visitor skimming for capability.
- `internal/ui/help.go`: compact, `[::b]Heading[::-]` tview markup, aimed at someone already running the app. Keep line wrapping consistent with the surrounding paragraphs (~70 cols inside the raw string).
- `docs/user-guide.md`: the authoritative long-form reference — tables for settings, exact behavior, edge cases. This is the one source `internal/consigliere`'s own documentation-search feature indexes, so a gap here is a gap the AI Consigliere itself can't answer from.
- `docs/installation.md`: only touch if install/package-manager/platform specifics changed.
- `docs/breakthrough.1`: only touch if it already covers the area (check first — much of the app, including the entire AI Consigliere, isn't in scope for the manpage at all and that's correct, not a gap).

## Step 4 — rebuild and test

```sh
gofmt -l .
go build ./...
go vet ./...
go test ./...
```
All must be clean before regenerating artifacts — a build/test break makes the regen below meaningless.

## Step 5 — regenerate the architecture artifacts (do this last, after every doc edit)

`docs/code-index.json` hashes README.md and every `docs/*.md` file, not just Go source — regenerating before finishing doc edits means regenerating again afterward. Do this once, after Step 3 is actually done:

```sh
go run ./cmd/gen-context-index -root . -output docs/code-index.json
```

The callgraph needs a **patched** `go-callvis`, not whatever's on `$PATH` — the plain installed binary crashes under this repo's Go SSA builder with its default `golang.org/x/tools` version, and even when it doesn't crash, cluster ordering is nondeterministic across runs (spurious diffs). If `/tmp/go-callvis-patched` doesn't exist in this session, rebuild it:

```sh
git clone https://github.com/ofabry/go-callvis /tmp/go-callvis-src
cd /tmp/go-callvis-src
git apply /home/jens/development/AI-agency/breakthrough/breakthrough/.github/callgraph-patches/go-callvis-determinism.patch
go get golang.org/x/tools@v0.49.0
go build -o /tmp/go-callvis-patched .
cd /home/jens/development/AI-agency/breakthrough/breakthrough
```

Then, from the repo root:
```sh
/tmp/go-callvis-patched -focus github.com/jagottsicher/breakthrough/cmd/breakthrough \
  -file docs/images/callgraph -rankdir TB ./...
```

Check `git status` afterward — if neither file actually changed (common for doc-only or small helper-function changes), there's nothing to re-commit; don't force a no-op commit.

## Step 6 — commit

Both artifacts (if changed) go in the **same commit** as the doc/code changes that caused them, per CLAUDE.md — never a separate "regen artifacts" commit. The release workflow fails with "git is in a dirty state" if a merged PR's regen doesn't match what CI generates fresh, so skipping this step doesn't surface until the next release, far from the PR that caused it.

## Related

For publishing the same change to the live website and the GitHub wiki (separate from this repo's own docs), see the `publish` skill.
