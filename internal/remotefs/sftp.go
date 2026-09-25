package remotefs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"github.com/jagottsicher/breakthrough/internal/fsops"
)

// dialDefaultTimeout bounds both the raw TCP connect and the whole
// SSH handshake (see ssh.ClientConfig's own Timeout field) when a
// caller doesn't set DialOptions.Timeout — long enough for a slow
// but working link, short enough that a silently dropping firewall
// doesn't hang the connection dialog indefinitely.
const dialDefaultTimeout = 10 * time.Second

// DialOptions is everything Dial needs: which endpoint (Connection),
// how to authenticate and verify its host key (AuthOptions,
// HostKeyPrompt), and how long to wait.
type DialOptions struct {
	Connection

	Auth AuthOptions

	// KnownHostsFile overrides where host keys are read from/appended
	// to; "" means DefaultKnownHostsFile().
	KnownHostsFile string
	HostKeyPrompt  HostKeyPrompt

	// Timeout bounds the connect+handshake; 0 means
	// dialDefaultTimeout.
	Timeout time.Duration
}

// SFTPClient is the Client implementation for a single SFTP session —
// one TCP connection, one SSH handshake, one SFTP subsystem channel
// on top of it. See Dial.
type SFTPClient struct {
	ssh        *ssh.Client
	sftp       *sftp.Client
	root       string
	authMethod AuthMethod
}

var _ Client = (*SFTPClient)(nil)

// AuthMethod reports which of the two broad kinds of method (see
// AuthMethod's own doc comment) actually authenticated this session —
// determined once, at Dial time, from whether authMethods' own
// password callback was ever reached (see Dial's own passwordUsed
// local for the full reasoning).
func (c *SFTPClient) AuthMethod() AuthMethod {
	return c.authMethod
}

// Dial authenticates to opts's endpoint and starts an SFTP session
// against it. ctx bounds the whole attempt, including a handshake
// that never completes at all (a host that accepts the TCP connection
// but then goes silent) — not just the initial TCP connect, which
// opts.Timeout alone would already cover.
func Dial(ctx context.Context, opts DialOptions) (*SFTPClient, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = dialDefaultTimeout
	}

	knownHosts := opts.KnownHostsFile
	if knownHosts == "" {
		var err error
		knownHosts, err = DefaultKnownHostsFile()
		if err != nil {
			return nil, fmt.Errorf("locating known_hosts: %w", err)
		}
	}
	hostKeyCB, err := hostKeyCallback(knownHosts, opts.HostKeyPrompt)
	if err != nil {
		return nil, err
	}

	var passwordUsed bool
	methods := authMethods(opts.Auth, &passwordUsed)
	if len(methods) == 0 {
		return nil, errors.New("no authentication method available: no running agent, no default private key, no password supplied")
	}

	config := &ssh.ClientConfig{
		User:            opts.User,
		Auth:            methods,
		HostKeyCallback: hostKeyCB,
		Timeout:         timeout,
	}

	sshClient, err := dialSSHContext(ctx, opts.Addr(), config)
	if err != nil {
		return nil, err
	}

	// UseConcurrentWrites: pkg/sftp's own default is off ("write
	// concurrency is... error prone", per its own doc comment) — plain
	// sequential Write calls wait for the server's own ack before
	// sending the next packet, which is fine over a slow link where
	// bandwidth is the real limit, but on a fast local network the
	// per-packet round trip itself becomes the bottleneck: a real,
	// live-reported case of copying between two machines on the same
	// LAN feeling far slower than the link itself could ever explain.
	// internal/ui's own pasteTransferFile takes on the one real risk
	// this trades in return (a failed transfer can otherwise leave a
	// "hole" — a later chunk landing before an earlier one that then
	// fails — instead of a cleanly truncated file) by truncating the
	// destination to empty on any copy error, so a failure still looks
	// unmistakably incomplete rather than silently corrupt.
	sftpClient, err := sftp.NewClient(sshClient, sftp.UseConcurrentWrites(true))
	if err != nil {
		_ = sshClient.Close()
		return nil, fmt.Errorf("starting sftp session: %w", err)
	}

	root, err := sftpClient.Getwd()
	if err != nil || root == "" {
		root = "/" // best-effort fallback — every SFTP server has a "/"
	}

	authMethod := AuthMethodKeyOrAgent
	if passwordUsed {
		authMethod = AuthMethodPassword
	}
	return &SFTPClient{ssh: sshClient, sftp: sftpClient, root: root, authMethod: authMethod}, nil
}

// dialSSHContext races the TCP connect + SSH handshake against ctx —
// neither ssh.Dial nor ssh.NewClientConn take a context natively, so
// cancellation is applied by hand: closing conn unblocks
// NewClientConn's own blocking read/write the moment ctx is done,
// exactly the same "closing the underlying connection is what actually
// interrupts an in-flight handshake" mechanism a context-aware
// database/sql driver relies on internally.
func dialSSHContext(ctx context.Context, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, &ConnectionRefusedError{Addr: addr, Err: err}
	}

	type result struct {
		client *ssh.Client
		err    error
	}
	done := make(chan result, 1)
	go func() {
		c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
		if err != nil {
			done <- result{nil, err}
			return
		}
		done <- result{ssh.NewClient(c, chans, reqs), nil}
	}()

	select {
	case <-ctx.Done():
		_ = conn.Close()
		return nil, ctx.Err()
	case r := <-done:
		return r.client, r.err
	}
}

func (c *SFTPClient) Root() string { return c.root }

// ListDir sorts its own result — directories before files, then
// case-insensitive name — before returning, the exact same order
// fsops.ListDir's own local implementation already sorts by. This
// isn't just cosmetic: Panel.applySortPreference (internal/ui/panel.go)
// assumes its own input already arrives grouped this way and only
// re-sorts *within* each group for every sort mode other than plain
// Name — an unsorted remote listing broke that precondition entirely,
// interleaving directories and files at random (a real, user-reported
// bug) rather than merely sorting each of the two groups differently.
func (c *SFTPClient) ListDir(dir string) ([]fsops.Entry, error) {
	infos, err := c.sftp.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]fsops.Entry, 0, len(infos))
	for _, fi := range infos {
		entries = append(entries, c.adaptLstatEntry(path.Join(dir, fi.Name()), fi))
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir // directories before files
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

func (c *SFTPClient) Stat(p string) (fsops.Entry, error) {
	fi, err := c.sftp.Stat(p) // follows symlinks, same contract as fsops.Stat
	if err != nil {
		return fsops.Entry{}, err
	}
	return adaptResolvedEntry(fi), nil
}

func (c *SFTPClient) Lstat(p string) (fsops.Entry, error) {
	fi, err := c.sftp.Lstat(p) // never follows a symlink, same contract as fsops.Info's own Lstat basis
	if err != nil {
		return fsops.Entry{}, err
	}
	return c.adaptLstatEntry(p, fi), nil
}

func (c *SFTPClient) Open(p string) (io.ReadCloser, error)    { return c.sftp.Open(p) }
func (c *SFTPClient) Create(p string) (io.WriteCloser, error) { return c.sftp.Create(p) }
func (c *SFTPClient) Mkdir(p string) error                    { return c.sftp.Mkdir(p) }
func (c *SFTPClient) Remove(p string) error                   { return c.sftp.Remove(p) }
func (c *SFTPClient) RemoveDirectory(p string) error          { return c.sftp.RemoveDirectory(p) }
func (c *SFTPClient) Rename(oldPath, newPath string) error    { return c.sftp.Rename(oldPath, newPath) }
func (c *SFTPClient) Chmod(p string, mode os.FileMode) error  { return c.sftp.Chmod(p, mode) }

// DiskUsage reports path's own filesystem block/inode usage via the
// statvfs@openssh.com SFTP extension — supported by every OpenSSH
// sftp-server, the de facto standard remote endpoint this project
// targets (see its own package doc comment). Percentages are computed
// against used+avail, not the raw block/inode total: some blocks/
// inodes are always reserved for root and excluded from that base,
// exactly the same convention fsops.FetchDiskUsage's own local percent
// already follows (parsed there straight from df's own printed
// column; computed here instead, since StatVFS returns raw counts,
// not a percentage of its own).
func (c *SFTPClient) DiskUsage(p string) (fsops.DiskUsage, error) {
	v, err := c.sftp.StatVFS(p)
	if err != nil {
		return fsops.DiskUsage{}, err
	}
	return diskUsageFromStatVFS(v), nil
}

// diskUsageFromStatVFS converts one raw StatVFS response into
// fsops.DiskUsage — split out from DiskUsage itself specifically so a
// test can exercise the actual unit conversion/percentage math
// directly against hand-built values, without needing a real SFTP
// server that speaks the statvfs@openssh.com extension at all (the
// project's own hermetic test server, built on pkg/sftp's simple
// *sftp.Server, doesn't implement it — only the heavier, handler-based
// request server does).
func diskUsageFromStatVFS(v *sftp.StatVFS) fsops.DiskUsage {
	usedBytes := int64(v.Frsize * (v.Blocks - v.Bfree))
	availBytes := int64(v.Frsize * v.Bavail)
	usedInodes := int64(v.Files - v.Ffree)
	availInodes := int64(v.Favail)
	return fsops.DiskUsage{
		UsedBytes:    usedBytes,
		AvailBytes:   availBytes,
		UsedInodes:   usedInodes,
		AvailInodes:  availInodes,
		UsePercent:   percentOfCounts(usedBytes, usedBytes+availBytes),
		InodePercent: percentOfCounts(usedInodes, usedInodes+availInodes),
	}
}

// percentOfCounts is part as a percentage of total, 0 for a zero or
// negative total rather than dividing by it — the same defensive
// "degenerate input, not a real filesystem, never panic over it"
// shape internal/ui's own percentOf already uses for the identical
// local Memory/Swap/Disk figures.
func percentOfCounts(part, total int64) int {
	if total <= 0 {
		return 0
	}
	return int(part * 100 / total)
}

func (c *SFTPClient) Close() error {
	sftpErr := c.sftp.Close()
	sshErr := c.ssh.Close()
	if sftpErr != nil {
		return sftpErr
	}
	return sshErr
}

// adaptLstatEntry builds an fsops.Entry from one ReadDir/Lstat-style
// os.FileInfo — fullPath is that entry's own complete remote path
// (dir joined with its name), needed to resolve a symlink's own
// target type via a second round trip, the same "Lstat then, for a
// symlink, also resolve the target" shape fsops.ListDir's own local
// describeEntry already follows.
func (c *SFTPClient) adaptLstatEntry(fullPath string, fi os.FileInfo) fsops.Entry {
	entry := fsops.Entry{
		Name:    fi.Name(),
		Mode:    fi.Mode(),
		Size:    fi.Size(),
		ModTime: fi.ModTime(),
	}

	if fi.Mode()&os.ModeSymlink == 0 {
		entry.Type = entryTypeFromMode(fi.Mode())
		entry.IsDir = entry.Type == fsops.TypeDir
		return entry
	}

	if target, err := c.sftp.ReadLink(fullPath); err == nil {
		entry.LinkTarget = target
	}
	resolved, err := c.sftp.Stat(fullPath)
	switch {
	case err != nil:
		entry.Type = fsops.TypeSymlinkBroken
	case resolved.IsDir():
		entry.Type = fsops.TypeSymlinkDir
		entry.IsDir = true
	default:
		entry.Type = fsops.TypeSymlinkFile
	}
	return entry
}

// adaptResolvedEntry builds an fsops.Entry from an already
// symlink-resolved os.FileInfo (Client.Stat's own contract) — never a
// symlink Type itself, since Stat never reports one.
func adaptResolvedEntry(fi os.FileInfo) fsops.Entry {
	entry := fsops.Entry{
		Name:    fi.Name(),
		Mode:    fi.Mode(),
		Size:    fi.Size(),
		ModTime: fi.ModTime(),
	}
	entry.Type = entryTypeFromMode(fi.Mode())
	entry.IsDir = entry.Type == fsops.TypeDir
	return entry
}

// entryTypeFromMode maps a resolved (non-symlink) os.FileMode to the
// fsops.EntryType it corresponds to — checked in this specific order
// because a character device's mode carries *both* ModeDevice and
// ModeCharDevice, per os.FileMode's own documented convention; a plain
// block device carries ModeDevice alone.
func entryTypeFromMode(mode os.FileMode) fsops.EntryType {
	switch {
	case mode&os.ModeDir != 0:
		return fsops.TypeDir
	case mode&os.ModeSocket != 0:
		return fsops.TypeSocket
	case mode&os.ModeNamedPipe != 0:
		return fsops.TypeFIFO
	case mode&os.ModeCharDevice != 0:
		return fsops.TypeCharDevice
	case mode&os.ModeDevice != 0:
		return fsops.TypeBlockDevice
	default:
		return fsops.TypeFile
	}
}
