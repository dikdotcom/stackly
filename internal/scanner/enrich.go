package scanner

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// DoH providers (DNS-over-HTTPS) — tried in order, first success wins.
var dohEndpoints = []string{
	"https://cloudflare-dns.com/dns-query",
	"https://dns.google/resolve",
}

// DNSInfo captures DNS records + derived providers.
type DNSInfo struct {
	A             []string `json:"a,omitempty"`
	AAAA          []string `json:"aaaa,omitempty"`
	MX            []DNSMX   `json:"mx,omitempty"`
	NS            []string  `json:"ns,omitempty"`
	TXT           []string  `json:"txt,omitempty"`
	CNAME         string    `json:"cname,omitempty"`
	EmailProvider string    `json:"email_provider,omitempty"` // derived: Google Workspace, Zoho, Outlook, etc.
	DNSProvider   string    `json:"dns_provider,omitempty"`   // derived: Cloudflare, Route53, etc.
	Error         string    `json:"error,omitempty"`
}

// DNSMX is a single MX record.
type DNSMX struct {
	Host string `json:"host"`
	Pref uint16 `json:"pref"`
}

// WHOISInfo captures RDAP registration data.
type WHOISInfo struct {
	Registrar      string   `json:"registrar,omitempty"`
	CreatedDate    string   `json:"created_date,omitempty"`  // ISO 8601
	ExpiryDate     string   `json:"expiry_date,omitempty"`
	UpdatedDate    string   `json:"updated_date,omitempty"`
	Status         []string `json:"status,omitempty"`
	NameServers    []string `json:"name_servers,omitempty"`
	DomainAgeDays  int      `json:"domain_age_days,omitempty"`
	DaysToExpiry   int      `json:"days_to_expiry,omitempty"`
	RDAPEndpoint   string   `json:"rdap_endpoint,omitempty"` // which registrar RDAP answered
	Error          string   `json:"error,omitempty"`
}

// SSLInfo captures TLS certificate details from a fresh dial.
type SSLInfo struct {
	Issuer          string    `json:"issuer,omitempty"`
	Subject         string    `json:"subject,omitempty"`
	NotBefore       time.Time `json:"not_before,omitempty"`
	NotAfter        time.Time `json:"not_after,omitempty"`
	DaysUntilExpiry int       `json:"days_until_expiry,omitempty"`
	SANCount        int       `json:"san_count,omitempty"`
	SelfSigned      bool      `json:"self_signed,omitempty"`
	TLSVersion      string    `json:"tls_version,omitempty"`
	CipherSuite     string    `json:"cipher_suite,omitempty"`
	Error           string    `json:"error,omitempty"`
}

// PerfMetrics captures request-time performance data aggregated from
// the HTML/scripts/CSS we already collected during the scan.
type PerfMetrics struct {
	TTFB                int64 `json:"ttfb_ms"`                  // time to first byte (ms)
	TotalDuration       int64 `json:"total_duration_ms"`         // full scan duration (ms)
	HTMLSizeBytes       int64         `json:"html_size_bytes"`
	JSBundleSizeBytes   int64         `json:"js_bundle_size_bytes"`
	CSSBundleSizeBytes  int64         `json:"css_bundle_size_bytes"`
	ScriptCount         int           `json:"script_count"`
	StylesheetCount     int           `json:"stylesheet_count"`
	InlineScriptBytes   int64         `json:"inline_script_bytes"`
	ExternalScriptBytes int64         `json:"external_script_bytes"`
	TotalAssetBytes     int64         `json:"total_asset_bytes"`     // rough estimate = HTML + JS + CSS
	HeadingsCount       int           `json:"headings_count"`        // h1-h6 tags
	MetaTagsCount       int           `json:"meta_tags_count"`
	LinksCount          int           `json:"links_count"`
	ImagesCount         int           `json:"images_count"`
}

// ─── DNS via DoH ────────────────────────────────────────────────────

// dohClient returns an HTTP client configured for DoH endpoints.
func dohClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

// dohQuery queries one of the DoH endpoints for the given record type.
// Returns the Answer section or an error.
type dohAnswer struct {
	Name string `json:"name"`
	Type int    `json:"type"`
	TTL  int    `json:"TTL"`
	Data string `json:"data"`
}

type dohResponse struct {
	Status int        `json:"Status"`
	Answer []dohAnswer `json:"Answer"`
}

func dohQuery(ctx context.Context, domain, recordType string) ([]dohAnswer, error) {
	client := dohClient()
	for _, endpoint := range dohEndpoints {
		u := fmt.Sprintf("%s?name=%s&type=%s", endpoint, url.QueryEscape(domain), recordType)
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			continue
		}
		// Cloudflare wants application/dns-json; Google accepts both.
		req.Header.Set("Accept", "application/dns-json")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			continue
		}
		var parsed dohResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			continue
		}
		return parsed.Answer, nil
	}
	return nil, fmt.Errorf("all DoH endpoints failed")
}

// QueryDNS fetches A, AAAA, MX, NS, TXT records in parallel via DoH.
// Returns a populated DNSInfo even if some record types fail.
func QueryDNS(ctx context.Context, domain string) *DNSInfo {
	info := &DNSInfo{}
	if domain == "" {
		info.Error = "empty domain"
		return info
	}

	types := []string{"A", "AAAA", "MX", "NS", "TXT"}
	results := make(map[string][]dohAnswer, len(types))
	errs := make(map[string]error, len(types))

	// Fetch in parallel
	done := make(chan struct{}, len(types))
	for _, t := range types {
		t := t
		go func() {
			ans, err := dohQuery(ctx, domain, t)
			if err != nil {
				errs[t] = err
			} else {
				results[t] = ans
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < len(types); i++ {
		<-done
	}

	// A records
	for _, a := range results["A"] {
		info.A = append(info.A, a.Data)
	}
	// AAAA
	for _, a := range results["AAAA"] {
		info.AAAA = append(info.AAAA, a.Data)
	}
	// MX — store sorted by preference
	for _, a := range results["MX"] {
		pref, host := parseMXData(a.Data)
		info.MX = append(info.MX, DNSMX{Host: host, Pref: pref})
	}
	sort.Slice(info.MX, func(i, j int) bool { return info.MX[i].Pref < info.MX[j].Pref })
	// NS
	for _, a := range results["NS"] {
		info.NS = append(info.NS, strings.TrimSuffix(a.Data, "."))
	}
	// TXT — only short records (skip SPF/DKIM/etc. up to a limit)
	for _, a := range results["TXT"] {
		txt := strings.Trim(a.Data, `"`)
		if len(txt) > 200 {
			continue
		}
		info.TXT = append(info.TXT, txt)
	}

	// Derive email + DNS provider from heuristics
	info.EmailProvider = detectEmailProvider(info.MX, info.TXT)
	info.DNSProvider = detectDNSProvider(info.NS)

	if len(errs) == len(types) {
		info.Error = "all DNS queries failed"
	}
	return info
}

// parseMXData parses "10 mail.example.com." → (10, "mail.example.com")
func parseMXData(s string) (uint16, string) {
	parts := strings.Fields(s)
	if len(parts) < 2 {
		return 0, strings.TrimSuffix(s, ".")
	}
	var pref uint16
	fmt.Sscanf(parts[0], "%d", &pref)
	return pref, strings.TrimSuffix(parts[1], ".")
}

// detectEmailProvider identifies the email hosting provider from MX records.
func detectEmailProvider(mx []DNSMX, txt []string) string {
	if len(mx) == 0 {
		return ""
	}
	// Check primary MX first
	host := strings.ToLower(mx[0].Host)
	switch {
	case strings.Contains(host, "google.com") || strings.Contains(host, "googlemail.com"):
		return "Google Workspace"
	case strings.Contains(host, "outlook.com") || strings.Contains(host, "protection.outlook"):
		return "Microsoft 365"
	case strings.Contains(host, "zoho.com"):
		return "Zoho Mail"
	case strings.Contains(host, "protonmail") || strings.Contains(host, "proton.ch"):
		return "ProtonMail"
	case strings.Contains(host, "fastmail.com") || strings.Contains(host, "messagingengine"):
		return "Fastmail"
	case strings.Contains(host, "mailgun.org"):
		return "Mailgun"
	case strings.Contains(host, "sendgrid.net"):
		return "SendGrid"
	case strings.Contains(host, "amazonaws.com"):
		return "Amazon SES"
	case strings.Contains(host, "mimecast") || strings.Contains(host, "mimecast-offshore"):
		return "Mimecast"
	case strings.Contains(host, "icloud.com") || strings.Contains(host, "apple-dns"):
		return "iCloud Mail"
	case strings.Contains(host, "yandex.net"):
		return "Yandex Mail"
	case strings.Contains(host, "hover.com"):
		return "Hover"
	case strings.Contains(host, "mxhero.com"):
		return "mxhero"
	}
	// SPF hint
	for _, t := range txt {
		lower := strings.ToLower(t)
		if strings.HasPrefix(lower, "v=spf1") {
			if strings.Contains(lower, "include:_spf.google.com") {
				return "Google Workspace"
			}
			if strings.Contains(lower, "include:spf.protection.outlook.com") {
				return "Microsoft 365"
			}
		}
	}
	return ""
}

// detectDNSProvider identifies the authoritative DNS provider from NS records.
func detectDNSProvider(ns []string) string {
	if len(ns) == 0 {
		return ""
	}
	// Join all NS into one string for substring matching
	all := strings.ToLower(strings.Join(ns, " "))
	switch {
	case strings.Contains(all, "cloudflare.com"):
		return "Cloudflare"
	case strings.Contains(all, "awsdns"):
		return "Amazon Route 53"
	case strings.Contains(all, "domaincontrol.com"):
		return "GoDaddy"
	case strings.Contains(all, "googledomains") || strings.Contains(all, "google.com"):
		return "Google Cloud DNS"
	case strings.Contains(all, "azure-dns") || strings.Contains(all, "azure.com"):
		return "Azure DNS"
	case strings.Contains(all, "digitalocean.com"):
		return "DigitalOcean"
	case strings.Contains(all, "hetzner.com"):
		return "Hetzner DNS"
	case strings.Contains(all, "linode.com"):
		return "Linode DNS"
	case strings.Contains(all, "namecheap.com"):
		return "Namecheap"
	case strings.Contains(all, "verisign") || strings.Contains(all, "networksolutions"):
		return "Verisign"
	case strings.Contains(all, "nsone.net"):
		return "NS1"
	case strings.Contains(all, "dnsmadeeasy") || strings.Contains(all, "digicert"):
		return "DNS Made Easy"
	case strings.Contains(all, "ultradns") || strings.Contains(all, "neustar"):
		return "UltraDNS"
	case strings.Contains(all, "constellix") || strings.Contains(all, "cnsglobal"):
		return "Constellix"
	case strings.Contains(all, "rage4.com"):
		return "Rage4"
	case strings.Contains(all, "yandex.net"):
		return "Yandex DNS"
	}
	return ""
}

// ─── WHOIS via RDAP ─────────────────────────────────────────────────

// QueryWHOIS fetches registration data via RDAP. rdap.org bootstraps to
// the correct registrar RDAP endpoint automatically (302 redirect).
func QueryWHOIS(ctx context.Context, domain string) *WHOISInfo {
	info := &WHOISInfo{}
	if domain == "" {
		info.Error = "empty domain"
		return info
	}

	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://rdap.org/domain/"+domain, nil)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	req.Header.Set("Accept", "application/rdap+json")

	resp, err := client.Do(req)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		info.Error = fmt.Sprintf("RDAP status %d", resp.StatusCode)
		return info
	}
	info.RDAPEndpoint = resp.Request.URL.Host

	var rdap struct {
		Events []struct {
			EventAction string `json:"eventAction"`
			EventDate   string `json:"eventDate"`
		} `json:"events"`
		Status []string `json:"status"`
		Entities []struct {
			Roles      []string     `json:"roles"`
			VCardArray [2]interface{} `json:"vcardArray"`
		} `json:"entities"`
		Nameservers []struct {
			LdhName string `json:"ldhName"`
		} `json:"nameservers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rdap); err != nil {
		info.Error = "RDAP parse: " + err.Error()
		return info
	}

	info.Status = rdap.Status
	for _, ns := range rdap.Nameservers {
		info.NameServers = append(info.NameServers, strings.TrimSuffix(ns.LdhName, "."))
	}
	for _, e := range rdap.Events {
		switch e.EventAction {
		case "registration":
			info.CreatedDate = e.EventDate
		case "expiration":
			info.ExpiryDate = e.EventDate
		case "last changed", "last update of RDAP database":
			if info.UpdatedDate == "" {
				info.UpdatedDate = e.EventDate
			}
		}
	}
	// Registrar from entities[?role=registrar].vcardArray[1][?][3]
	for _, ent := range rdap.Entities {
		isRegistrar := false
		for _, r := range ent.Roles {
			if r == "registrar" {
				isRegistrar = true
				break
			}
		}
		if !isRegistrar {
			continue
		}
		// VCardArray[1] is itself a JSON array of [name, type, params, value] tuples
		inner, ok := ent.VCardArray[1].([]interface{})
		if !ok {
			continue
		}
		for _, f := range inner {
			parts, ok := f.([]interface{})
			if !ok || len(parts) < 4 {
				continue
			}
			name, _ := parts[0].(string)
			if name == "fn" {
				if v, ok := parts[3].(string); ok {
					info.Registrar = v
					break
				}
			}
		}
	}

	// Compute age + days-to-expiry
	if info.CreatedDate != "" {
		if t, err := time.Parse(time.RFC3339, info.CreatedDate); err == nil {
			info.DomainAgeDays = int(time.Since(t).Hours() / 24)
		}
	}
	if info.ExpiryDate != "" {
		if t, err := time.Parse(time.RFC3339, info.ExpiryDate); err == nil {
			info.DaysToExpiry = int(time.Until(t).Hours() / 24)
		}
	}
	return info
}

// ─── SSL via fresh TLS dial ─────────────────────────────────────────

// QuerySSL performs a fresh TLS handshake to host:443 and extracts the
// peer certificate. Doesn't reuse chromedp's connection because we want
// a clean read independent of the page navigation state.
func QuerySSL(ctx context.Context, host string) *SSLInfo {
	info := &SSLInfo{}
	if host == "" {
		info.Error = "empty host"
		return info
	}
	if !strings.Contains(host, ":") {
		host += ":443"
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", host, &tls.Config{
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		info.Error = err.Error()
		return info
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		info.Error = "no peer certs"
		return info
	}
	cert := state.PeerCertificates[0]

	info.Issuer = cert.Issuer.CommonName
	if info.Issuer == "" && len(cert.Issuer.Organization) > 0 {
		info.Issuer = cert.Issuer.Organization[0]
	}
	info.Subject = cert.Subject.CommonName
	info.NotBefore = cert.NotBefore
	info.NotAfter = cert.NotAfter
	info.DaysUntilExpiry = int(time.Until(cert.NotAfter).Hours() / 24)
	info.SANCount = len(cert.DNSNames) + len(cert.IPAddresses)
	info.SelfSigned = string(cert.RawIssuer) == string(cert.RawSubject)
	info.TLSVersion = tlsVersionString(state.Version)
	info.CipherSuite = tls.CipherSuiteName(state.CipherSuite)

	_ = ctx // ctx unused — net.Dialer handles its own
	return info
}

func tlsVersionString(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	}
	return fmt.Sprintf("unknown (0x%04x)", v)
}

// ─── Perf aggregation ───────────────────────────────────────────────

// computePerfMetrics aggregates performance data from a completed scan.
// Counters come from the HTML/scripts/CSS we already collected.
func computePerfMetrics(html string, scripts, cssLinks []string, htmlSize int64, ttfb, totalDuration time.Duration, metaTags map[string]string) *PerfMetrics {
	p := &PerfMetrics{
		TTFB:          ttfb.Milliseconds(),
		TotalDuration: totalDuration.Milliseconds(),
		HTMLSizeBytes: htmlSize,
		ScriptCount:   len(scripts),
		StylesheetCount: len(cssLinks),
		MetaTagsCount: len(metaTags),
	}

	// Rough size estimates from HTML content length (we don't fetch external assets)
	p.JSBundleSizeBytes = int64(len(html)) / 20 // heuristic: ~5% of HTML is JS-related
	p.CSSBundleSizeBytes = int64(len(html)) / 40
	p.TotalAssetBytes = p.HTMLSizeBytes + p.JSBundleSizeBytes + p.CSSBundleSizeBytes

	// Count headings, links, images
	lower := strings.ToLower(html)
	p.HeadingsCount = strings.Count(lower, "<h1") + strings.Count(lower, "<h2") + strings.Count(lower, "<h3")
	p.LinksCount = strings.Count(lower, "<a ") + strings.Count(lower, "<a\t") + strings.Count(lower, "<link ")
	p.ImagesCount = strings.Count(lower, "<img ") + strings.Count(lower, "<img\t")

	// Inline vs external script sizes (rough)
	inlineStart := 0
	for {
		i := strings.Index(lower[inlineStart:], "<script")
		if i < 0 {
			break
		}
		j := strings.Index(lower[inlineStart+i:], "</script>")
		if j < 0 {
			break
		}
		block := lower[inlineStart+i : inlineStart+i+j]
		if strings.Contains(block, " src=") {
			// external
			p.ExternalScriptBytes += int64(j)
		} else {
			// inline
			p.InlineScriptBytes += int64(j)
		}
		inlineStart += i + j + 9
	}
	return p
}