package search

// Engine selects the filename-search backend (see Request.Engine) —
// meaningless once Content is anything but ContentNone, since content
// search always goes through grep/zgrep/zipgrep directly rather than
// filtering an Engine's own filename results first (see Request's own
// doc comment on why the two aren't combined).
type Engine int

const (
	EngineFind Engine = iota
	EngineLocate
)

// ContentMode selects whether Request searches file names (ContentNone)
// or file content instead (see Request's own doc comment).
type ContentMode int

const (
	ContentNone ContentMode = iota
	ContentGrep
	ContentGzip
	ContentZip
)

// Request describes one search to run (see Run). Pattern is matched
// against either file names (Content == ContentNone, via Engine — find
// or locate) or file content (any other Content value, via grep/zgrep/
// zipgrep) — never both at once: this is a deliberate scope decision
// for a first version, not a limitation of the underlying tools
// (find/grep genuinely could be composed, e.g. only grep within
// find's own results) — combining them is a reasonable later addition,
// not built here to keep the search dialog itself to one pattern field
// rather than two.
//
// Scope is a single directory Pattern is searched under (recursively,
// for everything except EngineLocate — see LocateArgs' own doc comment
// on why a locate search's own index has no directory scope to give it
// at the command level; internal/ui's search dialog instead filters
// locate's results by Scope itself once they come back).
type Request struct {
	Pattern string
	Scope   string
	Mode    Mode
	Engine  Engine
	Content ContentMode
}
