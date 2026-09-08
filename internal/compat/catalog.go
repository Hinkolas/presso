package compat

import (
	"errors"
	"fmt"
	"strings"
)

// Options is the server-side policy layered over the matrix.
type Options struct {
	// ImageTemplate renders a runtime image reference; {php} and {wp} are
	// required, {runtime} (frankenphp|legacy, for operators with one
	// repository per flavor) and {release} (the engine's release tag, see
	// Release) are optional.
	ImageTemplate string
	// Release fills {release} in the template. Runtime images are published
	// per engine release under the release tag, so the engine's own version
	// names the images its sites run; empty is only valid when the template
	// does not use {release} (dev builds against locally built images).
	Release string
	// LegacyEnabled offers the nginx + PHP-FPM runtime (PHP 7.4-8.1). Off, the
	// WordPress branches that need it disappear from the catalog.
	LegacyEnabled bool
	// DefaultWP is the branch or version preselected for new sites; empty
	// means the newest branch.
	DefaultWP string
	// PHPVersions restricts the shipped versions further; empty offers all.
	PHPVersions []string
}

// Catalog answers "what can this server run" and resolves requests.
type Catalog struct {
	m   *Matrix
	opt Options
}

// New validates the options against the matrix so misconfiguration fails at
// startup rather than on the first site.
func New(m *Matrix, opt Options) (*Catalog, error) {
	if m == nil {
		m = Default
	}
	if !strings.Contains(opt.ImageTemplate, "{php}") || !strings.Contains(opt.ImageTemplate, "{wp}") {
		return nil, errors.New("compat: image template must contain {php} and {wp}")
	}
	if strings.Contains(opt.ImageTemplate, "{release}") && opt.Release == "" {
		return nil, errors.New("compat: image template uses {release} but this is not a release build; set RUNTIME_IMAGE_TEMPLATE to locally built images (e.g. presso-runtime:php{php}-wp{wp})")
	}
	for _, v := range opt.PHPVersions {
		if !m.Ships(v) {
			return nil, fmt.Errorf("compat: PHP %s is not a shipped version (have %s)", v, strings.Join(m.phpVersions(), ", "))
		}
	}
	if opt.DefaultWP != "" {
		if _, ok := m.WordPressFor(opt.DefaultWP); !ok {
			return nil, fmt.Errorf("compat: default WordPress %q is not in the matrix", opt.DefaultWP)
		}
	}
	c := &Catalog{m: m, opt: opt}
	if _, err := c.Resolve("", ""); err != nil {
		return nil, fmt.Errorf("compat: default WordPress %q cannot run on this server: %w; adjust RUNTIME_PHP_VERSIONS, RUNTIME_DEFAULT_WP, or the legacy switch to select a published pair", c.defaultBranch().Branch, err)
	}
	return c, nil
}

// Matrix exposes the underlying table.
func (c *Catalog) Matrix() *Matrix { return c.m }

// LegacyEnabled reports the server policy.
func (c *Catalog) LegacyEnabled() bool { return c.opt.LegacyEnabled }

func (m *Matrix) phpVersions() []string {
	out := make([]string, 0, len(m.PHP))
	for _, p := range m.PHP {
		out = append(out, p.Version)
	}
	return out
}

func (c *Catalog) defaultBranch() WordPress {
	if w, ok := c.m.WordPressFor(c.opt.DefaultWP); ok {
		return w
	}
	return c.m.Latest()
}

// allowed applies the optional allowlist to a shipped version.
func (c *Catalog) allowed(php string) bool {
	if len(c.opt.PHPVersions) == 0 {
		return true
	}
	for _, v := range c.opt.PHPVersions {
		if v == php {
			return true
		}
	}
	return false
}

// Offered lists the PHP versions this server offers for a WordPress branch:
// compatible, shipped, allowlisted, published by the build policy, and
// (unless legacy is enabled) on the FrankenPHP runtime. Newest first.
func (c *Catalog) Offered(wp string) []string {
	var out []string
	for _, v := range c.published(wp) {
		if rt, _ := c.m.RuntimeFor(v); rt == RuntimeLegacy && !c.opt.LegacyEnabled {
			continue
		}
		out = append(out, v)
	}
	return out
}

// published is Offered before the legacy switch: what CI built for the branch
// under this server's allowlist. Empty for unpublished branches.
func (c *Catalog) published(wp string) []string {
	w, ok := c.m.WordPressFor(wp)
	if !ok || !c.m.Published(w.Branch) {
		return nil
	}
	var out []string
	versions := c.m.Compatible(w.Branch)
	if c.m.Build.NewestPHPOnly && len(versions) > 0 {
		versions = versions[:1]
	}
	for _, v := range versions {
		if !c.allowed(v) {
			continue
		}
		out = append(out, v)
	}
	return out
}

// Resolution is a fully resolved site runtime choice.
type Resolution struct {
	WPBranch  string
	WPVersion string
	PHP       string
	Runtime   string
	Image     string
}

// Resolve turns an operator's request into a Resolution. Empty wp means the
// default branch; empty php means the newest PHP offered for that branch.
// Errors, in order of precedence: unknown or unpublished WordPress, unknown
// PHP, PHP not compatible with the branch, legacy runtime disabled, PHP
// compatible but not built for the branch.
func (c *Catalog) Resolve(wp, php string) (Resolution, error) {
	var w WordPress
	if strings.TrimSpace(wp) == "" {
		w = c.defaultBranch()
	} else {
		var ok bool
		if w, ok = c.m.WordPressFor(wp); !ok {
			return Resolution{}, fmt.Errorf("%w: WordPress %s (supported: %s to %s)", ErrWPUnsupported, wp, c.m.WordPress[len(c.m.WordPress)-1].Branch, c.m.Latest().Branch)
		}
	}
	if !c.m.Published(w.Branch) {
		return Resolution{}, fmt.Errorf("%w: WordPress %s has no published runtime image (available: %s)", ErrWPUnsupported, w.Branch, strings.Join(c.publishedBranches(), ", "))
	}
	compatible := c.m.Compatible(w.Branch)
	published := c.published(w.Branch)
	php = strings.TrimSpace(php)
	if php == "" {
		if len(published) == 0 {
			return Resolution{}, fmt.Errorf("%w: no PHP version offered for WordPress %s", ErrPHPUnsupported, w.Branch)
		}
		php = published[0]
	}
	rt, ok := c.m.RuntimeFor(php)
	if !ok || !c.allowed(php) {
		return Resolution{}, fmt.Errorf("%w: PHP %s (offered: %s)", ErrPHPUnsupported, php, strings.Join(c.shippedAllowed(), ", "))
	}
	if !contains(compatible, php) {
		return Resolution{}, fmt.Errorf("%w: WordPress %s runs on PHP %s", ErrPHPIncompatible, w.Branch, strings.Join(compatible, ", "))
	}
	if rt == RuntimeLegacy && !c.opt.LegacyEnabled {
		return Resolution{}, fmt.Errorf("%w: PHP %s needs it", ErrLegacyDisabled, php)
	}
	if !contains(published, php) {
		return Resolution{}, fmt.Errorf("%w: no runtime image is built for WordPress %s on PHP %s (built: %s)", ErrPHPUnsupported, w.Branch, php, strings.Join(published, ", "))
	}
	return Resolution{WPBranch: w.Branch, WPVersion: w.Version, PHP: php, Runtime: rt, Image: c.Image(php, w.Branch)}, nil
}

func (c *Catalog) publishedBranches() []string {
	var out []string
	for _, w := range c.m.WordPress {
		if c.m.Published(w.Branch) {
			out = append(out, w.Branch)
		}
	}
	return out
}

// Image renders the image reference for a PHP version and a WordPress branch
// or version. The tag always carries the branch's pinned release, so a site
// detected on an older patch moves to the pinned one when recreated.
func (c *Catalog) Image(php, wp string) string {
	version := wp
	if w, ok := c.m.WordPressFor(wp); ok {
		version = w.Version
	}
	rt, _ := c.m.RuntimeFor(php)
	r := strings.NewReplacer("{php}", php, "{wp}", version, "{runtime}", rt, "{release}", c.opt.Release)
	return r.Replace(c.opt.ImageTemplate)
}

func (c *Catalog) shippedAllowed() []string {
	var out []string
	for _, p := range c.m.PHP {
		if c.allowed(p.Version) {
			out = append(out, p.Version)
		}
	}
	return out
}

// --- capability payload (GET /v1/system/runtime) ---

type RuntimeInfo struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	PHPVersions []string `json:"php_versions"`
	Enabled     bool     `json:"enabled"`
}

type WordPressInfo struct {
	Branch      string   `json:"branch"`
	Version     string   `json:"version"`
	Status      string   `json:"status"` // latest|outdated
	PHPVersions []string `json:"php_versions"`
	DefaultPHP  string   `json:"default_php"`
	Runtime     string   `json:"runtime"`
}

// Capabilities is what the UI needs to drive the version picker.
type Capabilities struct {
	Runtimes      []RuntimeInfo   `json:"runtimes"`
	WordPress     []WordPressInfo `json:"wordpress"`
	DefaultWP     string          `json:"default_wp"`
	LegacyEnabled bool            `json:"legacy_enabled"`
}

var runtimeText = map[string][2]string{
	RuntimeFrankenPHP: {"FrankenPHP", "Caddy + PHP in one process. PHP 8.2 and newer."},
	RuntimeLegacy:     {"Legacy · nginx + PHP-FPM", "For WordPress releases that need PHP 7.4 to 8.1."},
}

// Capabilities renders the server's offer. Both runtimes are always listed
// (a disabled legacy keeps its PHP versions so the UI can explain why they
// are missing). Published WordPress branches are always listed too: one the
// legacy switch has taken away comes with an empty php_versions list, so the
// UI can show it greyed out with the reason. Unpublished branches are
// omitted.
func (c *Catalog) Capabilities() Capabilities {
	out := Capabilities{LegacyEnabled: c.opt.LegacyEnabled, DefaultWP: c.defaultBranch().Branch}
	for _, id := range []string{RuntimeFrankenPHP, RuntimeLegacy} {
		info := RuntimeInfo{ID: id, Label: runtimeText[id][0], Description: runtimeText[id][1], Enabled: id == RuntimeFrankenPHP || c.opt.LegacyEnabled, PHPVersions: []string{}}
		for _, p := range c.m.PHP {
			if p.Runtime == id && c.allowed(p.Version) {
				info.PHPVersions = append(info.PHPVersions, p.Version)
			}
		}
		out.Runtimes = append(out.Runtimes, info)
	}
	out.WordPress = []WordPressInfo{}
	for i, w := range c.m.WordPress {
		published := c.published(w.Branch)
		if len(published) == 0 {
			continue
		}
		offered := c.Offered(w.Branch)
		info := WordPressInfo{Branch: w.Branch, Version: w.Version, Status: "outdated", PHPVersions: []string{}}
		if i == 0 {
			info.Status = "latest"
		}
		if len(offered) > 0 {
			info.PHPVersions, info.DefaultPHP = offered, offered[0]
			info.Runtime, _ = c.m.RuntimeFor(offered[0])
		} else {
			// Every built PHP for this branch is legacy and legacy is off.
			info.Runtime = RuntimeLegacy
		}
		out.WordPress = append(out.WordPress, info)
	}
	return out
}

// --- CI build matrix (pressod runtime-matrix) ---

// Pair is one image to build: a PHP version times a WordPress release.
type Pair struct {
	PHP     string `json:"php"`
	WP      string `json:"wp"`
	Branch  string `json:"branch"`
	Runtime string `json:"runtime"`
	Tag     string `json:"tag"`
}

// Pairs lists every image the server could request under its policy. Tags
// are the template's tag part with the default template, e.g. php8.4-wp6.8.8.
func (c *Catalog) Pairs() []Pair {
	var out []Pair
	for _, w := range c.m.WordPress {
		for _, php := range c.Offered(w.Branch) {
			rt, _ := c.m.RuntimeFor(php)
			out = append(out, Pair{PHP: php, WP: w.Version, Branch: w.Branch, Runtime: rt, Tag: "php" + php + "-wp" + w.Version})
		}
	}
	return out
}

// Base is one base image to build: PHP plus execution layer, no WordPress.
type Base struct {
	PHP        string `json:"php"`
	Runtime    string `json:"runtime"`
	Debian     string `json:"debian"`
	Dockerfile string `json:"dockerfile"`
}

// Bases lists the base images the pairs build on: one per PHP version that
// at least one published pair uses.
func (c *Catalog) Bases() []Base {
	used := map[string]bool{}
	for _, p := range c.Pairs() {
		used[p.PHP] = true
	}
	var out []Base
	for _, p := range c.m.PHP {
		if !used[p.Version] {
			continue
		}
		df := "runtime/base.Dockerfile"
		if p.Runtime == RuntimeLegacy {
			df = "runtime/base-legacy.Dockerfile"
		}
		out = append(out, Base{PHP: p.Version, Runtime: p.Runtime, Debian: p.Debian, Dockerfile: df})
	}
	return out
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
