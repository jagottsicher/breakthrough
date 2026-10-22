package filemeta

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// IsDocumentPath reports whether path's extension is one ForPath
// dispatches to a document extractor for (Office Open XML/EPUB/the
// old binary Office formats — see IsOLE2Path) rather than falling
// through to ForImage's content-sniffed decode — the Details sidebar
// uses this to decide whether to show a metadata section at all for a
// file with no preview image of its own (see renderDetailsSidebar),
// since unlike images (gated on a preview having actually decoded),
// these have no such signal to key off.
func IsDocumentPath(path string) bool {
	if IsOLE2Path(path) {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx", ".xlsx", ".pptx", ".epub":
		return true
	default:
		return false
	}
}

// ForPath picks the right extractor for path's own extension: Office
// Open XML (DOCX/XLSX/PPTX) and EPUB are both ZIP containers with no
// reliable way to tell them apart from an image by content alone (every
// one of them starts with the same "PK\x03\x04" ZIP signature), so
// these two go by extension; the old binary Office formats (DOC/XLS/
// PPT, see IsOLE2Path) and PDF go by extension too, for a different
// reason each — PDF because pdfinfo needs a real file path, not
// sniffed content, since it shells out rather than parsing anything
// itself; OLE2 because a Compound File's own magic number alone can't
// tell a DOC from an XLS from a PPT (mscfb reads the container either
// way; the extension is only needed to route here at all, see
// ForOLE2's own doc comment for what actually happens once it does).
// Everything else falls through to ForImage's own content-sniffed
// decode, which already returns ErrNotSupported for anything imagemeta
// doesn't recognize.
func ForPath(path string) ([]Field, error) {
	// Checked ahead of the extension switch below, by its own separate
	// allowlist (see IsVideoPath/viewer.LooksLikeVideoPath's own doc
	// comment on why that one is shared rather than duplicated here) —
	// a video container's own binary structure gives nothing reliable
	// to sniff across formats the way image.DecodeConfig does for
	// pictures, the same reasoning every case below already has for
	// its own format.
	if IsVideoPath(path) {
		return ForVideo(path)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx", ".xlsx", ".pptx":
		return ForOfficeOpenXML(path)
	case ".epub":
		return ForEPUB(path)
	case ".pdf":
		return ForPDF(path)
	case ".doc", ".xls", ".ppt":
		return ForOLE2(path)
	default:
		return ForImage(path)
	}
}

type ooxmlCoreProps struct {
	Title    string `xml:"title"`
	Creator  string `xml:"creator"`
	Subject  string `xml:"subject"`
	Created  string `xml:"created"`
	Modified string `xml:"modified"`
}

type ooxmlAppProps struct {
	Application string `xml:"Application"`
}

// ForOfficeOpenXML reads a DOCX/XLSX/PPTX file's own docProps/core.xml
// (Dublin Core — title/creator/subject/created/modified) and
// docProps/app.xml (which application wrote it) — the same handful of
// plain-XML files every Office Open XML document already carries,
// requiring nothing beyond the standard library's own archive/zip and
// encoding/xml (see feature_ideas.txt #26 for why this is built rather
// than pulled in as a dependency).
func ForOfficeOpenXML(path string) ([]Field, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()

	var core ooxmlCoreProps
	if err := unmarshalZipEntry(zr, "docProps/core.xml", &core); err != nil && !errors.Is(err, errZipEntryNotFound) {
		return nil, err
	}
	var app ooxmlAppProps
	if err := unmarshalZipEntry(zr, "docProps/app.xml", &app); err != nil && !errors.Is(err, errZipEntryNotFound) {
		return nil, err
	}

	var fields []Field
	add := func(label, value string) {
		if value == "" {
			return
		}
		fields = append(fields, Field{Label: label, Value: value})
	}
	add("Title", core.Title)
	add("Author", core.Creator)
	add("Subject", core.Subject)
	if t, ok := parseW3CDTF(core.Created); ok {
		add("Created", t.Format("2006-01-02 15:04:05"))
	}
	if t, ok := parseW3CDTF(core.Modified); ok {
		add("Modified", t.Format("2006-01-02 15:04:05"))
	}
	add("Application", app.Application)

	if len(fields) == 0 {
		return nil, ErrNoMetadata
	}
	return fields, nil
}

type opfPackage struct {
	Metadata struct {
		Title     string `xml:"title"`
		Creator   string `xml:"creator"`
		Language  string `xml:"language"`
		Date      string `xml:"date"`
		Publisher string `xml:"publisher"`
	} `xml:"metadata"`
}

type epubContainer struct {
	Rootfiles struct {
		Rootfile []struct {
			FullPath string `xml:"full-path,attr"`
		} `xml:"rootfile"`
	} `xml:"rootfiles"`
}

// ForEPUB reads an EPUB's own META-INF/container.xml (to find its OPF
// package file — EPUB allows it anywhere in the archive, unlike Office
// Open XML's fixed docProps/ path) and then that OPF's own <metadata>
// block (Dublin Core, the same vocabulary EPUB's own spec mandates).
func ForEPUB(path string) ([]Field, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()

	var container epubContainer
	if err := unmarshalZipEntry(zr, "META-INF/container.xml", &container); err != nil {
		if errors.Is(err, errZipEntryNotFound) {
			return nil, ErrNoMetadata
		}
		return nil, err
	}
	if len(container.Rootfiles.Rootfile) == 0 || container.Rootfiles.Rootfile[0].FullPath == "" {
		return nil, ErrNoMetadata
	}

	var pkg opfPackage
	if err := unmarshalZipEntry(zr, container.Rootfiles.Rootfile[0].FullPath, &pkg); err != nil {
		if errors.Is(err, errZipEntryNotFound) {
			return nil, ErrNoMetadata
		}
		return nil, err
	}

	var fields []Field
	add := func(label, value string) {
		if value == "" {
			return
		}
		fields = append(fields, Field{Label: label, Value: value})
	}
	add("Title", pkg.Metadata.Title)
	add("Author", pkg.Metadata.Creator)
	add("Language", pkg.Metadata.Language)
	add("Published", pkg.Metadata.Date)
	add("Publisher", pkg.Metadata.Publisher)

	if len(fields) == 0 {
		return nil, ErrNoMetadata
	}
	return fields, nil
}

var errZipEntryNotFound = fmt.Errorf("zip entry not found")

func unmarshalZipEntry(zr *zip.ReadCloser, name string, v any) error {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		data, err := io.ReadAll(rc)
		if err != nil {
			return err
		}
		return xml.Unmarshal(data, v)
	}
	return errZipEntryNotFound
}

// parseW3CDTF parses the handful of W3CDTF variants Office's own
// docProps/core.xml actually uses in practice (always with a "Z"/offset
// suffix for Created/Modified specifically) — not the full W3CDTF
// grammar (which also allows plain-year/year-month forms meant for
// publication dates, not these two machine-generated timestamps).
func parseW3CDTF(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
