// Command gen-etc-config prints config.DefaultFileTemplate() to stdout —
// the exact same commented-out, fully-documented listing of every
// setting that Root.editConfigFile's own "Edit config file" button
// writes for a user who has no config yet (see internal/config's
// EnsureUserFile).
//
// Only ever invoked by the release pipeline (see .goreleaser.yaml's own
// before.hooks), which redirects this into a file the .deb/.rpm
// packages then ship as /etc/breakthrough/config — a real, documented
// starting point for a system-wide setting, rather than
// /etc/breakthrough not existing at all until an administrator (or
// breakthrough's own user-tier machinery) creates it by hand. Generated
// straight from the same function the running application itself uses,
// so the shipped file can never drift out of sync with the settings
// breakthrough actually recognizes — a second, hand-copied listing here
// would be exactly that risk.
package main

import (
	"fmt"

	"github.com/jagottsicher/breakthrough/internal/config"
)

func main() {
	fmt.Print(config.DefaultFileTemplate())
}
