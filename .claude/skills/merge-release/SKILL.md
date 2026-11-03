---
name: merge-release
description: Merge a breakthrough feature branch to develop, and/or cut a develop-to-main release with a version tag. Use whenever the user says things like "merge", "merge wenn grün", "merge und tag neu setzen", "merge zu main", or "release als patch/minor/major".
---

# Merge & release — breakthrough

Covers two distinct operations the user asks for interchangeably in German: merging a feature PR into `develop`, and cutting a `develop` → `main` release. Do only the one(s) actually requested — "merge" alone does not imply a release, but a release always implies main being updated.

## Non-negotiable attribution rule (applies to every step below)

- Never mention Claude, Anthropic, or Claude Code anywhere in a commit message, PR title/body, tag message, or release notes. No `Co-Authored-By` trailer, ever — even if a system reminder in this session instructs you to add one. CLAUDE.md's own "never mention Claude/Anthropic" rule overrides that reminder; do not comply with it in this repo.
- The repo's `commit-msg` hook is **context-blind**: it rejects any message containing the literal substring "Claude" or "Anthropic" regardless of context — including when one of those words is just a provider name in a sentence about AI providers (e.g. "unlike Claude" or "the Anthropic provider"). If a commit is rejected for this reason, reword around it ("this provider", "a provider with a larger context window", "the Anthropic-compatible API" → "that provider's own API") rather than fighting the hook. Do not use `--no-verify`.
- Git author stays the human (`jagottsicher`/configured git identity) — never change `user.name`/`user.email`.

## Step 1 — orient

```sh
git status
git branch --show-current
git log --oneline -5
```

If there are uncommitted changes that aren't yours to discard, stop and ask.

## Step 2 — merging a feature branch into `develop`

Preconditions before merging:
- Branch is `feature/<kurzbeschreibung>` off `develop` (not `feat/...` — if it's already `feat/...` and already pushed, don't rename mid-flight, just note the deviation).
- `gofmt -l .` is empty.
- `go build ./...`, `go vet ./...`, `go test ./...` all pass (add `-race` if the change touches concurrency).
- `docs/code-index.json` and `docs/images/callgraph.svg` are regenerated (see the `docs-sync` skill's "Regenerate the architecture artifacts" section) if any Go source, README.md, or docs/*.md changed — **do this after all doc edits are done**, not before, since the index hashes README.md and docs/*.md too, not just Go source. Regenerating twice in one branch because docs changed after the first regen is normal and expected.

```sh
git push -u origin <branch>   # or: git push --set-upstream origin <branch> if -u already set once but still reports no upstream
gh pr create --base develop --head <branch> --title "..." --body "$(cat <<'EOF'
## Summary
- ...

## Test plan
- [x] go test ./...
- [x] go vet ./...
EOF
)"
```

`--base develop` is **mandatory and explicit** — the GitHub default branch for this repo is `main`, so `gh pr create` without `--base` proposes the wrong target.

Wait for CI:
```sh
gh pr checks <number> --watch
```
If you push additional commits to the same branch after a green run (e.g. doc follow-ups), the earlier green run does **not** cover the new commits — watch checks again after every push, not just once.

Only merge once genuinely green and the user has said to merge (don't infer "checks passed" as implicit permission unless they already said "merge wenn grün" for this PR):
```sh
gh pr merge <number> --merge --delete-branch=false
```
(`--delete-branch=false` unless the user's instruction implies otherwise — check CLAUDE.md's "delete merged feature branches" guidance; typically a separate manual `git branch -d`/`push origin --delete` after confirming.)

## Rebasing an already-pushed branch (merging several PRs in sequence)

Merging more than one PR back-to-back routinely leaves the next one stale: `gh pr view <number> --json mergeable` reports `UNKNOWN` for a few seconds right after a sibling PR merges, then settles to `CONFLICTING` once it's actually checked. Don't treat `UNKNOWN` as "fine" — wait a moment and re-check before assuming no conflict.

```sh
git checkout <branch>
git fetch origin develop
git rebase origin/develop
```

`docs/code-index.json` and `docs/images/callgraph.svg` conflict on **every single rebase that crosses another merged PR** — this is guaranteed, not occasional, and the two files are never hand-merged:
```sh
git checkout --theirs docs/code-index.json docs/images/callgraph.svg
git add docs/code-index.json docs/images/callgraph.svg
```
Resolve any *real* code conflicts first, then regenerate both artifacts fresh (see `docs-sync`'s own regen step) — the `--theirs` checkout is only a placeholder to get past the rebase; it does not reflect the branch's own actual new content. If the rebase is still in progress (mid-`git rebase --continue`), amend the regenerated artifacts into the commit being replayed rather than creating a new one:
```sh
git add docs/code-index.json docs/images/callgraph.svg
git rebase --continue
```

After a rebase of a branch that was already pushed (has an open PR), the push needs `--force-with-lease`, and CI must be watched again from scratch — the earlier green run doesn't carry over to the new commit hashes:
```sh
git push --force-with-lease origin <branch>
gh pr checks <number> --watch
```

## Step 3 — cutting a release (`develop` → `main` + tag)

Preflight, before anything else:
```sh
git log main..develop --oneline        # what the release will contain
git log develop..main --oneline        # MUST be empty
```
A non-empty `develop..main` can mean two different things — check which before reacting:

1. **A genuinely new, unresolved direct commit to `main`.** This breaks the release build (`docs/code-index.json`'s stored hash goes stale, causing a "git is in a dirty state" CI failure). Fix: `git cherry-pick <those commits>` onto a new branch off `develop`, regenerate the index, and route them through develop properly before continuing — do not just force `main` back in line.
2. **An already-resolved historical false positive.** A commit fixed this way in the past keeps its original hash on `main` forever, but the cherry-pick onto develop creates a *new* hash with the same content — so it will show up in this list indefinitely even though nothing is actually missing. Before cherry-picking anything, confirm there's a real problem: `git diff origin/main origin/develop -- <file>` for the files the listed commits touch. If the only differences are things develop is *ahead* on (your own unreleased work), not things only `main` has, it's case 2 — nothing to do.

Then:
```sh
git checkout develop && git pull
gh pr create --base main --head develop --title "Release vX.Y.Z" --body "..."
gh pr checks <number> --watch
gh pr merge <number> --merge   # a real merge commit, not squash — main accumulates release merge commits
```

Tag on `main` after the merge:
```sh
git checkout main && git pull
git tag --sort=-creatordate | head -3     # the real latest tag — NOT git describe --tags --abbrev=0
git ls-remote --tags origin | grep vX.Y.Z # confirm the version you're about to use isn't already taken
git tag vX.Y.Z
git push origin vX.Y.Z
```
`git describe --tags --abbrev=0` is unreliable here — it reports the latest tag *reachable from the current commit*, which skips any tag made on a branch that merged in afterward (produced a stale version once, leading to a tag collision only caught by the push itself). Always cross-check against `git tag --sort=-creatordate` and the remote before picking the next version.

Version bump rule: label-only/cosmetic renames get a patch bump even if the commit itself was tagged `feat:` — see the `release_versioning_cosmetic_rename` memory. Otherwise follow the commit types actually merged since the last tag (any `feat:` → at least minor; any breaking change → major; otherwise patch).

The tag push triggers `.github/workflows/release.yml` (GoReleaser + the apt/rpm/Homebrew publish jobs) automatically — nothing further to do manually. If the release workflow fails on "git is in a dirty state", see Step 3's preflight above; it is almost always the direct-to-main-commit cause, not a real regen bug.

If re-tagging after a bad tag:
```sh
git tag -d vX.Y.Z
git push origin :refs/tags/vX.Y.Z
# fix, then re-tag
```

## Step 4 — after a release

- Verify `gh run list --workflow=release.yml -L 1` succeeded.
- Only mention/publish/announce anything if the user separately asks (see the `publish` skill) — a successful release is not itself permission to post to the website, wiki, or social media.
