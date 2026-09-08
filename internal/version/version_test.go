package version

import "testing"

func TestReleaseOf(t *testing.T) {
	cases := map[string]string{
		"v0.1.0":              "v0.1.0",
		"v12.3.44":            "v12.3.44",
		"v0.1.0-rc.1":         "v0.1.0-rc.1",
		"v0.1.0-beta2":        "v0.1.0-beta2",
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
