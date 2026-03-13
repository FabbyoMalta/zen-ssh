package version

import "fmt"

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func String() string {
	return fmt.Sprintf("ZenSSH %s (commit=%s date=%s)", Version, Commit, Date)
}
