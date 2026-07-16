package handlers

import (
	"errors"
	"sync"
	"testing"
)

// --- loader injection & lazy loading ---

func TestCommandRegistry_LazyLoadSuccess(t *testing.T) {
	calls := 0
	reg := newCommandRegistryWithLoader(func() (map[string]CommandConfig, error) {
		calls++
		return map[string]CommandConfig{
			"-start": {Cmd: "-start", Description: "Запуск"},
		}, nil
	})

	// Not loaded yet.
	if reg.loaded {
		t.Fatal("registry should not be loaded before first access")
	}

	c, ok := reg.Get("-start")
	if !ok {
		t.Fatal("expected -start after load")
	}
	if c.Description != "Запуск" {
		t.Errorf("description = %q", c.Description)
	}
	if calls != 1 {
		t.Errorf("loader should be called once, got %d", calls)
	}

	// Subsequent access must NOT reload.
	_, _ = reg.Get("-start")
	_ = reg.All()
	if calls != 1 {
		t.Errorf("loader should still be called once (cached), got %d", calls)
	}
}

func TestCommandRegistry_LoaderErrorYieldsEmpty(t *testing.T) {
	calls := 0
	reg := newCommandRegistryWithLoader(func() (map[string]CommandConfig, error) {
		calls++
		return nil, errors.New("xkeen: command not found")
	})

	// Error → empty set, NOT a panic, and marked loaded (no retry storm).
	c, ok := reg.Get("-start")
	if ok {
		t.Errorf("expected no command on loader error, got %+v", c)
	}
	if reg.Count() != 0 {
		t.Errorf("expected 0 commands on error, got %d", reg.Count())
	}
	if !reg.loaded {
		t.Error("registry should be marked loaded even after error (no retry)")
	}

	// Second access must NOT re-invoke the failing loader.
	_ = reg.All()
	if calls != 1 {
		t.Errorf("loader should be called once even on error, got %d", calls)
	}
}

func TestCommandRegistry_EmptyResultYieldsEmpty(t *testing.T) {
	reg := newCommandRegistryWithLoader(func() (map[string]CommandConfig, error) {
		return map[string]CommandConfig{}, nil // empty xkeen help
	})
	if reg.Count() != 0 {
		t.Errorf("expected 0 commands, got %d", reg.Count())
	}
}

func TestCommandRegistry_NilResultYieldsEmpty(t *testing.T) {
	reg := newCommandRegistryWithLoader(func() (map[string]CommandConfig, error) {
		return nil, nil // defensive: nil map
	})
	if reg.Count() != 0 {
		t.Errorf("expected 0 commands for nil result, got %d", reg.Count())
	}
}

// --- Get / All ---

func TestCommandRegistry_GetMissing(t *testing.T) {
	reg := newCommandRegistryWithLoader(func() (map[string]CommandConfig, error) {
		return map[string]CommandConfig{"-start": {Cmd: "-start"}}, nil
	})
	if _, ok := reg.Get("-nope"); ok {
		t.Error("expected Get to return false for missing command")
	}
}

func TestCommandRegistry_AllReturnsCopy(t *testing.T) {
	reg := newCommandRegistryWithLoader(func() (map[string]CommandConfig, error) {
		return map[string]CommandConfig{"-a": {Cmd: "-a"}, "-b": {Cmd: "-b"}}, nil
	})
	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
	// Mutating the returned slice must not corrupt the registry cache.
	all[0] = CommandConfig{Cmd: "-mutated"}
	again := reg.All()
	for _, c := range again {
		if c.Cmd == "-mutated" {
			t.Error("registry cache was corrupted by mutating All() result")
		}
	}
}

// --- Refresh ---

func TestCommandRegistry_RefreshReplacesCache(t *testing.T) {
	gen := 1
	reg := newCommandRegistryWithLoader(func() (map[string]CommandConfig, error) {
		out := map[string]CommandConfig{
			"-start": {Cmd: "-start", Description: "gen1"},
		}
		if gen == 2 {
			out["-start"] = CommandConfig{Cmd: "-start", Description: "gen2"}
			out["-newflag"] = CommandConfig{Cmd: "-newflag", Description: "added after update"}
		}
		return out, nil
	})

	if c, _ := reg.Get("-start"); c.Description != "gen1" {
		t.Fatalf("initial desc = %q, want gen1", c.Description)
	}

	gen = 2
	reg.Refresh()

	if c, _ := reg.Get("-start"); c.Description != "gen2" {
		t.Errorf("after refresh desc = %q, want gen2", c.Description)
	}
	if _, ok := reg.Get("-newflag"); !ok {
		t.Error("expected -newflag after refresh")
	}
}

func TestCommandRegistry_RefreshOnErrorKeepsOldCache(t *testing.T) {
	gen := 1
	reg := newCommandRegistryWithLoader(func() (map[string]CommandConfig, error) {
		if gen == 1 {
			gen = 2
			return map[string]CommandConfig{"-start": {Cmd: "-start"}}, nil
		}
		return nil, errors.New("xkeen -help hung this time")
	})

	// First load succeeds.
	if _, ok := reg.Get("-start"); !ok {
		t.Fatal("expected -start after initial load")
	}

	// Refresh fails → previous cache must be retained (not wiped to empty).
	reg.Refresh()
	if _, ok := reg.Get("-start"); !ok {
		t.Error("expected previous commands retained after failed refresh")
	}
	if reg.Count() == 0 {
		t.Error("refresh error must not wipe the cache")
	}
}

// --- concurrency ---

func TestCommandRegistry_ConcurrentAccess(_ *testing.T) {
	reg := newCommandRegistryWithLoader(func() (map[string]CommandConfig, error) {
		out := make(map[string]CommandConfig, 20)
		for i := 0; i < 20; i++ {
			out["-cmd"] = CommandConfig{Cmd: "-cmd"}
		}
		return out, nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); _ = reg.All() }()
		go func() { defer wg.Done(); _, _ = reg.Get("-cmd") }()
		go func() { defer wg.Done(); _ = reg.Count() }()
	}
	wg.Wait()
	// No panic/race = pass (race detector runs in CI).
}

// --- production constructor ---

func TestNewCommandRegistry_Defaults(t *testing.T) {
	reg := NewCommandRegistry(DefaultXKeenPath)
	if reg.xkeenPath != DefaultXKeenPath {
		t.Errorf("xkeenPath = %q, want %q", reg.xkeenPath, DefaultXKeenPath)
	}
	if reg.loader == nil {
		t.Error("expected loader to be wired")
	}
	if reg.loaded {
		t.Error("registry should start unloaded (lazy)")
	}
}

func TestNewCommandRegistry_CustomPath(t *testing.T) {
	reg := NewCommandRegistry("/custom/path/xkeen")
	if reg.xkeenPath != "/custom/path/xkeen" {
		t.Errorf("xkeenPath = %q", reg.xkeenPath)
	}
}
