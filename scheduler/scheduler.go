// Package scheduler implements the 15+ background schedulers from P12.
// Each scheduler implements the Scheduler interface and is started/stopped by
// the registry on app startup/shutdown.
package scheduler

import (
	"context"
	"log/slog"
)

// Scheduler is the interface for all background schedulers.
type Scheduler interface {
	Name() string
	Start(ctx context.Context) error
	Stop() error
}

// Registry manages all registered schedulers.
type Registry struct {
	schedulers []Scheduler
}

// NewRegistry creates a new empty scheduler registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Register adds a scheduler to the registry.
func (r *Registry) Register(s Scheduler) {
	r.schedulers = append(r.schedulers, s)
}

// StartAll starts all registered schedulers. Each runs in its own goroutine
// with panic recovery so a single scheduler panic does not affect others.
func (r *Registry) StartAll(ctx context.Context) {
	for _, s := range r.schedulers {
		go func(s Scheduler) {
			defer func() {
				if rec := recover(); rec != nil {
					slog.Error("scheduler panicked during start",
						"name", s.Name(),
						"panic", rec,
					)
				}
			}()
			if err := s.Start(ctx); err != nil {
				slog.Warn("scheduler start failed",
					"name", s.Name(),
					"error", err,
				)
			}
		}(s)
	}
}

// StopAll stops all registered schedulers, logging errors but continuing
// through all schedulers.
func (r *Registry) StopAll() {
	for _, s := range r.schedulers {
		if err := s.Stop(); err != nil {
			slog.Warn("scheduler stop failed",
				"name", s.Name(),
				"error", err,
			)
		}
	}
}

// List returns the names of all registered schedulers.
func (r *Registry) List() []string {
	names := make([]string, len(r.schedulers))
	for i, s := range r.schedulers {
		names[i] = s.Name()
	}
	return names
}
