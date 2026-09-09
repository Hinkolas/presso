package version

import "testing"

func TestReleaseOf(t *testing.T) {
	cases := map[string]string{
		"v0.1.0":              "v0.1.0",
		"v12.3.44":            "v12.3.44",
		"v0.1.0-rc.1":         "v0.1.0-rc.1",
		"v0.1.0-beta.2":       "v0.1.0-beta.2",
		"v0.1.0-alpha.0":      "v0.1.0-alpha.0",
		"v0.1.0-beta2":        "",
		"v0.1.0-rc1":          "",
		"v0.1.0-alpha1":       "",
		"v0.1.0-rc.01":        "",
		"v00.1.0":             "",
		"v0.01.0":             "",
		"v0.1.00":             "",
		"v0.1.0-rc":           "",
		"v0.1.0-preview.1":    "",
		"v0.1.0+build.1":      "",
		"v0.1.0\n":            "",
		"v0.0.0-dev":          "",
		"v0.1.0-3-gabc1234":   "",
		"v0.1.0-dirty":        "",
		"v0.1.0-3-gabc-dirty": "",
		"0.1.0":               "",
		"":                    "",
	}
	for in, want := range cases {
		if got := ReleaseOf(in); got != want {
			t.Errorf("ReleaseOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsPrerelease(t *testing.T) {
	for _, v := range []string{"v0.1.0-alpha.1", "v0.1.0-beta.2", "v0.1.0-rc.1", "v1.0.0-rc.1"} {
		if !IsPrerelease(v) {
			t.Errorf("%q should be a prerelease", v)
		}
	}
	for _, v := range []string{"v0.1.0", "v0.99.0", "v1.0.0", "v0.1.0-rc1", "v0.0.0-dev", ""} {
		if IsPrerelease(v) {
			t.Errorf("%q should not be a prerelease", v)
		}
	}
}
