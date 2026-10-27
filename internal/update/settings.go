package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Hinkolas/presso/internal/events"
	"github.com/Hinkolas/presso/internal/store"
	"github.com/Hinkolas/presso/internal/version"
)

// Release channels. Stable offers only normal releases; beta also offers
// alpha, beta and rc releases, so a beta host always gets the highest
// release, stable or not.
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

// ErrInvalidSettings rejects an unknown channel.
var ErrInvalidSettings = errors.New("update: invalid settings")

// Settings are the admin's choices, kept in platform_kv.
type Settings struct {
	Channel string `json:"channel"`
	// AutoUpdate starts the update on its own when the background check finds
	// a newer release on the channel. Off by default.
	AutoUpdate bool `json:"auto_update"`
}

// SettingsPatch changes the fields that are set.
type SettingsPatch struct {
	Channel    *string `json:"channel"`
	AutoUpdate *bool   `json:"auto_update"`
}

// defaultSettings applies until the admin saves settings: the channel from
// UPDATE_CHANNEL (install.sh --channel), else the running build's, so a host
// installed from a prerelease stays on beta.
func (s *Service) defaultSettings() Settings {
	if s.cfg != nil && validChannel(s.cfg.Update.Channel) {
		return Settings{Channel: s.cfg.Update.Channel}
	}
	if version.IsPrerelease(version.Version) {
		return Settings{Channel: ChannelBeta}
	}
	return Settings{Channel: ChannelStable}
}

// settings returns the stored settings, loading them once.
func (s *Service) settings(ctx context.Context) Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settingsLoaded {
		return s.prefs
	}
	s.prefs = s.defaultSettings()
	if raw, err := s.st.GetKV(ctx, kvSettings); err == nil && len(raw) > 0 {
		var stored Settings
		if json.Unmarshal(raw, &stored) == nil && validChannel(stored.Channel) {
			s.prefs = stored
		}
	}
	s.settingsLoaded = true
	return s.prefs
}

func validChannel(c string) bool { return c == ChannelStable || c == ChannelBeta }

// UpdateSettings applies a patch. A channel change discards the previous
// channel's result and checks the new one right away; a failed check is
// reported in the returned status, not as an error.
func (s *Service) UpdateSettings(ctx context.Context, p SettingsPatch) (Status, error) {
	cur := s.settings(ctx)
	next := cur
	if p.Channel != nil {
		if !validChannel(*p.Channel) {
			return Status{}, fmt.Errorf("%w: channel must be %q or %q", ErrInvalidSettings, ChannelStable, ChannelBeta)
		}
		next.Channel = *p.Channel
	}
	if p.AutoUpdate != nil {
		next.AutoUpdate = *p.AutoUpdate
	}
	b, err := json.Marshal(next)
	if err != nil {
		return Status{}, err
	}
	if err := s.st.SetKV(ctx, store.SetKVParams{Key: kvSettings, Value: b, UpdatedAt: s.now()}); err != nil {
		return Status{}, err
	}
	s.mu.Lock()
	s.prefs, s.settingsLoaded = next, true
	if next.Channel != cur.Channel {
		s.latest, s.checkErr, s.checkKind = nil, "", ""
	}
	s.mu.Unlock()
	slog.InfoContext(ctx, "update: settings changed", "channel", next.Channel, "auto_update", next.AutoUpdate)

	if next.Channel != cur.Channel {
		if _, err := s.Check(ctx, true); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "update: check after channel change", "err", err)
		}
	}
	status := s.Status(ctx)
	if s.hub != nil {
		s.hub.Publish(events.TypeSystemUpdate, status)
	}
	return status, nil
}

// autoUpdate starts the update the background check found, once per
// release: a failed automatic update is not retried every interval, the
// admin starts it again by hand.
func (s *Service) autoUpdate(ctx context.Context, status Status) {
	if !s.settings(ctx).AutoUpdate || !status.Available || !status.Supported || status.JobID != nil {
		return
	}
	if tried, err := s.st.GetKV(ctx, kvAutoAttempt); err == nil && string(tried) == status.Latest {
		return
	}
	if err := s.st.SetKV(ctx, store.SetKVParams{Key: kvAutoAttempt, Value: []byte(status.Latest), UpdatedAt: s.now()}); err != nil {
		slog.WarnContext(ctx, "update: record automatic update", "err", err)
		return
	}
	job, err := s.StartUpdate(ctx, createdBySystem)
	if err != nil {
		slog.WarnContext(ctx, "update: automatic update", "to", status.Latest, "err", err)
		return
	}
	slog.InfoContext(ctx, "update: automatic update started", "to", status.Latest, "job", job.ID)
}
