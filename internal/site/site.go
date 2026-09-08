// Package site is the engine's site model and lifecycle: validation and
// creation of site records, the provisioning / start / stop / recreate /
// delete jobs, WP-CLI execution, one-click login links, cache purges and the
// edge route list. It composes internal/docker (containers),
// internal/platform (shared DB/Valkey/edge) and internal/jobs (background
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

	"github.com/Hinkolas/presso/internal/compat"
	"github.com/Hinkolas/presso/internal/config"
	"github.com/Hinkolas/presso/internal/docker"
	"github.com/Hinkolas/presso/internal/edge"
	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/jobs"
	"github.com/Hinkolas/presso/internal/platform"
	"github.com/Hinkolas/presso/internal/secrets"
	"github.com/Hinkolas/presso/internal/shots"
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
	commands sync.Mutex
	cfg      *config.API
	catalog  *compat.Catalog
	st       *store.Store
	dk       docker.Client
	box      *secrets.Box
	hub      *events.Hub
	jobs     *jobs.Runner
	platform Platform
	edge     *edge.Client
	shots    *shots.Service // nil disables previews
	warmer   Warmer         // nil disables the cache warmer
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
// database, Valkey ACLs, edge config push). *platform.Service implements it;
// tests substitute a fake.
type Platform interface {
	CreateSiteDatabase(ctx context.Context, dbName, user, password string) error
	DropSiteDatabase(ctx context.Context, dbName, user string) error
	ApplyCacheACL(ctx context.Context, user, password, keyPrefix string) error
	DeleteCacheACL(ctx context.Context, user string) error
	ApplyEdge(ctx context.Context) error
	SetEdgeRenderer(platform.EdgeRenderer)
}

// Warmer is what the site lifecycle needs from internal/warm. nil disables it.
type Warmer interface {
	// Trigger schedules a debounced warm run for the site; the warmer checks
	// eligibility (running, page cache on, opted in) itself.
	Trigger(id uuid.UUID)
	// Fetch requests one URL through the edge exactly as a visitor would.
	Fetch(ctx context.Context, url string) (status int, cacheStatus string, err error)
}

// Deps bundles the collaborators.
type Deps struct {
	Config *config.API
	// Catalog resolves WordPress/PHP choices to runtime images (required).
	Catalog  *compat.Catalog
	Store    *store.Store
	Docker   docker.Client
	Secrets  *secrets.Box
	Hub      *events.Hub
	Jobs     *jobs.Runner
	Platform Platform
	Edge     *edge.Client
	// Shots captures front-page previews; nil disables them.
	Shots *shots.Service
	// Warmer keeps opted-in sites' page caches warm; nil disables it.
	Warmer Warmer
}

func New(d Deps) *Service {
	if d.Catalog == nil {
		panic("site.New: Deps.Catalog is required")
	}
	s := &Service{
		cfg: d.Config, catalog: d.Catalog, st: d.Store, dk: d.Docker, box: d.Secrets, hub: d.Hub, jobs: d.Jobs,
		platform: d.Platform, edge: d.Edge, shots: d.Shots, warmer: d.Warmer, now: func() time.Time { return time.Now().UTC() },
	}
	if s.jobs != nil {
		// The plans mirror the Reporter.Step calls in jobs.go; the UI draws
		// its progress bar and "still to do" list from them.
		image := jobs.Step{Name: "image", Label: "Runtime image"}
		container := jobs.Step{Name: "container", Label: "Container"}
		health := jobs.Step{Name: "health", Label: "Health check"}
		unavailable := jobs.Step{Name: "unavailable", Label: "Disable traffic"}
		validate := jobs.Step{Name: "validate", Label: "Validate WordPress"}
		publish := jobs.Step{Name: "publish", Label: "Publish traffic"}
		preview := jobs.Step{Name: "preview", Label: "Preview"}
		s.jobs.Register(JobProvision, s.runProvision,
			image,
			jobs.Step{Name: "database", Label: "Database"},
			jobs.Step{Name: "valkey", Label: "Object cache"},
			jobs.Step{Name: "volume", Label: "Files volume"},
			container, health,
			jobs.Step{Name: "wordpress", Label: "WordPress install"},
			validate, publish,
			jobs.Step{Name: "warm", Label: "Warm-up"},
			preview,
		)
		applyPlan := []jobs.Step{image, unavailable, container, health, {Name: "wordpress", Label: "Database upgrade"}, validate, publish, preview}
		s.jobs.Register(JobStart, s.runStart, applyPlan...)
		s.jobs.Register(JobRestart, s.runRestart, applyPlan...)
		s.jobs.Register(JobRecreate, s.runRecreate, applyPlan...)
		s.jobs.Register(JobStop, s.runStop, unavailable, jobs.Step{Name: "stop", Label: "Stop container"})
		s.jobs.Register(JobDelete, s.runDelete,
			unavailable, container,
			jobs.Step{Name: "database", Label: "Database"},
			jobs.Step{Name: "valkey", Label: "Object cache"},
			jobs.Step{Name: "volume", Label: "Files volume"},
			jobs.Step{Name: "record", Label: "Site record"},
		)
	}
	if s.platform != nil {
		s.platform.SetEdgeRenderer(s.RenderEdge)
	}
	return s
}

// Catalog is the runtime catalog the service validates against.
func (s *Service) Catalog() *compat.Catalog { return s.catalog }

// --- payloads ---

// Site is the API representation.
type Site struct {
	DesiredState         string    `json:"desired_state"`
	TrafficReady         bool      `json:"traffic_ready"`
	ConfigRevision       int64     `json:"config_revision"`
	AppliedRevision      int64     `json:"applied_revision"`
	AppliedImage         string    `json:"applied_image"`
	ConfigurationPending bool      `json:"configuration_pending"`
	ID                   uuid.UUID `json:"id"`
	Slug                 string    `json:"slug"`
	Name                 string    `json:"name"`
	Status               string    `json:"status"`
	StatusMessage        string    `json:"status_message"`
	Health               string    `json:"health"`
	PrimaryDomain        string    `json:"primary_domain"`
	URL                  string    `json:"url"`
	TLSMode              string    `json:"tls_mode"`
	PHPVersion           string    `json:"php_version"`
	// WPVersion is the WordPress release the site runs ("" until detected
	// for sites created before versions were tracked); Runtime is
	// frankenphp or legacy (nginx + PHP-FPM).
	WPVersion  string    `json:"wp_version"`
	Runtime    string    `json:"runtime"`
	Image      string    `json:"image"`
	Profile    string    `json:"profile"`
	FileMods   string    `json:"file_mods"`
	PageCache  bool      `json:"page_cache"`
	AdminUser  string    `json:"admin_user"`
	AdminEmail string    `json:"admin_email"`
	Settings   Settings  `json:"settings"`
	Domains    []Domain  `json:"domains"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	// ScreenshotAt is when the front-page preview was last captured; null
	// when none exists yet (GET /v1/sites/{id}/screenshot serves it).
	ScreenshotAt *time.Time `json:"screenshot_at"`
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
		DesiredState: row.DesiredState, TrafficReady: row.TrafficReady, ConfigRevision: row.ConfigRevision, AppliedRevision: row.AppliedRevision, AppliedImage: row.AppliedImage, ConfigurationPending: row.ConfigRevision != row.AppliedRevision || row.Image != row.AppliedImage || (!row.TrafficReady && row.DesiredState == StatusRunning),
		ID: row.ID, Slug: row.Slug, Name: row.Name, Status: row.Status, StatusMessage: row.StatusMessage,
		PrimaryDomain: row.PrimaryDomain, URL: siteURL(row.PrimaryDomain, row.TlsMode), TLSMode: row.TlsMode,
		PHPVersion: row.PhpVersion, WPVersion: row.WpVersion, Runtime: row.Runtime, Image: row.Image, Profile: row.Profile, FileMods: row.FileMods,
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
	if at, ok := s.shots.Stat(row.ID); ok {
		out.ScreenshotAt = &at
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

// SiteURL is siteURL for other packages that hold a store.Site.
func SiteURL(host, tlsMode string) string { return siteURL(host, tlsMode) }

func isLocalhost(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

// --- creation ---

// CreateInput is the operator's request. AdminPassword is used once for the
// WordPress install and never stored in plaintext. WPVersion is a branch
// ("6.8") or release ("6.8.8"), empty for the server default; PHPVersion is
// empty for the newest PHP that release supports.
type CreateInput struct {
	Name          string
	Slug          string
	PrimaryDomain string
	TLSMode       string
	WPVersion     string
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

// validate normalizes the input and resolves the WordPress/PHP choice against
// the catalog. compat's sentinel errors pass through unwrapped so the API can
// report them by code.
func (in *CreateInput) validate(cat *compat.Catalog) (compat.Resolution, error) {
	var res compat.Resolution
	in.Name = strings.TrimSpace(in.Name)
	in.Slug = strings.TrimSpace(strings.ToLower(in.Slug))
	if in.Slug == "" {
		in.Slug = Slugify(in.Name)
	}
	if in.Name == "" {
		return res, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if !slugRe.MatchString(in.Slug) {
		return res, fmt.Errorf("%w: slug must be 2-41 chars of a-z, 0-9 and '-', starting with a letter or digit", ErrInvalidInput)
	}
	in.PrimaryDomain = strings.ToLower(strings.TrimSpace(in.PrimaryDomain))
	if in.PrimaryDomain == "" {
		in.PrimaryDomain = in.Slug + ".localhost"
	}
	if !edge.ValidHost(in.PrimaryDomain) {
		return res, fmt.Errorf("%w: primary_domain is not a valid hostname", ErrInvalidInput)
	}
	switch in.TLSMode {
	case "":
		in.TLSMode = "auto"
	case "auto", "off":
	default:
		return res, fmt.Errorf("%w: tls_mode must be auto or off", ErrInvalidInput)
	}
	if isLocalhost(in.PrimaryDomain) {
		in.TLSMode = "off"
	}
	var err error
	if res, err = cat.Resolve(in.WPVersion, in.PHPVersion); err != nil {
		return res, err
	}
	in.WPVersion, in.PHPVersion = res.WPVersion, res.PHP
	switch in.Profile {
	case "":
		in.Profile = "prod"
	case "prod", "dev":
	default:
		return res, fmt.Errorf("%w: profile must be prod or dev", ErrInvalidInput)
	}
	in.AdminUser = strings.TrimSpace(in.AdminUser)
	if in.AdminUser == "" {
		in.AdminUser = "admin"
	}
	if !loginRe.MatchString(in.AdminUser) {
		return res, fmt.Errorf("%w: admin_user contains unsupported characters", ErrInvalidInput)
	}
	in.AdminEmail = strings.TrimSpace(in.AdminEmail)
	if addr, err := mail.ParseAddress(in.AdminEmail); err != nil || addr.Address != in.AdminEmail {
		return res, fmt.Errorf("%w: admin_email is not a valid address", ErrInvalidInput)
	}
	if len(in.AdminPassword) < 8 {
		return res, fmt.Errorf("%w: admin_password must be at least 8 characters", ErrInvalidInput)
	}
	if strings.ContainsAny(in.AdminPassword, "\x00\n\r") {
		return res, fmt.Errorf("%w: admin_password contains unsupported characters", ErrInvalidInput)
	}
	if in.Title == "" {
		in.Title = in.Name
	}
	return res, nil
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
	s.commands.Lock()
	defer s.commands.Unlock()
	if s.jobs.Paused() {
		return Site{}, store.Job{}, jobs.ErrPaused
	}
	res, err := in.validate(s.catalog)
	if err != nil {
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
			PrimaryDomain: in.PrimaryDomain, TlsMode: in.TLSMode, PhpVersion: res.PHP, WpVersion: res.WPVersion,
			Runtime: res.Runtime, Image: res.Image, Profile: in.Profile, FileMods: "locked", PageCache: pageCache,
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
	s.logActivity(ctx, in.CreatedBy, id, "site.created", fmt.Sprintf("Site %s (%s) created: WordPress %s on PHP %s (%s)", in.Name, in.Slug, res.WPVersion, res.PHP, res.Runtime), nil)
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
