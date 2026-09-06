package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// Moby implements Client over the official Docker Go client.
type Moby struct {
	cli *client.Client
}

// New connects to the Docker daemon. host overrides DOCKER_HOST / the local
// socket when non-empty. The connection is lazy; call Ping to verify.
func New(host string) (*Moby, error) {
	opts := []client.Opt{client.FromEnv}
	if host != "" {
		opts = append(opts, client.WithHost(host))
	}
	cli, err := client.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("docker: client: %w", err)
	}
	return &Moby{cli: cli}, nil
}

func (m *Moby) Close() error { return m.cli.Close() }

func wrap(err error) error {
	if err == nil {
		return nil
	}
	if cerrdefs.IsNotFound(err) {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return fmt.Errorf("docker: %w", err)
}

func (m *Moby) Ping(ctx context.Context) (Info, error) {
	v, err := m.cli.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return Info{}, wrap(err)
	}
	return Info{Version: v.Version, APIVersion: v.APIVersion, OS: v.Os, Arch: v.Arch}, nil
}

func (m *Moby) EnsureNetwork(ctx context.Context, name string, labels map[string]string) (string, error) {
	res, err := m.cli.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
	if err == nil {
		return res.Network.ID, nil
	}
	if !cerrdefs.IsNotFound(err) {
		return "", wrap(err)
	}
	created, err := m.cli.NetworkCreate(ctx, name, client.NetworkCreateOptions{Driver: "bridge", Labels: labels})
	if err != nil {
		return "", wrap(err)
	}
	return created.ID, nil
}

func (m *Moby) EnsureVolume(ctx context.Context, name string, labels map[string]string) error {
	if _, err := m.cli.VolumeInspect(ctx, name, client.VolumeInspectOptions{}); err == nil {
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return wrap(err)
	}
	_, err := m.cli.VolumeCreate(ctx, client.VolumeCreateOptions{Name: name, Labels: labels})
	return wrap(err)
}

func (m *Moby) RemoveVolume(ctx context.Context, name string) error {
	_, err := m.cli.VolumeRemove(ctx, name, client.VolumeRemoveOptions{Force: true})
	if cerrdefs.IsNotFound(err) {
		return nil
	}
	return wrap(err)
}

func (m *Moby) EnsureImage(ctx context.Context, ref string, progress func(string)) error {
	if _, err := m.cli.ImageInspect(ctx, ref); err == nil {
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return wrap(err)
	}
	resp, err := m.cli.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return wrap(err)
	}
	defer resp.Close()
	// Pull status is chatty (one line per layer per percent); report only
	// status transitions to keep job logs readable.
	last := ""
	for msg, err := range resp.JSONMessages(ctx) {
		if err != nil {
			return fmt.Errorf("docker: pull %s: %w", ref, err)
		}
		if msg.Error != nil {
			return fmt.Errorf("docker: pull %s: %s", ref, msg.Error.Message)
		}
		if progress != nil && msg.Status != "" && msg.Status != last && !strings.Contains(msg.Status, "Downloading") && !strings.Contains(msg.Status, "Extracting") {
			last = msg.Status
			progress(msg.Status)
		}
	}
	return nil
}

func (m *Moby) CreateContainer(ctx context.Context, spec Spec) (string, error) {
	cfg := &container.Config{
		Image:  spec.Image,
		Cmd:    spec.Cmd,
		Env:    spec.Env,
		Labels: spec.Labels,
		User:   spec.User,
	}
	if spec.StopTimeout > 0 {
		t := int(spec.StopTimeout / time.Second)
		cfg.StopTimeout = &t
	}
	if hc := spec.Healthcheck; hc != nil {
		cfg.Healthcheck = &container.HealthConfig{
			Test: hc.Test, Interval: hc.Interval, Timeout: hc.Timeout, Retries: hc.Retries, StartPeriod: hc.StartPeriod,
		}
	}
	host := &container.HostConfig{
		ExtraHosts: spec.ExtraHosts,
		Resources:  container.Resources{Memory: spec.MemoryBytes, NanoCPUs: spec.NanoCPUs},
	}
	if spec.RestartPolicy != "" && spec.RestartPolicy != "no" {
		host.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyMode(spec.RestartPolicy)}
	}
	if spec.LogMaxSize != "" {
		host.LogConfig = container.LogConfig{Type: "json-file", Config: map[string]string{
			"max-size": spec.LogMaxSize, "max-file": strconv.Itoa(max(spec.LogMaxFile, 1)),
		}}
	}
	for _, mt := range spec.Mounts {
		host.Mounts = append(host.Mounts, mount.Mount{
			Type: mount.Type(mt.Type), Source: mt.Source, Target: mt.Target, ReadOnly: mt.ReadOnly,
		})
	}
	if len(spec.Ports) > 0 {
		host.PortBindings = network.PortMap{}
		cfg.ExposedPorts = network.PortSet{}
		for _, p := range spec.Ports {
			proto := p.Protocol
			if proto == "" {
				proto = "tcp"
			}
			port, err := network.ParsePort(fmt.Sprintf("%d/%s", p.ContainerPort, proto))
			if err != nil {
				return "", fmt.Errorf("docker: port %d/%s: %w", p.ContainerPort, proto, err)
			}
			cfg.ExposedPorts[port] = struct{}{}
			b := network.PortBinding{HostPort: strconv.Itoa(p.HostPort)}
			if p.HostIP != "" {
				addr, err := netip.ParseAddr(p.HostIP)
				if err != nil {
					return "", fmt.Errorf("docker: host ip %q: %w", p.HostIP, err)
				}
				b.HostIP = addr
			}
			host.PortBindings[port] = append(host.PortBindings[port], b)
		}
	}
	var netCfg *network.NetworkingConfig
	if spec.Network != "" {
		host.NetworkMode = container.NetworkMode(spec.Network)
		netCfg = &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{
			spec.Network: {Aliases: spec.NetworkAliases},
		}}
	}
	res, err := m.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: spec.Name, Config: cfg, HostConfig: host, NetworkingConfig: netCfg,
	})
	if err != nil {
		return "", wrap(err)
	}
	return res.ID, nil
}

func (m *Moby) StartContainer(ctx context.Context, id string) error {
	_, err := m.cli.ContainerStart(ctx, id, client.ContainerStartOptions{})
	return wrap(err)
}

func (m *Moby) StopContainer(ctx context.Context, id string, timeout time.Duration) error {
	t := int(timeout / time.Second)
	_, err := m.cli.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &t})
	return wrap(err)
}

func (m *Moby) RestartContainer(ctx context.Context, id string, timeout time.Duration) error {
	t := int(timeout / time.Second)
	_, err := m.cli.ContainerRestart(ctx, id, client.ContainerRestartOptions{Timeout: &t})
	return wrap(err)
}

func (m *Moby) RemoveContainer(ctx context.Context, id string, force bool) error {
	_, err := m.cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: force})
	if cerrdefs.IsNotFound(err) {
		return nil
	}
	return wrap(err)
}

func (m *Moby) InspectContainer(ctx context.Context, idOrName string) (*ContainerInfo, error) {
	res, err := m.cli.ContainerInspect(ctx, idOrName, client.ContainerInspectOptions{})
	if err != nil {
		return nil, wrap(err)
	}
	c := res.Container
	info := &ContainerInfo{
		ID:    c.ID,
		Name:  strings.TrimPrefix(c.Name, "/"),
		Image: c.Image,
	}
	if c.Config != nil {
		info.Labels = c.Config.Labels
		info.Image = c.Config.Image
		info.Env = c.Config.Env
	}
	info.CreatedAt = parseDockerTime(c.Created)
	if s := c.State; s != nil {
		info.State = string(s.Status)
		info.ExitCode = s.ExitCode
		info.StartedAt = parseDockerTime(s.StartedAt)
		info.FinishedAt = parseDockerTime(s.FinishedAt)
		info.Health = "none"
		if s.Health != nil {
			info.Health = string(s.Health.Status)
		}
	}
	return info, nil
}

func (m *Moby) ListContainers(ctx context.Context, filter map[string]string, all bool) ([]ContainerInfo, error) {
	f := client.Filters{}
	for k, v := range filter {
		f.Add("label", k+"="+v)
	}
	res, err := m.cli.ContainerList(ctx, client.ContainerListOptions{All: all, Filters: f})
	if err != nil {
		return nil, wrap(err)
	}
	out := make([]ContainerInfo, 0, len(res.Items))
	for _, c := range res.Items {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		info := ContainerInfo{
			ID: c.ID, Name: name, Image: c.Image, Labels: c.Labels,
			State: string(c.State), CreatedAt: time.Unix(c.Created, 0).UTC(),
		}
		info.Health = "none"
		if c.Health != nil && c.Health.Status != "" {
			info.Health = string(c.Health.Status)
		}
		out = append(out, info)
	}
	return out, nil
}

const defaultMaxOutput = 1 << 20

func (m *Moby) Exec(ctx context.Context, id string, opts ExecOptions) (ExecResult, error) {
	created, err := m.cli.ExecCreate(ctx, id, client.ExecCreateOptions{
		User: opts.User, Cmd: opts.Cmd, Env: opts.Env, WorkingDir: opts.WorkingDir,
		AttachStdout: true, AttachStderr: true, AttachStdin: opts.Stdin != nil,
	})
	if err != nil {
		return ExecResult{}, wrap(err)
	}
	att, err := m.cli.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return ExecResult{}, wrap(err)
	}
	defer att.Close()

	if opts.Stdin != nil {
		go func() {
			_, _ = io.Copy(att.Conn, opts.Stdin)
			_ = att.CloseWrite()
		}()
	}
	limit := opts.MaxOutput
	if limit <= 0 {
		limit = defaultMaxOutput
	}
	stdout := &boundedBuffer{limit: limit}
	stderr := &boundedBuffer{limit: limit}
	copyErr := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(stdout, stderr, att.Reader)
		copyErr <- err
	}()
	select {
	case err = <-copyErr:
	case <-ctx.Done():
		return ExecResult{}, fmt.Errorf("docker: exec: %w", ctx.Err())
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return ExecResult{}, fmt.Errorf("docker: exec output: %w", err)
	}
	insp, err := m.cli.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return ExecResult{}, wrap(err)
	}
	return ExecResult{
		Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: insp.ExitCode,
		Truncated: stdout.truncated || stderr.truncated,
	}, nil
}

// boundedBuffer keeps the first limit bytes and discards the rest.
type boundedBuffer struct {
	bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	room := b.limit - b.Len()
	if room <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > room {
		b.truncated = true
		b.Buffer.Write(p[:room])
		return len(p), nil
	}
	return b.Buffer.Write(p)
}

func (m *Moby) Logs(ctx context.Context, id string, opts LogOptions) (<-chan LogLine, <-chan error) {
	lines := make(chan LogLine, 256)
	errc := make(chan error, 1)
	lo := client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Timestamps: true, Follow: opts.Follow}
	switch {
	case opts.Tail > 0:
		lo.Tail = strconv.Itoa(opts.Tail)
	case opts.Since.IsZero():
		lo.Tail = "200"
	}
	if !opts.Since.IsZero() {
		lo.Since = opts.Since.UTC().Format(time.RFC3339Nano)
	}
	go func() {
		defer close(lines)
		defer close(errc)
		rc, err := m.cli.ContainerLogs(ctx, id, lo)
		if err != nil {
			errc <- wrap(err)
			return
		}
		defer rc.Close()
		outR, outW := io.Pipe()
		errR, errW := io.Pipe()
		done := make(chan struct{}, 2)
		go scanLines(ctx, outR, "stdout", lines, done)
		go scanLines(ctx, errR, "stderr", lines, done)
		_, copyErr := stdcopy.StdCopy(outW, errW, rc)
		outW.Close()
		errW.Close()
		<-done
		<-done
		if copyErr != nil && !errors.Is(copyErr, io.EOF) && ctx.Err() == nil {
			errc <- fmt.Errorf("docker: logs: %w", copyErr)
		}
	}()
	return lines, errc
}

func scanLines(ctx context.Context, r io.Reader, stream string, out chan<- LogLine, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		ts, text, _ := strings.Cut(sc.Text(), " ")
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			t, text = time.Now().UTC(), sc.Text()
		}
		select {
		case out <- LogLine{Time: t.UTC(), Stream: stream, Text: text}:
		case <-ctx.Done():
			// Drain so StdCopy's pipe writer does not block forever.
			_, _ = io.Copy(io.Discard, r)
			return
		}
	}
}

func (m *Moby) Stats(ctx context.Context, id string) (Stats, error) {
	res, err := m.cli.ContainerStats(ctx, id, client.ContainerStatsOptions{Stream: false, IncludePreviousSample: true})
	if err != nil {
		return Stats{}, wrap(err)
	}
	defer res.Body.Close()
	var s container.StatsResponse
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		return Stats{}, fmt.Errorf("docker: stats: %w", err)
	}
	out := Stats{Read: s.Read, MemLimit: s.MemoryStats.Limit}
	// cgroup v2 reports page cache under inactive_file, v1 under cache;
	// subtracting it matches what `docker stats` shows.
	out.MemUsage = s.MemoryStats.Usage
	if v, ok := s.MemoryStats.Stats["inactive_file"]; ok && v < out.MemUsage {
		out.MemUsage -= v
	} else if v, ok := s.MemoryStats.Stats["cache"]; ok && v < out.MemUsage {
		out.MemUsage -= v
	}
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	if cpuDelta > 0 && sysDelta > 0 {
		cpus := float64(s.CPUStats.OnlineCPUs)
		if cpus == 0 {
			cpus = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
		}
		if cpus == 0 {
			cpus = 1
		}
		out.CPUPercent = cpuDelta / sysDelta * cpus * 100
	}
	return out, nil
}

func (m *Moby) Events(ctx context.Context, filter map[string]string) (<-chan Event, <-chan error) {
	f := client.Filters{}
	f.Add("type", "container")
	for k, v := range filter {
		f.Add("label", k+"="+v)
	}
	res := m.cli.Events(ctx, client.EventsListOptions{Filters: f})
	out := make(chan Event, 64)
	errc := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errc)
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-res.Err:
				if err != nil && ctx.Err() == nil {
					errc <- wrap(err)
				}
				return
			case msg := <-res.Messages:
				action := string(msg.Action)
				ev := Event{
					Time:        time.Unix(0, msg.TimeNano).UTC(),
					Action:      action,
					ContainerID: msg.Actor.ID,
					Labels:      msg.Actor.Attributes,
					Name:        msg.Actor.Attributes["name"],
				}
				if strings.HasPrefix(action, "health_status:") {
					ev.Action = "health_status"
					ev.Health = strings.TrimSpace(strings.TrimPrefix(action, "health_status:"))
				}
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

func (m *Moby) DiskUsage(ctx context.Context) (DiskUsage, error) {
	res, err := m.cli.DiskUsage(ctx, client.DiskUsageOptions{Containers: true, Images: true, Volumes: true})
	if err != nil {
		return DiskUsage{}, wrap(err)
	}
	return DiskUsage{
		ImagesBytes:     res.Images.TotalSize,
		ContainersBytes: res.Containers.TotalSize,
		VolumesBytes:    res.Volumes.TotalSize,
	}, nil
}

func parseDockerTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || t.Unix() <= 0 {
		return time.Time{}
	}
	return t.UTC()
}
