package viewer

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// TokenKind is one syntax category Look colors differently — a
// deliberately small set, not chroma's own ~80 TokenTypes (see
// classify, which folds those down to these). Small on purpose: every
// kind here needs its own real color in internal/ui's own palette, and
// a terminal color scheme that has to define eighty of them is one
// nobody will ever actually tune by hand.
type TokenKind int

const (
	// KindPlain is ordinary text — anything the lexer didn't classify
	// as something more specific, and everything in a file no lexer
	// matched at all (see Highlight's own single-token fallback).
	KindPlain TokenKind = iota
	KindKeyword
	KindString
	KindComment
	KindNumber
	// KindName covers identifiers that carry structural meaning — a
	// YAML/INI key, an XML/HTML attribute or tag name, a function or
	// type name. Grouped together rather than split further because at
	// terminal-color granularity the distinction stops being legible.
	KindName
	// KindLiteral is a language's own built-in constants (true, false,
	// null, nil, ...) — visually distinct from a plain identifier is
	// worth one color, per-language spelling isn't.
	KindLiteral
	// KindDiffAdd/KindDiffDelete are a diff/patch's own added and
	// removed lines (chroma's GenericInserted/GenericDeleted) — the one
	// place where a "generic" chroma category really does deserve its
	// own color, since reading a diff is exactly what those colors are
	// for.
	KindDiffAdd
	KindDiffDelete
)

// Token is one run of text sharing a single TokenKind (see Highlight).
// Text is the file's own content verbatim — never escaped, trimmed, or
// otherwise altered here: internal/ui escapes it at render time (see
// its own showBuiltinLook), and doing it here too would double-escape.
type Token struct {
	Kind TokenKind
	Text string
}

// HighlightLimit is the largest content Highlight will actually lex.
// Beyond it, the whole input comes back as one KindPlain token instead
// (see Highlight): chroma's own lexers are regex-driven and cost real
// time per byte, and a multi-megabyte log — exactly the kind of file
// Look's own DefaultPreviewLimit still happily shows — would otherwise
// stall the UI for seconds before a single line appeared. Well above
// any realistic source file, well below a log worth worrying about.
const HighlightLimit = 1 << 20 // 1 MiB

// Highlight splits content into colored Tokens, choosing a lexer by
// path's own file name first and falling back to analysing content
// itself (chroma's own Match/Analyse — so an extensionless file that's
// obviously a shell script or YAML still gets colored). Content larger
// than HighlightLimit, or content no lexer recognizes at all, comes
// back as exactly one KindPlain token spanning the whole input, so
// callers never need a separate "not highlighted" branch — rendering a
// single plain token is the same code path as rendering many.
//
// Concatenating every returned Token's Text always reproduces content
// byte for byte: this only ever classifies, never rewrites. A lexer
// that fails partway through (malformed input for its own grammar)
// falls back to that same single plain token rather than returning a
// truncated file — showing the content uncolored is always better than
// showing only part of it.
func Highlight(path, content string) []Token {
	plain := []Token{{Kind: KindPlain, Text: content}}
	if len(content) > HighlightLimit {
		return plain
	}

	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		lexer = lexers.Analyse(content)
	}
	if lexer == nil {
		return plain
	}

	iterator, err := lexer.Tokenise(nil, content)
	if err != nil {
		return plain
	}

	var (
		tokens []Token
		b      strings.Builder
		kind   = KindPlain
		first  = true
	)
	for t := iterator(); t != chroma.EOF; t = iterator() {
		k := classify(t.Type)
		// Runs of the same kind are merged into one Token rather than
		// emitted per chroma token: a line of ordinary code produces
		// dozens of adjacent identical-kind tokens, and each one would
		// otherwise become its own redundant pair of color tags in the
		// rendered output.
		if first {
			kind, first = k, false
		} else if k != kind {
			tokens = append(tokens, Token{Kind: kind, Text: b.String()})
			b.Reset()
			kind = k
		}
		b.WriteString(t.Value)
	}
	if b.Len() > 0 {
		tokens = append(tokens, Token{Kind: kind, Text: b.String()})
	}
	if len(tokens) == 0 {
		return plain
	}
	return tokens
}

// classify folds one chroma TokenType into this package's own much
// smaller TokenKind set (see TokenKind's own doc comment on why it
// stays small). Category-based (TokenType.InCategory) rather than a
// table of every individual constant: chroma groups its own types
// numerically by category exactly so a consumer can do this, and a
// hand-maintained table of eighty constants would silently miss
// whatever a future chroma version adds.
func classify(t chroma.TokenType) TokenKind {
	switch {
	case t.InCategory(chroma.Comment):
		return KindComment
	// InSubCategory, not InCategory, for these two specifically:
	// chroma's own Category() divides by 1000, so LiteralString (3100)
	// and LiteralNumber (3200) share category Literal (3000) — an
	// InCategory(LiteralString) test therefore also matches every
	// number, silently swallowing them into the string color (a real
	// bug this package's own tests caught). SubCategory divides by 100,
	// which is exactly the granularity that tells these two apart.
	case t.InSubCategory(chroma.LiteralString):
		return KindString
	case t.InSubCategory(chroma.LiteralNumber):
		return KindNumber
	case t == chroma.GenericInserted:
		return KindDiffAdd
	case t == chroma.GenericDeleted:
		return KindDiffDelete
	case t.InCategory(chroma.Keyword):
		// A language's own built-in constants live under Keyword in
		// chroma (KeywordConstant: true/false/nil/null) — split back out
		// here, since "a value" and "a control-flow word" reading alike
		// is exactly what a reader doesn't want.
		if t == chroma.KeywordConstant {
			return KindLiteral
		}
		return KindKeyword
	case t.InCategory(chroma.Name):
		switch t {
		case chroma.NameAttribute, chroma.NameTag, chroma.NameFunction, chroma.NameClass, chroma.NameBuiltin:
			return KindName
		}
		// Every other Name (a plain variable, a label, ...) stays
		// uncolored: coloring literally every identifier in a file
		// leaves nothing visually quiet enough to read against.
		return KindPlain
	case t.InCategory(chroma.Literal):
		return KindLiteral
	default:
		return KindPlain
	}
}
