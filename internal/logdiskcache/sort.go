package logdiskcache

import "sort"

// sortIdentitySummaries orders by Label — a plain helper rather than
// inlining sort.Slice at the one call site, so Identities' own doc
// comment can point at a named function instead of a lambda.
func sortIdentitySummaries(s []IdentitySummary) {
	sort.Slice(s, func(i, j int) bool { return s[i].Label < s[j].Label })
}
