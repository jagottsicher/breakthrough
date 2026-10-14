package compare

import "testing"

func TestParseUnifiedDiffOneFileOneHunk(t *testing.T) {
	diff := `diff --git a/foo.txt b/foo.txt
index abc123..def456 100644
--- a/foo.txt
+++ b/foo.txt
@@ -1,3 +1,3 @@
 same
-old line
+new line
 same2
`
	files := ParseUnifiedDiff(diff)
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1: %+v", len(files), files)
	}
	f := files[0]
	if f.Path != "foo.txt" {
		t.Errorf("Path = %q, want foo.txt", f.Path)
	}
	if len(f.Pairs) != 3 {
		t.Fatalf("len(Pairs) = %d, want 3: %+v", len(f.Pairs), f.Pairs)
	}
	if f.Pairs[0].Kind != PairSame || f.Pairs[0].Left != "same" || f.Pairs[0].LeftNo != 1 || f.Pairs[0].RightNo != 1 {
		t.Errorf("Pairs[0] = %+v, want Same/same at 1/1", f.Pairs[0])
	}
	if f.Pairs[1].Kind != PairChanged || f.Pairs[1].Left != "old line" || f.Pairs[1].Right != "new line" {
		t.Errorf("Pairs[1] = %+v, want Changed old line/new line", f.Pairs[1])
	}
	if f.Pairs[1].LeftNo != 2 || f.Pairs[1].RightNo != 2 {
		t.Errorf("Pairs[1] line numbers = %d/%d, want 2/2", f.Pairs[1].LeftNo, f.Pairs[1].RightNo)
	}
	if f.Pairs[2].Kind != PairSame || f.Pairs[2].Left != "same2" || f.Pairs[2].LeftNo != 3 {
		t.Errorf("Pairs[2] = %+v, want Same/same2 at line 3", f.Pairs[2])
	}
}

func TestParseUnifiedDiffInsertsAGapBetweenTwoHunksOfTheSameFile(t *testing.T) {
	diff := `diff --git a/foo.txt b/foo.txt
--- a/foo.txt
+++ b/foo.txt
@@ -1,2 +1,2 @@
 line1
-line2
+LINE2
@@ -50,2 +50,2 @@
 line50
-line51
+LINE51
`
	files := ParseUnifiedDiff(diff)
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(files))
	}
	pairs := files[0].Pairs
	// Same, Changed, Gap, Same, Changed = 5 rows.
	if len(pairs) != 5 {
		t.Fatalf("len(pairs) = %d, want 5: %+v", len(pairs), pairs)
	}
	if pairs[2].Kind != PairGap {
		t.Errorf("pairs[2].Kind = %v, want PairGap: %+v", pairs[2].Kind, pairs[2])
	}
	if pairs[3].LeftNo != 50 || pairs[3].RightNo != 50 {
		t.Errorf("pairs[3] (second hunk's own first line) numbers = %d/%d, want 50/50 (the hunk header's own line number, not a running count from the first hunk)", pairs[3].LeftNo, pairs[3].RightNo)
	}
}

func TestParseUnifiedDiffMultipleFiles(t *testing.T) {
	diff := `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1,1 +1,1 @@
-old a
+new a
diff --git a/b.txt b/b.txt
--- a/b.txt
+++ b/b.txt
@@ -1,1 +1,1 @@
-old b
+new b
`
	files := ParseUnifiedDiff(diff)
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2: %+v", len(files), files)
	}
	if files[0].Path != "a.txt" || files[1].Path != "b.txt" {
		t.Errorf("paths = %q, %q, want a.txt, b.txt", files[0].Path, files[1].Path)
	}
	if len(files[0].Pairs) != 1 || files[0].Pairs[0].Left != "old a" || files[0].Pairs[0].Right != "new a" {
		t.Errorf("files[0].Pairs = %+v, want one Changed row old a/new a", files[0].Pairs)
	}
	if len(files[1].Pairs) != 1 || files[1].Pairs[0].Left != "old b" || files[1].Pairs[0].Right != "new b" {
		t.Errorf("files[1].Pairs = %+v, want one Changed row old b/new b", files[1].Pairs)
	}
}

func TestParseUnifiedDiffNewFile(t *testing.T) {
	diff := `diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..abc123
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+line one
+line two
`
	files := ParseUnifiedDiff(diff)
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(files))
	}
	pairs := files[0].Pairs
	if len(pairs) != 2 {
		t.Fatalf("len(pairs) = %d, want 2: %+v", len(pairs), pairs)
	}
	for i, want := range []string{"line one", "line two"} {
		if pairs[i].Kind != PairOnlyRight || pairs[i].Right != want || pairs[i].LeftNo != 0 {
			t.Errorf("pairs[%d] = %+v, want OnlyRight/%s with LeftNo=0", i, pairs[i], want)
		}
	}
}

func TestParseUnifiedDiffDeletedFile(t *testing.T) {
	diff := `diff --git a/gone.txt b/gone.txt
deleted file mode 100644
index abc123..0000000
--- a/gone.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-line one
-line two
`
	files := ParseUnifiedDiff(diff)
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(files))
	}
	pairs := files[0].Pairs
	if len(pairs) != 2 {
		t.Fatalf("len(pairs) = %d, want 2: %+v", len(pairs), pairs)
	}
	for i, want := range []string{"line one", "line two"} {
		if pairs[i].Kind != PairOnlyLeft || pairs[i].Left != want || pairs[i].RightNo != 0 {
			t.Errorf("pairs[%d] = %+v, want OnlyLeft/%s with RightNo=0", i, pairs[i], want)
		}
	}
}

func TestParseUnifiedDiffRenameWithNoContentChangeHasNoPairs(t *testing.T) {
	diff := `diff --git a/old-name.txt b/new-name.txt
similarity index 100%
rename from old-name.txt
rename to new-name.txt
`
	files := ParseUnifiedDiff(diff)
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(files))
	}
	if files[0].Path != "new-name.txt" {
		t.Errorf("Path = %q, want new-name.txt", files[0].Path)
	}
	if len(files[0].Pairs) != 0 {
		t.Errorf("Pairs = %+v, want none (pure rename, no content change)", files[0].Pairs)
	}
}

func TestParseUnifiedDiffEmptyInputReturnsNoFiles(t *testing.T) {
	files := ParseUnifiedDiff("")
	if len(files) != 0 {
		t.Errorf("len(files) = %d, want 0", len(files))
	}
}

func TestParseUnifiedDiffIgnoresNoNewlineMarker(t *testing.T) {
	diff := `diff --git a/foo.txt b/foo.txt
--- a/foo.txt
+++ b/foo.txt
@@ -1,1 +1,1 @@
-old
\ No newline at end of file
+new
\ No newline at end of file
`
	files := ParseUnifiedDiff(diff)
	if len(files) != 1 {
		t.Fatalf("len(files) = %d, want 1", len(files))
	}
	pairs := files[0].Pairs
	if len(pairs) != 1 {
		t.Fatalf("len(pairs) = %d, want 1 (the \\ marker lines must not become content): %+v", len(pairs), pairs)
	}
	if pairs[0].Kind != PairChanged || pairs[0].Left != "old" || pairs[0].Right != "new" {
		t.Errorf("pairs[0] = %+v, want Changed old/new", pairs[0])
	}
}
