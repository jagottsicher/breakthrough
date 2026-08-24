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
// "node_modules") — matched against each path component (not a full
// path) via filepath.Match, so plain names match exactly the same as
// before (no glob metacharacters, so only an identical component
// matches) while also allowing a real glob pattern like ".*" — which
// internal/ui's search dialog's own "Skip hidden" toggle appends here
// rather than needing its own separate mechanism (see
// underIgnoredDir's own caller in Runner for the matching, FindArgs
// for find's own real prune, which this backs for both). Unlike Scope,
// this stays in effect for EngineLocate too: an entry only ends up
// here because the user typed it in or turned on Skip hidden
// themselves, never a silent default, so there's no equivalent risk of
// surprising, near-total filtering. For EngineFind it's a real prune,
// so an ignored tree is never even walked; for EngineLocate, whose own
// index has no traversal to prune, it's applied as a client-side
// filter instead.
//
// CaseSensitive, false by default, matches every one of find/locate/
// grep's own case-insensitive default matching (see FindArgs/
// LocateArgs/GrepArgs) — true switches all of them to case-sensitive
// matching instead.
//
// NonRecursive/FollowSymlinks only apply to EngineFind (MC's own "Find
// recursively"/"Follow symlinks" checkboxes — see FindArgs); meaningless
// for EngineLocate, whose own index has no live traversal to shape this
// way. NonRecursive is named for the opposite of find's own actual
// default (always recursive) specifically so a Request's own zero value
// still means "recursive" — the same reason CaseSensitive above defaults
// false rather than true.
//
// WholeWords/FirstHit only apply once Content is anything but
// ContentNone (MC's own "Whole words"/"First hit" checkboxes — see
// GrepArgs/ZgrepArgs/ZipgrepArgs): match whole words only, and stop
// after the first match per file, respectively.
type Request struct {
	Pattern        string
	Scope          string
	Mode           Mode
	Engine         Engine
	Content        ContentMode
	IgnoreDirs     []string
	CaseSensitive  bool
	NonRecursive   bool
	FollowSymlinks bool
	WholeWords     bool
	FirstHit       bool
}
