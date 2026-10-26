package compat

import (
	"errors"
	"strings"
	"testing"
)

const tmpl = "presso-runtime:php{php}-wp{wp}"

// full is the matrix without the build policy: every compatible pair.
var full = func() *Matrix {
	m := *Default
	m.Build = Build{}
	return &m
}()

func mustCatalog(t *testing.T, opt Options) *Catalog {
	t.Helper()
	return mustCatalogOn(t, Default, opt)
}

func mustCatalogOn(t *testing.T, m *Matrix, opt Options) *Catalog {
	t.Helper()
	if opt.ImageTemplate == "" {
		opt.ImageTemplate = tmpl
	}
	c, err := New(m, opt)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMatrixShape(t *testing.T) {
	if err := Default.Validate(); err != nil {
		t.Fatal(err)
	}
	if Default.Latest().Branch != "7.1" || len(Default.WordPress) != 16 || len(Default.PHP) != 7 {
		t.Fatalf("matrix: latest %s, %d branches, %d php", Default.Latest().Branch, len(Default.WordPress), len(Default.PHP))
	}
	if !Default.Published("6.9") || Default.Published("6.8") || !full.Published("6.8") {
		t.Fatal("Published")
	}
	badBuild := *Default
	badBuild.Build = Build{Branches: []string{"4.9"}}
	if err := badBuild.Validate(); err == nil {
		t.Fatal("unknown build branch accepted")
	}
	if Branch("6.8.8") != "6.8" || Branch("7.1") != "7.1" || Branch("7") != "7" {
		t.Fatal("Branch")
	}
	if w, ok := Default.WordPressFor("6.8.3"); !ok || w.Version != "6.8.8" {
		t.Fatalf("WordPressFor by patch: %+v %v", w, ok)
	}
	if _, ok := Default.WordPressFor("4.9"); ok {
		t.Fatal("4.9 should be unknown")
	}
	bad := *Default
	bad.WordPress = append([]WordPress{{Branch: "6.8", Version: "6.9.1", PHP: []string{"8.4"}}}, bad.WordPress...)
	if err := bad.Validate(); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestPatchPins(t *testing.T) {
	// Lowest releases that fix CVE-2026-109618 and CVE-2026-109619. A pin may
	// move forward, never back below these.
	floor := map[string]string{"8.5": "8.5.12", "8.4": "8.4.27", "8.3": "8.3.36", "8.2": "8.2.35"}
	for _, p := range Default.PHP {
		if min, ok := floor[p.Version]; ok && compareVersions(p.Patch, min) < 0 {
			t.Errorf("php %s pinned to %s, below the security floor %s", p.Version, p.Patch, min)
		}
	}
	for _, patch := range []string{"", "8.5", "8.5.", "8.4.27", "8.5.12-rc1", "8.5.x"} {
		bad := *Default
		bad.PHP = append([]PHP(nil), Default.PHP...)
		bad.PHP[0].Patch = patch
		if err := bad.Validate(); err == nil {
			t.Errorf("patch %q accepted for php %s", patch, bad.PHP[0].Version)
		}
	}
}

func TestNewestPHP(t *testing.T) {
	cases := map[string]string{"7.1": "8.5", "6.9": "8.5", "6.8": "8.4", "6.6": "8.3", "6.3": "8.2", "6.1": "8.2", "6.0": "8.1", "5.9": "8.1", "5.6": "8.0"}
	for wp, want := range cases {
		if got, ok := Default.NewestPHP(wp); !ok || got != want {
			t.Errorf("NewestPHP(%s) = %s, want %s", wp, got, want)
		}
	}
	if c := Default.Compatible("6.9"); strings.Join(c, ",") != "8.5,8.4,8.3,8.2,8.1,8.0,7.4" {
		t.Fatalf("Compatible(6.9) = %v (7.3/7.2 are not shipped)", c)
	}
	if rt, _ := Default.RuntimeFor("8.1"); rt != RuntimeLegacy {
		t.Fatal("8.1 is legacy")
	}
	if rt, _ := Default.RuntimeFor("8.2"); rt != RuntimeFrankenPHP {
		t.Fatal("8.2 is frankenphp")
	}
}

func TestResolve(t *testing.T) {
	on := mustCatalogOn(t, full, Options{LegacyEnabled: true})
	off := mustCatalogOn(t, full, Options{LegacyEnabled: false})

	res, err := on.Resolve("", "")
	if err != nil || res.WPBranch != "7.1" || res.PHP != "8.5" || res.Runtime != RuntimeFrankenPHP || res.Image != "presso-runtime:php8.5-wp7.1" {
		t.Fatalf("default: %+v %v", res, err)
	}
	res, err = on.Resolve("6.0", "")
	if err != nil || res.WPVersion != "6.0.14" || res.PHP != "8.1" || res.Runtime != RuntimeLegacy || res.Image != "presso-runtime:php8.1-wp6.0.14" {
		t.Fatalf("6.0: %+v %v", res, err)
	}
	if res, err = on.Resolve("6.8.3", "8.2"); err != nil || res.Image != "presso-runtime:php8.2-wp6.8.8" {
		t.Fatalf("patch to pinned: %+v %v", res, err)
	}

	errCases := []struct {
		c      *Catalog
		wp, ph string
		want   error
	}{
		{on, "4.9", "", ErrWPUnsupported},
		{on, "6.8", "7.3", ErrPHPUnsupported},
		{on, "6.6", "8.4", ErrPHPIncompatible},
		{on, "6.0", "8.2", ErrPHPIncompatible},
		{off, "6.0", "", ErrLegacyDisabled},
		{off, "6.8", "8.1", ErrLegacyDisabled},
	}
	for _, tc := range errCases {
		if _, err := tc.c.Resolve(tc.wp, tc.ph); !errors.Is(err, tc.want) {
			t.Errorf("Resolve(%q,%q) = %v, want %v", tc.wp, tc.ph, err, tc.want)
		}
	}

	if _, err := New(full, Options{ImageTemplate: "x:{php}"}); err == nil {
		t.Fatal("template without {wp} accepted")
	}
	if _, err := New(full, Options{ImageTemplate: tmpl, PHPVersions: []string{"7.3"}}); err == nil {
		t.Fatal("unknown allowlist entry accepted")
	}
	if _, err := New(full, Options{ImageTemplate: tmpl, DefaultWP: "6.0"}); err == nil {
		t.Fatal("default WP 6.0 without legacy accepted")
	}
	restricted := mustCatalogOn(t, full, Options{LegacyEnabled: true, PHPVersions: []string{"8.3", "8.1"}, DefaultWP: "6.6"})
	if res, err := restricted.Resolve("", ""); err != nil || res.PHP != "8.3" || res.WPBranch != "6.6" {
		t.Fatalf("restricted: %+v %v", res, err)
	}
	if _, err := restricted.Resolve("6.6", "8.2"); !errors.Is(err, ErrPHPUnsupported) {
		t.Fatalf("allowlist: %v", err)
	}
	if _, err := New(full, Options{ImageTemplate: "x:{release}-php{php}-wp{wp}"}); err == nil {
		t.Fatal("{release} template without a release accepted")
	}
	withRelease := mustCatalogOn(t, full, Options{ImageTemplate: "ghcr.io/x/presso-runtime:{release}-php{php}-wp{wp}", Release: "v0.3.0"})
	if img := withRelease.Image("8.5", "7.1"); img != "ghcr.io/x/presso-runtime:v0.3.0-php8.5-wp7.1" {
		t.Fatalf("release image: %s", img)
	}
	withRuntime := mustCatalogOn(t, full, Options{ImageTemplate: "ghcr.io/x/presso-{runtime}:php{php}-wp{wp}", LegacyEnabled: true})
	if img := withRuntime.Image("8.0", "5.6"); img != "ghcr.io/x/presso-legacy:php8.0-wp5.6.19" {
		t.Fatalf("runtime placeholder: %s", img)
	}
}

// TestBuildPolicy covers the shipped policy: the newest branches on their
// newest PHP, and nothing on the legacy runtime. PHP 8.1 and older receive no
// upstream security fixes and the official images carry no backports, so no
// legacy pair is published even where the server enables the legacy switch.
func TestBuildPolicy(t *testing.T) {
	on := mustCatalog(t, Options{LegacyEnabled: true})
	for _, p := range on.Pairs() {
		if p.Runtime != RuntimeFrankenPHP {
			t.Errorf("published legacy pair %s", p.Tag)
		}
	}
	if p := on.Pairs(); len(p) != 2 || p[0].Tag != "php8.5-wp7.1" || p[1].Tag != "php8.5-wp6.9.7" {
		t.Fatalf("pairs: %+v", p)
	}
	if b := on.Bases(); len(b) != 1 || b[0].PHP != "8.5" {
		t.Fatalf("bases: %+v", b)
	}
	if _, err := on.Resolve("5.9", ""); !errors.Is(err, ErrWPUnsupported) {
		t.Fatalf("5.9 is unpublished: %v", err)
	}
}

// TestLegacyBuildPolicy covers a policy that publishes a legacy branch, as
// the shipped one did before PHP 8.1 lost its security fixes.
func TestLegacyBuildPolicy(t *testing.T) {
	m := *Default
	m.Build = Build{Branches: []string{"7.1", "6.9", "5.9"}, NewestPHPOnly: true}
	on := mustCatalogOn(t, &m, Options{LegacyEnabled: true})
	off := mustCatalogOn(t, &m, Options{LegacyEnabled: false})

	want := map[string][2]string{"7.1": {"8.5", RuntimeFrankenPHP}, "6.9": {"8.5", RuntimeFrankenPHP}, "5.9": {"8.1", RuntimeLegacy}}
	for wp, w := range want {
		res, err := on.Resolve(wp, "")
		if err != nil || res.PHP != w[0] || res.Runtime != w[1] {
			t.Errorf("Resolve(%s): %+v %v", wp, res, err)
		}
	}
	if _, err := on.Resolve("6.8", ""); !errors.Is(err, ErrWPUnsupported) {
		t.Fatalf("unpublished branch: %v", err)
	}
	if _, err := on.Resolve("6.9", "8.4"); !errors.Is(err, ErrPHPUnsupported) {
		t.Fatalf("compatible but unbuilt php: %v", err)
	}
	if _, err := on.Resolve("5.9", "8.4"); !errors.Is(err, ErrPHPIncompatible) {
		t.Fatalf("incompatible php: %v", err)
	}
	if _, err := off.Resolve("5.9", ""); !errors.Is(err, ErrLegacyDisabled) {
		t.Fatalf("legacy off: %v", err)
	}

	if p := on.Pairs(); len(p) != 3 || p[0].Tag != "php8.5-wp7.1" || p[2].Tag != "php8.1-wp5.9.16" {
		t.Fatalf("pairs: %+v", p)
	}
	if b := on.Bases(); len(b) != 2 || b[0].PHP != "8.5" || b[1].PHP != "8.1" {
		t.Fatalf("bases: %+v", b)
	}
	if len(off.Pairs()) != 2 || len(off.Bases()) != 1 {
		t.Fatalf("legacy off: %d pairs %d bases", len(off.Pairs()), len(off.Bases()))
	}

	caps := on.Capabilities()
	if len(caps.WordPress) != 3 || caps.WordPress[2].Branch != "5.9" || caps.WordPress[2].Runtime != RuntimeLegacy || len(caps.WordPress[0].PHPVersions) != 1 {
		t.Fatalf("caps on: %+v", caps.WordPress)
	}
	caps = off.Capabilities()
	// 5.9 stays listed, without PHP versions, so the UI can grey it out.
	if len(caps.WordPress) != 3 || len(caps.WordPress[2].PHPVersions) != 0 || caps.WordPress[2].DefaultPHP != "" || caps.WordPress[2].Runtime != RuntimeLegacy {
		t.Fatalf("caps off: %+v", caps.WordPress)
	}
}

func TestCapabilitiesAndPairs(t *testing.T) {
	on := mustCatalogOn(t, full, Options{LegacyEnabled: true})
	off := mustCatalogOn(t, full, Options{LegacyEnabled: false})

	caps := on.Capabilities()
	if caps.DefaultWP != "7.1" || !caps.LegacyEnabled || len(caps.WordPress) != 16 || len(caps.Runtimes) != 2 {
		t.Fatalf("caps on: %+v", caps)
	}
	if caps.WordPress[0].Status != "latest" || caps.WordPress[1].Status != "outdated" {
		t.Fatalf("status: %+v", caps.WordPress[:2])
	}
	if w := caps.WordPress[11]; w.Branch != "6.0" || w.DefaultPHP != "8.1" || w.Runtime != RuntimeLegacy {
		t.Fatalf("6.0 row: %+v", w)
	}

	caps = off.Capabilities()
	if caps.LegacyEnabled || len(caps.WordPress) != 16 || len(caps.WordPress[10].PHPVersions) == 0 || len(caps.WordPress[11].PHPVersions) != 0 {
		t.Fatalf("caps off: %d branches, 6.1 %v, 6.0 %v", len(caps.WordPress), caps.WordPress[10].PHPVersions, caps.WordPress[11].PHPVersions)
	}
	if caps.Runtimes[1].Enabled || strings.Join(caps.Runtimes[1].PHPVersions, ",") != "8.1,8.0,7.4" {
		t.Fatalf("legacy runtime info: %+v", caps.Runtimes[1])
	}
	if strings.Join(caps.WordPress[0].PHPVersions, ",") != "8.5,8.4,8.3,8.2" {
		t.Fatalf("7.1 offered without legacy: %v", caps.WordPress[0].PHPVersions)
	}

	if n := len(on.Pairs()); n != 72 {
		t.Fatalf("pairs on: %d", n)
	}
	if n := len(off.Pairs()); n != 27 {
		t.Fatalf("pairs off: %d", n)
	}
	if p := on.Pairs()[0]; p.Tag != "php8.5-wp7.1" || p.Runtime != RuntimeFrankenPHP {
		t.Fatalf("first pair: %+v", p)
	}
	if b := on.Bases(); len(b) != 7 || b[6].Debian != "bullseye" || b[6].Dockerfile != "runtime/base-legacy.Dockerfile" || b[0].Debian != "" {
		t.Fatalf("bases: %+v", b)
	}
	if b := off.Bases(); len(b) != 4 {
		t.Fatalf("bases off: %d", len(b))
	}
}
