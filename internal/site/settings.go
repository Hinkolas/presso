package site

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// Settings are the per-site knobs rendered into the container environment.
// Zero values mean "platform default"; Normalize fills them in so the rest
// of the code never sees an empty field.
type Settings struct {
	PHPMemoryLimit    string  `json:"php_memory_limit"`
	UploadMaxFilesize string  `json:"upload_max_filesize"`
	PostMaxSize       string  `json:"post_max_size"`
	MaxExecutionTime  int     `json:"max_execution_time"`
	MaxInputVars      int     `json:"max_input_vars"`
	OpcacheMemoryMB   int     `json:"opcache_memory_mb"`
	ContainerMemoryMB int     `json:"container_memory_mb"`
	ContainerCPUs     float64 `json:"container_cpus"`
	// CacheTTL is the page-cache TTL in seconds.
	CacheTTL int `json:"cache_ttl"`
	// CacheSWR lets the edge serve an expired page from cache while it
	// fetches a fresh copy in the background (stale-while-revalidate).
	CacheSWR bool `json:"cache_swr"`
	// CacheWarm opts the site into the sitemap cache warmer. Engine-side only:
	// the container never sees it.
	CacheWarm bool `json:"cache_warm"`
}

func DefaultSettings() Settings {
	return Settings{
		PHPMemoryLimit:    "256M",
		UploadMaxFilesize: "64M",
		PostMaxSize:       "64M",
		MaxExecutionTime:  120,
		MaxInputVars:      3000,
		OpcacheMemoryMB:   128,
		ContainerMemoryMB: 512,
		ContainerCPUs:     1.0,
		CacheTTL:          43200,
	}
}

var phpSizeRe = regexp.MustCompile(`^[1-9][0-9]{0,4}[MG]$`)

// Normalize fills zero fields with defaults.
func (s Settings) Normalize() Settings {
	d := DefaultSettings()
	if s.PHPMemoryLimit == "" {
		s.PHPMemoryLimit = d.PHPMemoryLimit
	}
	if s.UploadMaxFilesize == "" {
		s.UploadMaxFilesize = d.UploadMaxFilesize
	}
	if s.PostMaxSize == "" {
		s.PostMaxSize = d.PostMaxSize
	}
	if s.MaxExecutionTime == 0 {
		s.MaxExecutionTime = d.MaxExecutionTime
	}
	if s.MaxInputVars == 0 {
		s.MaxInputVars = d.MaxInputVars
	}
	if s.OpcacheMemoryMB == 0 {
		s.OpcacheMemoryMB = d.OpcacheMemoryMB
	}
	if s.ContainerMemoryMB == 0 {
		s.ContainerMemoryMB = d.ContainerMemoryMB
	}
	if s.ContainerCPUs == 0 {
		s.ContainerCPUs = d.ContainerCPUs
	}
	if s.CacheTTL == 0 {
		s.CacheTTL = d.CacheTTL
	}
	return s
}

// Validate checks ranges; the message is safe to show to the operator.
func (s Settings) Validate() error {
	for name, v := range map[string]string{"php_memory_limit": s.PHPMemoryLimit, "upload_max_filesize": s.UploadMaxFilesize, "post_max_size": s.PostMaxSize} {
		if !phpSizeRe.MatchString(v) {
			return fmt.Errorf("%w: %s must look like 256M or 1G", ErrInvalidInput, name)
		}
	}
	if s.MaxExecutionTime < 10 || s.MaxExecutionTime > 3600 {
		return fmt.Errorf("%w: max_execution_time must be between 10 and 3600", ErrInvalidInput)
	}
	if s.MaxInputVars < 1000 || s.MaxInputVars > 100000 {
		return fmt.Errorf("%w: max_input_vars must be between 1000 and 100000", ErrInvalidInput)
	}
	if s.OpcacheMemoryMB < 32 || s.OpcacheMemoryMB > 2048 {
		return fmt.Errorf("%w: opcache_memory_mb must be between 32 and 2048", ErrInvalidInput)
	}
	if s.ContainerMemoryMB < 128 || s.ContainerMemoryMB > 65536 {
		return fmt.Errorf("%w: container_memory_mb must be between 128 and 65536", ErrInvalidInput)
	}
	if s.ContainerCPUs < 0.1 || s.ContainerCPUs > 64 {
		return fmt.Errorf("%w: container_cpus must be between 0.1 and 64", ErrInvalidInput)
	}
	if s.CacheTTL < 60 || s.CacheTTL > 7*86400 {
		return fmt.Errorf("%w: cache_ttl must be between 60 and 604800 seconds", ErrInvalidInput)
	}
	return nil
}

// parseSettings decodes the stored JSON, tolerating old or empty rows.
func parseSettings(raw string) Settings {
	var s Settings
	_ = json.Unmarshal([]byte(raw), &s)
	return s.Normalize()
}

// ParseSettings is parseSettings for other packages that hold a store.Site.
func ParseSettings(raw string) Settings { return parseSettings(raw) }

func (s Settings) encode() string {
	b, _ := json.Marshal(s)
	return string(b)
}

// requiresRecreate reports whether moving from a to b needs a new container
// (anything the entrypoint or PHP reads from the environment; the cache TTL
// and SWR flag count because the system plugin emits them as Cache-Control).
func requiresRecreate(a, b Settings) bool {
	a.CacheWarm, b.CacheWarm = false, false // engine-only
	return a != b
}
