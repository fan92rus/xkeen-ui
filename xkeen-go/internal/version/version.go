// Package version provides build version information.
package version

var (
	// Version is set via ldflags during build
	Version = "dev"
	// BuildDate is set via ldflags during build
	BuildDate = "unknown"
	// GitCommit is set via ldflags during build
	GitCommit = "unknown"
	// BuildBranch is set via ldflags during build
	BuildBranch = "master"
)

// SetVersion initializes version info from main.
func SetVersion(v, bd, gc string) {
	Version = v
	BuildDate = bd
	GitCommit = gc
}

// GetVersion returns the current version string.
func GetVersion() string {
	return Version
}

// GetBuildDate returns the build date.
func GetBuildDate() string {
	return BuildDate
}

// GetGitCommit returns the git commit hash.
func GetGitCommit() string {
	return GitCommit
}

// GetBuildBranch returns the git branch the binary was built from.
func GetBuildBranch() string {
	return BuildBranch
}
