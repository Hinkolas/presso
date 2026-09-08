package compat

import "testing"

func TestRestrictionsCannotInventPublishedPairs(t *testing.T) {
	unrestricted, err := New(Default, Options{ImageTemplate: "runtime:php{php}-wp{wp}", LegacyEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	published := map[string]bool{}
	for _, p := range unrestricted.Pairs() {
		published[p.WP+"/"+p.PHP] = true
	}
	for _, php := range Default.PHP {
		c, err := New(Default, Options{ImageTemplate: "runtime:php{php}-wp{wp}", LegacyEnabled: true, PHPVersions: []string{php.Version}})
		if err != nil {
			continue
		}
		for _, p := range c.Pairs() {
			if !published[p.WP+"/"+p.PHP] {
				t.Fatalf("host restriction invented %+v", p)
			}
		}
	}
	if Default.Build.NewestPHPOnly {
		newest := Default.Compatible(Default.Latest().Branch)
		if len(newest) > 1 {
			if _, err := New(Default, Options{ImageTemplate: "runtime:php{php}-wp{wp}", LegacyEnabled: true, PHPVersions: []string{newest[1]}}); err == nil {
				t.Fatal("default without a published image accepted")
			}
		}
	}
}
