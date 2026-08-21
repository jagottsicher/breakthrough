//go:build unix && !darwin

package fsops

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
)

// ListUsers returns every local user account, parsed from /etc/passwd
// and sorted case-insensitively by name. Not available on macOS — see
// users_darwin.go's own doc comment on why.
func ListUsers() ([]SystemUser, error) {
	entries, err := parsePasswdLike("/etc/passwd")
	if err != nil {
		return nil, err
	}
	users := make([]SystemUser, len(entries))
	for i, e := range entries {
		users[i] = SystemUser{Name: e.name, UID: e.id}
	}
	return users, nil
}

// ListGroups is ListUsers' counterpart for /etc/group.
func ListGroups() ([]SystemGroup, error) {
	entries, err := parsePasswdLike("/etc/group")
	if err != nil {
		return nil, err
	}
	groups := make([]SystemGroup, len(entries))
	for i, e := range entries {
		groups[i] = SystemGroup{Name: e.name, GID: e.id}
	}
	return groups, nil
}

// passwdEntry is one parsed line of /etc/passwd or /etc/group.
type passwdEntry struct {
	name string
	id   int
}

// parsePasswdLike parses /etc/passwd or /etc/group's shared colon-
// separated line format — the name in the first field, the numeric id in
// the third (uid for /etc/passwd, gid for /etc/group; both happen to
// land at the same field index despite the different field names, since
// both formats put exactly one field — password placeholder — before
// it). A malformed line is skipped rather than failing the whole listing
// over one bad entry.
func parsePasswdLike(path string) ([]passwdEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var entries []passwdEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 3 || fields[0] == "" {
			continue
		}
		id, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}
		entries = append(entries, passwdEntry{name: fields[0], id: id})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].name) < strings.ToLower(entries[j].name)
	})
	return entries, nil
}
