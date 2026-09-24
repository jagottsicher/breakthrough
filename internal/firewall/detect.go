package firewall

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// runUFWStatus/runNFTRuleset/runIPTablesSave are package-level swappable
// vars — the same "mockable exec wrapper" idiom internal/ui's own dirSize
// already uses — so DetectBackend and ReadSnapshot can be exercised by a
// test without a real ufw/nft/iptables binary ever needing to be present or
// runnable (root is required for several of these in practice). Each
// wraps its own Output() error through wrapExitError, so a real failure
// (most commonly a permission error — several of these do need root)
// surfaces its own real stderr text, not just Go's own bare "exit status
// N" — see wrapExitError's own doc comment for the real, reported gap
// this closes.
var (
	runUFWStatus = func() (string, error) {
		out, err := exec.Command("ufw", "status", "verbose").Output()
		return string(out), wrapExitError(err)
	}
	runNFTRuleset = func() ([]byte, error) {
		out, err := exec.Command("nft", "-j", "list", "ruleset").Output()
		return out, wrapExitError(err)
	}
	runIPTablesSave = func() (string, error) {
		out, err := exec.Command("iptables-save").Output()
		return string(out), wrapExitError(err)
	}
	lookPath = exec.LookPath
)

// wrapExitError enriches err with the failed command's own stderr, if
// any was captured — turning Go's own bare "exit status 4" (all
// *exec.ExitError.Error() ever says on its own) into something
// actually actionable, e.g. "exit status 4: iptables-save: Permission
// denied (you must be root)". A real, reported gap: every one of
// ufw/nft/iptables-save can fail this way when breakthrough isn't
// running as root, and the bare exit code alone gives no hint that
// permissions are the reason. cmd.Output() already populates
// ExitError.Stderr for exactly this (see os/exec's own doc comment on
// Cmd.Output) — this only has to surface it. Left unchanged for any
// other error (the binary itself missing or failing to start, where
// there is no stderr to add) or a clean nil.
func wrapExitError(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			return fmt.Errorf("%w: %s", err, stderr)
		}
	}
	return err
}

// DetectBackend picks exactly one backend to read rules from, in this
// order:
//
//  1. UFW, if the "ufw" binary exists and reports "Status: active" — UFW is
//     itself only ever a front-end over nftables or iptables, so once it's
//     managing this host's rules, reading the layer underneath it would
//     show the exact same rules a second time, in less friendly shape.
//  2. nftables, if "nft" exists and its ruleset actually contains anything —
//     on a modern distro, "iptables" and "nftables" are two different
//     front-ends onto the very same underlying nftables ruleset (the
//     "iptables-nft" compatibility layer); preferring nft's own native JSON
//     view avoids relying on that compatibility translation.
//  3. iptables, if "iptables-save" exists — the legacy (non-nftables-backed)
//     case, or simply a system where nft itself isn't installed.
//  4. BackendNone if nothing above applies — no firewall this package
//     understands is actually active, not an error.
func DetectBackend() Backend {
	if _, err := lookPath("ufw"); err == nil {
		if out, err := runUFWStatus(); err == nil && UFWActive(out) {
			return BackendUFW
		}
	}
	if _, err := lookPath("nft"); err == nil {
		if out, err := runNFTRuleset(); err == nil && nftRulesetNonEmpty(out) {
			return BackendNFTables
		}
	}
	if _, err := lookPath("iptables-save"); err == nil {
		return BackendIPTables
	}
	return BackendNone
}

// nftRulesetNonEmpty reports whether data (nft -j list ruleset's raw JSON
// output) actually declares any table at all — an empty ruleset (no tables
// ever created) is nft's own default, indistinguishable from "not really in
// use" for this screen's purposes, so it falls through to the iptables
// check rather than claiming the nftables backend for an empty result. This
// checks for any table, not just rules ParseNFTRuleset itself understands,
// so an nftables setup with only a default-drop policy and no explicit
// rules still counts as "actually in use".
func nftRulesetNonEmpty(data []byte) bool {
	var doc struct {
		Nftables []struct {
			Table json.RawMessage `json:"table,omitempty"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return false
	}
	for _, item := range doc.Nftables {
		if item.Table != nil {
			return true
		}
	}
	return false
}
