package fsops

import (
	"os"
	"path/filepath"
)

// MaxLinkChainDepth caps how many hops ResolveChain follows before giving
// up and reporting the chain as too deep — matches Linux's own ELOOP
// behavior (which kicks in around 40), so a chain this code refuses to
// finish is one the OS itself would refuse to open too.
const MaxLinkChainDepth = 40

// LinkChain is what following a symlink, transitively through any
// further symlinks it points to, leads to.
type LinkChain struct {
	// Hops is each step's target, in the order followed — Hops[0] is the
	// same target Stat's own LinkTarget field already reports for the
	// starting symlink; later entries are only present for a genuine
	// multi-hop chain.
	Hops []string

	// Final is the fully resolved path once a non-symlink is reached, or
	// the last hop attempted if Broken/Cyclic/TooDeep — in practice this
	// always equals Hops' own last element once Hops is non-empty (both
	// are set from the same value), kept as its own field mainly so
	// FinalIsDir has something to describe.
	Final      string
	FinalIsDir bool // meaningless if Broken, Cyclic, or TooDeep

	Broken  bool // some hop's target doesn't exist
	Cyclic  bool // a hop revisited a path already seen earlier in this chain
	TooDeep bool // MaxLinkChainDepth was reached without resolving
}

// ResolveChain follows path (which must itself be a symlink — callers
// check Info.IsSymlink first) through as many further symlinks as it
// takes to reach something that isn't one, recording every hop along the
// way. Unlike os.Stat or filepath.EvalSymlinks, which also resolve the
// whole chain but only report the final destination, this is for
// surfacing each intermediate link too — see the Info overlay's "Chain"
// line, shown once there's more than one hop to show.
func ResolveChain(path string) LinkChain {
	var chain LinkChain
	seen := make(map[string]bool)
	current := path

	for i := 0; i < MaxLinkChainDepth; i++ {
		fi, err := os.Lstat(current)
		if err != nil {
			chain.Broken = true
			chain.Final = current
			return chain
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			chain.Final = current
			chain.FinalIsDir = fi.IsDir()
			return chain
		}
		if seen[current] {
			chain.Cyclic = true
			chain.Final = current
			return chain
		}
		seen[current] = true

		target, err := os.Readlink(current)
		if err != nil {
			chain.Broken = true
			chain.Final = current
			return chain
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(current), target)
		}
		chain.Hops = append(chain.Hops, target)
		current = target
	}

	chain.TooDeep = true
	chain.Final = current
	return chain
}
