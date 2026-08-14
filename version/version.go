// Package version exposes build metadata about the compiled binary.
//
// The exported variables are intentionally left empty at compile time and are
// populated via -ldflags -X by scripts/build.sh. When the binary is built with
// `go build` directly (no ldflags), they report "unknown".
package version

// unknown is the placeholder reported when build metadata was not injected.
const unknown = "unknown"

// Build metadata, injected at link time by scripts/build.sh.
var (
	// Version is the release this binary was cut from — the git tag for a
	// published release, `git describe` for a local build. It is what a bug
	// report cites and what the GitHub Action pins, neither of which wants a
	// commit hash.
	Version = unknown
	// CommitHash is the git commit the binary was built from.
	CommitHash = unknown
	// BuildTime is the UTC timestamp the binary was built at.
	BuildTime = unknown
	// CommitTime is the committer timestamp of CommitHash.
	CommitTime = unknown
)
