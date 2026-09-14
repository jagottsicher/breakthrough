package batchrename

import "fmt"

// Every small int-backed enum in Rules has a fixed, human-readable
// spelling — "upper", "prefix", "by-name" — used in two places: the UI's
// own enum fields (which cycle and display these strings — see
// internal/ui's batchrenamecatalog.go) and preset files on disk (see
// preset.go), where an int would be meaningless to anyone opening the
// JSON in an editor. The MarshalText/UnmarshalText pairs below are
// what make encoding/json write and read those spellings; an
// unrecognized spelling on the way in is an error, not silently the
// zero value, so a typo in a hand-edited preset is reported rather than
// quietly turning a step off.

// enumSpelling is one (value, spelling) pair of a small enum, listed
// in the order the UI cycles through them.
type enumSpelling[T ~int] struct {
	value    T
	spelling string
}

func spellingOf[T ~int](table []enumSpelling[T], v T) string {
	for _, e := range table {
		if e.value == v {
			return e.spelling
		}
	}
	return table[0].spelling
}

func parseSpelling[T ~int](table []enumSpelling[T], kind, s string) (T, error) {
	for _, e := range table {
		if e.spelling == s {
			return e.value, nil
		}
	}
	return table[0].value, fmt.Errorf("unknown %s %q", kind, s)
}

var caseModes = []enumSpelling[CaseMode]{
	{CaseNone, "none"},
	{CaseUpper, "upper"},
	{CaseLower, "lower"},
	{CaseTitle, "title"},
	{CaseSentence, "sentence"},
}

var numberPositions = []enumSpelling[NumberPosition]{
	{NumberNone, "none"},
	{NumberPrefix, "prefix"},
	{NumberSuffix, "suffix"},
}

var numberOrders = []enumSpelling[NumberOrder]{
	{OrderAsListed, "as-listed"},
	{OrderByName, "by-name"},
	{OrderByModTime, "by-mtime"},
}

var extensionModes = []enumSpelling[ExtensionMode]{
	{ExtensionKeep, "keep"},
	{ExtensionLower, "lower"},
	{ExtensionUpper, "upper"},
	{ExtensionRemove, "remove"},
	{ExtensionSetTo, "set"},
}

func (m CaseMode) String() string { return spellingOf(caseModes, m) }

// ParseCaseMode is String's inverse; an unknown spelling is CaseNone
// plus an error.
func ParseCaseMode(s string) (CaseMode, error) { return parseSpelling(caseModes, "case mode", s) }

func (m CaseMode) MarshalText() ([]byte, error) { return []byte(m.String()), nil }

func (m *CaseMode) UnmarshalText(b []byte) error {
	v, err := ParseCaseMode(string(b))
	*m = v
	return err
}

func (p NumberPosition) String() string { return spellingOf(numberPositions, p) }

// ParseNumberPosition is String's inverse.
func ParseNumberPosition(s string) (NumberPosition, error) {
	return parseSpelling(numberPositions, "number position", s)
}

func (p NumberPosition) MarshalText() ([]byte, error) { return []byte(p.String()), nil }

func (p *NumberPosition) UnmarshalText(b []byte) error {
	v, err := ParseNumberPosition(string(b))
	*p = v
	return err
}

func (o NumberOrder) String() string { return spellingOf(numberOrders, o) }

// ParseNumberOrder is String's inverse.
func ParseNumberOrder(s string) (NumberOrder, error) {
	return parseSpelling(numberOrders, "number order", s)
}

func (o NumberOrder) MarshalText() ([]byte, error) { return []byte(o.String()), nil }

func (o *NumberOrder) UnmarshalText(b []byte) error {
	v, err := ParseNumberOrder(string(b))
	*o = v
	return err
}

func (m ExtensionMode) String() string { return spellingOf(extensionModes, m) }

// ParseExtensionMode is String's inverse.
func ParseExtensionMode(s string) (ExtensionMode, error) {
	return parseSpelling(extensionModes, "extension mode", s)
}

func (m ExtensionMode) MarshalText() ([]byte, error) { return []byte(m.String()), nil }

func (m *ExtensionMode) UnmarshalText(b []byte) error {
	v, err := ParseExtensionMode(string(b))
	*m = v
	return err
}
