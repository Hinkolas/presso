package platform

import (
	"context"
	"strings"
	"testing"

	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/docker/fake"
	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/secrets"
	"github.com/Hinkolas/presso/internal/testdb"
)

func testConfig() *config.API {
	return &config.API{
		Platform: config.Platform{Managed: true, Network: "presso-test", MariaDBImage: "presso-mariadb:test", MariaDBListen: "127.0.0.1:3306",
			ValkeyImage: "presso-valkey:test", ValkeyListen: "127.0.0.1:6379", ValkeyMaxMemory: "64mb"},
		Edge: config.Edge{Image: "presso-edge:test", HTTPListen: "0.0.0.0:80", HTTPSListen: "0.0.0.0:443"},
	}
}

func TestReconcileCreatesAndRecreates(t *testing.T) {
	st := testdb.New(t)
	dk := fake.New()
	box, _ := secrets.New(strings.Repeat("s", 32))
	cfg := testConfig()
	svc := New(cfg, dk, st, box, events.New(), nil)
	ctx := context.Background()

	// Valkey/MariaDB are unreachable in tests; ACL apply is skipped because
	// there are no sites. Reconcile must still succeed for the containers.
	if err := svc.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(dk.Created) != 3 {
		t.Fatalf("expected 3 containers, created %d", len(dk.Created))
	}
	for _, name := range []string{NameMariaDB, NameValkey, NameEdge} {
		info, err := dk.InspectContainer(ctx, name)
		if err != nil || !info.Running() {
			t.Fatalf("%s: %v %+v", name, err, info)
		}
		if info.Labels[docker.LabelManaged] != "true" || info.Labels[labelHash] == "" {
			t.Fatalf("%s labels: %v", name, info.Labels)
		}
	}
	if !dk.Volumes[NameMariaDB+"-data"] || !dk.Volumes[NameEdge+"-cache"] {
		t.Fatalf("volumes: %v", dk.Volumes)
	}
	comps := svc.Components()
	if len(comps) != 4 || comps[1].Name != "mariadb" || comps[1].State != "running" {
		t.Fatalf("components: %+v", comps)
	}

	// Second reconcile is a no-op.
	if err := svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if len(dk.Created) != 3 {
		t.Fatalf("recreated without change: %d", len(dk.Created))
	}

	// Stopped container is started again.
	edgeInfo, _ := dk.InspectContainer(ctx, NameEdge)
	_ = dk.StopContainer(ctx, edgeInfo.ID, 0)
	_ = svc.Reconcile(ctx)
	edgeInfo, _ = dk.InspectContainer(ctx, NameEdge)
	if !edgeInfo.Running() {
		t.Fatal("stopped edge not restarted")
	}

	// Image change → recreate.
	cfg.Edge.Image = "presso-edge:v2"
	if err := svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if len(dk.Created) != 4 || dk.Created[3].Image != "presso-edge:v2" || len(dk.Removed) != 1 {
		t.Fatalf("expected edge recreate: created=%d removed=%d", len(dk.Created), len(dk.Removed))
	}

	// The secrets were persisted and are stable across calls.
	a, _ := svc.secret(ctx, kvMariaDBRoot)
	b, _ := svc.secret(ctx, kvMariaDBRoot)
	if a == "" || a != b {
		t.Fatal("root password not stable")
	}
	found := false
	for _, e := range dk.Created[0].Env {
		if e == "MARIADB_ROOT_PASSWORD="+a {
			found = true
		}
	}
	if !found {
		t.Fatal("root password not passed to mariadb")
	}
}

func TestEdgeSpecPorts(t *testing.T) {
	svc := New(testConfig(), fake.New(), nil, nil, nil, nil)
	spec := svc.edgeSpec()
	if len(spec.Ports) != 3 {
		t.Fatalf("ports: %+v", spec.Ports)
	}
	for _, p := range spec.Ports {
		if p.ContainerPort != 80 && p.ContainerPort != 443 {
			t.Fatalf("management port published: %+v", p)
		}
	}
	if spec.Ports[2].Protocol != "udp" {
		t.Fatalf("h3 udp port missing: %+v", spec.Ports[2])
	}
}

func TestLegacyCacheIsNotAutomaticallyReplaced(t *testing.T) {
	dk := fake.New()
	ctx := context.Background()
	id, err := dk.CreateContainer(ctx, docker.Spec{Name: "presso-redis", Image: "redis:7-alpine", Labels: managedLabels("redis")})
	if err != nil {
		t.Fatal(err)
	}
	_ = dk.StartContainer(ctx, id)
	svc := New(testConfig(), dk, nil, nil, nil, nil)
	if err := svc.Reconcile(ctx); err == nil || !strings.Contains(err.Error(), "automatic migration is unsupported") {
		t.Fatalf("legacy cache should require an explicit transition: %v", err)
	}
	info, err := dk.InspectContainer(ctx, id)
	if err != nil || !info.Running() || len(dk.Removed) != 0 || len(dk.Created) != 1 || len(dk.Networks) != 0 {
		t.Fatal("legacy installation was changed")
	}
}

func TestSpecHashExtraHosts(t *testing.T) {
	base := docker.Spec{Name: "x", Image: "img:1", Cmd: []string{"run"}, Env: []string{"A=1"}, RestartPolicy: "unless-stopped"}
	empty := base
	empty.ExtraHosts = []string{}
	if specHash(base) != specHash(empty) {
		t.Fatal("an empty ExtraHosts list must not change the hash (existing components would be recreated on upgrade)")
	}
	withHosts := base
	withHosts.ExtraHosts = []string{"host.docker.internal:host-gateway"}
	if specHash(base) == specHash(withHosts) {
		t.Fatal("ExtraHosts must be part of the hash")
	}
}

func TestEdgeRecreatePullsBeforeRemove(t *testing.T) {
	st := testdb.New(t)
	dk := fake.New()
	box, _ := secrets.New(strings.Repeat("s", 32))
	cfg := testConfig()
	svc := New(cfg, dk, st, box, events.New(), nil)
	ctx := context.Background()
	if err := svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if !svc.edgeDirty.Load() {
		t.Fatal("a freshly created edge must be marked dirty so its config is pushed")
	}
	// The edge spec carries the host-gateway entry the panel vhost needs.
	if hosts := svc.edgeSpec().ExtraHosts; len(hosts) != 1 || hosts[0] != "host.docker.internal:host-gateway" {
		t.Fatalf("edge extra hosts: %v", hosts)
	}
	cfg.Edge.Image = "presso-edge:v2"
	if err := svc.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if len(dk.Removed) != 1 || len(dk.Created) != 4 || dk.Created[3].Image != "presso-edge:v2" {
		t.Fatalf("expected one recreate: removed=%d created=%d", len(dk.Removed), len(dk.Created))
	}
}

func TestBrowserSpecKeepsDevToolsOnLoopback(t *testing.T) {
	cfg := testConfig()
	cfg.Shots.Image = "presso-browser:test"
	spec := New(cfg, fake.New(), testdb.New(t), nil, events.New(), nil).browserSpec()
	if spec.Image != "presso-browser:test" || strings.Join(spec.Entrypoint, " ") != "presso-browser" || len(spec.Ports) != 0 {
		t.Fatalf("browser spec: %+v", spec)
	}
	args := strings.Join(spec.Cmd, " ")
	for _, want := range []string{"--remote-debugging-address=127.0.0.1", "--remote-debugging-port=9223", "--host-resolver-rules=MAP * presso-edge, EXCLUDE presso-edge"} {
		if !strings.Contains(args, want) {
			t.Fatalf("browser args lack %q: %s", want, args)
		}
	}
}
