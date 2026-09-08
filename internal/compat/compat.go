// Package compat is the WordPress / PHP compatibility matrix and the policy
// that turns an operator's version choice into a runtime image. The matrix
// (matrix.json, embedded) is the single source of truth for what presso can
// run: the shipped PHP versions with their execution layer, and every
// WordPress branch with the PHP versions wordpress.org lists as supported.
// The same data drives the UI's version picker, request validation, and the
// CI matrix that pre-builds runtime images (`pressod runtime-matrix`).
package compat

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Runtime identifiers as stored on sites and reported to the UI. Inside the
// images the same split is called PRESSO_FLAVOR=frankenphp|fpm.
const (
	RuntimeFrankenPHP = "frankenphp"
	RuntimeLegacy     = "legacy"
)

// Sentinel errors returned by Catalog.Resolve; the API maps each to a code.
var (
	ErrWPUnsupported   = errors.New("unsupported WordPress version")
	ErrPHPUnsupported  = errors.New("unsupported PHP version")
	ErrPHPIncompatible = errors.New("PHP version is not compatible with this WordPress version")
	ErrLegacyDisabled  = errors.New("the legacy runtime (nginx + PHP-FPM) is disabled on this server")
)

// PHP is one shipped PHP version. Debian is the base distribution of the
// legacy (php-fpm) image; empty for FrankenPHP, whose base image decides.
type PHP struct {
	Version string `json:"version"`
	Runtime string `json:"runtime"`
	Debian  string `json:"debian,omitempty"`
}

// WordPress is one branch pinned to the release presso builds images for.
// PHP is the full list wordpress.org documents, newest first; not every
// entry is shipped (see Matrix.Compatible).
type WordPress struct {
	Branch  string   `json:"branch"`
	Version string   `json:"version"`
	PHP     []string `json:"php"`
}

// Build is the publishing policy: which of the compatible (PHP, WordPress)
// pairs CI actually builds and the engine therefore offers. Branches empty
// means every branch; NewestPHPOnly keeps just the newest offered PHP per
// branch. Widening it is a one-line change here; both CI and the engine
// follow, so the registry always holds exactly what sites can request.
type Build struct {
	Branches      []string `json:"branches"`
	NewestPHPOnly bool     `json:"newest_php_only"`
}

// Matrix is the embedded compatibility table. Both lists are newest first;
// that order is what the UI shows and what "newest compatible" means.
type Matrix struct {
	Build     Build       `json:"build"`
	PHP       []PHP       `json:"php"`
	WordPress []WordPress `json:"wordpress"`
}

//go:embed matrix.json
var raw []byte

// Default is the embedded matrix. A malformed file fails at init, never at
// request time.
var Default = mustLoad(raw)

func mustLoad(b []byte) *Matrix {
	var m Matrix
	if err := json.Unmarshal(b, &m); err != nil {
		panic("compat: matrix.json: " + err.Error())
	}
	if err := m.Validate(); err != nil {
		panic("compat: matrix.json: " + err.Error())
	}
	return &m
}

// Validate checks the structural invariants the rest of the package relies
// on: known runtimes, unique branches, pinned versions inside their branch,
// and newest-first ordering of both lists.
func (m *Matrix) Validate() error {
	if len(m.PHP) == 0 || len(m.WordPress) == 0 {
		return errors.New("php and wordpress lists must not be empty")
	}
	seen := map[string]bool{}
	for i, p := range m.PHP {
		if p.Runtime != RuntimeFrankenPHP && p.Runtime != RuntimeLegacy {
			return fmt.Errorf("php %s: unknown runtime %q", p.Version, p.Runtime)
		}
		if p.Runtime == RuntimeLegacy && p.Debian == "" {
			return fmt.Errorf("php %s: legacy entries need a debian release", p.Version)
		}
		if seen[p.Version] {
			return fmt.Errorf("php %s: listed twice", p.Version)
		}
		seen[p.Version] = true
		if i > 0 && compareVersions(m.PHP[i-1].Version, p.Version) <= 0 {
			return fmt.Errorf("php %s: list must be newest first", p.Version)
		}
	}
	branches := map[string]bool{}
	for i, w := range m.WordPress {
		if branches[w.Branch] {
			return fmt.Errorf("wordpress %s: listed twice", w.Branch)
		}
		branches[w.Branch] = true
		if Branch(w.Version) != w.Branch {
			return fmt.Errorf("wordpress %s: pinned version %s is not in the branch", w.Branch, w.Version)
		}
		if len(w.PHP) == 0 {
			return fmt.Errorf("wordpress %s: no compatible php versions", w.Branch)
		}
		for j := 1; j < len(w.PHP); j++ {
			if compareVersions(w.PHP[j-1], w.PHP[j]) <= 0 {
				return fmt.Errorf("wordpress %s: php list must be newest first", w.Branch)
			}
		}
		if i > 0 && compareVersions(m.WordPress[i-1].Branch, w.Branch) <= 0 {
			return fmt.Errorf("wordpress %s: list must be newest first", w.Branch)
		}
	}
	for _, b := range m.Build.Branches {
		if !branches[b] {
			return fmt.Errorf("build: branch %s is not in the wordpress list", b)
		}
	}
	return nil
}

// Published reports whether the build policy covers a WordPress branch.
func (m *Matrix) Published(branch string) bool {
	if len(m.Build.Branches) == 0 {
		return true
	}
	for _, b := range m.Build.Branches {
		if b == branch {
			return true
		}
	}
	return false
}

// Branch reduces a version to its major.minor branch: "6.8.8" -> "6.8",
// "7.1" -> "7.1".
func Branch(version string) string {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return version
	}
	return parts[0] + "." + parts[1]
}

// Latest is the newest WordPress branch.
func (m *Matrix) Latest() WordPress { return m.WordPress[0] }

// WordPressFor finds a branch by branch ("6.8"), pinned version ("6.8.8") or
// any version inside the branch ("6.8.3").
func (m *Matrix) WordPressFor(version string) (WordPress, bool) {
	version = strings.TrimSpace(version)
	if version == "" {
		return WordPress{}, false
	}
	b := Branch(version)
	for _, w := range m.WordPress {
		if w.Branch == b {
			return w, true
		}
	}
	return WordPress{}, false
}

// RuntimeFor reports the execution layer of a shipped PHP version.
func (m *Matrix) RuntimeFor(php string) (string, bool) {
	for _, p := range m.PHP {
		if p.Version == php {
			return p.Runtime, true
		}
	}
	return "", false
}

// Ships reports whether presso builds images for this PHP version.
func (m *Matrix) Ships(php string) bool {
	_, ok := m.RuntimeFor(php)
	return ok
}

// Compatible lists the shipped PHP versions the WordPress branch supports,
// newest first. Nil when the branch is unknown.
func (m *Matrix) Compatible(wp string) []string {
	w, ok := m.WordPressFor(wp)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(w.PHP))
	for _, p := range m.PHP {
		for _, v := range w.PHP {
			if v == p.Version {
				out = append(out, v)
				break
			}
		}
	}
	return out
}

// NewestPHP is the first entry of Compatible.
func (m *Matrix) NewestPHP(wp string) (string, bool) {
	c := m.Compatible(wp)
	if len(c) == 0 {
		return "", false
	}
	return c[0], true
}

// compareVersions orders dotted numeric versions; non-numeric segments sort
// as zero. Returns -1, 0, 1.
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}
