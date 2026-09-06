// Package site is the engine's site model and lifecycle: validation and
// creation of site records, the provisioning / start / stop / recreate /
// delete jobs, WP-CLI execution, one-click login links, cache purges and the
// edge route list. It composes internal/docker (containers),
// internal/platform (shared DB/Redis/edge) and internal/jobs (background
// execution); the HTTP layer only translates to and from this package.
package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/edge"
	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/platform"
	"github.com/Hinkolas/presso/internal/secrets"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/utils"
)

// Status values stored in sites.status.
const (
	StatusProvisioning = "provisioning"
	StatusRunning      = "running"
	StatusStopped      = "stopped"
	StatusError        = "error"
	StatusDeleting     = "deleting"
)

// Job kinds registered with the runner.
const (
	JobProvision = "site.provision"
	JobStart     = "site.start"
	JobStop      = "site.stop"
	JobRestart   = "site.restart"
	JobRecreate  = "site.recreate"
	JobDelete    = "site.delete"
)

const (
	// containerPort is where FrankenPHP listens inside the site container.
	containerPort = 8080
	// wpPath is the WordPress root inside the runtime image.
	wpPath = "/var/www/html"
	// opsUser runs WP-CLI (RUNTIME.md section 8).
	opsUser = "ops"
)

type Service struct {
	cfg      *config.API
	st       *store.Store
	dk       docker.Client
	box      *secrets.Box
	hub      *events.Hub
	jobs     *jobs.Runner
	platform Platform
	edge     *edge.Client
	now      func() time.Time

	// observed holds the live container health per site, fed by the status
	// watcher; it is not persisted.
	observed sync.Map // uuid.UUID -> observedState
}

type observedState struct {
	State  string
	Health string
}

// Platform is what the site lifecycle needs from internal/platform (shared
// database, Redis ACLs, edge config push). *platform.Service implements it;
// tests substitute a fake.
type Platform interface {
	CreateSiteDatabase(ctx context.Context, dbName, user, password string) error
	DropSiteDatabase(ctx context.Context, dbName, user string) error
	ApplyRedisACL(ctx context.Context, user, password, keyPrefix string) error
	DeleteRedisACL(ctx context.Context, user string) error
	ApplyEdge(ctx context.Context) error
	SetEdgeRenderer(platform.EdgeRenderer)
}

// Deps bundles the collaborators.
type Deps struct {
	Config   *config.API
	Store    *store.Store
	Docker   docker.Client
	Secrets  *secrets.Box
	Hub      *events.Hub
	Jobs     *jobs.Runner
	Platform Platform
	Edge     *edge.Client
}

func New(d Deps) *Service {
	s := &Service{
		cfg: d.Config, st: d.Store, dk: d.Docker, box: d.Secrets, hub: d.Hub, jobs: d.Jobs,
		platform: d.Platform, edge: d.Edge, now: func() time.Time { return time.Now().UTC() },
	}
	if s.jobs != nil {
		s.jobs.Register(JobProvision, s.runProvision)
		s.jobs.Register(JobStart, s.runStart)
		s.jobs.Register(JobStop, s.runStop)
		s.jobs.Register(JobRestart, s.runRestart)
		s.jobs.Register(JobRecreate, s.runRecreate)
		s.jobs.Register(JobDelete, s.runDelete)
	}
	if s.platform != nil {
		s.platform.SetEdgeRenderer(s.RenderEdge)
	}
	return s
}

// --- payloads ---

// Site is the API representation.
type Site struct {
	ID            uuid.UUID `json:"id"`
	Slug          string    `json:"slug"`
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	StatusMessage string    `json:"status_message"`
	Health        string    `json:"health"`
	PrimaryDomain string    `json:"primary_domain"`
	URL           string    `json:"url"`
	TLSMode       string    `json:"tls_mode"`
	PHPVersion    string    `json:"php_version"`
	Image         string    `json:"image"`
	Profile       string    `json:"profile"`
	FileMods      string    `json:"file_mods"`
	PageCache     bool      `json:"page_cache"`
	AdminUser     string    `json:"admin_user"`
	AdminEmail    string    `json:"admin_email"`
	Settings      Settings  `json:"settings"`
	Domains       []Domain  `json:"domains"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Domain struct {
	ID   uuid.UUID `json:"id"`
	Host string    `json:"host"`
	Kind string    `json:"kind"`
}

// Payload builds the API view of a site row, including domains and the live
// health.
func (s *Service) Payload(ctx context.Context, row store.Site) (Site, error) {
	domains, err := s.st.ListSiteDomains(ctx, row.ID)
	if err != nil {
		return Site{}, err
	}
	return s.payloadWith(row, domains), nil
}

func (s *Service) payloadWith(row store.Site, domains []store.SiteDomain) Site {
	out := Site{
		ID: row.ID, Slug: row.Slug, Name: row.Name, Status: row.Status, StatusMessage: row.StatusMessage,
		PrimaryDomain: row.PrimaryDomain, URL: siteURL(row.PrimaryDomain, row.TlsMode), TLSMode: row.TlsMode,
		PHPVersion: row.PhpVersion, Image: row.Image, Profile: row.Profile, FileMods: row.FileMods,
		PageCache: row.PageCache, AdminUser: row.AdminUser, AdminEmail: row.AdminEmail,
		Settings: parseSettings(row.Settings), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		Health: "unknown", Domains: []Domain{},
	}
	if v, ok := s.observed.Load(row.ID); ok {
		out.Health = v.(observedState).Health
	} else if row.Status != StatusRunning {
		out.Health = "none"
	}
	for _, d := range domains {
		out.Domains = append(out.Domains, Domain{ID: d.ID, Host: d.Host, Kind: d.Kind})
	}
	return out
}

// List returns every site with domains attached (two queries, not N+1).
func (s *Service) List(ctx context.Context) ([]Site, error) {
	rows, err := s.st.ListSites(ctx)
	if err != nil {
		return nil, err
	}
	domains, err := s.st.ListAllDomains(ctx)
	if err != nil {
		return nil, err
	}
	bySite := map[uuid.UUID][]store.SiteDomain{}
	for _, d := range domains {
		bySite[d.SiteID] = append(bySite[d.SiteID], d)
	}
	out := make([]Site, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.payloadWith(r, bySite[r.ID]))
	}
	return out, nil
}

// Get loads one site row.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (store.Site, error) {
	row, err := s.st.GetSiteByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return store.Site{}, ErrNotFound
	}
	return row, err
}

// ActiveJob returns the queued or running job for the site, if any.
func (s *Service) ActiveJob(ctx context.Context, id uuid.UUID) (*store.Job, error) {
	active, err := s.st.ListActiveJobsBySite(ctx, nullID(id))
	if err != nil || len(active) == 0 {
		return nil, err
	}
	return &active[0], nil
}

// siteURL is the canonical public URL; .localhost never gets TLS because
// browsers resolve it to loopback and treat plain HTTP there as secure.
func siteURL(host, tlsMode string) string {
	if tlsMode == "off" || isLocalhost(host) {
		return "http://" + host
	}
	return "https://" + host
}

func isLocalhost(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

// --- creation ---

// CreateInput is the operator's request. AdminPassword is used once for the
// WordPress install and never stored in plaintext.
type CreateInput struct {
	Name          string
	Slug          string
	PrimaryDomain string
	TLSMode       string
	PHPVersion    string
	PageCache     *bool
	Profile       string
	AdminUser     string
	AdminEmail    string
	AdminPassword string
	Title         string
	CreatedBy     string
}

var (
	slugRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,40}$`)
	loginRe = regexp.MustCompile(`^[a-zA-Z0-9_.@-]{1,60}$`)
)

// Slugify derives a slug from a display name (best effort; the caller can
// override).
func Slugify(name string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case !prevDash:
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func (in *CreateInput) validate(cfg *config.API) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.TrimSpace(strings.ToLower(in.Slug))
	if in.Slug == "" {
		in.Slug = Slugify(in.Name)
	}
	if in.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if !slugRe.MatchString(in.Slug) {
		return fmt.Errorf("%w: slug must be 2-41 chars of a-z, 0-9 and '-', starting with a letter or digit", ErrInvalidInput)
	}
	in.PrimaryDomain = strings.ToLower(strings.TrimSpace(in.PrimaryDomain))
	if in.PrimaryDomain == "" {
		in.PrimaryDomain = in.Slug + ".localhost"
	}
	if !edge.ValidHost(in.PrimaryDomain) {
		return fmt.Errorf("%w: primary_domain is not a valid hostname", ErrInvalidInput)
	}
	switch in.TLSMode {
	case "":
		in.TLSMode = "auto"
	case "auto", "off":
	default:
		return fmt.Errorf("%w: tls_mode must be auto or off", ErrInvalidInput)
	}
	if isLocalhost(in.PrimaryDomain) {
		in.TLSMode = "off"
	}
	if in.PHPVersion == "" {
		in.PHPVersion = cfg.DefaultPHP
	}
	if !containsString(cfg.PHPVersions, in.PHPVersion) {
		return fmt.Errorf("%w: php_version must be one of %v", ErrInvalidInput, cfg.PHPVersions)
	}
	switch in.Profile {
	case "":
		in.Profile = "prod"
	case "prod", "dev":
	default:
		return fmt.Errorf("%w: profile must be prod or dev", ErrInvalidInput)
	}
	in.AdminUser = strings.TrimSpace(in.AdminUser)
	if in.AdminUser == "" {
		in.AdminUser = "admin"
	}
	if !loginRe.MatchString(in.AdminUser) {
		return fmt.Errorf("%w: admin_user contains unsupported characters", ErrInvalidInput)
	}
	in.AdminEmail = strings.TrimSpace(in.AdminEmail)
	if addr, err := mail.ParseAddress(in.AdminEmail); err != nil || addr.Address != in.AdminEmail {
		return fmt.Errorf("%w: admin_email is not a valid address", ErrInvalidInput)
	}
	if len(in.AdminPassword) < 8 {
		return fmt.Errorf("%w: admin_password must be at least 8 characters", ErrInvalidInput)
	}
	if strings.ContainsAny(in.AdminPassword, "\x00\n\r") {
		return fmt.Errorf("%w: admin_password contains unsupported characters", ErrInvalidInput)
	}
	if in.Title == "" {
		in.Title = in.Name
	}
	return nil
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// provisionArgs are stored (JSON) on the provisioning job. The admin
// password travels encrypted so the jobs table never holds it in clear.
type provisionArgs struct {
	Title            string `json:"title"`
	AdminPasswordEnc []byte `json:"admin_password_enc"`
}

// Create validates, records the site and enqueues provisioning.
func (s *Service) Create(ctx context.Context, in CreateInput) (Site, store.Job, error) {
	if err := in.validate(s.cfg); err != nil {
		return Site{}, store.Job{}, err
	}
	if _, err := s.st.GetSiteBySlug(ctx, in.Slug); err == nil {
		return Site{}, store.Job{}, ErrSlugTaken
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Site{}, store.Job{}, err
	}
	if _, err := s.st.GetSiteDomainByHost(ctx, in.PrimaryDomain); err == nil {
		return Site{}, store.Job{}, ErrDomainTaken
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Site{}, store.Job{}, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Site{}, store.Job{}, err
	}
	sec, err := s.newSecrets()
	if err != nil {
		return Site{}, store.Job{}, err
	}
	pageCache := true
	if in.PageCache != nil {
		pageCache = *in.PageCache
	}
	now := s.now()
	shortID := strings.ReplaceAll(id.String(), "-", "")[:6]
	dbIdent := "wp_" + strings.ReplaceAll(in.Slug, "-", "_") + "_" + shortID

	var row store.Site
	err = s.st.WithTx(ctx, func(q *store.Queries) error {
		var err error
		row, err = q.CreateSite(ctx, store.CreateSiteParams{
			ID: id, Slug: in.Slug, Name: in.Name, Status: StatusProvisioning, StatusMessage: "queued",
			PrimaryDomain: in.PrimaryDomain, TlsMode: in.TLSMode, PhpVersion: in.PHPVersion,
			Image: s.cfg.RuntimeImage(in.PHPVersion), Profile: in.Profile, FileMods: "locked", PageCache: pageCache,
			DbName: dbIdent, DbUser: dbIdent, DbPasswordEnc: sec.dbPasswordEnc,
			RedisUser: "site-" + id.String(), RedisPasswordEnc: sec.redisPasswordEnc,
			SaltsEnc: sec.saltsEnc, PurgeTokenHash: sec.purgeTokenHash, LoginSecretEnc: sec.loginSecretEnc,
			AdminUser: in.AdminUser, AdminEmail: in.AdminEmail, Settings: DefaultSettings().encode(),
			VolumeName: "presso-site-" + in.Slug + "-content", CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			if store.IsUniqueViolation(err) {
				return ErrSlugTaken
			}
			return err
		}
		did, _ := uuid.NewV7()
		_, err = q.CreateSiteDomain(ctx, store.CreateSiteDomainParams{ID: did, SiteID: id, Host: in.PrimaryDomain, Kind: "primary", CreatedAt: now})
		if store.IsUniqueViolation(err) {
			return ErrDomainTaken
		}
		return err
	})
	if err != nil {
		return Site{}, store.Job{}, err
	}

	pwEnc, err := s.box.EncryptString(in.AdminPassword)
	if err != nil {
		return Site{}, store.Job{}, err
	}
	job, err := s.jobs.Enqueue(ctx, JobProvision, nullID(id), provisionArgs{Title: in.Title, AdminPasswordEnc: pwEnc}, in.CreatedBy)
	if err != nil {
		return Site{}, store.Job{}, err
	}
	s.logActivity(ctx, in.CreatedBy, id, "site.created", fmt.Sprintf("Site %s (%s) created", in.Name, in.Slug), nil)
	payload, err := s.Payload(ctx, row)
	if err != nil {
		return Site{}, store.Job{}, err
	}
	s.publish(payload)
	return payload, job, nil
}

// --- secrets ---

type siteSecrets struct {
	dbPasswordEnc, redisPasswordEnc, saltsEnc, loginSecretEnc []byte
	purgeTokenHash                                            []byte
}

var saltNames = []string{"AUTH_KEY", "SECURE_AUTH_KEY", "LOGGED_IN_KEY", "NONCE_KEY", "AUTH_SALT", "SECURE_AUTH_SALT", "LOGGED_IN_SALT", "NONCE_SALT"}

func (s *Service) newSecrets() (siteSecrets, error) {
	var out siteSecrets
	enc := func(v string) ([]byte, error) { return s.box.EncryptString(v) }
	var err error
	dbPw, _ := utils.RandomToken(24)
	if out.dbPasswordEnc, err = enc(dbPw); err != nil {
		return out, err
	}
	redisPw, _ := utils.RandomToken(24)
	if out.redisPasswordEnc, err = enc(redisPw); err != nil {
		return out, err
	}
	salts := map[string]string{}
	for _, n := range saltNames {
		v, err := utils.RandomToken(48)
		if err != nil {
			return out, err
		}
		salts[n] = v
	}
	sj, _ := json.Marshal(salts)
	if out.saltsEnc, err = s.box.Encrypt(sj); err != nil {
		return out, err
	}
	loginSecret, err := utils.RandomToken(32)
	if err != nil {
		return out, err
	}
	if out.loginSecretEnc, err = enc(loginSecret); err != nil {
		return out, err
	}
	out.purgeTokenHash = hashToken(purgeTokenFor(loginSecret))
	return out, nil
}

// --- misc helpers ---

func nullID(id uuid.UUID) uuid.NullUUID { return uuid.NullUUID{UUID: id, Valid: true} }

func (s *Service) publish(site Site) {
	if s.hub != nil {
		s.hub.Publish(events.TypeSiteStatus, site)
	}
}

// setStatus persists and broadcasts a status change.
func (s *Service) setStatus(ctx context.Context, id uuid.UUID, status, message string) {
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.st.SetSiteStatus(wctx, store.SetSiteStatusParams{Status: status, StatusMessage: message, UpdatedAt: s.now(), ID: id}); err != nil {
		return
	}
	if row, err := s.st.GetSiteByID(wctx, id); err == nil {
		if p, err := s.Payload(wctx, row); err == nil {
			s.publish(p)
		}
	}
}

// logActivity records an audit entry (best effort).
func (s *Service) logActivity(ctx context.Context, userID string, siteID uuid.UUID, kind, message string, meta any) {
	id, _ := uuid.NewV7()
	metaJSON := "{}"
	if meta != nil {
		if b, err := json.Marshal(meta); err == nil {
			metaJSON = string(b)
		}
	}
	var uid uuid.NullUUID
	if u, err := uuid.Parse(userID); err == nil {
		uid = nullID(u)
	}
	var sid uuid.NullUUID
	if siteID != uuid.Nil {
		sid = nullID(siteID)
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, _ = s.st.CreateActivity(wctx, store.CreateActivityParams{ID: id, Ts: s.now(), UserID: uid, SiteID: sid, Kind: kind, Message: message, Meta: metaJSON})
}
