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
// Scope is a single directory Pattern is searched under (recursively)
// for EngineFind. It's ignored outright for EngineLocate: locate's own
// index has no directory-scope argument to give it at the command
// level (see LocateArgs' own doc comment), and an earlier version of
// this package filtered locate's own whole-system results down to
// Scope client-side instead — which sounds reasonable but wasn't, in
// practice: internal/ui's search dialog defaults Scope to wherever the
// panel currently is, so that filter silently discarded almost every
// result for almost any real search (a real user report — "locate
// findet wieder nichts"), defeating the entire reason to pick locate
// over find in the first place: searching the whole system fast, not
// one directory.
//
// IgnoreDirs names directories to skip entirely (e.g. ".git",
// "node_modules") — matched by exact name, not a full path, so a
// matching directory is skipped wherever it appears. Unlike Scope, this
// stays in effect for EngineLocate too: a name only ends up here
// because the user typed it in themselves, never a silent default, so
// there's no equivalent risk of surprising, near-total filtering. For
// EngineFind it's a real prune (see FindArgs), so an ignored tree is
// never even walked; for EngineLocate, whose own index has no
// traversal to prune, it's applied as a client-side filter instead
// (see underIgnoredDir's own caller in Runner).
type Request struct {
	Pattern    string
	Scope      string
	Mode       Mode
	Engine     Engine
	Content    ContentMode
	IgnoreDirs []string
}
