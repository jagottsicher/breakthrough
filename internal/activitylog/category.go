package activitylog

// Category groups log entries by which subsystem an action belongs
// to — independent of Level (see its own doc comment): the user's own
// explicit request was to be able to turn "the individual things" off
// within whatever level is otherwise configured, not just move a
// single dial up or down. Each one mirrors an existing internal
// package/feature boundary this app already has, rather than a new
// grouping invented just for logging.
type Category string

const (
	// CategoryFileOps covers Copy/Cut/Paste, Rename, Trash/Remove/
	// Restore, Multiply, and New file/New dir — internal/fsops' own
	// territory.
	CategoryFileOps Category = "fileops"
	// CategoryPermissions covers chmod/chown.
	CategoryPermissions Category = "permissions"
	// CategoryArchive covers Compress/Extract — internal/archive and
	// this app's own compress.go.
	CategoryArchive Category = "archive"
	// CategoryTextOps covers Sed Replace and Batch Rename —
	// internal/replace and internal/batchrename.
	CategoryTextOps Category = "textops"
	// CategoryRsync covers a Rsync run, foreground or backgrounded.
	CategoryRsync Category = "rsync"
	// CategoryRemote covers connecting/disconnecting an SFTP
	// connection and any remote file transfer — internal/remotefs.
	CategoryRemote Category = "remote"
	// CategoryShell covers the embedded bash line, "Open with…", and
	// Edit — anything that hands a path to an external program.
	CategoryShell Category = "shell"
)

// Categories is every category this package defines, in the fixed
// order the Options screen lists them — for anything that needs to
// enumerate them (the Options catalog, a test) without hand-
// maintaining a second copy of this list.
func Categories() []Category {
	return []Category{
		CategoryFileOps,
		CategoryPermissions,
		CategoryArchive,
		CategoryTextOps,
		CategoryRsync,
		CategoryRemote,
		CategoryShell,
	}
}

// Label is a category's own human-readable name, for the Options
// screen.
func (c Category) Label() string {
	switch c {
	case CategoryFileOps:
		return "File operations (copy, move, rename, trash, remove, multiply, new file/dir)"
	case CategoryPermissions:
		return "Permission changes (chmod, chown)"
	case CategoryArchive:
		return "Archives (compress, extract)"
	case CategoryTextOps:
		return "Sed Replace and Batch Rename"
	case CategoryRsync:
		return "Rsync"
	case CategoryRemote:
		return "Remote connections and transfers (SFTP)"
	case CategoryShell:
		return "Shell (bash line, Open with…, Edit)"
	default:
		return string(c)
	}
}
