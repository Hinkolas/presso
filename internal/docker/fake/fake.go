// Package fake is an in-memory docker.Client for tests: containers, volumes,
// networks and images live in maps, Start marks a container running and
// healthy, Exec delegates to an optional hook, and tests drive the event and
// log streams by hand. It records every created Spec for assertions.
package fake

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Hinkolas/presso/internal/docker"
)

type Container struct {
	docker.ContainerInfo
	Spec docker.Spec
}

type Client struct {
	mu         sync.Mutex
	seq        int
	Containers map[string]*Container // by id
	Volumes    map[string]bool
	Networks   map[string]bool
	Images     map[string]bool
	Created    []docker.Spec
	Removed    []string
	// ExecFn handles Exec calls; nil means exit 0 with empty output.
	ExecFn func(id string, opts docker.ExecOptions) (docker.ExecResult, error)
	// HealthOnStart is the health reported after Start ("healthy" default).
	HealthOnStart string
	// FailCreate, when set, makes CreateContainer fail with this error once.
	FailCreate error
	// LogLines returned by Logs for any container.
	LogLines []docker.LogLine
	// StatsFn optionally answers Stats.
	StatsFn func(id string) (docker.Stats, error)

	events chan docker.Event
}

func New() *Client {
	return &Client{
		Containers:    map[string]*Container{},
		Volumes:       map[string]bool{},
		Networks:      map[string]bool{},
		Images:        map[string]bool{},
		HealthOnStart: "healthy",
		events:        make(chan docker.Event, 64),
	}
}

// Emit pushes an event to whoever is consuming Events.
func (c *Client) Emit(ev docker.Event) { c.events <- ev }

func (c *Client) Ping(context.Context) (docker.Info, error) {
	return docker.Info{Version: "fake", APIVersion: "1.99", OS: "linux", Arch: "arm64"}, nil
}

func (c *Client) EnsureNetwork(_ context.Context, name string, _ map[string]string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Networks[name] = true
	return "net-" + name, nil
}

func (c *Client) EnsureVolume(_ context.Context, name string, _ map[string]string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Volumes[name] = true
	return nil
}

func (c *Client) RemoveVolume(_ context.Context, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.Volumes, name)
	return nil
}

func (c *Client) EnsureImage(_ context.Context, ref string, progress func(string)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.Images[ref] && progress != nil {
		progress("Pulling from " + ref)
	}
	c.Images[ref] = true
	return nil
}

func (c *Client) CreateContainer(_ context.Context, spec docker.Spec) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.FailCreate != nil {
		err := c.FailCreate
		c.FailCreate = nil
		return "", err
	}
	for _, existing := range c.Containers {
		if existing.Name == spec.Name {
			return "", fmt.Errorf("docker: conflict: container name %q already in use", spec.Name)
		}
	}
	c.seq++
	id := fmt.Sprintf("ctr%04d%s", c.seq, strings.Repeat("0", 56))
	c.Containers[id] = &Container{
		ContainerInfo: docker.ContainerInfo{
			ID: id, Name: spec.Name, Image: spec.Image, Labels: spec.Labels, State: "created",
			Health: "none", CreatedAt: time.Now().UTC(), Env: spec.Env,
		},
		Spec: spec,
	}
	c.Created = append(c.Created, spec)
	return id, nil
}

func (c *Client) get(idOrName string) (*Container, error) {
	if ctr, ok := c.Containers[idOrName]; ok {
		return ctr, nil
	}
	for _, ctr := range c.Containers {
		if ctr.Name == idOrName {
			return ctr, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", docker.ErrNotFound, idOrName)
}

func (c *Client) StartContainer(_ context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctr, err := c.get(id)
	if err != nil {
		return err
	}
	ctr.State = "running"
	ctr.Health = c.HealthOnStart
	ctr.StartedAt = time.Now().UTC()
	return nil
}

func (c *Client) StopContainer(_ context.Context, id string, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctr, err := c.get(id)
	if err != nil {
		return err
	}
	ctr.State = "exited"
	ctr.Health = "none"
	ctr.FinishedAt = time.Now().UTC()
	return nil
}

func (c *Client) RestartContainer(ctx context.Context, id string, t time.Duration) error {
	if err := c.StopContainer(ctx, id, t); err != nil {
		return err
	}
	return c.StartContainer(ctx, id)
}

func (c *Client) RemoveContainer(_ context.Context, id string, force bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctr, err := c.get(id)
	if err != nil {
		return nil
	}
	if ctr.State == "running" && !force {
		return fmt.Errorf("docker: conflict: container %s is running", id)
	}
	delete(c.Containers, ctr.ID)
	c.Removed = append(c.Removed, ctr.ID)
	return nil
}

func (c *Client) InspectContainer(_ context.Context, idOrName string) (*docker.ContainerInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctr, err := c.get(idOrName)
	if err != nil {
		return nil, err
	}
	info := ctr.ContainerInfo
	return &info, nil
}

func (c *Client) ListContainers(_ context.Context, filter map[string]string, all bool) ([]docker.ContainerInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []docker.ContainerInfo
outer:
	for _, ctr := range c.Containers {
		if !all && ctr.State != "running" {
			continue
		}
		for k, v := range filter {
			if ctr.Labels[k] != v {
				continue outer
			}
		}
		out = append(out, ctr.ContainerInfo)
	}
	return out, nil
}

func (c *Client) Exec(_ context.Context, id string, opts docker.ExecOptions) (docker.ExecResult, error) {
	c.mu.Lock()
	ctr, err := c.get(id)
	fn := c.ExecFn
	c.mu.Unlock()
	if err != nil {
		return docker.ExecResult{}, err
	}
	if ctr.State != "running" {
		return docker.ExecResult{}, fmt.Errorf("docker: container %s is not running", id)
	}
	if fn != nil {
		return fn(ctr.ID, opts)
	}
	return docker.ExecResult{ExitCode: 0}, nil
}

func (c *Client) Logs(ctx context.Context, _ string, opts docker.LogOptions) (<-chan docker.LogLine, <-chan error) {
	lines := make(chan docker.LogLine, len(c.LogLines)+1)
	errc := make(chan error, 1)
	go func() {
		defer close(lines)
		defer close(errc)
		c.mu.Lock()
		snapshot := append([]docker.LogLine(nil), c.LogLines...)
		c.mu.Unlock()
		for _, l := range snapshot {
			select {
			case lines <- l:
			case <-ctx.Done():
				return
			}
		}
		if opts.Follow {
			<-ctx.Done()
		}
	}()
	return lines, errc
}

func (c *Client) Stats(_ context.Context, id string) (docker.Stats, error) {
	if c.StatsFn != nil {
		return c.StatsFn(id)
	}
	return docker.Stats{Read: time.Now().UTC(), CPUPercent: 1.5, MemUsage: 100 << 20, MemLimit: 512 << 20}, nil
}

func (c *Client) Events(ctx context.Context, _ map[string]string) (<-chan docker.Event, <-chan error) {
	out := make(chan docker.Event)
	errc := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errc)
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-c.events:
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, errc
}

func (c *Client) DiskUsage(context.Context) (docker.DiskUsage, error) {
	return docker.DiskUsage{ImagesBytes: 1 << 30, ContainersBytes: 1 << 20, VolumesBytes: 1 << 28}, nil
}

// Compile-time interface check.
var _ docker.Client = (*Client)(nil)
