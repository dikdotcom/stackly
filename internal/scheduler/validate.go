package scheduler

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// MaxURLLength caps user-provided URLs to prevent DoS via huge strings.
// 2048 is the practical maximum most clients/servers accept.
const MaxURLLength = 2048

// allowedSchemes is the whitelist for both scan and webhook URLs.
// file://, gopher://, ftp://, etc. are rejected — they're SSRF vectors.
var allowedSchemes = map[string]bool{
	"http":  true,
	"https": true,
}

// ValidateScanURL validates a URL that the scanner will navigate to.
// By default internal/loopback targets are rejected (production-safe).
// Set STACKLY_ALLOW_PRIVATE_TARGETS=1 to permit (dev mode for scanning
// localhost apps).
func ValidateScanURL(raw string) error {
	allowPrivate := os.Getenv("STACKLY_ALLOW_PRIVATE_TARGETS") != ""
	return validateURL(raw, allowPrivate)
}

// ValidateWebhookURL validates a URL the dispatcher will POST to.
// Internal targets are ALWAYS rejected — webhook delivery is outbound
// and must not be used to probe internal services. No override.
func ValidateWebhookURL(raw string) error {
	return validateURL(raw, false)
}

// validateURL is the shared validator. allowPrivate relaxes the IP filter
// for cases where pointing at a local dev server is intentional.
func validateURL(raw string, allowPrivate bool) error {
	if raw == "" {
		return fmt.Errorf("url is empty")
	}
	if len(raw) > MaxURLLength {
		return fmt.Errorf("url too long (%d > %d chars)", len(raw), MaxURLLength)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if !allowedSchemes[strings.ToLower(u.Scheme)] {
		return fmt.Errorf("scheme %q not allowed (use http or https)", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}

	// If the host is an IP literal, validate directly. Otherwise resolve
	// DNS and check every returned IP. Note: this is vulnerable to DNS
	// rebinding (attacker returns public IP at validation time, private
	// IP at request time). The bar is raised, not eliminated. For full
	// protection, the dialer itself must re-validate per connection.
	ips := make([]net.IP, 0)
	if ip := net.ParseIP(host); ip != nil {
		ips = append(ips, ip)
	} else {
		resolved, err := net.LookupIP(host)
		if err != nil {
			return fmt.Errorf("dns lookup failed for %q: %w", host, err)
		}
		if len(resolved) == 0 {
			return fmt.Errorf("no addresses for %q", host)
		}
		ips = append(ips, resolved...)
	}

	if allowPrivate {
		return nil
	}

	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("blocked %s (internal/private/loopback/link-local)", ip)
		}
	}
	return nil
}

// isBlockedIP returns true for IPs that should never be reached from
// a server-side context. Covers:
//   - 0.0.0.0, ::                  (unspecified)
//   - 127.0.0.0/8, ::1             (loopback)
//   - 10.0.0.0/8, 172.16.0.0/12,
//     192.168.0.0/16, fc00::/7     (private)
//   - 169.254.0.0/16, fe80::/10    (link-local — incl. cloud metadata)
//   - 224.0.0.0/4, ff00::/8        (multicast)
//   - 240.0.0.0/4                  (reserved)
//
// Go's net.IP methods (IsLoopback, IsPrivate, etc.) handle IPv4-mapped
// IPv6 (::ffff:1.2.3.4) correctly, so no special unwrapping needed.
func isBlockedIP(ip net.IP) bool {
	return ip.IsUnspecified() ||
		ip.IsLoopback() ||
		ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsPrivate()
}

// ScheduleQuotaByTier maps tier name to max schedules. Admin gets a
// very high number rather than math.MaxInt to keep logs readable.
var ScheduleQuotaByTier = map[string]int{
	"free":  3,
	"basic": 20,
	"pro":   100,
	"admin": 999999,
}

// DefaultNoAuthQuota is the cap when auth is disabled (dev mode). All
// schedules share the "default" owner, so this is a global limit.
const DefaultNoAuthQuota = 50

// QuotaForOwner returns the schedule quota for the given owner.
// `tier` should be the user's tier; pass "free" if unknown or no auth.
func QuotaForOwner(tier string, hasAuth bool) int {
	if !hasAuth {
		return DefaultNoAuthQuota
	}
	if q, ok := ScheduleQuotaByTier[tier]; ok {
		return q
	}
	return ScheduleQuotaByTier["free"]
}
