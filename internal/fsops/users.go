//go:build unix

package fsops

// SystemUser is one local user account, as ListUsers reports it.
type SystemUser struct {
	Name string
	UID  int
}

// SystemGroup is ListGroups' own counterpart.
type SystemGroup struct {
	Name string
	GID  int
}
