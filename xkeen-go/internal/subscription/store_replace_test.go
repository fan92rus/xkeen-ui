package subscription

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// TestReplaceProxiesForSubscription_Basic replaces proxies for one subscription
// while keeping others intact.
func TestReplaceProxiesForSubscription_Basic(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	// Seed: sub-A has 2 proxies, sub-B has 3 proxies
	store.SetProxies([]*ProxyEntry{
		{Tag: "a1", SubscriptionID: "sub-a"},
		{Tag: "a2", SubscriptionID: "sub-a"},
		{Tag: "b1", SubscriptionID: "sub-b"},
		{Tag: "b2", SubscriptionID: "sub-b"},
		{Tag: "b3", SubscriptionID: "sub-b"},
	})

	// Replace sub-A proxies with 1 new
	err := store.ReplaceProxiesForSubscription("sub-a", []*ProxyEntry{
		{Tag: "a-new", SubscriptionID: "sub-a"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := store.GetProxies()
	if len(got) != 4 { // 1 new sub-a + 3 sub-b
		t.Fatalf("expected 4 proxies, got %d", len(got))
	}

	// Verify sub-b proxies survived
	bCount := 0
	aNewCount := 0
	for _, p := range got {
		if p.SubscriptionID == "sub-b" {
			bCount++
		}
		if p.Tag == "a-new" {
			aNewCount++
		}
	}
	if bCount != 3 {
		t.Errorf("expected 3 sub-b proxies preserved, got %d", bCount)
	}
	if aNewCount != 1 {
		t.Errorf("expected 1 new sub-a proxy, got %d", aNewCount)
	}
}

// TestReplaceProxiesForSubscription_RemovesOrphans verifies that orphaned
// entries (empty SubscriptionID) are cleaned up during replacement.
func TestReplaceProxiesForSubscription_RemovesOrphans(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	store.SetProxies([]*ProxyEntry{
		{Tag: "a1", SubscriptionID: "sub-a"},
		{Tag: "orphan", SubscriptionID: ""},
	})

	err := store.ReplaceProxiesForSubscription("sub-a", []*ProxyEntry{
		{Tag: "a-new", SubscriptionID: "sub-a"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := store.GetProxies()
	if len(got) != 1 {
		t.Fatalf("expected 1 proxy (orphan removed), got %d", len(got))
	}
	if got[0].Tag != "a-new" {
		t.Errorf("expected a-new, got %q", got[0].Tag)
	}
}

// TestReplaceProxiesForSubscription_EmptyNew clears all proxies for a sub
// while keeping others.
func TestReplaceProxiesForSubscription_EmptyNew(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	store.SetProxies([]*ProxyEntry{
		{Tag: "a1", SubscriptionID: "sub-a"},
		{Tag: "b1", SubscriptionID: "sub-b"},
	})

	err := store.ReplaceProxiesForSubscription("sub-a", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := store.GetProxies()
	if len(got) != 1 {
		t.Fatalf("expected 1 proxy (sub-b only), got %d", len(got))
	}
	if got[0].SubscriptionID != "sub-b" {
		t.Errorf("expected sub-b to survive, got %q", got[0].SubscriptionID)
	}
}

// TestReplaceProxiesForSubscription_ConcurrentNoDataLoss is the key test for
// the TOCTOU race: multiple goroutines replace different subscriptions
// concurrently, and NO data should be lost.
//
// Without an atomic ReplaceProxiesForSubscription (i.e. with a manual
// GetProxies → modify → SetProxies pattern), parallel goroutines read the same
// snapshot, each remove their own subscription's old proxies, and overwrite
// each other's results — losing newly-fetched proxies from sibling
// goroutines.
func TestReplaceProxiesForSubscription_ConcurrentNoDataLoss(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "subscriptions.json"))

	// Seed: 5 subscriptions, each with 1 initial proxy
	const numSubs = 5
	seed := make([]*ProxyEntry, numSubs)
	for i := 0; i < numSubs; i++ {
		seed[i] = &ProxyEntry{
			Tag:            fmt.Sprintf("old-%d", i),
			SubscriptionID: fmt.Sprintf("sub-%d", i),
		}
	}
	store.SetProxies(seed)

	// Concurrently replace each subscription's proxies with 1 NEW proxy
	var wg sync.WaitGroup
	for i := 0; i < numSubs; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			newProxy := &ProxyEntry{
				Tag:            fmt.Sprintf("new-%d", idx),
				SubscriptionID: fmt.Sprintf("sub-%d", idx),
			}
			if err := store.ReplaceProxiesForSubscription(
				fmt.Sprintf("sub-%d", idx), []*ProxyEntry{newProxy}, nil,
			); err != nil {
				t.Errorf("replace failed: %v", err)
			}
		}(i)
	}
	wg.Wait()

	// Verify: every subscription must have exactly 1 NEW proxy, none of the
	// OLD proxies should survive, and no subscription should be missing.
	got := store.GetProxies()
	if len(got) != numSubs {
		t.Fatalf("expected %d proxies, got %d (DATA LOSS — TOCTOU race)", numSubs, len(got))
	}

	seen := make(map[string]bool)
	for _, p := range got {
		if p.Tag[:4] != "new-" {
			t.Errorf("stale proxy survived: tag=%q sub=%q (TOCTOU race)", p.Tag, p.SubscriptionID)
		}
		seen[p.SubscriptionID] = true
	}
	if len(seen) != numSubs {
		t.Errorf("expected %d distinct subscriptions, got %d: %v", numSubs, len(seen), seen)
	}
}
