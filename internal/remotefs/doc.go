// Package remotefs connects a panel to a directory tree that doesn't
// live on the local machine — an SSH/SFTP account on another host,
// browsed and manipulated the same way a local directory already is.
//
// Client deliberately mirrors the shape internal/fsops's own local
// operations already have (ListDir, Stat, Rename, ...), reusing
// fsops.Entry itself rather than a parallel remote-only row type: the
// panel/properties/viewer code that already renders an fsops.Entry
// doesn't need to know or care whether it came from a local Lstat or a
// remote SFTP round trip. Fields a wire protocol simply has no
// equivalent for (Nlink, MountPoint, the real access(2)-based
// Unreadable check) are left at their zero value for a remote entry —
// a real permission problem still surfaces as an error the moment
// something actually tries to open or list that entry, exactly as it
// would locally.
//
// Only SFTP is implemented so far (see sftp.go) — plain SSH gives a
// pure-Go, no-CGO transport with no separate daemon or mount step, the
// protocol this project's own sysadmin audience already reaches for
// first. FTP is a natural second Client behind the same interface;
// SMB/CIFS and NFS are deliberately not attempted here at all — both
// are, in practice, kernel/OS mount facilities rather than something a
// small in-process wire-protocol client can responsibly replace, and
// belong to a later, separate feature that wraps the real `mount`
// tooling instead (this project's own established preference for real
// POSIX tools over reimplementing them, see internal/fsops's own
// du/df/grep integrations).
package remotefs
