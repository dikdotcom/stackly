package scheduler

import (
	"os"
	"strings"
	"testing"
)

func TestValidateWebhookURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr string // substring; "" means should pass
	}{
		// Reject obvious bad cases.
		{"empty", "", "empty"},
		{"missing scheme", "example.com", "scheme"},
		{"file scheme", "file:///etc/passwd", "scheme"},
		{"gopher scheme", "gopher://internal:6379/_FLUSHALL", "scheme"},
		{"ftp scheme", "ftp://internal/data", "scheme"},

		// Loopback IPv4
		{"loopback ipv4", "http://127.0.0.1/", "blocked"},
		{"loopback ipv4 alt", "http://127.0.0.1:8080/admin", "blocked"},

		// Loopback IPv6
		{"loopback ipv6", "http://[::1]/", "blocked"},

		// Private RFC1918
		{"private 10/8", "http://10.0.0.1/", "blocked"},
		{"private 172.16/12", "http://172.16.0.1/", "blocked"},
		{"private 192.168/16", "http://192.168.1.1/", "blocked"},

		// Link-local (incl. cloud metadata)
		{"link-local 169.254", "http://169.254.169.254/latest/meta-data/", "blocked"},
		{"link-local ipv6", "http://[fe80::1]/", "blocked"},

		// Multicast
		{"multicast v4", "http://224.0.0.1/", "blocked"},

		// IPv4-mapped IPv6 (::ffff:127.0.0.1) — Go's IsLoopback handles these
		{"ipv4-mapped loopback", "http://[::ffff:127.0.0.1]/", "blocked"},

		// Private IPv6 ULA
		{"ula ipv6", "http://[fc00::1]/", "blocked"},

		// Unspecified
		{"unspecified v4", "http://0.0.0.0/", "blocked"},

		// Length cap
		{"too long", "http://example.com/" + strings.Repeat("a", MaxURLLength), "too long"},

		// Missing host
		{"missing host", "http:///path", "missing host"},

		// Valid cases — example.com resolves to public IPs
		{"public https", "https://example.com/", ""},
		{"public http with port", "http://example.com:8080/path", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateWebhookURL(c.url)
			if c.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("expected error containing %q, got nil", c.wantErr)
				return
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), c.wantErr)
			}
		})
	}
}

func TestValidateScanURL_AllowPrivate(t *testing.T) {
	// Default: blocked
	if err := ValidateScanURL("http://127.0.0.1:3000/"); err == nil {
		t.Error("expected 127.0.0.1 to be blocked by default")
	}

	// With env override: allowed
	os.Setenv("STACKLY_ALLOW_PRIVATE_TARGETS", "1")
	defer os.Unsetenv("STACKLY_ALLOW_PRIVATE_TARGETS")

	// Note: this hits DNS for the localhost lookup. Skip if it fails
	// (e.g., in CI without DNS) — main check is that private IPs pass
	// when override is on.
	if err := ValidateScanURL("http://127.0.0.1:3000/"); err != nil {
		// Might fail on DNS lookup if 127.0.0.1 can't be resolved literally
		// via LookupIP — check the error is about DNS not about blocking.
		if !strings.Contains(err.Error(), "dns lookup failed") {
			t.Errorf("expected pass or DNS error, got: %v", err)
		}
	}
}

func TestQuotaForOwner(t *testing.T) {
	// With auth, tier-based
	if got := QuotaForOwner("free", true); got != 3 {
		t.Errorf("free quota = %d, want 3", got)
	}
	if got := QuotaForOwner("admin", true); got != 999999 {
		t.Errorf("admin quota = %d, want 999999", got)
	}
	// Unknown tier falls back to free
	if got := QuotaForOwner("mystery", true); got != 3 {
		t.Errorf("unknown tier quota = %d, want 3 (free fallback)", got)
	}
	// Without auth → global default
	if got := QuotaForOwner("free", false); got != DefaultNoAuthQuota {
		t.Errorf("no-auth quota = %d, want %d", got, DefaultNoAuthQuota)
	}
}
