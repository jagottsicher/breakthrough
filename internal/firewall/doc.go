// Package firewall reads a Linux host's own currently active firewall
// rules — UFW, nftables, or iptables, whichever one actually governs
// traffic right now — and normalizes them into one backend-independent
// Rule shape (see rule.go), so internal/ui's own Firewall screen never
// needs to know or care which of the three is in play.
//
// Detection (DetectBackend) picks exactly one backend rather than
// merging all three: on a modern distro, "iptables" and "nftables" are
// two different front-ends onto the very same underlying nftables
// ruleset (the "iptables-nft" compatibility layer), and UFW is itself
// only ever a front-end over one of those two — reading more than one
// of them at once would show the same real rules two or three times
// over, not more information. See DetectBackend's own doc comment for
// the exact preference order and reasoning.
//
// Every parser here (ParseUFWStatusVerbose, ParseNFTRuleset,
// ParseIPTablesSave) is a pure function over already-captured text —
// the same shape internal/ui's own parseFindmntJSON takes for the
// Mounts screen — so the whole rule model is unit-tested without ever
// touching a real system's actual firewall state. ReadSnapshot is the
// only place that actually shells out, the real "ufw"/"nft"/
// "iptables-save" binary in every case, never a netlink/nftables
// library reimplementation of any of them.
package firewall
