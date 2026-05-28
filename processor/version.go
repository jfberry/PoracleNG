// Package processor exposes the PoracleNG processor version.
package processor

import "runtime/debug"

// Version is the PoracleNG processor version. Bump on each release.
const Version = "5.1.0"

// Commit, Branch, and Date are overridden at build time via
// -ldflags "-X github.com/pokemon/poracleng/processor.Commit=... -X ...Branch=... -X ...Date=...".
//
// Go cannot embed the git branch on its own (debug.ReadBuildInfo only exposes
// the revision, time, and dirty flag), so Branch is always injected — by
// scripts/goldflags.sh for local builds (Makefile / start.sh) and by build
// args for the Docker image, where .git is not in the build context. When
// Commit/Date are unset, BuildInfo falls back to Go's embedded VCS metadata.
var (
	Commit string
	Branch string
	Date   string
)

// BuildInfo returns the version, commit, branch, and build date for display.
//
// ldflag-injected values take precedence; the build scripts control their
// exact format (short SHA, optional "-dirty" suffix). When Commit/Date are
// unset (a bare `go build ./cmd/processor` from a git checkout), it falls back
// to Go's embedded VCS metadata. Branch has no VCS fallback and stays empty
// when not injected. Missing commit/date yield "unknown" so output is always
// well-formed.
func BuildInfo() (version, commit, branch, date string) {
	version, commit, branch, date = Version, Commit, Branch, Date

	if commit == "" || date == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			var rev, modified string
			for _, s := range info.Settings {
				switch s.Key {
				case "vcs.revision":
					rev = s.Value
				case "vcs.time":
					if date == "" {
						date = s.Value
					}
				case "vcs.modified":
					modified = s.Value
				}
			}
			if commit == "" && rev != "" {
				if len(rev) > 8 {
					rev = rev[:8]
				}
				commit = rev
				if modified == "true" {
					commit += "-dirty"
				}
			}
		}
	}

	if commit == "" {
		commit = "unknown"
	}
	if date == "" {
		date = "unknown"
	}
	return
}

// DisplayVersion returns the version with the branch suffix appended when a
// branch is known, e.g. "5.1.0-develop" (or just "5.1.0" on a tag/release
// build where no branch is injected).
func DisplayVersion() string {
	version, _, branch, _ := BuildInfo()
	if branch != "" {
		return version + "-" + branch
	}
	return version
}
