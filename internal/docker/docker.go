// Package docker is the engine's view of the Docker Engine: the handful of
// operations presso needs (images, volumes, networks, containers, exec, logs,
// stats, events), expressed with presso's own types so that the rest of the
// engine never imports the moby client. Client is an interface so services
// and API tests run against the in-memory implementation in the fake
// subpackage; the moby implementation lives in moby.go.
package docker

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound is returned when a container, volume, network or image does
// not exist. Callers use errors.Is.
var ErrNotFound = errors.New("docker: not found")

// Labels every presso-managed object carries, so the engine can find its own
// containers after a restart and the status watcher can filter events.
const (
	LabelManaged = "presso.managed"
	LabelKind    = "presso.kind" // site|edge|mariadb|redis|browser|network
	LabelSiteID  = "presso.site_id"
	LabelSlug    = "presso.slug"
	// LabelRuntime and LabelWPVersion mirror the site row (frankenphp|legacy,
	// e.g. 6.8.8) so `docker ps` alone tells what a container runs.
	LabelRuntime   = "presso.runtime"
	LabelWPVersion = "presso.wp_version"

	// LabelComposeProject is the label Docker Compose stamps on its objects.
	// presso does not use Compose, but Docker Desktop groups containers that
	// share this label into one collapsible stack, so every presso object
	// carries it with ComposeProject as the value purely for that UI grouping.
	LabelComposeProject = "com.docker.compose.project"
	ComposeProject      = "presso"
)

// Labels returns the label set for a presso-managed object of the given
// kind, merged with any extra labels. Every caller that creates a container,
// volume or network goes through here so the managed and grouping labels
// never drift apart.
func Labels(kind string, extra map[string]string) map[string]string {
	labels := map[string]string{
		LabelManaged:        "true",
		LabelKind:           kind,
		LabelComposeProject: ComposeProject,
	}
	for k, v := range extra {
		labels[k] = v
	}
	return labels
}

// Client is the subset of the Docker API presso uses.
type Client interface {
	// Ping reports daemon reachability and version.
	Ping(ctx context.Context) (Info, error)

	EnsureNetwork(ctx context.Context, name string, labels map[string]string) (string, error)
	EnsureVolume(ctx context.Context, name string, labels map[string]string) error
	RemoveVolume(ctx context.Context, name string) error

	// EnsureImage pulls ref unless it is present locally. progress receives
	// human-readable pull status lines (may be nil).
	EnsureImage(ctx context.Context, ref string, progress func(string)) error

	CreateContainer(ctx context.Context, spec Spec) (string, error)
	StartContainer(ctx context.Context, id string) error
	// StopContainer sends SIGTERM and kills after timeout.
	StopContainer(ctx context.Context, id string, timeout time.Duration) error
	RestartContainer(ctx context.Context, id string, timeout time.Duration) error
	RemoveContainer(ctx context.Context, id string, force bool) error
	// InspectContainer accepts an id or name; ErrNotFound when absent.
	InspectContainer(ctx context.Context, idOrName string) (*ContainerInfo, error)
	// ListContainers returns containers matching every label in filter
	// (all=true includes stopped ones).
	ListContainers(ctx context.Context, filter map[string]string, all bool) ([]ContainerInfo, error)

	// Exec runs a command inside a running container and waits for it.
	Exec(ctx context.Context, id string, opts ExecOptions) (ExecResult, error)
	// Logs streams log lines until ctx is done, the container exits (when
	// following) or the tail is exhausted (when not following). The channel
	// is closed when the stream ends; a terminal error, if any, is sent on
	// the error channel before closing.
	Logs(ctx context.Context, id string, opts LogOptions) (<-chan LogLine, <-chan error)
	// Stats takes one resource sample (with a one-second CPU delta).
	Stats(ctx context.Context, id string) (Stats, error)
	// Events streams container events matching every label in filter.
	Events(ctx context.Context, filter map[string]string) (<-chan Event, <-chan error)
	DiskUsage(ctx context.Context) (DiskUsage, error)
}

type Info struct {
	Version    string
	APIVersion string
	OS         string
	Arch       string
}

// Spec describes a container to create. Everything presso needs and nothing
// more; values map straight onto the Docker create request.
type Spec struct {
	Name  string
	Image string
	// Entrypoint overrides the image's ENTRYPOINT when non-nil.
	Entrypoint     []string
	Cmd            []string
	Env            []string
	Labels         map[string]string
	User           string
	Network        string
	NetworkAliases []string
	Mounts         []Mount
	Ports          []PortBinding
	// ExtraHosts entries have the form "host:ip" (ip may be "host-gateway").
	ExtraHosts []string
	// MemoryBytes and NanoCPUs are hard limits; zero means unlimited.
	MemoryBytes int64
	NanoCPUs    int64
	// RestartPolicy is a Docker policy name: "unless-stopped", "always",
	// "no". Empty means "no".
	RestartPolicy string
	// StopTimeout is how long Docker waits after SIGTERM before SIGKILL.
	StopTimeout time.Duration
	// LogMaxSize/LogMaxFile bound the json-file log driver (e.g. "10m", 3).
	LogMaxSize string
	LogMaxFile int
	// Healthcheck overrides the image's HEALTHCHECK when non-nil.
	Healthcheck *Healthcheck
}

type MountType string

const (
	MountVolume MountType = "volume"
	MountBind   MountType = "bind"
	MountTmpfs  MountType = "tmpfs"
)

type Mount struct {
	Type     MountType
	Source   string // volume name or host path; empty for tmpfs
	Target   string
	ReadOnly bool
}

type PortBinding struct {
	HostIP        string
	HostPort      int
	ContainerPort int
	Protocol      string // tcp (default) or udp
}

type Healthcheck struct {
	Test        []string // e.g. {"CMD", "curl", "-f", "http://localhost/"}
	Interval    time.Duration
	Timeout     time.Duration
	Retries     int
	StartPeriod time.Duration
}

// ContainerInfo is the inspected state presso cares about.
type ContainerInfo struct {
	ID     string
	Name   string // without the leading slash
	Image  string
	Labels map[string]string
	// State is Docker's: created|running|paused|restarting|removing|exited|dead.
	State string
	// Health is none|starting|healthy|unhealthy.
	Health     string
	ExitCode   int
	StartedAt  time.Time
	FinishedAt time.Time
	CreatedAt  time.Time
	// Env is the container's configured environment (used to detect drift).
	Env []string
}

func (c *ContainerInfo) Running() bool { return c != nil && c.State == "running" }

type ExecOptions struct {
	User       string
	Cmd        []string
	Env        []string
	WorkingDir string
	Stdin      io.Reader
	// MaxOutput caps captured stdout and stderr each; 0 means 1 MiB.
	MaxOutput int
}

type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	// Truncated is set when either stream hit MaxOutput.
	Truncated bool
}

type LogOptions struct {
	Follow bool
	// Tail limits the initial backlog; 0 means all (Docker's default) when
	// Since is set, else 200.
	Tail  int
	Since time.Time
}

type LogLine struct {
	Time   time.Time `json:"time"`
	Stream string    `json:"stream"` // stdout|stderr
	Text   string    `json:"text"`
}

type Stats struct {
	Read       time.Time
	CPUPercent float64
	MemUsage   uint64
	MemLimit   uint64
}

// Event is a container lifecycle event.
type Event struct {
	Time        time.Time
	Action      string // start|die|stop|kill|health_status|destroy|...
	ContainerID string
	Name        string
	Labels      map[string]string
	// Health is set for health_status events: healthy|unhealthy.
	Health string
}

type DiskUsage struct {
	ImagesBytes     int64
	ContainersBytes int64
	VolumesBytes    int64
}
