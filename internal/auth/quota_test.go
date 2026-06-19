package auth

import (
	"path/filepath"
	"testing"
	"time"
)

func TestQuotaForTier(t *testing.T) {
	cases := []struct {
		tier string
		want int
	}{
		{"free", 100},
		{"basic", 1000},
		{"pro", 10000},
		{"admin", 1 << 30},
		{"unknown", 100}, // fallback to free
		{"", 100},        // empty = free
	}
	for _, c := range cases {
		if got := QuotaForTier(c.tier); got != c.want {
			t.Errorf("QuotaForTier(%q) = %d, want %d", c.tier, got, c.want)
		}
	}
}

func TestCheckAndIncrementQuota(t *testing.T) {
	dir := t.TempDir()
	// Set env BEFORE NewStore so bootstrapFromEnv picks it up.
	t.Setenv("STACKLY_API_KEYS", "testkey123:free")

	store, err := NewStore(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	u := store.AuthenticateAPIKey("testkey123")
	if u == nil {
		t.Fatal("user not loaded")
	}
	if u.Tier != "free" {
		t.Fatalf("tier = %q, want free", u.Tier)
	}

	// First 100 should succeed
	for i := 0; i < 100; i++ {
		info, err := store.CheckAndIncrementQuota("testkey123")
		if err != nil {
			t.Fatalf("scan %d: unexpected error: %v", i+1, err)
		}
		if info.Used != i+1 {
			t.Errorf("scan %d: Used = %d, want %d", i+1, info.Used, i+1)
		}
	}

	// 101st should fail
	info, err := store.CheckAndIncrementQuota("testkey123")
	if err != ErrQuotaExceeded {
		t.Errorf("scan 101: err = %v, want ErrQuotaExceeded", err)
	}
	if info.Used != 100 {
		t.Errorf("scan 101: Used = %d, want 100 (not incremented)", info.Used)
	}
	if info.Remaining != 0 {
		t.Errorf("scan 101: Remaining = %d, want 0", info.Remaining)
	}
}

func TestQuotaReset(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STACKLY_API_KEYS", "testkey456:free")

	store, err := NewStore(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	// Burn 5 scans
	for i := 0; i < 5; i++ {
		_, err := store.CheckAndIncrementQuota("testkey456")
		if err != nil {
			t.Fatalf("scan %d: %v", i+1, err)
		}
	}

	// Force MonthReset into the past → next call should reset to 1 (the new increment).
	store.mu.Lock()
	u := store.users["testkey456"]
	u.MonthReset = time.Now().Add(-1 * time.Hour)
	u.ScanCount = 99 // pretend user burned all but 1
	store.mu.Unlock()

	info, err := store.CheckAndIncrementQuota("testkey456")
	if err != nil {
		t.Fatalf("post-reset scan: %v", err)
	}
	if info.Used != 1 {
		t.Errorf("post-reset Used = %d, want 1 (reset then +1)", info.Used)
	}
	if info.MonthReset.IsZero() {
		t.Error("MonthReset should be set after reset")
	}
	if !info.MonthReset.After(time.Now()) {
		t.Error("MonthReset should be in the future")
	}
}

func TestAdminUnlimited(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STACKLY_API_KEYS", "adminkey:admin")

	store, err := NewStore(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Stop()

	// Burn 200 scans (more than free limit) — admin should never block.
	for i := 0; i < 200; i++ {
		info, err := store.CheckAndIncrementQuota("adminkey")
		if err != nil {
			t.Fatalf("admin scan %d: %v", i+1, err)
		}
		if !info.Unlimited {
			t.Errorf("admin scan %d: info.Unlimited = false", i+1)
		}
	}
}
