// Package apikey resolves and stores API keys for the AI Consigliere's
// own provider backends (see internal/consigliere), in three tiers,
// tried in this fixed order — not a per-provider setting, the same
// "no abstraction before a second real need" reasoning that makes it
// a fixed chain rather than a configurable one:
//
//  1. An environment variable (e.g. ANTHROPIC_API_KEY) — the names the
//     providers' own official SDKs/CLIs already read, so a key already
//     exported for other tools just works here too, with no file on
//     disk at all.
//  2. A plain file at a path the user configures themselves, anywhere
//     they choose — an explicit, deliberate "I'm managing this myself,
//     unencrypted" choice, the same shape AWS CLI's own ~/.aws/credentials
//     already is: protected only by filesystem permissions, nothing
//     more, and documented as such.
//  3. A key typed into breakthrough's own Options screen — encrypted
//     at rest (see Store/Session below) rather than OS-keyring-backed:
//     breakthrough's own primary environment is a headless server over
//     SSH, where no Secret Service/Keychain daemon is typically
//     running at all, so the usual desktop-app answer (hand it to the
//     OS keyring) would silently just not work on the machines this
//     app is mostly used on. A passphrase the user sets themselves,
//     run through scrypt into an AES-256-GCM key, is self-contained
//     instead — the same trust model pass/age already use for the
//     identical underlying worry: an accidentally-copied or
//     accidentally-shared config directory must not hand over a
//     usable credential along with it.
//
// Deliberately free of any internal/ui import — every piece here is
// testable without a Root, and the Options screen that will
// eventually drive it is just one more caller, not something this
// package depends on.
package apikey
