package filemeta

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// buildZip writes a minimal zip archive with the given name->content
// entries to path — the same "generate it, don't ship it" approach
// image_test.go's buildExifJPEG already uses.
func buildZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

const ooxmlCoreXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/">
  <dc:title>Quarterly Report</dc:title>
  <dc:creator>Jane Doe</dc:creator>
  <dc:subject>Finance</dc:subject>
  <dcterms:created xsi:type="dcterms:W3CDTF" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">2026-01-15T10:30:00Z</dcterms:created>
  <dcterms:modified xsi:type="dcterms:W3CDTF" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">2026-02-20T08:00:00Z</dcterms:modified>
</cp:coreProperties>`

const ooxmlAppXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties">
  <Application>Microsoft Office Word</Application>
</Properties>`

func TestForOfficeOpenXMLRealDocx(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.docx")
	buildZip(t, path, map[string]string{
		"docProps/core.xml": ooxmlCoreXML,
		"docProps/app.xml":  ooxmlAppXML,
	})

	got, err := ForOfficeOpenXML(path)
	if err != nil {
		t.Fatalf("ForOfficeOpenXML() error = %v, want nil", err)
	}
	want := []Field{
		{Label: "Title", Value: "Quarterly Report"},
		{Label: "Author", Value: "Jane Doe"},
		{Label: "Subject", Value: "Finance"},
		{Label: "Created", Value: "2026-01-15 10:30:00"},
		{Label: "Modified", Value: "2026-02-20 08:00:00"},
		{Label: "Application", Value: "Microsoft Office Word"},
	}
	if len(got) != len(want) {
		t.Fatalf("ForOfficeOpenXML() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestForOfficeOpenXMLMissingCorePropsReturnsErrNoMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.docx")
	buildZip(t, path, map[string]string{"word/document.xml": "<document/>"})

	_, err := ForOfficeOpenXML(path)
	if !errors.Is(err, ErrNoMetadata) {
		t.Errorf("ForOfficeOpenXML() error = %v, want ErrNoMetadata", err)
	}
}

func TestForOfficeOpenXMLNotAZipFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.docx")
	if err := os.WriteFile(path, []byte("not a zip file at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ForOfficeOpenXML(path)
	if err == nil {
		t.Fatal("ForOfficeOpenXML() on a non-zip file: want an error, got nil")
	}
	if errors.Is(err, ErrNoMetadata) || errors.Is(err, ErrNotSupported) {
		t.Errorf("ForOfficeOpenXML() error = %v, want a genuine read error, not a routine state", err)
	}
}

const epubContainerXML = `<?xml version="1.0"?>
<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

const epubOPFXML = `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" unique-identifier="BookId" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>The Go Programming Language</dc:title>
    <dc:creator>Alan Donovan</dc:creator>
    <dc:language>en</dc:language>
    <dc:date>2015-10-26</dc:date>
    <dc:publisher>Addison-Wesley</dc:publisher>
  </metadata>
</package>`

func TestForEPUBRealFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.epub")
	buildZip(t, path, map[string]string{
		"META-INF/container.xml": epubContainerXML,
		"OEBPS/content.opf":      epubOPFXML,
		"mimetype":               "application/epub+zip",
	})

	got, err := ForEPUB(path)
	if err != nil {
		t.Fatalf("ForEPUB() error = %v, want nil", err)
	}
	want := []Field{
		{Label: "Title", Value: "The Go Programming Language"},
		{Label: "Author", Value: "Alan Donovan"},
		{Label: "Language", Value: "en"},
		{Label: "Published", Value: "2015-10-26"},
		{Label: "Publisher", Value: "Addison-Wesley"},
	}
	if len(got) != len(want) {
		t.Fatalf("ForEPUB() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("field %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestForEPUBMissingContainerReturnsErrNoMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.epub")
	buildZip(t, path, map[string]string{"mimetype": "application/epub+zip"})

	_, err := ForEPUB(path)
	if !errors.Is(err, ErrNoMetadata) {
		t.Errorf("ForEPUB() error = %v, want ErrNoMetadata", err)
	}
}

func TestForPathDispatchesByExtension(t *testing.T) {
	dir := t.TempDir()

	docxPath := filepath.Join(dir, "x.docx")
	buildZip(t, docxPath, map[string]string{"docProps/core.xml": ooxmlCoreXML})
	if got, err := ForPath(docxPath); err != nil || len(got) == 0 {
		t.Errorf("ForPath(%q) = %+v, %v, want OOXML fields, no error", docxPath, got, err)
	}

	epubPath := filepath.Join(dir, "x.epub")
	buildZip(t, epubPath, map[string]string{
		"META-INF/container.xml": epubContainerXML,
		"OEBPS/content.opf":      epubOPFXML,
	})
	if got, err := ForPath(epubPath); err != nil || len(got) == 0 {
		t.Errorf("ForPath(%q) = %+v, %v, want EPUB fields, no error", epubPath, got, err)
	}

	txtPath := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(txtPath, []byte("plain text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ForPath(txtPath); !errors.Is(err, ErrNotSupported) {
		t.Errorf("ForPath(%q) error = %v, want ErrNotSupported (falls through to ForImage)", txtPath, err)
	}
}
