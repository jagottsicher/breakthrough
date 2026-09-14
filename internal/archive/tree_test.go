package archive

import "testing"

func namesOf(entries []Entry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Path
	}
	return names
}

func TestChildrenAtRoot(t *testing.T) {
	entries := []Entry{
		{Path: "README.md"},
		{Path: "src/main.go"},
		{Path: "src/lib/util.go"},
	}
	got := Children(entries, "")
	SortByName(got)
	want := []string{"src", "README.md"} // directories first, then by name
	if !equalStrings(namesOf(got), want) {
		t.Errorf("Children(root) = %v, want %v", namesOf(got), want)
	}
	for _, e := range got {
		if e.Path == "src" && !e.IsDir {
			t.Errorf("synthesized \"src\" entry should be IsDir: %+v", e)
		}
	}
}

func TestChildrenOneLevelDown(t *testing.T) {
	entries := []Entry{
		{Path: "README.md"},
		{Path: "src/main.go"},
		{Path: "src/lib/util.go"},
	}
	got := Children(entries, "src")
	SortByName(got)
	want := []string{"src/lib", "src/main.go"}
	if !equalStrings(namesOf(got), want) {
		t.Errorf("Children(src) = %v, want %v", namesOf(got), want)
	}
}

func TestChildrenPrefersRealDirectoryEntry(t *testing.T) {
	explicit := Entry{Path: "src", IsDir: true, Size: 42}
	entries := []Entry{explicit, {Path: "src/main.go"}}
	got := Children(entries, "")
	for _, e := range got {
		if e.Path == "src" && e != explicit {
			t.Errorf("Children(root) returned a synthesized \"src\" (%+v) instead of the real explicit entry (%+v)", e, explicit)
		}
	}
}

func TestChildrenEmptyDirectory(t *testing.T) {
	entries := []Entry{{Path: "empty", IsDir: true}}
	got := Children(entries, "")
	if len(got) != 1 || got[0].Path != "empty" || !got[0].IsDir {
		t.Errorf("Children(root) = %v, want [empty (dir)]", got)
	}
	if got := Children(entries, "empty"); len(got) != 0 {
		t.Errorf("Children(empty) = %v, want none", got)
	}
}

func TestChildrenDoesNotLeakSiblingSubtree(t *testing.T) {
	entries := []Entry{{Path: "a/one.txt"}, {Path: "b/two.txt"}}
	got := Children(entries, "a")
	if len(got) != 1 || got[0].Path != "a/one.txt" {
		t.Errorf("Children(a) = %v, want [a/one.txt] only", got)
	}
}
