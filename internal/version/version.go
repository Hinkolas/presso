// Package version carries the build version stamped at link time. It lives in
// its own package (not cmd/) so internals can report it without importing a
// main package.
package version

import (
	"regexp"
	"strings"
)

// Version is overridden by the release build:
//
//	go build -ldflags "-X github.com/Hinkolas/presso/internal/version.Version=v0.1.0"
var Version = "v0.0.0-dev"

// ReleasePattern is the publication policy, shared by the engine, tag task,
// and release workflow through cmd/release-version. The standalone installer's
// copy is checked by scripts/check-release-gates.rb. Only dotted alpha/beta/rc
// suffixes are published; no leading zeros, build metadata or git-describe tags.
const ReleasePattern = `^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(alpha|beta|rc)\.(0|[1-9][0-9]*))?$`

var releaseRe = regexp.MustCompile(ReleasePattern)

// Release returns Version when this binary is a release build, "" otherwise.
// Release builds resolve {release} in image templates and may self-update;
// dev builds do neither, so a checkout never pulls images that do not exist
// or overwrites a `go run` temp binary.
func Release() string { return ReleaseOf(Version) }

// ReleaseOf is Release for an arbitrary version string (tests, update checks).
func ReleaseOf(v string) string {
	if releaseRe.MatchString(v) {
		return v
	}
	return ""
}

// IsPrerelease distinguishes suffix releases from normal releases, including
// normal 0.x releases during initial development. Invalid versions return false.
func IsPrerelease(v string) bool {
	return ReleaseOf(v) != "" && strings.Contains(v, "-")
}
