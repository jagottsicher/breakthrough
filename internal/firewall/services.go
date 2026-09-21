package firewall

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// ServiceLookup maps a "port/protocol" key (e.g. "22/tcp") to the
// well-known service name /etc/services associates with it (e.g.
// "ssh") — the same file getent (already a Toolbox entry) itself reads
// from, so this reflects whatever services this actual install knows
// about rather than a hardcoded, eventually stale list baked into this
// package. A lookup miss is simply an empty string, never an error: an
// unnamed port is a completely ordinary, expected case, not a problem
// worth reporting.
type ServiceLookup map[string]string

// Name returns port/protocol's own service name, or "" if none is
// known. protocol empty is treated as "tcp" — the overwhelmingly common
// case for a named service, and the only sensible guess when a rule
// itself doesn't pin one down.
func (s ServiceLookup) Name(port int, protocol string) string {
	if protocol == "" {
		protocol = "tcp"
	}
	return s[fmt.Sprintf("%d/%s", port, strings.ToLower(protocol))]
}

// LoadServiceLookup reads path (in practice always "/etc/services")
// following its own documented format: "name  port/proto  [aliases...]
// [# comment]", one entry per line, blank lines and "#"-led comments
// ignored. The first name seen for a given port/proto wins, matching
// how the file itself is conventionally read (the canonical name always
// listed before any alias).
func LoadServiceLookup(path string) (ServiceLookup, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseServices(f)
}

func parseServices(r io.Reader) (ServiceLookup, error) {
	out := ServiceLookup{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name, portProto := fields[0], fields[1]
		if _, ok := out[portProto]; ok {
			continue
		}
		if slash := strings.IndexByte(portProto, '/'); slash > 0 {
			if _, err := strconv.Atoi(portProto[:slash]); err != nil {
				continue
			}
		}
		out[portProto] = name
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
