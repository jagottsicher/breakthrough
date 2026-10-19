// Package rclone drives the real rclone(1) binary to list, mount, and
// unmount cloud-storage remotes (S3 and its clones, Azure Blob, Aliyun
// OSS, FTP, WebDAV — which covers both Nextcloud and MagentaCloud —
// OneDrive, Dropbox, Google Drive, and the rest of rclone's own ~70
// backends) — never reimplements any cloud protocol itself, the same
// "shell out to the real, established tool" convention this project
// already applies to findmnt/rsync/git (see internal/mounts,
// internal/rsync, internal/git) rather than reinventing it. Unlike
// those, rclone is not interchangeable with some other tool for this
// purpose — "as many providers as possible" is rclone's own product
// definition, so the package is named after it directly, the same
// reasoning internal/rsync and internal/git already apply to their own
// names.
//
// Secrets never pass through this package. rclone owns its own
// backend credentials in its own config file (~/.config/rclone/
// rclone.conf, optionally rclone-encrypted); this package only ever
// calls `rclone listremotes --long --json`, which reports a remote's
// name and type and nothing else. `rclone config dump` is deliberately
// never called from here: a real run against a throwaway S3 remote
// confirmed it returns access_key_id/secret_access_key in plain text
// in its JSON output (and other backends' passwords only obscured, not
// encrypted) — exactly the secret this package must never touch, let
// alone log or persist. That split mirrors internal/remotefs's own
// Connection type, which just as deliberately never carries a
// password/passphrase/private key either.
package rclone

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/jagottsicher/breakthrough/internal/mounts"
)

// ErrNotAvailable is returned by every exported function here once
// Available reports false — so a caller (internal/ui's own cloud-mount
// menu) can show "rclone not found" and disable the relevant actions
// rather than attempt a command that can only fail, per this project's
// own "no silent failures" rule.
var ErrNotAvailable = errors.New("rclone: binary not found on PATH")

// Remote is one remote already configured in rclone's own config file —
// deliberately just its name and backend type, exactly what
// `rclone listremotes --long --json` reports and nothing more (see
// this package's own doc comment on why `rclone config dump`, which
// would additionally hand back secrets, is never called).
type Remote struct {
	Name string
	Type string
}

// MountOptions configures one Mount call beyond the bare remote/target
// pair every call needs.
type MountOptions struct {
	// CacheDir, if set, is passed as --cache-dir so the VFS write cache
	// (see Mount's own doc comment on why that cache is mandatory, not
	// optional) lands somewhere the caller controls and can bound,
	// rather than rclone's own default location.
	CacheDir string
	// ExtraArgs is appended verbatim after this package's own fixed
	// flags — an escape hatch for a provider-specific flag
	// (e.g. --vfs-cache-max-size) without this package needing to grow
	// a field for every single one rclone itself supports.
	ExtraArgs []string
}

// runListRemotes/runMount/runUnmount/lookPath are package-level
// swappable vars — the same "mockable exec wrapper" idiom
// internal/firewall's own DetectBackend already establishes — so
// ListRemotes/Mount/Unmount/Available can be exercised by a test
// without a real rclone binary, or a real mount, ever needing to exist.
var (
	runListRemotes = func() ([]byte, error) {
		out, err := exec.Command("rclone", "listremotes", "--long", "--json").Output()
		return out, wrapExitError(err)
	}
	runMount = func(args []string) error {
		_, err := exec.Command("rclone", args...).Output()
		return wrapExitError(err)
	}
	runUnmount = func(target string) error {
		_, err := exec.Command(unmountCommand(), "-u", target).Output()
		return wrapExitError(err)
	}
	runConfigCreate = func(args []string) error {
		_, err := exec.Command("rclone", args...).Output()
		return wrapExitError(err)
	}
	runConfigDelete = func(name string) error {
		_, err := exec.Command("rclone", "config", "delete", name).Output()
		return wrapExitError(err)
	}
	runTestRemote = func(args []string) error {
		_, err := exec.Command("rclone", args...).Output()
		return wrapExitError(err)
	}
	runObscure = func(plaintext string) (string, error) {
		out, err := exec.Command("rclone", "obscure", plaintext).Output()
		return strings.TrimSpace(string(out)), wrapExitError(err)
	}
	lookPath = exec.LookPath
)

// unmountCommand names the real program that unmounts a FUSE mount —
// "fusermount" on Linux (present, directly or as the distro's own
// compatibility symlink, on every Linux this project targets); macOS's
// macFUSE instead registers as an ordinary mount, unmounted the same
// way any other one is, via the system's own "umount". BSD is not
// covered yet — left for whoever actually needs it, per this project's
// own "don't build for a platform nobody has verified on" stance,
// rather than guessing at a BSD-specific binary name here.
func unmountCommand() string {
	if runtime.GOOS == "darwin" {
		return "umount"
	}
	return "fusermount"
}

// wrapExitError enriches err with the failed command's own stderr, the
// same gap-closing reasoning internal/firewall's own wrapExitError
// already documents for ufw/nft/iptables-save: a bare "exit status 1"
// tells a sysadmin nothing a real rclone error message would.
func wrapExitError(err error) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			return fmt.Errorf("%w: %s", err, stderr)
		}
	}
	return err
}

// Available reports whether the rclone binary itself is even on PATH.
// Every other exported function in this package calls this first and
// returns ErrNotAvailable instead of attempting a command that could
// only ever fail — see ErrNotAvailable's own doc comment.
func Available() bool {
	_, err := lookPath("rclone")
	return err == nil
}

// listRemotesEntry mirrors one element of `rclone listremotes --long
// --json`'s own array — confirmed against a real run with
// mys3/mywebdav/myftp remotes configured, not guessed: besides name
// and type it also reports "source" and "description", neither of
// which this package has any use for yet.
type listRemotesEntry struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ListRemotes reports every remote already configured in rclone's own
// config file, by shelling out to `rclone listremotes --long --json`
// — never `rclone config dump` (see this package's own doc comment on
// why).
func ListRemotes() ([]Remote, error) {
	if !Available() {
		return nil, ErrNotAvailable
	}
	out, err := runListRemotes()
	if err != nil {
		return nil, err
	}
	return parseListRemotesJSON(out)
}

// parseListRemotesJSON is ListRemotes' own pure parsing step, split out
// so a test can exercise it against a captured fixture without
// shelling out to a real rclone at all.
func parseListRemotesJSON(data []byte) ([]Remote, error) {
	var entries []listRemotesEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	remotes := make([]Remote, 0, len(entries))
	for _, e := range entries {
		remotes = append(remotes, Remote{Name: e.Name, Type: e.Type})
	}
	return remotes, nil
}

// Mount mounts remote (rclone's own "name:" syntax, e.g. "mys3:") onto
// target, which must already exist as a real, empty, writable
// directory — this package never creates one itself (see internal/ui's
// own cloud-mount dialog for that half of the contract).
//
// Always passes --vfs-cache-mode full: without it, writing to an
// object-store backend (S3 and its clones, Azure, Aliyun OSS) fails or
// silently truncates rather than actually working — confirmed against
// rclone's own VFS documentation, and non-negotiable here since
// "mounts you can write to" is this package's whole reason to exist,
// not an afterthought a caller could opt into later.
//
// Runs detached (--daemon): the mount itself is meant to outlive this
// one call, the same way a real `mount` command returns immediately
// once the mount is established rather than blocking for as long as
// the filesystem stays mounted.
func Mount(remote, target string, opts MountOptions) error {
	if !Available() {
		return ErrNotAvailable
	}
	args := []string{"mount", remote, target, "--daemon", "--vfs-cache-mode", "full"}
	if opts.CacheDir != "" {
		args = append(args, "--cache-dir", opts.CacheDir)
	}
	args = append(args, opts.ExtraArgs...)
	return runMount(args)
}

// Unmount unmounts whatever is mounted at target — see unmountCommand
// for which real program that is on the current OS.
func Unmount(target string) error {
	if !Available() {
		return ErrNotAvailable
	}
	return runUnmount(target)
}

// CreateStaticRemote configures a brand-new rclone remote with static
// credentials — S3 and its clones, Azure Blob, Aliyun OSS, FTP, WebDAV
// (Nextcloud, MagentaCloud) — by shelling out to `rclone config create
// name type key=value ... --non-interactive`. Each entry of params is
// one already-formed "key=value" pair, exactly rclone's own CLI
// syntax, rather than a map: argument order then stays deterministic,
// which matters for testing this without a real rclone, and building
// those pairs (which keys a given backend type needs) is the Connect
// dialog's own job, not this package's.
//
// OAuth backends (OneDrive, Dropbox, Google Drive) deliberately go
// through a different path, not this function — see internal/ui's own
// cloud-mount dialog for the narrower "configure elsewhere, then pick
// it here" scope OAuth gets in this slice.
//
// Refuses outright if name already names a configured remote, rather
// than overwriting it: this writes to rclone's own config file, state
// breakthrough doesn't own, and a caller could easily not realize
// already has a remote by that name — the same "no silent overwrite"
// reasoning AddMountEntry already applies to breakthrough's own
// config.
func CreateStaticRemote(name, backendType string, params []string) error {
	if !Available() {
		return ErrNotAvailable
	}
	existing, err := ListRemotes()
	if err != nil {
		return err
	}
	for _, r := range existing {
		if r.Name == name {
			return fmt.Errorf("rclone: a remote named %q already exists", name)
		}
	}
	args := append([]string{"config", "create", name, backendType, "--non-interactive"}, params...)
	return runConfigCreate(args)
}

// DeleteRemote removes name from rclone's own config file entirely —
// `rclone config delete <name>`, confirmed to be a no-op, not an
// error, for a name that was never there in the first place (the same
// "absence isn't an error" contract RemoveMountEntry already follows
// for breakthrough's own side). This is a real, permanent loss of
// whatever credentials that remote held — callers (see internal/ui's
// own removeCloudMount) must only ever call this once they're certain
// nothing else still depends on the remote existing.
func DeleteRemote(name string) error {
	if !Available() {
		return ErrNotAvailable
	}
	return runConfigDelete(name)
}

// IsMounted reports whether target is currently a live mountpoint, by
// reusing internal/mounts.Read rather than re-implementing its own
// findmnt handling — confirmed by a real test mount that rclone's own
// FUSE mounts do show up there (fstype "fuse.rclone", source in
// rclone's own "remote:/path" form, no bracket notation, so never
// misread as a bind mount by Read's own isBindMountSource), the same
// way any other real mount does.
func IsMounted(target string) (bool, error) {
	entries, err := mounts.Read()
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Target == target {
			return true, nil
		}
	}
	return false, nil
}

// ObscurePassword runs `rclone obscure` on plaintext and returns the
// obscured result — needed only for TestRemote's own on-the-fly
// connection string (see its own doc comment on why): unlike `rclone
// config create`, confirmed via a real round trip through `rclone
// reveal` to auto-obscure a password-type field for you,
// rclone's own ":type,key=value:" on-the-fly syntax expects an
// already-obscured value and fails ("couldn't decrypt password")
// against a plain one — confirmed the same way, against a real
// WebDAV connection string.
func ObscurePassword(plaintext string) (string, error) {
	if !Available() {
		return "", ErrNotAvailable
	}
	return runObscure(plaintext)
}

// quoteRemoteParam wraps value in single quotes, doubling any
// embedded single quote — rclone's own on-the-fly connection-string
// escaping, confirmed against a real run (an unescaped value
// containing a literal "'" otherwise breaks parsing the same way an
// unquoted colon does, see quoteRemoteParams' own doc comment).
func quoteRemoteParam(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// quoteRemoteParams quotes every "key=value" pair's own value half
// for safe use inside an on-the-fly connection string
// (":type,key='value',...:") — never the key half, which rclone's own
// syntax never allows quoting at all. Every value is quoted
// unconditionally, not just ones that look like they need it: a
// WebDAV URL's "https://" colon is exactly the case that silently
// breaks unquoted (misparsed as the backend/path separator,
// confirmed against a real run), so guessing which values are "safe"
// to leave bare is itself the bug.
func quoteRemoteParams(params []string) []string {
	quoted := make([]string, len(params))
	for i, p := range params {
		key, value, ok := strings.Cut(p, "=")
		if !ok {
			quoted[i] = p
			continue
		}
		quoted[i] = key + "=" + quoteRemoteParam(value)
	}
	return quoted
}

// TestRemote checks whether backendType can actually be reached with
// params — rclone's own on-the-fly ":type,key=value,...:" remote
// syntax, confirmed empirically to never touch rclone's config file
// at all (no RCLONE_CONFIG file gets created even when it doesn't
// exist yet): a real connectivity check without ever persisting
// anything, for the "Test" button alongside OK/Create (see
// internal/ui's own cloud-remote dialog). Any value that needs
// obscuring per rclone's own backend definition (a true password
// field, not a plain credential like S3's secret_access_key — see
// ObscurePassword's own doc comment) must already be obscured by the
// caller before it ever reaches params; this function has no way to
// know which of an arbitrary backend's own keys that applies to.
//
// Bounded to a single retry, a connect timeout, and an I/O timeout
// (--low-level-retries 1 --retries 1 --contimeout --timeout) — this is
// a correctness check, not a real transfer: a slow or unreachable
// endpoint should fail fast, not hang for as long as rclone's own
// defaults would. Both timeout flags are set, deliberately: a real run
// against an unreachable host confirmed --timeout alone (rclone's own
// post-connect I/O stall timeout) does nothing for a host that never
// even finishes the TCP handshake in the first place — that case hung
// for rclone's own --contimeout default of a full minute until this
// was added.
func TestRemote(backendType string, params []string, timeout time.Duration) error {
	if !Available() {
		return ErrNotAvailable
	}
	connStr := ":" + backendType
	if len(params) > 0 {
		connStr += "," + strings.Join(quoteRemoteParams(params), ",")
	}
	connStr += ":"
	args := []string{
		"lsd", connStr,
		"--low-level-retries", "1",
		"--retries", "1",
		"--contimeout", timeout.String(),
		"--timeout", timeout.String(),
	}
	return runTestRemote(args)
}
