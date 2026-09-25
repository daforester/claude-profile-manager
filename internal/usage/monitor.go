package usage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"claude-profile-manager/internal/profile"
)

// Monitor polls usage for every profile that has a Claude Code login.
type Monitor struct {
	root     string
	store    *profile.Store
	interval func() time.Duration
	onUpdate func(profileID string, u Usage)

	mu      sync.Mutex
	data    map[string]Usage
	nextDue map[string]time.Time
	backoff map[string]time.Duration
	wake    chan string // profile ID to refresh now ("" = all)
}

// NewMonitor creates a monitor; onUpdate is called from a background
// goroutine whenever a profile's snapshot changes.
func NewMonitor(root string, store *profile.Store, interval func() time.Duration, onUpdate func(string, Usage)) *Monitor {
	m := &Monitor{
		root: root, store: store, interval: interval, onUpdate: onUpdate,
		data: map[string]Usage{}, nextDue: map[string]time.Time{}, backoff: map[string]time.Duration{},
		wake: make(chan string, 16),
	}
	m.loadCache()
	return m
}

// Get returns the latest snapshot for a profile.
func (m *Monitor) Get(id string) Usage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[id]
}

// Refresh asks for an immediate refresh of one profile ("" = all).
func (m *Monitor) Refresh(id string) {
	select {
	case m.wake <- id:
	default:
	}
}

// Run polls until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	m.pollDue(ctx, "")
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-m.wake:
			m.mu.Lock()
			for pid := range m.nextDue {
				if id == "" || id == pid {
					delete(m.nextDue, pid)
				}
			}
			m.mu.Unlock()
			m.pollDue(ctx, id)
		case <-tick.C:
			m.pollDue(ctx, "")
		}
	}
}

func (m *Monitor) pollDue(ctx context.Context, only string) {
	now := time.Now()
	for _, p := range m.store.List() {
		if only != "" && p.ID != only {
			continue
		}
		m.mu.Lock()
		due := m.nextDue[p.ID]
		m.mu.Unlock()
		if now.Before(due) {
			continue
		}
		m.pollOne(ctx, p)
	}
}

func (m *Monitor) pollOne(ctx context.Context, p *profile.Profile) {
	prev := m.Get(p.ID)
	next := prev
	interval := m.interval()
	delay := interval

	creds, err := ReadCredentials(p)
	switch {
	case errors.Is(err, ErrNoLogin):
		next = Usage{NoLogin: true}
	case err != nil:
		next.Err = err.Error()
	default:
		cctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		u, ferr := Fetch(cctx, creds.AccessToken)
		cancel()
		var rl *RateLimitError
		switch {
		case ferr == nil:
			u.Plan = creds.SubscriptionType
			next = u
			m.mu.Lock()
			delete(m.backoff, p.ID)
			m.mu.Unlock()
		case errors.As(ferr, &rl):
			m.mu.Lock()
			b := m.backoff[p.ID]*2 + time.Minute
			if b < interval {
				b = interval * 2
			}
			if b > 30*time.Minute {
				b = 30 * time.Minute
			}
			if rl.RetryAfter > b {
				b = rl.RetryAfter
			}
			m.backoff[p.ID] = b
			m.mu.Unlock()
			delay = b
			next.Err = ferr.Error()
		default:
			if !creds.ExpiresAt.IsZero() && time.Now().After(creds.ExpiresAt) {
				next.Err = "login token expired — run Claude Code in this profile to refresh it"
			} else {
				next.Err = ferr.Error()
			}
			next.NoLogin = false
		}
	}

	m.mu.Lock()
	m.data[p.ID] = next
	m.nextDue[p.ID] = time.Now().Add(delay)
	m.mu.Unlock()
	m.saveCache()
	if m.onUpdate != nil {
		m.onUpdate(p.ID, next)
	}
}

func (m *Monitor) cachePath() string { return filepath.Join(m.root, "usage-cache.json") }

func (m *Monitor) loadCache() {
	b, err := os.ReadFile(m.cachePath())
	if err != nil {
		return
	}
	var d map[string]Usage
	if json.Unmarshal(b, &d) == nil {
		m.data = d
	}
}

func (m *Monitor) saveCache() {
	m.mu.Lock()
	b, err := json.Marshal(m.data)
	m.mu.Unlock()
	if err == nil {
		_ = os.WriteFile(m.cachePath(), b, 0o600)
	}
}
