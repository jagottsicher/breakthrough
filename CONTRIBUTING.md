# Contributing to breakthrough

breakthrough is a POSIX-friendly, window/menu-based TUI file manager for
the terminal, written in Go with [tcell](https://github.com/gdamore/tcell)
and [tview](https://github.com/rivo/tview). Contributions of any size are
welcome — this is early-stage and there's a lot to build.

## Prerequisites

- Go, matching the version in [`go.mod`](go.mod) or newer
- `git`

## Local setup

```console
git clone <your-fork-url>
cd breakthrough
go build ./...
go test ./...
```

## Branch workflow

- Fork from **`develop`**, not `main`.
- Create feature branches off `develop`, named `feature/<short-description>`.
- **Open your pull request against `develop`.** GitHub's "New Pull Request"
  button may still default to `main` for older forks/bookmarks — please
  double-check the base branch before submitting. `main` is reserved for
  tagged releases only and never receives commits directly.
- After a feature branch is merged, delete it (GitHub does this
  automatically for branches in this repo).

## Code style

- Run `gofmt`, `go vet ./...`, and `golangci-lint run` before committing.
- Any change to code, packages, imports, or symbols must come with a
  freshly regenerated `docs/code-index.json` and `docs/images/callgraph.svg`
  (see [Architecture](README.md#architecture)) — CI rejects a pull request
  where the committed files don't match what a fresh run produces.
- `make check` runs all of the above (`gofmt`, `go vet`, `golangci-lint`,
  `go test -race -cover`, both generators) in one pass and fails if
  anything is left out of sync; see `make help` for the individual
  targets it's built from.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat:`, `fix:`, `refactor:`, `docs:`, `chore:`, `test:`).
- Comments are in English — please comment generously, especially around
  non-obvious edge cases.

## Tests

Changes to filesystem logic (`internal/fsops` — copy, move, rename, trash)
must come with accompanying tests. That package carries the highest risk
of data loss in this project if something goes wrong, so untested changes
there won't be merged.

## Further reading

For the broader concept, architecture, and vision behind breakthrough, see
[`docs/whitepaper.md`](docs/whitepaper.md).
