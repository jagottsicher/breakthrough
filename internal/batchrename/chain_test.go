package batchrename

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// names lists dir's own entries, sorted — the shape every test here
// compares the end state of a directory against.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func writeNamed(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readNamed(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestPlanAllowsARenameChain is the headline case: "a" becomes "b"
// while "b" becomes "c". "b" exists on disk, but only until its own
// rename runs — Plan must report both as ordinary Changes, not refuse
// a's as an overwrite the way Total Commander does.
func TestPlanAllowsARenameChain(t *testing.T) {
	dir := t.TempDir()
	writeNamed(t, dir, map[string]string{"1-a.txt": "one", "2-a.txt": "two"})
	// Re-number starting at 2: TrimFront 2 strips the old "1-"/"2-",
	// the prefix counter puts "2-"/"3-" back on. So "1-a.txt" becomes
	// "2-a.txt" — the name the second file currently has — while that
	// second file itself becomes "3-a.txt": a genuine chain.
	rules := Rules{TrimFront: 2, NumberPosition: NumberPrefix, NumberStart: 2, NumberDigits: 1}
	paths := []string{filepath.Join(dir, "1-a.txt"), filepath.Join(dir, "2-a.txt")}

	result := Plan(paths, rules)
	if len(result.Problems) != 0 {
		t.Fatalf("chain should not be a problem, got %v", result.Problems)
	}
	want := map[string]string{
		filepath.Join(dir, "1-a.txt"): filepath.Join(dir, "2-a.txt"),
		filepath.Join(dir, "2-a.txt"): filepath.Join(dir, "3-a.txt"),
	}
	if len(result.Changes) != len(want) {
		t.Fatalf("expected %d changes, got %v", len(want), result.Changes)
	}
	for _, c := range result.Changes {
		if want[c.From] != c.To {
			t.Errorf("change %s -> %s, want -> %s", c.From, c.To, want[c.From])
		}
	}

	// And it actually lands, contents following their names.
	if _, err := Apply(result.Changes); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := names(t, dir); !equal(got, []string{"2-a.txt", "3-a.txt"}) {
		t.Fatalf("names after apply = %v, want [2-a.txt 3-a.txt]", got)
	}
	if readNamed(t, dir, "2-a.txt") != "one" || readNamed(t, dir, "3-a.txt") != "two" {
		t.Error("contents didn't follow their names through the chain")
	}
}

// TestApplyOrdersAChainSoNothingIsClobbered builds a real three-link
// chain on disk and checks the content of every file afterward — the
// only proof that the right file ended up under the right name, not
// just that the names exist.
func TestApplyOrdersAChainSoNothingIsClobbered(t *testing.T) {
	dir := t.TempDir()
	writeNamed(t, dir, map[string]string{"a": "A", "b": "B", "c": "C"})
	changes := []Change{
		{From: filepath.Join(dir, "a"), To: filepath.Join(dir, "b")}, // must wait for b to move
		{From: filepath.Join(dir, "b"), To: filepath.Join(dir, "c")}, // must wait for c to move
		{From: filepath.Join(dir, "c"), To: filepath.Join(dir, "d")},
	}

	applied, err := Apply(changes)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(applied) != 3 {
		t.Fatalf("expected 3 applied steps, got %d: %v", len(applied), applied)
	}
	if got := names(t, dir); !equal(got, []string{"b", "c", "d"}) {
		t.Fatalf("names after apply = %v, want [b c d]", got)
	}
	if readNamed(t, dir, "b") != "A" || readNamed(t, dir, "c") != "B" || readNamed(t, dir, "d") != "C" {
		t.Error("contents didn't follow their names through the chain")
	}

	if _, err := Undo(applied); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if got := names(t, dir); !equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("names after undo = %v, want [a b c]", got)
	}
	if readNamed(t, dir, "a") != "A" || readNamed(t, dir, "b") != "B" || readNamed(t, dir, "c") != "C" {
		t.Error("undo didn't restore every file's own content under its original name")
	}
}

// TestApplySwapsTwoFilesThroughATemporaryName: "a" to "b" and "b" to
// "a" at once — a cycle no ordering can satisfy, so one side goes
// through a temporary name. Checked by content, and that no temporary
// is left behind.
func TestApplySwapsTwoFilesThroughATemporaryName(t *testing.T) {
	dir := t.TempDir()
	writeNamed(t, dir, map[string]string{"a": "A", "b": "B"})
	changes := []Change{
		{From: filepath.Join(dir, "a"), To: filepath.Join(dir, "b")},
		{From: filepath.Join(dir, "b"), To: filepath.Join(dir, "a")},
	}

	steps := Order(changes)
	if len(steps) != 3 {
		t.Fatalf("a swap needs exactly 3 physical steps (one via a temp), got %d: %v", len(steps), steps)
	}

	applied, err := Apply(changes)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := names(t, dir); !equal(got, []string{"a", "b"}) {
		t.Fatalf("names after swap = %v, want [a b] with no temporary left over", got)
	}
	if readNamed(t, dir, "a") != "B" || readNamed(t, dir, "b") != "A" {
		t.Error("swap didn't exchange the contents")
	}

	if _, err := Undo(applied); err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if readNamed(t, dir, "a") != "A" || readNamed(t, dir, "b") != "B" {
		t.Error("undo didn't swap them back")
	}
	if got := names(t, dir); !equal(got, []string{"a", "b"}) {
		t.Fatalf("names after undo = %v, want [a b]", got)
	}
}

// TestOrderRotatesAThreeCycle covers a cycle longer than a swap
// (a->b->c->a): still exactly one temporary, everything lands.
func TestOrderRotatesAThreeCycle(t *testing.T) {
	dir := t.TempDir()
	writeNamed(t, dir, map[string]string{"a": "A", "b": "B", "c": "C"})
	changes := []Change{
		{From: filepath.Join(dir, "a"), To: filepath.Join(dir, "b")},
		{From: filepath.Join(dir, "b"), To: filepath.Join(dir, "c")},
		{From: filepath.Join(dir, "c"), To: filepath.Join(dir, "a")},
	}
	if steps := Order(changes); len(steps) != 4 {
		t.Fatalf("a 3-cycle needs 4 physical steps, got %d: %v", len(steps), steps)
	}
	if _, err := Apply(changes); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if readNamed(t, dir, "b") != "A" || readNamed(t, dir, "c") != "B" || readNamed(t, dir, "a") != "C" {
		t.Error("rotation didn't land every content under its new name")
	}
	if got := names(t, dir); !equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("names after rotation = %v, want [a b c]", got)
	}
}

// TestPlanStillRefusesARealOverwrite: the destination is occupied by
// something that is NOT moving — a real overwrite, still refused.
func TestPlanStillRefusesARealOverwrite(t *testing.T) {
	dir := t.TempDir()
	writeNamed(t, dir, map[string]string{"draft.txt": "d", "final.txt": "f"})
	rules := Rules{Find: "draft", Replace: "final"}
	result := Plan([]string{filepath.Join(dir, "draft.txt")}, rules)
	if len(result.Changes) != 0 || len(result.Problems) != 1 {
		t.Fatalf("expected one overwrite problem and no change, got changes=%v problems=%v", result.Changes, result.Problems)
	}
	if !strings.Contains(result.Problems[0].Reason, "overwrite") {
		t.Errorf("reason = %q, want an overwrite explanation", result.Problems[0].Reason)
	}
}

// TestPlanRefusesAChainWhoseOccupantIsNotMovingAfterAll: "a" wants
// "b"'s name, "b" is in the batch, but b's own rename is itself a
// problem — so b stays put, and a's rename is the overwrite it looks
// like. The settling loop is what catches this second-order case.
func TestPlanRefusesAChainWhoseOccupantIsNotMovingAfterAll(t *testing.T) {
	dir := t.TempDir()
	writeNamed(t, dir, map[string]string{"x1": "1", "x2": "2", "x3": "outsider"})
	// x1 wants x2's name, x2 wants x3's — but x3 belongs to an outsider
	// that isn't going anywhere, so x2 can't move, so x1's rename is
	// the overwrite it looks like after all: both must be refused, in
	// that order of discovery (x2 first, then x1 on the second pass).
	kept, problems := settleCollisions([]Change{
		{From: filepath.Join(dir, "x1"), To: filepath.Join(dir, "x2")},
		{From: filepath.Join(dir, "x2"), To: filepath.Join(dir, "x3")},
	})
	if len(kept) != 0 || len(problems) != 2 {
		t.Fatalf("expected both refused, got kept=%v problems=%v", kept, problems)
	}
	if filepath.Base(problems[0].Path) != "x2" || filepath.Base(problems[1].Path) != "x1" {
		t.Errorf("problems reported in order %s, %s — want x2 (real overwrite) first, then x1 (chain that fell through)", problems[0].Path, problems[1].Path)
	}

	// And the control: the same chain with x3 free is entirely fine.
	dir2 := t.TempDir()
	writeNamed(t, dir2, map[string]string{"x1": "1", "x2": "2"})
	kept, problems = settleCollisions([]Change{
		{From: filepath.Join(dir2, "x1"), To: filepath.Join(dir2, "x2")},
		{From: filepath.Join(dir2, "x2"), To: filepath.Join(dir2, "x3")},
	})
	if len(kept) != 2 || len(problems) != 0 {
		t.Fatalf("expected the whole chain kept, got kept=%v problems=%v", kept, problems)
	}
}

// TestPlanAllowsACaseOnlyRenameOnACaseInsensitiveFilesystem: on a
// case-insensitive filesystem (macOS APFS by default) "readme.txt" and
// "README.txt" stat as the same existing file — that must read as
// "same file, fine", not as an overwrite. On a case-sensitive
// filesystem (Linux) the target simply doesn't exist, so the same
// Plan trivially succeeds either way; the SameFile branch is what
// this pins on the platforms where it matters.
func TestPlanAllowsACaseOnlyRenameOnACaseInsensitiveFilesystem(t *testing.T) {
	dir := t.TempDir()
	writeNamed(t, dir, map[string]string{"readme.txt": "r"})
	result := Plan([]string{filepath.Join(dir, "readme.txt")}, Rules{Case: CaseUpper})
	if len(result.Problems) != 0 {
		t.Fatalf("case-only rename should never be a collision, got %v", result.Problems)
	}
	if len(result.Changes) != 1 || filepath.Base(result.Changes[0].To) != "README.txt" {
		t.Fatalf("changes = %v, want readme.txt -> README.txt", result.Changes)
	}
	if _, err := Apply(result.Changes); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := names(t, dir); !equal(got, []string{"README.txt"}) {
		t.Fatalf("names after case rename = %v, want [README.txt]", got)
	}
}

// TestRenameLeavesADirectoryExtensionAloneByDefault: a folder called
// "my.project" is a folder called "my.project" — its ".project" is
// not an extension to lowercase, drop, or replace, unless
// ExtensionOnDirs says otherwise.
func TestRenameLeavesADirectoryExtensionAloneByDefault(t *testing.T) {
	rules := Rules{ExtensionMode: ExtensionRemove}
	got, err := Rename(rules, Input{Name: "my.project", IsDir: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "my.project" {
		t.Errorf("directory with ExtensionRemove = %q, want unchanged my.project", got)
	}

	got, err = Rename(Rules{ExtensionMode: ExtensionRemove, ExtensionOnDirs: true}, Input{Name: "my.project", IsDir: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "my" {
		t.Errorf("directory with ExtensionOnDirs = %q, want my", got)
	}

	// The base-name steps see the whole name for a directory: Trim 3
	// off the back of "my.project" removes "ect", not ".pro" + ext.
	got, err = Rename(Rules{TrimBack: 3}, Input{Name: "my.project", IsDir: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "my.proj" {
		t.Errorf("directory TrimBack 3 = %q, want my.proj", got)
	}
}

// TestPlanDetectsDirectoriesItself: Plan stats each path, so a real
// directory in the batch gets Input.IsDir set without the caller
// having to say so.
func TestPlanDetectsDirectoriesItself(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "my.project"), 0o755); err != nil {
		t.Fatal(err)
	}
	result := Plan([]string{filepath.Join(dir, "my.project")}, Rules{ExtensionMode: ExtensionRemove})
	if len(result.Changes) != 0 {
		t.Errorf("a directory's dotted name must not be treated as an extension: %v", result.Changes)
	}
}

func TestFindReplaceRegexBackrefs(t *testing.T) {
	cases := []struct {
		replace, want string
	}{
		{`$2-$1`, "2024-report"},
		{`${2}-${1}`, "2024-report"},
		{`\2-\1`, "2024-report"},
		{`\2_\1`, "2024_report"},
	}
	for _, c := range cases {
		got, err := Rename(Rules{Regex: true, Find: `^(\w+)-(\d+)$`, Replace: c.replace}, Input{Name: "report-2024.pdf"})
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want+".pdf" {
			t.Errorf("Replace %q = %q, want %q", c.replace, got, c.want+".pdf")
		}
	}
}

func TestBackrefsToGoLeavesEverythingElseAlone(t *testing.T) {
	cases := map[string]string{
		"plain":        "plain",
		`\1`:           "${1}",
		`\1\2`:         "${1}${2}",
		`\10`:          "${1}0", // sed's own reading: group 1, then a literal 0
		`\0`:           `\0`,    // not a group reference this accepts
		`\n`:           `\n`,
		`price: $5`:    `price: $5`,
		`trailing \`:   `trailing \`,
		`C:\dir\1x`:    `C:\dir${1}x`,
		`already ${1}`: `already ${1}`,
	}
	for in, want := range cases {
		if got := backrefsToGo(in); got != want {
			t.Errorf("backrefsToGo(%q) = %q, want %q", in, got, want)
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
