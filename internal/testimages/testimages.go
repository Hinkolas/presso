// Package testimages names the locally built images the Docker integration
// tests run against. CI builds them from this checkout under these tags
// (.github/workflows/verify.yml); PRESSO_TEST_*_IMAGE overrides each one.
package testimages

import "os"

func get(key, tag string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return tag
}

func Edge() string    { return get("PRESSO_TEST_EDGE_IMAGE", "presso-edge:release-verification") }
func MariaDB() string { return get("PRESSO_TEST_MARIADB_IMAGE", "presso-mariadb:release-verification") }
func Valkey() string  { return get("PRESSO_TEST_VALKEY_IMAGE", "presso-valkey:release-verification") }
func Browser() string { return get("PRESSO_TEST_BROWSER_IMAGE", "presso-browser:release-verification") }
