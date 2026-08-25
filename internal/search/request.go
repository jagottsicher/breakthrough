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
// zipgrep).
//
// NamePattern/NameMode additionally restrict a *content* search
// (Content != ContentNone) to files matching a name pattern first,
// searched via Engine the same as a filename search would be, before
// ever grepping anything — per the user's own explicit request: typing
// something into both Filename and Content used to search Content's
// own pattern across every single file under Scope, silently ignoring
// Filename outright the moment Content had anything in it. Left empty
// (the zero value), a content search still runs across every file
// under Scope exactly as before — this is filtering *additional* to
// Content, not a replacement for the Filename/Content-are-mutually-
// exclusive split above, which stays: a search is still never both "by
// name alone" and "by content alone" — NamePattern only ever narrows a
// content search, it never runs standalone the way Pattern does for
// Content == ContentNone.
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
//
// IncludeArchives additionally searches inside every zip/tar(.gz/.bz2/
// .xz) archive found under Scope for a member whose own name matches
// Pattern — a plain filename search only (Content == ContentNone);
// meaningless otherwise, the same as NamePattern/NameMode only ever
// narrowing a content search rather than running standalone (see this
// struct's own doc comment above). One level deep only: an archive
// inside another archive isn't itself opened — a deliberate "Stufe A"
// scope decision, not a limitation of the approach (see
// internal/search's own archive.go). A match here is reported with
// Result.ArchiveMember set.
type Request struct {
	Pattern         string
	NamePattern     string
	NameMode        Mode
	Scope           string
	Mode            Mode
	Engine          Engine
	Content         ContentMode
	IgnoreDirs      []string
	CaseSensitive   bool
	NonRecursive    bool
	FollowSymlinks  bool
	WholeWords      bool
	FirstHit        bool
	IncludeArchives bool
}
