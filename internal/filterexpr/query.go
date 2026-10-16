package filterexpr

import (
	"fmt"
	"strings"
)

// QueryFilter is a parsed boolean text-search expression — see
// ParseQuery. Unlike SizeFilter/MtimeFilter, this one is a real
// expression tree (queryNode), not a flat clause list: AND/OR/NAND/NOR/
// NOT can nest arbitrarily and parentheses override precedence, which a
// flat list has no way to represent. This is a deliberate departure
// from this package's own established "no OR, a range is two AND
// clauses" stance (see SizeFilter's own doc comment) — that stance
// still holds for Size; a free-text search is a different enough shape
// of problem (the user explicitly asked for "keywordA AND (keywordB OR
// keywordC)", which is this package's first query that is not just a
// conjunction of independent clauses) to deserve its own grammar rather
// than being forced into the existing one.
type QueryFilter struct {
	root queryNode
}

// Match reports whether source or message (both already known to the
// caller — the entry's own two text fields, see internal/ui's
// logAuditMatchesKeyword) satisfy the parsed expression. Every leaf
// term matches case-insensitively against either field, exactly the
// "Contains in Message OR Source" rule the flat substring filter this
// replaces already had — only how several terms combine is new.
func (f QueryFilter) Match(source, message string) bool {
	if f.root == nil {
		return true
	}
	return f.root.eval(strings.ToLower(source), strings.ToLower(message))
}

// PositiveTerms is every term in the expression that is NOT inside a
// NOT/NAND/NOR branch, deduplicated, in first-occurrence order — what
// internal/ui's logAuditHighlight highlights. A term under negation is
// excluded deliberately: highlighting a term whose own *absence* is
// what made an entry match would be actively misleading (see this
// method's own call site for the full reasoning). NAND/NOR negate both
// of their own operands for this purpose, the same as De Morgan's laws
// say "a NAND b" / "a NOR b" really mean: "(not a) or (not b)" /
// "(not a) and (not b)" respectively, never a positive mention of
// either a or b.
func (f QueryFilter) PositiveTerms() []string {
	if f.root == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	collectPositiveTerms(f.root, false, &out, seen)
	return out
}

func collectPositiveTerms(n queryNode, negated bool, out *[]string, seen map[string]bool) {
	switch t := n.(type) {
	case *queryTerm:
		if negated {
			return
		}
		key := strings.ToLower(t.text)
		if seen[key] {
			return
		}
		seen[key] = true
		*out = append(*out, t.text)
	case *queryNot:
		collectPositiveTerms(t.operand, !negated, out, seen)
	case *queryBinary:
		childNegated := negated
		if t.op == opNand || t.op == opNor {
			childNegated = !negated
		}
		collectPositiveTerms(t.left, childNegated, out, seen)
		collectPositiveTerms(t.right, childNegated, out, seen)
	}
}

// queryNode is one node of a parsed expression tree — a term, a NOT, or
// a binary AND/OR/NAND/NOR. Deliberately unexported: PositiveTerms/Match
// are the whole public surface a caller needs, the same "the grammar's
// own internals stay private, only the parsed result's behavior is
// exported" shape SizeFilter/MtimeFilter already establish.
type queryNode interface {
	eval(sourceLower, messageLower string) bool
}

type queryTerm struct{ text string }

func (t *queryTerm) eval(sourceLower, messageLower string) bool {
	lower := strings.ToLower(t.text)
	return strings.Contains(messageLower, lower) || strings.Contains(sourceLower, lower)
}

type queryNot struct{ operand queryNode }

func (n *queryNot) eval(sourceLower, messageLower string) bool {
	return !n.operand.eval(sourceLower, messageLower)
}

type queryOp int

const (
	opAnd queryOp = iota
	opOr
	opNand
	opNor
)

type queryBinary struct {
	op          queryOp
	left, right queryNode
}

func (b *queryBinary) eval(sourceLower, messageLower string) bool {
	l := b.left.eval(sourceLower, messageLower)
	r := b.right.eval(sourceLower, messageLower)
	switch b.op {
	case opAnd:
		return l && r
	case opOr:
		return l || r
	case opNand:
		return !l || !r
	default: // opNor
		return !l && !r
	}
}

// ParseQuery parses a boolean text-search expression:
//
//   - A bare word is a term, matched case-insensitively against either
//     Source or Message (see QueryFilter.Match) — exactly the substring
//     rule the plain keyword filter this replaces already had.
//   - Two terms next to each other with no operator between them
//     ("foo bar") mean AND — the common case of typing several words
//     with no connector needs to keep working the way it always did,
//     not silently match everything.
//   - AND, OR, NAND, NOR, NOT (case-insensitive) combine terms or
//     parenthesized sub-expressions — "NAND"/"NOR" are the negated
//     forms of AND/OR ("a NAND b" is "not (a and b)", "a NOR b" is
//     "not (a or b)"), not a typo for "and"/"or".
//   - Parentheses group a sub-expression to override the default
//     precedence (NOT binds tightest, then AND/NAND, then OR/NOR,
//     left-associative) — "keywordA AND (keywordB OR keywordC)" means
//     exactly what it reads as.
//   - A term that needs to contain a literal "and"/"or"/"not"/"nand"/
//     "nor", a parenthesis, or whitespace: wrap it in double quotes,
//     e.g. `"not found"` searches for the literal text "not found"
//     rather than negating a term "found". A quote cannot itself be
//     escaped inside one — keep it to the one restriction every other
//     grammar in this package already accepts in exchange for staying
//     simple.
//
// An empty expression is rejected the same way ParseSize/ParseMtime
// reject one — the caller (logAuditMatchesKeyword) already treats a
// blank field as its own "no filter" case before ever calling this.
func ParseQuery(expr string) (QueryFilter, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return QueryFilter{}, fmt.Errorf("filterexpr: empty query expression")
	}
	toks, err := tokenizeQuery(expr)
	if err != nil {
		return QueryFilter{}, err
	}
	p := &queryParser{toks: toks}
	root, err := parseOrImpl(p)
	if err != nil {
		return QueryFilter{}, err
	}
	if p.pos != len(p.toks) {
		return QueryFilter{}, fmt.Errorf("filterexpr: unexpected %q in query expression", p.toks[p.pos].text)
	}
	return QueryFilter{root: root}, nil
}

type queryTokenKind int

const (
	tokTerm queryTokenKind = iota
	tokAnd
	tokOr
	tokNand
	tokNor
	tokNot
	tokLParen
	tokRParen
)

type queryToken struct {
	kind queryTokenKind
	text string // the raw term text, only meaningful for tokTerm
}

// queryKeywords maps a case-folded bare word to the operator token it
// means — checked before falling back to tokTerm, so typing "and"/"or"/
// "not"/"nand"/"nor" unquoted is always read as the operator; ParseQuery's
// own doc comment is where quoting one of these as a literal term is
// documented.
var queryKeywords = map[string]queryTokenKind{
	"and":  tokAnd,
	"or":   tokOr,
	"nand": tokNand,
	"nor":  tokNor,
	"not":  tokNot,
}

// tokenizeQuery splits expr into tokens — parentheses are always their
// own token (never part of a word, so a term touching one directly,
// "foo(bar)", splits into "foo", "(", "bar", ")" rather than one word;
// quote a term that genuinely needs a literal parenthesis in it).
func tokenizeQuery(expr string) ([]queryToken, error) {
	var toks []queryToken
	runes := []rune(expr)
	i := 0
	for i < len(runes) {
		c := runes[i]
		switch c {
		case ' ', '\t', '\n', '\r':
			i++
		case '(':
			toks = append(toks, queryToken{kind: tokLParen})
			i++
		case ')':
			toks = append(toks, queryToken{kind: tokRParen})
			i++
		case '"':
			j := i + 1
			for j < len(runes) && runes[j] != '"' {
				j++
			}
			if j >= len(runes) {
				return nil, fmt.Errorf("filterexpr: unterminated %q in query expression", `"`)
			}
			text := string(runes[i+1 : j])
			if text == "" {
				return nil, fmt.Errorf("filterexpr: empty quoted term in query expression")
			}
			toks = append(toks, queryToken{kind: tokTerm, text: text})
			i = j + 1
		default:
			j := i
			for j < len(runes) && runes[j] != ' ' && runes[j] != '\t' && runes[j] != '\n' && runes[j] != '\r' &&
				runes[j] != '(' && runes[j] != ')' && runes[j] != '"' {
				j++
			}
			word := string(runes[i:j])
			if kind, ok := queryKeywords[strings.ToLower(word)]; ok {
				toks = append(toks, queryToken{kind: kind})
			} else {
				toks = append(toks, queryToken{kind: tokTerm, text: word})
			}
			i = j
		}
	}
	return toks, nil
}

// queryParser is a plain recursive-descent parser over toks — pos is
// the next unconsumed token. Grammar, tightest-to-loosest:
//
//	or      := and ( (OR|NOR) and )*
//	and     := not ( (AND|NAND)? not )*   -- a missing operator means AND
//	not     := NOT not | primary
//	primary := '(' or ')' | TERM
type queryParser struct {
	toks []queryToken
	pos  int
}

func (p *queryParser) peek() (queryToken, bool) {
	if p.pos >= len(p.toks) {
		return queryToken{}, false
	}
	return p.toks[p.pos], true
}

func parseOrImpl(p *queryParser) (queryNode, error) {
	left, err := parseAnd(p)
	if err != nil {
		return nil, err
	}
	for {
		tok, ok := p.peek()
		if !ok || (tok.kind != tokOr && tok.kind != tokNor) {
			return left, nil
		}
		p.pos++
		right, err := parseAnd(p)
		if err != nil {
			return nil, err
		}
		op := opOr
		if tok.kind == tokNor {
			op = opNor
		}
		left = &queryBinary{op: op, left: left, right: right}
	}
}

// startsOperand reports whether tok could begin a fresh "not"
// production — used by parseAnd to recognize an implicit AND (two
// operands with no operator token between them at all).
func startsOperand(tok queryToken) bool {
	switch tok.kind {
	case tokTerm, tokNot, tokLParen:
		return true
	default:
		return false
	}
}

func parseAnd(p *queryParser) (queryNode, error) {
	left, err := parseNot(p)
	if err != nil {
		return nil, err
	}
	for {
		tok, ok := p.peek()
		if !ok {
			return left, nil
		}
		switch {
		case tok.kind == tokAnd || tok.kind == tokNand:
			p.pos++
			right, err := parseNot(p)
			if err != nil {
				return nil, err
			}
			op := opAnd
			if tok.kind == tokNand {
				op = opNand
			}
			left = &queryBinary{op: op, left: left, right: right}
		case startsOperand(tok):
			// No explicit operator between two operands — implicit AND
			// (see ParseQuery's own doc comment on "foo bar" meaning
			// "foo AND bar").
			right, err := parseNot(p)
			if err != nil {
				return nil, err
			}
			left = &queryBinary{op: opAnd, left: left, right: right}
		default:
			return left, nil
		}
	}
}

func parseNot(p *queryParser) (queryNode, error) {
	tok, ok := p.peek()
	if ok && tok.kind == tokNot {
		p.pos++
		operand, err := parseNot(p)
		if err != nil {
			return nil, err
		}
		return &queryNot{operand: operand}, nil
	}
	return parsePrimary(p)
}

func parsePrimary(p *queryParser) (queryNode, error) {
	tok, ok := p.peek()
	if !ok {
		return nil, fmt.Errorf("filterexpr: query expression ends unexpectedly, expected a term or \"(\"")
	}
	switch tok.kind {
	case tokTerm:
		p.pos++
		return &queryTerm{text: tok.text}, nil
	case tokLParen:
		p.pos++
		inner, err := parseOrImpl(p)
		if err != nil {
			return nil, err
		}
		closing, ok := p.peek()
		if !ok || closing.kind != tokRParen {
			return nil, fmt.Errorf("filterexpr: missing closing \")\" in query expression")
		}
		p.pos++
		return inner, nil
	default:
		return nil, fmt.Errorf("filterexpr: expected a term or \"(\", found an operator in query expression")
	}
}
