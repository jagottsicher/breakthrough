package viewer

import "testing"

func TestCSVDelimiterFor(t *testing.T) {
	cases := []struct {
		path      string
		wantDelim rune
		wantOK    bool
	}{
		{"data.csv", ',', true},
		{"DATA.CSV", ',', true},
		{"data.tsv", '\t', true},
		{"data.tab", '\t', true},
		{"notes.txt", 0, false},
		{"data.csv.bak", 0, false},
	}
	for _, c := range cases {
		delim, ok := CSVDelimiterFor(c.path)
		if ok != c.wantOK || (ok && delim != c.wantDelim) {
			t.Errorf("CSVDelimiterFor(%q) = (%q, %v), want (%q, %v)", c.path, delim, ok, c.wantDelim, c.wantOK)
		}
	}
}

func TestFormatCSVTableAlignsColumns(t *testing.T) {
	content := "name,age,city\nAlice,30,NYC\nBob,7,Los Angeles\n"
	got, err := FormatCSVTable(content, ',')
	if err != nil {
		t.Fatal(err)
	}
	want := "name   age  city\n" +
		"Alice  30   NYC\n" +
		"Bob    7    Los Angeles\n"
	if got != want {
		t.Errorf("FormatCSVTable =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatCSVTableTab(t *testing.T) {
	content := "a\tbb\tccc\nx\tyy\tzzz\n"
	got, err := FormatCSVTable(content, '\t')
	if err != nil {
		t.Fatal(err)
	}
	want := "a  bb  ccc\n" +
		"x  yy  zzz\n"
	if got != want {
		t.Errorf("FormatCSVTable =\n%q\nwant\n%q", got, want)
	}
}

// TestFormatCSVTableRaggedRows pins the padding behavior for a row with
// fewer fields than the table's own widest one — real, messy CSV files
// (a trailing row missing its last field, say) shouldn't blow up the
// whole preview.
func TestFormatCSVTableRaggedRows(t *testing.T) {
	content := "a,b,c\n1,2\n"
	got, err := FormatCSVTable(content, ',')
	if err != nil {
		t.Fatal(err)
	}
	want := "a  b  c\n" +
		"1  2  \n"
	if got != want {
		t.Errorf("FormatCSVTable =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatCSVTableEmptyContent(t *testing.T) {
	got, err := FormatCSVTable("", ',')
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("FormatCSVTable(\"\") = %q, want empty", got)
	}
}

// TestFormatCSVTableTrimsLeadingSpaceAfterDelimiter pins
// TrimLeadingSpace's own reason for being on: a human-formatted CSV
// with a space after each comma shouldn't have that space become part
// of the next cell's own content, which would otherwise defeat the
// column alignment this function exists to add.
func TestFormatCSVTableTrimsLeadingSpaceAfterDelimiter(t *testing.T) {
	content := "a, bb, ccc\nx, y, z\n"
	got, err := FormatCSVTable(content, ',')
	if err != nil {
		t.Fatal(err)
	}
	want := "a  bb  ccc\n" +
		"x  y   z\n"
	if got != want {
		t.Errorf("FormatCSVTable =\n%q\nwant\n%q", got, want)
	}
}
