package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// helper: build a fresh store + auth for each test
func newTestAuth(t *testing.T, env map[string]string) (*Auth, *Store) {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "users.json")

	// Set env BEFORE creating store
	for k, v := range env {
		os.Setenv(k, v)
		defer os.Unsetenv(k)
	}

	store, err := NewStore(storePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(store.Stop)

	auth := New(store, os.Getenv("STACKLY_JWT_SECRET"))
	return auth, store
}

func newRequest(headers map[string]string) *http.Request {
	r := httptest.NewRequest("GET", "/api/scan", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

// ─── Store tests ──────────────────────────────────────────

func TestStore_BootstrapFromEnv(t *testing.T) {
	auth, store := newTestAuth(t, map[string]string{
		"STACKLY_API_KEYS": "k1:free,k2:pro,k3:admin",
	})

	if !auth.Enabled() {
		t.Fatal("auth should be enabled")
	}

	users := store.AllUsers()
	if len(users) != 3 {
		t.Fatalf("want 3 users, got %d", len(users))
	}

	// Build a tier lookup by name (key is redacted in AllUsers output)
	tierByName := map[string]string{}
	for _, u := range users {
		tierByName[u.Name] = u.Tier
	}

	for _, c := range []struct{ name, tier string }{
		{"k1", "free"},
		{"k2", "pro"},
		{"k3", "admin"},
	} {
		if got := tierByName[c.name]; got != c.tier {
			t.Errorf("user %s: want tier %q, got %q", c.name, c.tier, got)
		}
	}
}

func TestStore_Authenticate(t *testing.T) {
	_, store := newTestAuth(t, map[string]string{
		"STACKLY_API_KEYS": "secret1,secret2",
	})

	u := store.AuthenticateAPIKey("secret1")
	if u == nil {
		t.Fatal("secret1 should authenticate")
	}
	if u.Name == "" {
		t.Error("user should have a name")
	}

	u = store.AuthenticateAPIKey("nonexistent")
	if u != nil {
		t.Error("nonexistent key should not authenticate")
	}
}

func TestStore_Disabled(t *testing.T) {
	_, store := newTestAuth(t, map[string]string{
		"STACKLY_API_KEYS": "k1",
	})
	u := store.AuthenticateAPIKey("k1")
	u.Disabled = true
	// Manual disable — done outside AuthenticateAPIKey check would still pass
	// because the AuthenticateAPIKey uses u.Disabled. Verify:
	if store.AuthenticateAPIKey("k1") != nil {
		t.Error("disabled user should not authenticate")
	}
}

func TestStore_RecordUsage(t *testing.T) {
	_, store := newTestAuth(t, map[string]string{
		"STACKLY_API_KEYS": "k1",
	})

	for i := 0; i < 5; i++ {
		store.RecordUsage("k1")
	}

	usage := store.Usage("k1")
	if usage == nil {
		t.Fatal("usage should exist")
	}
	if usage["request_count"].(int64) != 5 {
		t.Errorf("want 5 requests, got %v", usage["request_count"])
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "users.json")

	os.Setenv("STACKLY_API_KEYS", "persisted")
	defer os.Unsetenv("STACKLY_API_KEYS")

	s1, err := NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	s1.RecordUsage("persisted")
	s1.Stop() // force save

	// Reload
	s2, err := NewStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Stop()

	usage := s2.Usage("persisted")
	if usage == nil || usage["request_count"].(int64) != 1 {
		t.Errorf("usage not persisted: %v", usage)
	}
}

// ─── Auth middleware tests ────────────────────────────────

func TestAuth_Middleware_NoKey(t *testing.T) {
	auth, _ := newTestAuth(t, map[string]string{"STACKLY_API_KEYS": "k1"})

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	h := auth.Middleware(next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newRequest(nil))

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
	if called {
		t.Error("next handler should not be called")
	}
}

func TestAuth_Middleware_ValidKey(t *testing.T) {
	auth, _ := newTestAuth(t, map[string]string{"STACKLY_API_KEYS": "k1"})

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := auth.Middleware(next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newRequest(map[string]string{"X-API-Key": "k1"}))

	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d", rr.Code)
	}
	if !called {
		t.Error("next handler should be called")
	}
}

func TestAuth_Middleware_BasicAuth(t *testing.T) {
	auth, _ := newTestAuth(t, map[string]string{
		"STACKLY_BASIC_USERS": "alice:wonderland",
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := auth.Middleware(next)

	// alice:wonderland in base64 is YWxpY2U6d29uZGVybGFuZA==
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newRequest(map[string]string{
		"Authorization": "Basic YWxpY2U6d29uZGVybGFuZA==",
	}))

	if rr.Code != http.StatusOK {
		t.Errorf("want 200, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAuth_Middleware_BasicAuth_WrongPass(t *testing.T) {
	auth, _ := newTestAuth(t, map[string]string{
		"STACKLY_BASIC_USERS": "alice:wonderland",
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := auth.Middleware(next)

	// alice:wrongpass base64 = YWxpY2U6d3JvbmdwYXNz
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newRequest(map[string]string{
		"Authorization": "Basic YWxpY2U6d3JvbmdwYXNz",
	}))

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

func TestAuth_Middleware_RateLimit(t *testing.T) {
	auth, store := newTestAuth(t, map[string]string{"STACKLY_API_KEYS": "k1"})

	// Override the user's rate limit to 2/min
	store.mu.Lock()
	store.users["k1"].RateLimit = 2
	store.mu.Unlock()

	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})
	h := auth.Middleware(next)

	for i := 0; i < 5; i++ {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, newRequest(map[string]string{"X-API-Key": "k1"}))
		if i < 2 && rr.Code != http.StatusOK {
			t.Errorf("req %d: want 200, got %d", i, rr.Code)
		}
		if i >= 2 && rr.Code != http.StatusTooManyRequests {
			t.Errorf("req %d: want 429, got %d", i, rr.Code)
		}
	}
	if called != 2 {
		t.Errorf("next called %d times, want 2", called)
	}
}

func TestAuth_Middleware_HealthBypass(t *testing.T) {
	auth, _ := newTestAuth(t, map[string]string{"STACKLY_API_KEYS": "k1"})

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := auth.Middleware(next)

	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/health", nil)
	h.ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Errorf("health should bypass auth: got %d", rr.Code)
	}
	if !called {
		t.Error("health endpoint should be called")
	}
}

// ─── Helper tests ─────────────────────────────────────────

func TestRedactKey(t *testing.T) {
	cases := []struct{ in, out string }{
		{"short", "***"},
		{"verylongkey123456", "very...3456"},
		{"", "***"},
	}
	for _, c := range cases {
		got := redactKey(c.in)
		if got != c.out {
			t.Errorf("redactKey(%q): want %q, got %q", c.in, c.out, got)
		}
	}
}

func TestSplitKeyTier(t *testing.T) {
	cases := []struct {
		in       string
		def      string
		wantK    string
		wantTier string
	}{
		{"key1:tier2", "free", "key1", "tier2"},
		{"key1", "free", "key1", "free"},
		{"key1:", "free", "key1", "free"},
		{"key1:pro:extra", "free", "key1", "pro:extra"}, // extra ignored
	}
	for _, c := range cases {
		k, t2 := splitKeyTier(c.in, c.def)
		if k != c.wantK || t2 != c.wantTier {
			t.Errorf("splitKeyTier(%q): want (%q,%q), got (%q,%q)", c.in, c.wantK, c.wantTier, k, t2)
		}
	}
}

func TestDefaultRateForTier(t *testing.T) {
	cases := map[string]int{
		"admin": 10000,
		"pro":   600,
		"basic": 120,
		"":      60,
		"free":  60,
	}
	for tier, want := range cases {
		if got := defaultRateForTier(tier); got != want {
			t.Errorf("defaultRateForTier(%q): want %d, got %d", tier, want, got)
		}
	}
}

// ─── JWT tests (skipped if no secret) ─────────────────────

func TestAuth_Middleware_JWT(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping JWT test in short mode")
	}
	secret := "test-secret"
	auth, _ := newTestAuth(t, map[string]string{
		"STACKLY_JWT_SECRET": secret,
	})
	auth.jwtSecret = []byte(secret)

	// Generate a JWT
	tok := jwtTestToken(t, secret, "user-123", "pro", time.Now().Add(time.Hour))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := auth.Middleware(next)

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, newRequest(map[string]string{
		"Authorization": "Bearer " + tok,
	}))

	if rr.Code != http.StatusOK {
		t.Errorf("JWT should authenticate: got %d body=%s", rr.Code, rr.Body.String())
	}
}