package compare

import "testing"

func TestSideBySideOnIdenticalContentPairsEveryLineAsSame(t *testing.T) {
	content := "same\ncontent\nhere\n"
	pairs := SideBySide(content, content)
	if len(pairs) != 3 {
		t.Fatalf("len(pairs) = %d, want 3", len(pairs))
	}
	for i, want := range []string{"same", "content", "here"} {
		p := pairs[i]
		if p.Kind != PairSame || p.Left != want || p.Right != want {
			t.Errorf("pairs[%d] = %+v, want Left=Right=%q Kind=PairSame", i, p, want)
		}
		if p.LeftNo != i+1 || p.RightNo != i+1 {
			t.Errorf("pairs[%d] line numbers = %d/%d, want %d/%d", i, p.LeftNo, p.RightNo, i+1, i+1)
		}
	}
}

func TestSideBySideOnASingleLineReplacementPairsItAsChanged(t *testing.T) {
	old := "line one\nline two\nline three\n"
	new := "line one\nline TWO\nline three\n"
	pairs := SideBySide(old, new)
	if len(pairs) != 3 {
		t.Fatalf("len(pairs) = %d, want 3: %+v", len(pairs), pairs)
	}
	if pairs[0].Kind != PairSame || pairs[0].Left != "line one" {
		t.Errorf("pairs[0] = %+v, want Same/line one", pairs[0])
	}
	if pairs[1].Kind != PairChanged || pairs[1].Left != "line two" || pairs[1].Right != "line TWO" {
		t.Errorf("pairs[1] = %+v, want Changed line two/line TWO", pairs[1])
	}
	if pairs[1].LeftNo != 2 || pairs[1].RightNo != 2 {
		t.Errorf("pairs[1] line numbers = %d/%d, want 2/2", pairs[1].LeftNo, pairs[1].RightNo)
	}
	if pairs[2].Kind != PairSame || pairs[2].Left != "line three" {
		t.Errorf("pairs[2] = %+v, want Same/line three", pairs[2])
	}
}

func TestSideBySideOnAnAddedLineReportsOnlyRightWithABlankLeft(t *testing.T) {
	old := "first\nlast\n"
	new := "first\ninserted\nlast\n"
	pairs := SideBySide(old, new)
	if len(pairs) != 3 {
		t.Fatalf("len(pairs) = %d, want 3: %+v", len(pairs), pairs)
	}
	mid := pairs[1]
	if mid.Kind != PairOnlyRight || mid.Right != "inserted" || mid.Left != "" {
		t.Errorf("pairs[1] = %+v, want OnlyRight/inserted with blank Left", mid)
	}
	if mid.LeftNo != 0 || mid.RightNo != 2 {
		t.Errorf("pairs[1] line numbers = %d/%d, want 0/2", mid.LeftNo, mid.RightNo)
	}
	if pairs[2].LeftNo != 2 || pairs[2].RightNo != 3 {
		t.Errorf("pairs[2] (last) line numbers = %d/%d, want 2/3 (right side counts the insert)", pairs[2].LeftNo, pairs[2].RightNo)
	}
}

func TestSideBySideOnARemovedLineReportsOnlyLeftWithABlankRight(t *testing.T) {
	old := "first\nremoved\nlast\n"
	new := "first\nlast\n"
	pairs := SideBySide(old, new)
	if len(pairs) != 3 {
		t.Fatalf("len(pairs) = %d, want 3: %+v", len(pairs), pairs)
	}
	mid := pairs[1]
	if mid.Kind != PairOnlyLeft || mid.Left != "removed" || mid.Right != "" {
		t.Errorf("pairs[1] = %+v, want OnlyLeft/removed with blank Right", mid)
	}
	if mid.LeftNo != 2 || mid.RightNo != 0 {
		t.Errorf("pairs[1] line numbers = %d/%d, want 2/0", mid.LeftNo, mid.RightNo)
	}
}

func TestSideBySideCoversTheWholeFileNotJustAContextWindowAroundChanges(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n"
	new := "a\nb\nc\nd\ne\nf\ng\nh\ni\nCHANGED\n"
	pairs := SideBySide(old, new)
	if len(pairs) != 10 {
		t.Fatalf("len(pairs) = %d, want 10 (every line of the longer file, not a trimmed hunk): %+v", len(pairs), pairs)
	}
	if pairs[0].Left != "a" || pairs[0].Kind != PairSame {
		t.Errorf("pairs[0] = %+v, want Same/a (far from the only change, still present)", pairs[0])
	}
	if pairs[9].Kind != PairChanged || pairs[9].Left != "j" || pairs[9].Right != "CHANGED" {
		t.Errorf("pairs[9] = %+v, want Changed j/CHANGED", pairs[9])
	}
}

func TestSideBySideOnEmptyContentReturnsNoPairs(t *testing.T) {
	pairs := SideBySide("", "")
	if len(pairs) != 0 {
		t.Errorf("len(pairs) = %d, want 0", len(pairs))
	}
}

func TestSideBySideHandlesAFinalLineWithNoTrailingNewlineOnEitherSide(t *testing.T) {
	old := "one\ntwo"
	new := "one\ntwo"
	pairs := SideBySide(old, new)
	if len(pairs) != 2 || pairs[1].Left != "two" || pairs[1].Right != "two" {
		t.Errorf("pairs = %+v, want 2 pairs, last = two/two (no newline artifact)", pairs)
	}
}
