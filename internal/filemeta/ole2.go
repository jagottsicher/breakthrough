package filemeta

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/richardlehane/mscfb"
	"github.com/richardlehane/msoleps"
	"github.com/richardlehane/msoleps/types"
)

// IsOLE2Path reports whether path's extension is one ForOLE2 handles
// — the old, binary "Compound File Binary" Office formats (DOC/XLS/
// PPT), predating Office Open XML's own ZIP-based DOCX/XLSX/PPTX —
// mirrors IsDocumentPath's own role for those, so the Details sidebar
// can show a metadata section for these too even though, like the
// Office Open XML/EPUB formats IsDocumentPath already covers, they
// never get a preview image of their own to key off instead.
func IsOLE2Path(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".doc", ".xls", ".ppt":
		return true
	default:
		return false
	}
}

// ole2SummaryFieldOrder maps msoleps' own SummaryInformation property
// names (see its own sets.go — the fixed vocabulary every Compound
// File's "\x05SummaryInformation" stream uses, per the MS-OLEPS spec)
// to this package's display labels, in the order feature_ideas.txt #26
// settled on — the same handful Office Open XML's own
// ForOfficeOpenXML already shows (Title/Author/Subject/Created/
// Modified/Application), so a DOC and a DOCX read the same way here
// regardless of which of the two formats a given file actually is.
// LastAuthor/RevNumber/EditTime/LastPrinted/CodePage are deliberately
// left out — internal, rarely-interesting detail, the same "technical
// fields, not document metadata" line ForPDF's own pdfinfoFieldLabels
// already draws.
var ole2SummaryFieldOrder = []struct{ raw, display string }{
	{"Title", "Title"},
	{"Subject", "Subject"},
	{"Author", "Author"},
	{"CreateTime", "Created"},
	{"LastSaveTime", "Modified"},
	{"AppName", "Application"},
}

// ForOLE2 reads path's own "\x05SummaryInformation" stream — present
// in every real DOC/XLS/PPT file, per the MS-OLEPS spec, the Compound
// File format's own closest equivalent to Office Open XML's
// docProps/core.xml+app.xml — via mscfb (the Compound File Binary
// container reader) and msoleps (the MS-OLEPS property-set decoder
// layered on top of it), rather than a hand-rolled implementation of
// either binary format: both are small, focused, pure-Go libraries
// that already exist for exactly this (see feature_ideas.txt #26's
// own note naming richardlehane/mscfb as the candidate).
func ForOLE2(path string) ([]Field, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	doc, err := mscfb.New(f)
	if err != nil {
		return nil, err
	}

	byRaw := make(map[string]string, len(ole2SummaryFieldOrder))
	props := msoleps.New()
	for entry, entryErr := doc.Next(); entryErr == nil; entry, entryErr = doc.Next() {
		// entry.Name excludes the leading control character that
		// marks a special stream like this one — Initial is mscfb's
		// own separate, already-numeric way to check for it (verified
		// directly against a real file, not assumed from the docs
		// alone: printing entry.Name here shows a clean
		// "SummaryInformation", no \x05 prefix).
		if entry.Name != "SummaryInformation" || !msoleps.IsMSOLEPS(entry.Initial) {
			continue
		}
		if err := props.Reset(doc); err != nil {
			// A malformed SummaryInformation stream in an otherwise
			// valid Compound File is treated the same as "nothing
			// usable found" below, not surfaced as a read error —
			// the file itself opened and parsed as a real CFB
			// container just fine.
			break
		}
		for _, p := range props.Property {
			byRaw[p.Name] = ole2PropertyString(p.T)
		}
		break // the one SummaryInformation stream is all ForOLE2 reads
	}

	var fields []Field
	for _, m := range ole2SummaryFieldOrder {
		if v, ok := byRaw[m.raw]; ok && v != "" {
			fields = append(fields, Field{Label: m.display, Value: v})
		}
	}
	if len(fields) == 0 {
		return nil, ErrNoMetadata
	}
	return fields, nil
}

// ole2PropertyString renders one decoded MS-OLEPS property value as
// display text — a real time.Time, formatted the same
// "2006-01-02 15:04:05" shape ForOfficeOpenXML's own Created/Modified
// fields already use, for CreateTime/LastSaveTime specifically, T's
// own String() for everything else (Title/Subject/Author/AppName are
// all plain text types already).
//
// Explicitly converted to UTC first — types.FileTime.Time() calls
// time.Unix, which Go's own documentation states returns a Time in
// the *host machine's* local Location, not UTC (verified directly
// against msoleps' own filetime.go, not assumed): without this, the
// exact same file would display a different Created/Modified time
// depending on which timezone breakthrough happens to be running in,
// a real bug this package's own test caught by running in CI under a
// different zone than it was first written in.
func ole2PropertyString(t types.Type) string {
	if ft, ok := t.(types.FileTime); ok {
		return ft.Time().UTC().Format("2006-01-02 15:04:05")
	}
	return t.String()
}
