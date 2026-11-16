package state

import (
	"context"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
)

// Registry owns one Presence state machine per Discord account. Coalescing and
// diff detection stay inside each Presence, so two accounts can never merge
// into one update or suppress each other's activity.
type Registry struct {
	interval time.Duration
	logger   *zap.Logger

	mu        sync.Mutex
	presences map[string]*Presence
	ctx       context.Context
	started   bool
	stopped   bool
}

// NewRegistry returns an empty per-account registry. interval is the
// per-account coalesce window.
func NewRegistry(interval time.Duration, logger *zap.Logger) *Registry {
	return &Registry{
		interval:  interval,
		logger:    logger,
		presences: make(map[string]*Presence),
	}
}

// Start starts the registry and every presence registered so far. Presences
// registered later start immediately.
func (r *Registry) Start(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.ctx = ctx
	r.started = true
	r.stopped = false
	for _, presence := range r.presences {
		presence.Start(ctx)
	}
}

// For returns the presence state machine for account, creating and starting it
// on first use.
func (r *Registry) For(account string) *Presence {
	r.mu.Lock()
	defer r.mu.Unlock()

	if presence, ok := r.presences[account]; ok {
		return presence
	}

	presence := NewPresence(r.interval, r.logger)
	r.presences[account] = presence
	if r.started && !r.stopped {
		presence.Start(r.ctx)
	}
	return presence
}

// Snapshot returns the account's current presence snapshot. An unknown account
// reports false so callers can answer 404 instead of inventing an empty state.
func (r *Registry) Snapshot(account string) (types.PresenceSnapshot, bool) {
	r.mu.Lock()
	presence, ok := r.presences[account]
	r.mu.Unlock()
	if !ok {
		return types.PresenceSnapshot{}, false
	}
	return presence.Snapshot(), true
}

// Accounts lists the registered account identities in lexical order.
func (r *Registry) Accounts() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	accounts := make([]string, 0, len(r.presences))
	for account := range r.presences {
		accounts = append(accounts, account)
	}
	sort.Strings(accounts)
	return accounts
}

// Stop stops every presence. It is idempotent.
func (r *Registry) Stop() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	presences := make([]*Presence, 0, len(r.presences))
	for _, presence := range r.presences {
		presences = append(presences, presence)
	}
	r.mu.Unlock()

	for _, presence := range presences {
		presence.Stop()
	}
}
