package site

import "testing"

func TestSettingsRecreate(t *testing.T) {
	base := DefaultSettings()
	ttl := base
	ttl.CacheTTL = 600
	swr := base
	swr.CacheSWR = true
	warm := base
	warm.CacheWarm = true
	if !requiresRecreate(base, ttl) {
		t.Fatal("cache_ttl feeds the plugin's Cache-Control: must recreate")
	}
	if !requiresRecreate(base, swr) {
		t.Fatal("cache_swr feeds the plugin's Cache-Control: must recreate")
	}
	if requiresRecreate(base, warm) {
		t.Fatal("cache_warm is engine-only: must not recreate")
	}
	if requiresRecreate(base, base) {
		t.Fatal("identical settings recreate")
	}
}

func TestSettingsParse(t *testing.T) {
	s := parseSettings("{}")
	if s.CacheTTL != 43200 || s.CacheSWR || s.CacheWarm {
		t.Fatalf("defaults: %+v", s)
	}
	s = parseSettings(`{"cache_swr":true,"cache_warm":true,"cache_ttl":60}`)
	if s.CacheTTL != 60 || !s.CacheSWR || !s.CacheWarm {
		t.Fatalf("parsed: %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}
