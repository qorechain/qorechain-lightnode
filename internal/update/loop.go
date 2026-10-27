package update

import (
	"context"
	"log/slog"
	"math/rand" // nosemgrep: go.lang.security.audit.crypto.math_random.math-random-used -- start-up jitter only, nothing secret
	"sync"
	"time"
)

// Status is what the node knows about releases, for the dashboard and QoreX.
type Status struct {
	Network         string    `json:"network"`
	Mode            string    `json:"mode"`
	Current         string    `json:"current"`
	Latest          string    `json:"latest,omitempty"`
	MinCompatible   string    `json:"min_compatible,omitempty"`
	Notes           string    `json:"notes,omitempty"`
	UpdateAvailable bool      `json:"update_available"`
	Mandatory       bool      `json:"mandatory"` // this version is below the release's minCompatible
	CanInstall      bool      `json:"can_install"`
	Blocked         string    `json:"blocked,omitempty"` // why it only announces
	LastCheck       time.Time `json:"last_check,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
}

var (
	mu     sync.Mutex
	status Status
)

// CurrentStatus is a copy of the latest status.
func CurrentStatus() Status {
	mu.Lock()
	defer mu.Unlock()
	return status
}

func setStatus(f func(*Status)) {
	mu.Lock()
	defer mu.Unlock()
	f(&status)
}

// Intervals. Variables so tests can shorten them.
var (
	firstCheck = 2 * time.Minute
	every      = 6 * time.Hour
	retry      = time.Hour
)

// Loop checks the channel until ctx ends. With mode "auto" and a binary it may
// replace, it installs a newer signed release and calls restart with the path
// to exec; restart is expected to stop the daemon and not return.
func Loop(ctx context.Context, u *Updater, mode string, logger *slog.Logger, restart func(exe string)) {
	if mode == "" {
		mode = "auto"
	}
	setStatus(func(s *Status) { *s = Status{Network: u.Network, Mode: mode, Current: u.Current} })
	if mode == "off" {
		return
	}
	if u.Network == "" {
		setStatus(func(s *Status) { s.Blocked = "no release channel for this chain" })
		return
	}
	exe, err := Executable()
	can, why := false, "cannot locate the running binary"
	if err == nil {
		can, why = CanReplace(exe)
	}
	if mode != "auto" {
		can, why = false, "mode is "+mode
	}
	setStatus(func(s *Status) { s.CanInstall, s.Blocked = can, why })

	announced := ""
	wait := firstCheck + time.Duration(rand.Int63n(int64(firstCheck)+1))
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = every

		m, err := u.Check(ctx)
		now := time.Now().UTC()
		if err != nil {
			setStatus(func(s *Status) { s.LastCheck, s.LastError = now, err.Error() })
			logger.Debug("release check failed", "error", err)
			continue
		}
		newer := Newer(m.Version, u.Current)
		mandatory := Newer(m.MinCompatible, u.Current)
		setStatus(func(s *Status) {
			s.LastCheck, s.LastError = now, ""
			s.Latest, s.MinCompatible, s.Notes = m.Version, m.MinCompatible, m.Notes
			s.UpdateAvailable, s.Mandatory = newer, mandatory
		})
		if !newer {
			continue
		}
		switch {
		case mandatory:
			logger.Error("this light node version is no longer supported; update now",
				"current", u.Current, "latest", m.Version, "min_compatible", m.MinCompatible)
		case announced != m.Version:
			logger.Warn("a new light node version is available", "current", u.Current, "latest", m.Version, "notes", m.Notes)
		}
		announced = m.Version
		if !can {
			if why != "mode is notify" {
				logger.Info("not installing it automatically", "reason", why)
			}
			continue
		}

		staged, err := u.Download(ctx, m.Version, exe)
		if err == nil {
			err = Swap(exe, staged)
		}
		if err != nil {
			setStatus(func(s *Status) { s.LastError = err.Error() })
			logger.Warn("automatic update failed; this version keeps running", "latest", m.Version, "error", err)
			wait = retry
			continue
		}
		logger.Info("installed the new light node version; restarting", "from", u.Current, "to", m.Version, "previous_kept_as", exe+".old")
		restart(exe)
		return
	}
}
