package subscription

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler handles periodic subscription refresh and auto-apply.
type Scheduler struct {
	mu      sync.RWMutex
	store   *Store
	fetcher *Fetcher

	// cron scheduler for auto-apply
	cron      *cron.Cron
	cronEntry cron.EntryID

	// ctx/cancel control the interval checker goroutine lifetime.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// OnUpdate is called after a successful fetch cycle.
	OnUpdate func()

	// RestartCmd is the command to restart xkeen (e.g. "xkeen -restart").
	RestartCmd string

	// xrayDir is the xray config directory for writing generated files.
	xrayDir string

	// metricsPort is the Xray metrics port (0 = disabled).
	metricsPort int

	// mark is the sockopt.mark value applied to all outbounds during generation
	// (0 = none, 255 = proxy_entware on). Atomic because SetMark is called from
	// an HTTP handler while writeConfigFiles runs from the scheduler goroutine.
	mark atomic.Int64

	// applyMu serializes config-file generation+writes between auto-apply and the HTTP Apply handler.
	applyMu sync.Mutex
}

// NewScheduler creates a new scheduler.
func NewScheduler(store *Store, fetcher *Fetcher) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		store:      store,
		fetcher:    fetcher,
		RestartCmd: "xkeen -restart",
		ctx:        ctx,
		cancel:     cancel,
	}
}

// SetXrayDir sets the xray config directory for auto-apply file writes.
func (s *Scheduler) SetXrayDir(dir string) {
	s.xrayDir = dir
}

// SetMetricsPort sets the Xray metrics port and immediately writes/removes 08_metrics.json.
func (s *Scheduler) SetMetricsPort(port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metricsPort = port
	if s.xrayDir == "" {
		return
	}
	metricsPath := s.xrayDir + "/08_metrics.json"
	if port > 0 {
		metricsJSON := GenerateMetricsJSON(port)
		if metricsJSON != nil {
			if err := atomicWrite(metricsPath, metricsJSON); err != nil {
				log.Printf("[scheduler] failed to write metrics config: %v", err)
			}
		}
	} else {
		_ = os.Remove(metricsPath)
	}
}

// SetMark sets the sockopt.mark value applied to all outbounds during generation.
// 0 disables marking; 255 enables Entware traffic proxy mode.
func (s *Scheduler) SetMark(mark int) {
	s.mark.Store(int64(mark))
}

// recoverPanic catches a panic in a scheduler goroutine, logs it, and lets the
// goroutine complete normally so the WaitGroup is not held forever.
func (s *Scheduler) recoverPanic(name string) {
	if r := recover(); r != nil {
		log.Printf("[subscription] recovered panic in %s: %v", name, r)
	}
}

// WithApplyLock runs fn while holding the apply mutex, serializing file
// generation+writes between the scheduler's auto-apply and the HTTP Apply handler.
func (s *Scheduler) WithApplyLock(fn func() error) error {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	return fn()
}

// atomicWrite writes data to path atomically: write to path+".tmp", then rename.
// This prevents partial/corrupted files on crash mid-write.
func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		// Clean up the orphaned temp file on rename failure.
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Start begins the per-minute subscription interval checker
// and restores auto-apply cron if enabled in config.
func (s *Scheduler) Start() {
	s.startIntervalChecker()

	enabled, cronExpr := s.store.GetAutoApply()
	if enabled && cronExpr != "" {
		if err := s.enableCron(cronExpr); err != nil {
			log.Printf("[subscription] failed to restore auto-apply cron: %v", err)
		} else {
			log.Printf("[subscription] auto-apply cron restored: %s", cronExpr)
		}
	}
}

// Stop gracefully stops all schedulers and waits for goroutines to finish.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.cron != nil {
		s.cron.Stop()
		s.cron = nil
		log.Println("[subscription] auto-apply cron stopped")
	}
	s.mu.Unlock()

	// Cancel context to signal interval checker to stop, then wait.
	s.cancel()
	s.wg.Wait()
}

// UpdateAutoApply enables/disables the auto-apply cron and updates the expression.
func (s *Scheduler) UpdateAutoApply(enabled bool, cronExpr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop existing cron
	if s.cron != nil {
		s.cron.Stop()
		s.cron = nil
	}

	if !enabled || cronExpr == "" {
		log.Println("[subscription] auto-apply disabled")
		return nil
	}

	return s.startCronLocked(cronExpr)
}

// enableCron starts the cron (acquires lock internally).
func (s *Scheduler) enableCron(cronExpr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startCronLocked(cronExpr)
}

// startCronLocked starts the cron scheduler (caller must hold the lock).
func (s *Scheduler) startCronLocked(cronExpr string) error {
	c := cron.New()

	id, err := c.AddFunc(cronExpr, func() {
		log.Println("[subscription] auto-apply cron triggered")
		s.runAutoApply()
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}
	_ = id

	c.Start()
	s.cron = c
	s.cronEntry = id
	log.Printf("[subscription] auto-apply cron started: %s", cronExpr)
	return nil
}

// GetNextRun returns the next scheduled run time, or zero time if cron is not active.
func (s *Scheduler) GetNextRun() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.cron == nil {
		return time.Time{}
	}
	entries := s.cron.Entries()
	if len(entries) == 0 {
		return time.Time{}
	}
	return entries[0].Next
}

// runAutoApply executes the full cycle: fetch all → filter → generate files → restart.
func (s *Scheduler) runAutoApply() {
	// 1. Fetch all subscriptions
	if err := s.RefreshAll(); err != nil {
		log.Printf("[subscription] auto-apply: fetch failed: %v", err)
		return
	}

	// 2. Check we have proxies
	allProxies := s.store.GetProxies()
	if len(allProxies) == 0 {
		log.Println("[subscription] auto-apply: no proxies after fetch, skipping")
		return
	}

	// 3. Generate and write config files (profiles handle filtering internally)
	profiles := s.store.GetProfiles()
	if err := s.WithApplyLock(func() error {
		return s.writeConfigFiles(allProxies, profiles)
	}); err != nil {
		log.Printf("[subscription] auto-apply: write failed: %v", err)
		return
	}

	// 5. Restart xkeen
	if s.RestartCmd != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "sh", "-c", s.RestartCmd).CombinedOutput() //nolint:gosec // RestartCmd is from config, set by user
		if err != nil {
			log.Printf("[subscription] auto-apply: restart failed: %v (%s)", err, string(out))
		} else {
			log.Println("[subscription] auto-apply: xkeen restarted")
		}
	}

	_ = s.store.SetGeneratedAt(time.Now())
	log.Printf("[subscription] auto-apply complete: %d proxies applied", len(allProxies))

	if s.OnUpdate != nil {
		s.OnUpdate()
	}
}

// writeConfigFiles generates outbounds, routing, and observatory and writes them to xrayDir.
func (s *Scheduler) writeConfigFiles(allProxies []*ProxyEntry, profiles []Profile) error {
	dir := s.xrayDir
	if dir == "" {
		return fmt.Errorf("xray config dir not set")
	}

	// Generate outbounds for ALL proxies (filtering is handled per-profile via
	// balancer selectors). Both outbounds and routing receive the SAME allProxies
	// list so the first-proxy "proxy" tag and selectors stay consistent.
	outboundsJSON, err := GenerateOutboundsJSON(allProxies, int(s.mark.Load()))
	if err != nil {
		return fmt.Errorf("generate outbounds: %w", err)
	}

	// Read existing routing for merge
	var existingRouting json.RawMessage
	routingPath := dir + "/05_routing.json"
	if data, err := os.ReadFile(routingPath); err == nil {
		existingRouting = data
	}

	// Generate routing with the same allProxies list
	routingJSON, err := GenerateRoutingJSON(allProxies, profiles, existingRouting)
	if err != nil {
		return fmt.Errorf("generate routing: %w", err)
	}

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	// Write outbounds
	if err := atomicWrite(dir+"/04_outbounds.json", outboundsJSON); err != nil {
		return fmt.Errorf("write outbounds: %w", err)
	}

	// Write routing
	if err := atomicWrite(routingPath, routingJSON); err != nil {
		return fmt.Errorf("write routing: %w", err)
	}

	// Observatory
	obsPath := dir + "/07_observatory.json"
	if NeedsObservatory(profiles) {
		obsJSON, err := GenerateObservatoryJSON()
		if err != nil {
			return fmt.Errorf("generate observatory: %w", err)
		}
		if err := atomicWrite(obsPath, obsJSON); err != nil {
			return fmt.Errorf("write observatory: %w", err)
		}
	} else {
		_ = os.Remove(obsPath)
	}

	// Metrics — GenerateMetricsJSON includes policy.system for traffic stats
	metricsPath := dir + "/08_metrics.json"
	if s.metricsPort > 0 {
		metricsJSON := GenerateMetricsJSON(s.metricsPort)
		if metricsJSON != nil {
			if err := atomicWrite(metricsPath, metricsJSON); err != nil {
				return fmt.Errorf("write metrics: %w", err)
			}
		}
	} else {
		_ = os.Remove(metricsPath)
	}

	return nil
}

// ---------- Per-subscription interval checker ----------

// startIntervalChecker runs a goroutine that checks every minute
// which subscriptions need refresh based on their individual interval.
func (s *Scheduler) startIntervalChecker() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.recoverPanic("intervalChecker")
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		// Check immediately on start
		s.checkAndRefresh()

		for {
			select {
			case <-ticker.C:
				s.checkAndRefresh()
			case <-s.ctx.Done():
				return
			}
		}
	}()
	log.Println("[subscription] interval checker started")
}

// checkAndRefresh iterates enabled subscriptions and refreshes those
// whose interval has elapsed. Each subscription is refreshed in its own
// goroutine so slow fetches don't block the ticker for all subscriptions.
func (s *Scheduler) checkAndRefresh() {
	cfg := s.store.GetConfig()
	now := time.Now()

	var wg sync.WaitGroup
	var mu sync.Mutex
	needsUpdate := false

	for _, sub := range cfg.Subscriptions {
		if !sub.Enabled || sub.IsBuiltin || sub.Interval <= 0 {
			continue
		}
		nextRefresh := sub.LastFetch.Add(time.Duration(sub.Interval) * time.Minute)
		if now.After(nextRefresh) || sub.LastFetch.IsZero() {
			sub := sub // capture loop variable
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer s.recoverPanic("checkAndRefresh")
				log.Printf("[subscription] auto-refreshing %q (%s)", sub.Name, sub.ID)
				if err := s.RefreshOne(sub.ID); err != nil {
					log.Printf("[subscription] auto-refresh failed for %q: %v", sub.Name, err)
				} else {
					mu.Lock()
					needsUpdate = true
					mu.Unlock()
				}
			}()
		}
	}

	wg.Wait()

	if needsUpdate && s.OnUpdate != nil {
		s.OnUpdate()
	}
}

// RefreshAll fetches all enabled subscriptions and updates the store's proxy cache.
// Subscriptions are fetched in parallel goroutines, each with its own 30s timeout.
// Each successfully refreshed subscription is committed atomically via
// ReplaceProxiesForSubscription, so concurrent RefreshOne/HTTP fetch calls
// cannot lose data.
func (s *Scheduler) RefreshAll() error {
	cfg := s.store.GetConfig()

	var (
		mu         sync.Mutex
		results    map[string][]*ProxyEntry // sub.ID -> fetched entries
		anySuccess bool
		wg         sync.WaitGroup
	)
	results = make(map[string][]*ProxyEntry)

	for _, sub := range cfg.Subscriptions {
		if !sub.Enabled || sub.IsBuiltin {
			continue
		}

		sub := sub // capture loop variable
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer s.recoverPanic("RefreshAll")

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			result, err := s.fetcher.FetchWithCascade(ctx, sub.URL)
			cancel()

			if err != nil {
				log.Printf("[subscription] refresh failed for %q (%s): %v", sub.Name, sub.ID, err)
				sub.LastError = err.Error()
				sub.LastFetch = time.Now()
				_ = s.store.UpdateSubscription(&sub)
				return
			}

			sub.LastFetch = time.Now()
			sub.LastError = ""
			sub.ProxyCount = len(result.Entries)
			sub.LastSource = result.Source
			_ = s.store.UpdateSubscription(&sub)

			// Tag entries with subscription ID
			for _, e := range result.Entries {
				e.SubscriptionID = sub.ID
			}

			mu.Lock()
			results[sub.ID] = result.Entries
			anySuccess = true
			mu.Unlock()
		}()
	}

	wg.Wait()

	if anySuccess {
		// Commit each subscription atomically to avoid losing data to
		// concurrent RefreshOne/HTTP fetch calls. GenerateTags runs under
		// the store lock via the transform callback for consistency.
		for subID, entries := range results {
			if err := s.store.ReplaceProxiesForSubscription(subID, entries, func(merged []*ProxyEntry) {
				GenerateTags(merged)
			}); err != nil {
				log.Printf("[subscription] failed to commit refresh for %q: %v", subID, err)
			}
		}
		if s.OnUpdate != nil {
			s.OnUpdate()
		}
	}

	return nil
}

// RefreshOne fetches a single subscription by ID, parses it,
// updates the subscription metadata in the store, and rebuilds the proxy cache.
func (s *Scheduler) RefreshOne(id string) error {
	sub, err := s.store.GetSubscription(id)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := s.fetcher.FetchWithCascade(ctx, sub.URL)
	if err != nil {
		sub.LastError = err.Error()
		sub.LastFetch = time.Now()
		_ = s.store.UpdateSubscription(sub)
		return err
	}

	sub.LastFetch = time.Now()
	sub.LastError = ""
	sub.ProxyCount = len(result.Entries)
	sub.LastSource = result.Source

	if err := s.store.UpdateSubscription(sub); err != nil {
		return err
	}

	// Tag entries with subscription ID
	for _, e := range result.Entries {
		e.SubscriptionID = id
	}

	// Atomically replace proxies for this subscription while keeping others.
	// Uses a store-level lock to avoid the TOCTOU race that a manual
	// GetProxies → modify → SetProxies sequence would introduce when
	// multiple subscriptions refresh in parallel. GenerateTags runs under
	// the same lock (as a transform callback) so tags are consistent with
	// what gets persisted.
	return s.store.ReplaceProxiesForSubscription(id, result.Entries, func(merged []*ProxyEntry) {
		GenerateTags(merged)
	})
}
