package logdiskcache

import (
	"context"
	"encoding/gob"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/jagottsicher/breakthrough/internal/logview"
)

const mb = 1 << 20

func mustOpen(t *testing.T, root string) *Cache {
	t.Helper()
	c, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return c
}

func TestPutThenGetRoundTrips(t *testing.T) {
	c := mustOpen(t, t.TempDir())
	entries := []logview.Entry{{Message: "hello", Raw: "hello", File: "app.log.1"}}

	if err := c.Put("remote:a@hosta:22", "key1", entries, 10*mb); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1")
	if !ok {
		t.Fatal("Get: want hit after Put")
	}
	if len(got) != 1 || got[0].Message != "hello" {
		t.Errorf("Get = %+v, want the entries just Put", got)
	}
}

func TestGetMissesForUnknownKeyOrIdentity(t *testing.T) {
	c := mustOpen(t, t.TempDir())
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "x"}}, 10*mb); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := c.Get(context.Background(), "remote:a@hosta:22", "other-key"); ok {
		t.Error("Get(other-key) = hit, want miss")
	}
	if _, ok := c.Get(context.Background(), "remote:b@hostb:22", "key1"); ok {
		t.Error("Get under a different identity = hit, want miss — must not leak across connections")
	}
}

// TestIdenticalKeyAcrossIdentitiesStaysSeparate pins the same real bug
// Phase 1 fixed for the RAM cache, now at the disk layer: two
// connections that happen to share the same path|size|mtime key must
// never collide into one cache entry.
func TestIdenticalKeyAcrossIdentitiesStaysSeparate(t *testing.T) {
	c := mustOpen(t, t.TempDir())
	key := "/var/log/app.log.1|1234|1700000000"

	if err := c.Put("remote:a@hosta:22", key, []logview.Entry{{Message: "from A"}}, 10*mb); err != nil {
		t.Fatalf("Put A: %v", err)
	}
	if err := c.Put("remote:b@hostb:22", key, []logview.Entry{{Message: "from B"}}, 10*mb); err != nil {
		t.Fatalf("Put B: %v", err)
	}

	gotA, ok := c.Get(context.Background(), "remote:a@hosta:22", key)
	if !ok || gotA[0].Message != "from A" {
		t.Errorf("Get A = %+v, %v, want entries from A", gotA, ok)
	}
	gotB, ok := c.Get(context.Background(), "remote:b@hostb:22", key)
	if !ok || gotB[0].Message != "from B" {
		t.Errorf("Get B = %+v, %v, want entries from B", gotB, ok)
	}
}

func TestPutWithNonPositiveMaxBytesDisablesCaching(t *testing.T) {
	c := mustOpen(t, t.TempDir())
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "x"}}, 0); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1"); ok {
		t.Error("Get after a non-positive maxBytes Put = hit, want miss")
	}
	if c.TotalBytes() != 0 {
		t.Errorf("TotalBytes() = %d, want 0", c.TotalBytes())
	}
}

func TestPutEvictsLeastRecentlyUsedAcrossIdentitiesWhenOverBudget(t *testing.T) {
	c := mustOpen(t, t.TempDir())
	big := make([]logview.Entry, 2000)
	for i := range big {
		big[i] = logview.Entry{Message: "padding to make this entry batch reasonably large for a byte budget test"}
	}

	// Measure one batch's own real on-disk size first, rather than
	// guessing at gob's own per-entry overhead — the budget below only
	// needs to fit a bit more than two such batches, forcing eviction
	// once a third lands.
	probe := mustOpen(t, t.TempDir())
	if err := probe.Put("probe", "probe", big, 1<<30); err != nil {
		t.Fatalf("size probe Put: %v", err)
	}
	batchBytes := probe.TotalBytes()
	budget := batchBytes*2 + batchBytes/2

	if err := c.Put("remote:a@hosta:22", "key1", big, budget); err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	if err := c.Put("remote:b@hostb:22", "key2", big, budget); err != nil {
		t.Fatalf("Put 2: %v", err)
	}
	// Touch key1 again, after key2's own Put, so key1 is now the more
	// recently used of the two.
	if _, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1"); !ok {
		t.Fatal("setup: want key1 cached")
	}
	// A third, different identity's Put should now evict whichever of
	// key1/key2 is least recently used — key2 (never Get'd again
	// after its own Put) should go, key1 (Get'd most recently) should
	// survive.
	if err := c.Put("remote:c@hostc:22", "key3", big, budget); err != nil {
		t.Fatalf("Put 3: %v", err)
	}

	if _, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1"); !ok {
		t.Error("key1 was evicted, want it to survive (most recently used)")
	}
	if _, ok := c.Get(context.Background(), "remote:b@hostb:22", "key2"); ok {
		t.Error("key2 survived, want it evicted (least recently used)")
	}
}

func TestClearIdentityOnlyRemovesThatIdentity(t *testing.T) {
	c := mustOpen(t, t.TempDir())
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}}, 10*mb); err != nil {
		t.Fatalf("Put A: %v", err)
	}
	if err := c.Put("remote:b@hostb:22", "key1", []logview.Entry{{Message: "b"}}, 10*mb); err != nil {
		t.Fatalf("Put B: %v", err)
	}
	if err := c.ClearIdentity("remote:a@hosta:22"); err != nil {
		t.Fatalf("ClearIdentity: %v", err)
	}
	if _, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1"); ok {
		t.Error("cleared identity still has a hit")
	}
	if _, ok := c.Get(context.Background(), "remote:b@hostb:22", "key1"); !ok {
		t.Error("untouched identity lost its own entry")
	}
}

func TestClearAllRemovesEveryIdentity(t *testing.T) {
	c := mustOpen(t, t.TempDir())
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}}, 10*mb); err != nil {
		t.Fatalf("Put A: %v", err)
	}
	if err := c.Put("remote:b@hostb:22", "key1", []logview.Entry{{Message: "b"}}, 10*mb); err != nil {
		t.Fatalf("Put B: %v", err)
	}
	if err := c.ClearAll(); err != nil {
		t.Fatalf("ClearAll: %v", err)
	}
	if _, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1"); ok {
		t.Error("a still cached after ClearAll")
	}
	if _, ok := c.Get(context.Background(), "remote:b@hostb:22", "key1"); ok {
		t.Error("b still cached after ClearAll")
	}
	if c.TotalBytes() != 0 {
		t.Errorf("TotalBytes() = %d, want 0 after ClearAll", c.TotalBytes())
	}
	if len(c.Identities()) != 0 {
		t.Errorf("Identities() = %v, want none after ClearAll", c.Identities())
	}
}

func TestIdentitiesListsLabelAndSize(t *testing.T) {
	c := mustOpen(t, t.TempDir())
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}}, 10*mb); err != nil {
		t.Fatalf("Put: %v", err)
	}
	ids := c.Identities()
	if len(ids) != 1 {
		t.Fatalf("len(Identities()) = %d, want 1", len(ids))
	}
	if ids[0].Label != "remote:a@hosta:22" {
		t.Errorf("Label = %q, want the plaintext identity, not a hash", ids[0].Label)
	}
	if ids[0].Bytes <= 0 || ids[0].Files != 1 {
		t.Errorf("summary = %+v, want positive Bytes and 1 file", ids[0])
	}
}

// TestManifestSurvivesReopen pins persistence across process restarts
// — the whole point of a disk cache over the existing RAM-only
// RotatedCache is that it outlives one session.
func TestManifestSurvivesReopen(t *testing.T) {
	root := t.TempDir()
	c1 := mustOpen(t, root)
	if err := c1.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}}, 10*mb); err != nil {
		t.Fatalf("Put: %v", err)
	}

	c2 := mustOpen(t, root)
	got, ok := c2.Get(context.Background(), "remote:a@hosta:22", "key1")
	if !ok || len(got) != 1 || got[0].Message != "a" {
		t.Errorf("Get after reopen = %+v, %v, want the entry Put before reopening", got, ok)
	}
}

func TestOpenTreatsCorruptManifestAsFreshStart(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, manifestFileName), []byte("not json at all"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	c := mustOpen(t, root)
	if _, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1"); ok {
		t.Error("Get on a fresh-start cache = hit, want miss")
	}
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}}, 10*mb); err != nil {
		t.Fatalf("Put after corrupt-manifest recovery: %v", err)
	}
}

// TestOpenWipesOrphanedDataOnCorruptManifest pins the actual risk a
// corrupt manifest creates: without this, the old identity directory
// stays on disk forever, unreachable by the fresh, empty manifest,
// silently eating part of the byte budget TotalBytes claims is free.
func TestOpenWipesOrphanedDataOnCorruptManifest(t *testing.T) {
	root := t.TempDir()
	c := mustOpen(t, root)
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}}, 10*mb); err != nil {
		t.Fatalf("Put: %v", err)
	}
	orphanedDir := filepath.Join(root, identityDir("remote:a@hosta:22"))
	if _, err := os.Stat(orphanedDir); err != nil {
		t.Fatalf("setup: identity dir should exist: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, manifestFileName), []byte("not json at all"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mustOpen(t, root)
	if _, err := os.Stat(orphanedDir); !os.IsNotExist(err) {
		t.Errorf("orphaned identity dir still present after a corrupt-manifest reopen: err = %v, want IsNotExist", err)
	}
}

func TestDirectoryAndFilePermissionsAreOwnerOnly(t *testing.T) {
	root := t.TempDir()
	c := mustOpen(t, root)
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}}, 10*mb); err != nil {
		t.Fatalf("Put: %v", err)
	}

	idDir := identityDir("remote:a@hosta:22")
	fi, err := os.Stat(filepath.Join(root, idDir))
	if err != nil {
		t.Fatalf("Stat identity dir: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != dirMode {
		t.Errorf("identity dir perm = %o, want %o", perm, dirMode)
	}

	fi, err = os.Stat(filepath.Join(root, idDir, fileName("key1")))
	if err != nil {
		t.Fatalf("Stat cache file: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != fileMode {
		t.Errorf("cache file perm = %o, want %o", perm, fileMode)
	}

	fi, err = os.Stat(filepath.Join(root, manifestFileName))
	if err != nil {
		t.Fatalf("Stat manifest: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != fileMode {
		t.Errorf("manifest perm = %o, want %o", perm, fileMode)
	}
}

// sameBacking mirrors the exact check used during the live
// measurement against feature_ideas.txt's own 192.168.1.106 test run
// — pointer identity, not content equality. Asserting Message == Raw
// instead would pass even without realiasEntries ever running
// (content is identical either way), which proves nothing about
// whether the RAM-doubling this exists to fix was actually undone.
func sameBacking(a, b string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	return unsafe.StringData(a) == unsafe.StringData(b)
}

func TestGetRealiasesMessageAndRawAfterDecode(t *testing.T) {
	c := mustOpen(t, t.TempDir())
	// Mirrors what ParseAll's own FormatPlain path does: Message and
	// Raw assigned from the exact same string value, see
	// readLogAuditFile's own doc comment in internal/ui/logaudit.go.
	line := "this line has no recognizable timestamp level or structure of any kind"
	fresh := []logview.Entry{{Message: line, Raw: line}}
	if !sameBacking(fresh[0].Message, fresh[0].Raw) {
		t.Fatal("test setup: fresh entry must share a backing array before Put, like a real ParseAll result")
	}

	if err := c.Put("remote:a@hosta:22", "key1", fresh, 10*mb); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1")
	if !ok {
		t.Fatal("Get: want hit")
	}
	if !sameBacking(got[0].Message, got[0].Raw) {
		t.Error("Message and Raw do not share a backing array after Get — the RAM-doubling fix did not run")
	}
}

func TestGetRealiasesFileAcrossWholeBatch(t *testing.T) {
	c := mustOpen(t, t.TempDir())
	file := "app.log.1"
	fresh := make([]logview.Entry, 5)
	for i := range fresh {
		fresh[i] = logview.Entry{File: file, Line: i + 1}
	}
	if err := c.Put("remote:a@hosta:22", "key1", fresh, 10*mb); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1")
	if !ok || len(got) != 5 {
		t.Fatalf("Get = %+v, %v, want 5 entries", got, ok)
	}
	// Deliberately compares two non-canonical entries against each
	// other (1 and 2), not against entry 0 — entry 0's own File is
	// realiasEntries' own canonical value every other entry gets
	// re-pointed at, so comparing against it specifically wouldn't
	// prove entries 1..4 were actually touched versus just happening
	// to equal the one value everything is measured against.
	for i := 2; i < len(got); i++ {
		if !sameBacking(got[1].File, got[i].File) {
			t.Errorf("entry %d's File does not share a backing array with entry 1's — the per-batch File re-aliasing did not run", i)
		}
	}
}

// TestGetTreatsUndecodableCacheFileAsAMiss covers a cache file that
// isn't even valid gob at all — e.g. truncated by a crash mid-write
// before the rename in Put ever happened, or disk corruption.
func TestGetTreatsUndecodableCacheFileAsAMiss(t *testing.T) {
	root := t.TempDir()
	c := mustOpen(t, root)
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}}, 10*mb); err != nil {
		t.Fatalf("Put: %v", err)
	}

	path := filepath.Join(root, identityDir("remote:a@hosta:22"), fileName("key1"))
	if err := os.WriteFile(path, []byte("not a valid gob stream"), fileMode); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1"); ok {
		t.Error("Get on an undecodable cache file = hit, want miss")
	}
}

// TestGetTreatsVersionMismatchAsAMiss covers a cache file that decodes
// just fine as a cacheRecord but was written by a build whose own
// cacheFormatVersion was different — e.g. after an upgrade that
// changed what an Entry field means without gob itself being able to
// detect that (gob only rejects an incompatible *type*, not a
// same-type field whose meaning silently changed). Writes a real,
// validly gob-encoded cacheRecord directly, bypassing Put (which
// would always write the current version), so this exercises the
// `rec.Version != cacheFormatVersion` branch itself rather than the
// decode-error branch the test above already covers.
func TestGetTreatsVersionMismatchAsAMiss(t *testing.T) {
	root := t.TempDir()
	c := mustOpen(t, root)
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "a"}}, 10*mb); err != nil {
		t.Fatalf("Put: %v", err)
	}

	path := filepath.Join(root, identityDir("remote:a@hosta:22"), fileName("key1"))
	f, err := os.OpenFile(path, os.O_TRUNC|os.O_WRONLY, fileMode)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	rec := cacheRecord{Version: cacheFormatVersion + 1, Entries: []logview.Entry{{Message: "from the future"}}}
	if err := gob.NewEncoder(f).Encode(rec); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1"); ok {
		t.Error("Get on a version-mismatched (but validly encoded) cache file = hit, want miss")
	}
}

func TestPutOverwritesExistingKeyRatherThanDuplicating(t *testing.T) {
	c := mustOpen(t, t.TempDir())
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "old"}}, 10*mb); err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	if err := c.Put("remote:a@hosta:22", "key1", []logview.Entry{{Message: "new"}}, 10*mb); err != nil {
		t.Fatalf("Put 2: %v", err)
	}
	got, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1")
	if !ok || len(got) != 1 || got[0].Message != "new" {
		t.Errorf("Get = %+v, %v, want just the newest Put's own entries", got, ok)
	}
	ids := c.Identities()
	if len(ids) != 1 || ids[0].Files != 1 {
		t.Errorf("Identities() = %+v, want exactly 1 file (overwrite, not a duplicate)", ids)
	}
}

func TestPutRejectsAFileLargerThanTheWholeBudget(t *testing.T) {
	c := mustOpen(t, t.TempDir())
	big := make([]logview.Entry, 5000)
	for i := range big {
		big[i] = logview.Entry{Message: "enough padding that this batch alone exceeds a tiny byte budget for this test"}
	}
	if err := c.Put("remote:a@hosta:22", "key1", big, 100); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := c.Get(context.Background(), "remote:a@hosta:22", "key1"); ok {
		t.Error("a file bigger than the whole budget was cached anyway")
	}
	if c.TotalBytes() != 0 {
		t.Errorf("TotalBytes() = %d, want 0", c.TotalBytes())
	}
}

func TestOpenCreatesRootDirectoryIfMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "does", "not", "exist", "yet")
	c := mustOpen(t, root)
	fi, err := os.Stat(root)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !fi.IsDir() {
		t.Fatal("root is not a directory")
	}
	if perm := fi.Mode().Perm(); perm != dirMode {
		t.Errorf("root perm = %o, want %o", perm, dirMode)
	}
	_ = c // only existence/permissions matter for this test
}
