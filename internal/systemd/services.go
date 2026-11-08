// Package systemd reads this host's own systemd service units via the
// real systemctl binary — the same "shell out to the real tool instead
// of reimplementing it" principle internal/mounts already follows for
// findmnt. Kept as its own package, like internal/mounts/internal/
// firewall, so the UI layer only ever renders an already assembled
// []Unit. Read-only: starting, stopping, enabling, or disabling a unit
// is deliberately out of scope for this first version (see
// feature_ideas.txt).
package systemd

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

// Unit is one systemd service unit, ready for display — Load/Active/Sub
// are systemctl's own three-state vocabulary (e.g. "loaded"/"active"/
// "running", or "not-found"/"inactive"/"dead" for a unit that doesn't
// exist), rendered verbatim rather than translated into this app's own
// categories: a sysadmin reading this screen already knows what these
// mean, and a value this package has never seen before (a future
// systemd release's own new state) still shows up as-is instead of
// being silently dropped by a switch that only knew the ones observed
// while writing it.
type Unit struct {
	Name        string
	Load        string
	Active      string
	Sub         string
	Description string
}

// unitNode mirrors one entry of "systemctl list-units ... --output=json".
type unitNode struct {
	Unit        string `json:"unit"`
	Load        string `json:"load"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Description string `json:"description"`
}

// parseUnitsJSON parses systemctl's own JSON array into []Unit.
func parseUnitsJSON(data []byte) ([]Unit, error) {
	var nodes []unitNode
	if err := json.Unmarshal(data, &nodes); err != nil {
		return nil, err
	}
	units := make([]Unit, 0, len(nodes))
	for _, n := range nodes {
		units = append(units, Unit{
			Name:        n.Unit,
			Load:        n.Load,
			Active:      n.Active,
			Sub:         n.Sub,
			Description: n.Description,
		})
	}
	return units, nil
}

// Read lists every service unit systemd knows about — "--all" so a
// unit that exists but isn't currently active (most of a typical
// system) still shows up, not just the running ones; "--type=service"
// so this never duplicates the Mounts screen's own device/automount/
// mount-unit territory (internal/mounts, a quite different kind of
// noise "--all" alone would otherwise pull in). A single call: the
// JSON already reports a failed unit's own Active as "failed", so
// there is no second "--state=failed" invocation to make and merge.
func Read() ([]Unit, error) {
	out, err := exec.Command("systemctl", "list-units", "--all", "--type=service", "--output=json").Output()
	if err != nil {
		return nil, fmt.Errorf("systemctl list-units: %w", err)
	}
	return parseUnitsJSON(out)
}
