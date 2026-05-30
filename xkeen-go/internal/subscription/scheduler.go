package subscription

import (
	"context"
	"log"
	"sync"
	"time"
)

// Scheduler handles periodic subscription refresh.
type Scheduler struct {
	mu      sync.RWMutex
	store   *Store
	fetcher *Fetcher
	stopCh  chan struct{}
	stopped bool
	wg      sync.WaitGroup

	// OnUpdate is called after a successful fetch cycle.
	// Used to notify handlers/UI that proxy list changed.
	OnUpdate func()
}

// NewScheduler creates a new scheduler. Call Start() to begin.
func NewScheduler(store *Store, fetcher *Fetcher) *Scheduler {
	return &Scheduler{
		store:   store,
		fetcher: fetcher,
		stopCh:  make(chan struct{}),
		stopped: false,
	}
}

// Start begins the scheduler loop. Safe to call multiple times;
// subsequent calls are no-ops.
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.stopCh != nil && !s.stopped {
		// Already running
		s.mu.Unlock()
		return
	}
	s.stopCh = make(chan struct{})
	s.stopped = false
	s.mu.Unlock()

	s.wg.Add(1)
	go s.loop()
	log.Println("[subscription] scheduler started")
}

// Stop gracefully stops the scheduler. Safe to call multiple times.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	close(s.stopCh)
	s.mu.Unlock()

	s.wg.Wait()
	log.Println("[subscription] scheduler stopped")
}

// loop is the main ticker goroutine. It checks every minute which
// subscriptions need refresh.
func (s *Scheduler) loop() {
	defer s.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Check immediately on start.
	s.checkAndRefresh()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.checkAndRefresh()
		}
	}
}

// checkAndRefresh iterates enabled subscriptions and refreshes those
// whose interval has elapsed.
func (s *Scheduler) checkAndRefresh() {
	cfg := s.store.GetConfig()
	now := time.Now()

	needsUpdate := false
	for _, sub := range cfg.Subscriptions {
		if !sub.Enabled || sub.Interval <= 0 {
			continue
		}
		nextRefresh := sub.LastFetch.Add(time.Duration(sub.Interval) * time.Minute)
		if now.After(nextRefresh) || sub.LastFetch.IsZero() {
			log.Printf("[subscription] auto-refreshing %q (%s)", sub.Name, sub.ID)
			if err := s.RefreshOne(sub.ID); err != nil {
				log.Printf("[subscription] auto-refresh failed for %q: %v", sub.Name, err)
			} else {
				needsUpdate = true
			}
		}
	}

	if needsUpdate && s.OnUpdate != nil {
		s.OnUpdate()
	}
}

// RefreshAll fetches all enabled subscriptions and updates the store's proxy cache.
func (s *Scheduler) RefreshAll() error {
	cfg := s.store.GetConfig()

	var merged []*ProxyEntry
	anySuccess := false

	for _, sub := range cfg.Subscriptions {
		if !sub.Enabled {
			continue
		}

		// Fetch raw entries
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		entries, err := s.fetcher.Fetch(ctx, sub.URL)
		cancel()
		if err != nil {
			log.Printf("[subscription] refresh failed for %q (%s): %v", sub.Name, sub.ID, err)
			sub.LastError = err.Error()
			sub.LastFetch = time.Now()
			_ = s.store.UpdateSubscription(&sub)
			continue
		}

		// Apply current filters
		filters := s.store.GetFilters()
		filtered := ApplyFilter(entries, filters)

		sub.LastFetch = time.Now()
		sub.LastError = ""
		sub.ProxyCount = len(filtered)
		_ = s.store.UpdateSubscription(&sub)

		merged = append(merged, filtered...)
		anySuccess = true
	}

	if anySuccess {
		s.store.SetProxies(merged)

		if s.OnUpdate != nil {
			s.OnUpdate()
		}
	}

	return nil
}

// RefreshOne fetches a single subscription by ID, parses it, applies filters,
// updates the subscription metadata in the store, and rebuilds the proxy cache.
func (s *Scheduler) RefreshOne(id string) error {
	sub, err := s.store.GetSubscription(id)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	entries, err := s.fetcher.Fetch(ctx, sub.URL)
	if err != nil {
		// Update error state
		sub.LastError = err.Error()
		sub.LastFetch = time.Now()
		_ = s.store.UpdateSubscription(sub)
		return err
	}

	// Apply current filters
	filters := s.store.GetFilters()
	filtered := ApplyFilter(entries, filters)

	// Update subscription metadata
	sub.LastFetch = time.Now()
	sub.LastError = ""
	sub.ProxyCount = len(filtered)

	if err := s.store.UpdateSubscription(sub); err != nil {
		return err
	}

	// Store filtered proxies (replace entire cache for single-subscription case)
	s.store.SetProxies(filtered)

	return nil
}


