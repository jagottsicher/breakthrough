package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveChainSingleHop(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	chain := ResolveChain(link)

	if len(chain.Hops) != 1 || chain.Hops[0] != target {
		t.Errorf("Hops = %v, want [%q]", chain.Hops, target)
	}
	if chain.Broken || chain.Cyclic || chain.TooDeep {
		t.Errorf("chain = %+v, want a clean resolution", chain)
	}
	if chain.FinalIsDir {
		t.Error("FinalIsDir = true, want false — the target is a file")
	}
}

func TestResolveChainMultiHop(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "final.txt")
	mid := filepath.Join(dir, "mid")
	start := filepath.Join(dir, "start")

	if err := os.WriteFile(final, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(final, mid); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mid, start); err != nil {
		t.Fatal(err)
	}

	chain := ResolveChain(start)

	want := []string{mid, final}
	if len(chain.Hops) != len(want) {
		t.Fatalf("Hops = %v, want %v", chain.Hops, want)
	}
	for i := range want {
		if chain.Hops[i] != want[i] {
			t.Errorf("Hops[%d] = %q, want %q", i, chain.Hops[i], want[i])
		}
	}
	if chain.Final != final {
		t.Errorf("Final = %q, want %q", chain.Final, final)
	}
	if chain.Broken || chain.Cyclic || chain.TooDeep {
		t.Errorf("chain = %+v, want a clean resolution", chain)
	}
}

func TestResolveChainToDirectory(t *testing.T) {
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "target-dir")
	link := filepath.Join(dir, "link")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetDir, link); err != nil {
		t.Fatal(err)
	}

	chain := ResolveChain(link)
	if !chain.FinalIsDir {
		t.Error("FinalIsDir = false, want true")
	}
}

func TestResolveChainBroken(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "nope"), link); err != nil {
		t.Fatal(err)
	}

	chain := ResolveChain(link)
	if !chain.Broken {
		t.Error("Broken = false, want true")
	}
	if chain.Cyclic || chain.TooDeep {
		t.Errorf("chain = %+v, want only Broken set", chain)
	}
}

func TestResolveChainBrokenMidway(t *testing.T) {
	dir := t.TempDir()
	start := filepath.Join(dir, "start")
	mid := filepath.Join(dir, "mid")
	if err := os.Symlink(mid, start); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "nope"), mid); err != nil {
		t.Fatal(err)
	}

	chain := ResolveChain(start)
	if !chain.Broken {
		t.Error("Broken = false, want true")
	}
	// The chain should still show how far it got before breaking.
	if len(chain.Hops) != 2 {
		t.Errorf("Hops = %v, want 2 entries (mid, then the dangling target)", chain.Hops)
	}
}

func TestResolveChainCyclic(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.Symlink(b, a); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(a, b); err != nil {
		t.Fatal(err)
	}

	chain := ResolveChain(a)
	if !chain.Cyclic {
		t.Error("Cyclic = false, want true")
	}
	if chain.Broken || chain.TooDeep {
		t.Errorf("chain = %+v, want only Cyclic set", chain)
	}
}

func TestResolveChainSelfLoop(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "self")
	if err := os.Symlink(link, link); err != nil {
		t.Fatal(err)
	}

	chain := ResolveChain(link)
	if !chain.Cyclic {
		t.Error("Cyclic = false, want true for a symlink pointing at itself")
	}
}

// TestResolveChainRelativeTarget pins that a relative Readlink result is
// resolved against the *symlink's own* directory, not the process's
// working directory or the chain's starting point.
func TestResolveChainRelativeTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(dir, "sub", "final.txt")
	if err := os.WriteFile(final, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// sub/link -> final.txt (relative), which must resolve to sub/final.txt.
	link := filepath.Join(dir, "sub", "link")
	if err := os.Symlink("final.txt", link); err != nil {
		t.Fatal(err)
	}

	chain := ResolveChain(link)
	if len(chain.Hops) != 1 || chain.Hops[0] != final {
		t.Errorf("Hops = %v, want [%q]", chain.Hops, final)
	}
}
