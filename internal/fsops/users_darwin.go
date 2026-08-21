//go:build darwin

package fsops

import "fmt"

// ListUsers always fails on macOS: /etc/passwd there only lists legacy
// system accounts, not real user accounts — those live in Open
// Directory, reachable only through cgo-only APIs or by shelling out to
// dscl/dscacheutil, neither of which fits this project's "no CGO unless
// necessary" architecture guideline. Callers should fall back to a plain
// text prompt on any error from this — see Root.openOwnerGroupPicker.
func ListUsers() ([]SystemUser, error) {
	return nil, fmt.Errorf("fsops: listing all users isn't supported on macOS")
}

// ListGroups always fails on macOS — see ListUsers' own doc comment on
// why.
func ListGroups() ([]SystemGroup, error) {
	return nil, fmt.Errorf("fsops: listing all groups isn't supported on macOS")
}
