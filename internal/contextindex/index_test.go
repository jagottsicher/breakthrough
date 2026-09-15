package contextindex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildIndexesFilesPackagesSymbolsAndImports(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.test/app\n\ngo 1.27\n")
	mustWrite(t, filepath.Join(root, "main.go"), "package main\n\nimport \"example.test/app/internal/thing\"\n\nfunc main() { thing.Run() }\n")
	mustWrite(t, filepath.Join(root, "internal/thing/thing.go"), "package thing\n\nconst Answer = 42\n\nfunc Run() {}\n\ntype Worker struct{}\nfunc (Worker) Work() {}\n")
	mustWrite(t, filepath.Join(root, "README.md"), "context\n")

	index, err := Build(root, Options{Exclude: []string{filepath.Join(root, "docs/index.json")}})
	if err != nil {
		t.Fatal(err)
	}
	if index.MerkleRoot == "" || len(index.Files) != 4 || len(index.Packages) != 2 {
		t.Fatalf("unexpected index summary: files=%d packages=%d root=%q", len(index.Files), len(index.Packages), index.MerkleRoot)
	}
	if !hasSymbol(index.Symbols, "Run", "function") || !hasSymbol(index.Symbols, "Work", "method") || !hasSymbol(index.Symbols, "Answer", "const") {
		t.Fatalf("expected function, method, and const symbols, got %#v", index.Symbols)
	}
	mainPackage := findPackage(index.Packages, "example.test/app")
	if !contains(mainPackage.Imports, "example.test/app/internal/thing") {
		t.Fatalf("main package imports = %#v", mainPackage.Imports)
	}
}

func TestBuildSkipsGitEntryWhenItIsAPlainFile(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.test/app\n")
	mustWrite(t, filepath.Join(root, "README.md"), "context\n")
	// A git worktree (or a submodule checkout) has ".git" as a plain file
	// pointing at the real git directory elsewhere, not a directory itself.
	// The indexer must skip it exactly like the ordinary ".git" directory a
	// regular clone has, or the index picks up an untracked, environment-
	// specific entry and a Merkle root that depends on where the worktree
	// happens to live on disk.
	mustWrite(t, filepath.Join(root, ".git"), "gitdir: /some/other/path/.git/worktrees/example\n")

	index, err := Build(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range index.Files {
		if file.Path == ".git" {
			t.Fatalf("expected .git to be excluded, got it indexed: %#v", file)
		}
	}
	if len(index.Files) != 2 {
		t.Fatalf("expected only go.mod and README.md to be indexed, got %#v", index.Files)
	}
}

// TestBuildSkipsRootLevelBinDirectory pins a real, previously-unnoticed
// gap: nothing excluded bin/ (GoReleaser's/a local `go build -o
// bin/...`'s own gitignored output) before, so a binary someone
// happened to have built locally silently ended up in the committed
// index — caught only once CI's own fresh checkout, with no such
// binary lying around, regenerated a different one and the two
// disagreed.
func TestBuildSkipsRootLevelBinDirectory(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.test/app\n")
	mustWrite(t, filepath.Join(root, "README.md"), "context\n")
	mustWrite(t, filepath.Join(root, "bin/app"), "not a real binary, just test content\n")

	index, err := Build(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range index.Files {
		if file.Path == "bin/app" {
			t.Fatalf("expected bin/app to be excluded, got it indexed: %#v", file)
		}
	}
	if len(index.Files) != 2 {
		t.Fatalf("expected only go.mod and README.md to be indexed, got %#v", index.Files)
	}
}

func TestMerkleRootChangesWithFileContent(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.test/app\n")
	mustWrite(t, filepath.Join(root, "README.md"), "one\n")
	first, err := Build(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first.MerkleRoot != second.MerkleRoot {
		t.Fatalf("identical trees have different roots: %s != %s", first.MerkleRoot, second.MerkleRoot)
	}
	mustWrite(t, filepath.Join(root, "README.md"), "two\n")
	third, err := Build(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first.MerkleRoot == third.MerkleRoot {
		t.Fatal("changing file content did not change Merkle root")
	}
}

func TestWriteProducesStableJSON(t *testing.T) {
	root := t.TempDir()
	index := Index{Version: 1, MerkleRoot: "root"}
	path := filepath.Join(root, "index.json")
	if err := Write(path, index); err != nil {
		t.Fatal(err)
	}
	var decoded Index
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.MerkleRoot != "root" {
		t.Fatalf("decoded root = %q", decoded.MerkleRoot)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasSymbol(symbols []Symbol, name, kind string) bool {
	for _, symbol := range symbols {
		if symbol.Name == name && symbol.Kind == kind {
			return true
		}
	}
	return false
}

func findPackage(packages []Package, importPath string) Package {
	for _, pkg := range packages {
		if pkg.ImportPath == importPath {
			return pkg
		}
	}
	return Package{}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
