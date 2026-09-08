// Package version carries the build version stamped at link time. It lives in
// its own package (not cmd/) so internals can report it without importing a
// main package.
package version

import "regexp"

// Version is overridden by the release build:
//
//	go build -ldflags "-X github.com/Hinkolas/presso/internal/version.Version=v0.1.0"
var Version = "v0.0.0-dev"

// releaseRe accepts clean release tags (v1.2.3, v1.2.3-rc.1) and rejects
// what `git describe` produces between tags (v1.2.3-4-gabc1234, -dirty) and
// the dev default.
var releaseRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(-(rc|beta|alpha)\.?\d+)?$`)

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
