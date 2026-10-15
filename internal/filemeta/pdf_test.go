package filemeta

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const canonicalPdfinfoOutput = `Title:          Quarterly Report
Author:         Jane Doe
Creator:        Microsoft Word
Producer:       macOS Version 26.0 (Build 26A5300f) Quartz PDFContext
CreationDate:   Thu Jan 15 10:30:00 2026
ModDate:        Fri Feb 20 08:00:00 2026
Tagged:         no
UserProperties: no
Suppressed:     no
Form:           none
Pages:          12
Page size:      612 x 792 pts (letter)
File size:      48213 bytes
PDF version:    1.7
`

func TestParsePdfinfoOutputCanonical(t *testing.T) {
	got := parsePdfinfoOutput(canonicalPdfinfoOutput)
	want := []Field{
		{Label: "Title", Value: "Quarterly Report"},
		{Label: "Author", Value: "Jane Doe"},
		{Label: "Creator", Value: "Microsoft Word"},
		{Label: "Producer", Value: "macOS Version 26.0 (Build 26A5300f) Quartz PDFContext"},
		{Label: "Created", Value: "Thu Jan 15 10:30:00 2026"},
		{Label: "Modified", Value: "Fri Feb 20 08:00:00 2026"},
		{Label: "Pages", Value: "12"},
	}
	if len(got) != len(want) {
		t.Fatalf("parsePdfinfoOutput() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParsePdfinfoOutputIgnoresTechnicalFields(t *testing.T) {
	got := parsePdfinfoOutput(canonicalPdfinfoOutput)
	for _, f := range got {
		switch f.Label {
		case "Tagged", "UserProperties", "Suppressed", "Form", "Page size", "File size", "PDF version":
			t.Errorf("parsePdfinfoOutput() included technical field %+v, want only document metadata", f)
		}
	}
}

func TestParsePdfinfoOutputEmptyValuesOmitted(t *testing.T) {
	got := parsePdfinfoOutput("Title:\nAuthor:         \nPages:          3\n")
	want := []Field{{Label: "Pages", Value: "3"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("parsePdfinfoOutput() = %+v, want %+v", got, want)
	}
}

func TestParsePdfinfoOutputEmpty(t *testing.T) {
	if got := parsePdfinfoOutput(""); len(got) != 0 {
		t.Errorf("parsePdfinfoOutput(\"\") = %+v, want empty", got)
	}
}

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not available in this environment: %v", name, err)
	}
}

// buildMinimalPDFWithInfo mirrors internal/viewer's own buildMinimalPDF
// (same hand-built, offset-tracked approach — not imported across
// packages for a single small test fixture, the same call
// internal/viewer's own test file already makes for its own similarly
// small helpers), with an added Info dictionary object and a trailer
// /Info entry pointing to it, so pdfinfo has something real to report.
func buildMinimalPDFWithInfo(title, author string) []byte {
	var buf bytes.Buffer
	var offsets [7]int // index 1..6 used; 0 is the xref free-list head

	obj := func(n int, body string) {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}

	buf.WriteString("%PDF-1.4\n")
	obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj(3, "<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 4 0 R >> >> /MediaBox [0 0 200 100] /Contents 5 0 R >>")
	obj(4, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	stream := "BT /F1 12 Tf 10 50 Td (hello) Tj ET"
	obj(5, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	obj(6, fmt.Sprintf("<< /Title (%s) /Author (%s) >>", title, author))

	xrefStart := buf.Len()
	buf.WriteString("xref\n0 7\n")
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	buf.WriteString("trailer\n<< /Size 7 /Root 1 0 R /Info 6 0 R >>\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xrefStart)

	return buf.Bytes()
}

func TestForPDFRealFile(t *testing.T) {
	requireTool(t, "pdfinfo")

	dir := t.TempDir()
	path := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(path, buildMinimalPDFWithInfo("Quarterly Report", "Jane Doe"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ForPDF(path)
	if err != nil {
		t.Fatalf("ForPDF() error = %v, want nil", err)
	}
	foundTitle, foundAuthor, foundPages := false, false, false
	for _, f := range got {
		switch f.Label {
		case "Title":
			foundTitle = f.Value == "Quarterly Report"
		case "Author":
			foundAuthor = f.Value == "Jane Doe"
		case "Pages":
			foundPages = f.Value == "1"
		}
	}
	if !foundTitle || !foundAuthor || !foundPages {
		t.Errorf("ForPDF() = %+v, want Title=Quarterly Report, Author=Jane Doe, Pages=1", got)
	}
}

func TestForPDFCorruptFile(t *testing.T) {
	requireTool(t, "pdfinfo")

	dir := t.TempDir()
	path := filepath.Join(dir, "broken.pdf")
	if err := os.WriteFile(path, []byte("not a real pdf at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ForPDF(path)
	if err == nil {
		t.Fatal("ForPDF() on a corrupt file: want an error, got nil")
	}
	if errors.Is(err, ErrNoMetadata) || errors.Is(err, ErrPopplerUtilsMissing) {
		t.Errorf("ForPDF() error = %v, want a genuine pdfinfo failure, not a routine state", err)
	}
}

func TestForPathDispatchesPDFByExtension(t *testing.T) {
	requireTool(t, "pdfinfo")

	dir := t.TempDir()
	path := filepath.Join(dir, "x.pdf")
	if err := os.WriteFile(path, buildMinimalPDFWithInfo("T", "A"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, err := ForPath(path); err != nil || len(got) == 0 {
		t.Errorf("ForPath(%q) = %+v, %v, want PDF fields, no error", path, got, err)
	}
}
