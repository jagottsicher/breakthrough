// Package filterexpr parses the small comparison-operator languages
// behind the filter dropdown's own size and modified-time rows (see
// internal/ui/filtermenu.go) — kept separate from internal/ui so the
// grammar and its matching rules have their own focused tests, the same
// "parsing logic lives apart from the UI code that calls it" split
// internal/replace and internal/batchrename already follow for their
// own respective parsers.
//
// Both ParseSize and ParseMtime treat a syntax error as an ordinary
// Go error, not a panic — the UI layer's own convention (see
// filterByText's own doc comment in internal/ui/panel.go) is to treat
// an unparseable expression as "no filter yet" while the user is still
// mid-keystroke, exactly the same as an incomplete regex or an
// unterminated glob bracket already gets treated there. Neither
// function reaches out to the filesystem or the clock on its own:
// ParseMtime takes "now" as an explicit parameter rather than calling
// time.Now() internally, so a test (and, for that matter, a caller)
// never has to fight a moving target to get a deterministic answer.
package filterexpr
