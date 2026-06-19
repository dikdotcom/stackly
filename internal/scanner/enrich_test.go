package scanner

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestQueryDNS_Live fetches real DNS for example.com (stable, no-flake record).
// Skip if outbound HTTPS isn't available (CI environments, sandboxes).
func TestQueryDNS_Live(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live DNS test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	info := QueryDNS(ctx, "example.com")
	if info.Error != "" {
		t.Skipf("DNS unavailable in this environment: %s", info.Error)
	}
	if len(info.NS) == 0 {
		t.Errorf("expected NS records for example.com, got none")
	}
	if len(info.A) == 0 {
		t.Errorf("expected A records for example.com, got none")
	}
	if info.DNSProvider == "" {
		t.Errorf("expected DNSProvider heuristic to fire, got empty")
	}
	t.Logf("example.com: NS=%v, A=%v, Provider=%s", info.NS, info.A, info.DNSProvider)
}

// TestDetectEmailProvider validates the heuristic against known MX strings.
func TestDetectEmailProvider(t *testing.T) {
	cases := []struct {
		mx   []DNSMX
		want string
	}{
		{[]DNSMX{{Host: "aspmx.l.google.com", Pref: 10}}, "Google Workspace"},
		{[]DNSMX{{Host: "mx.zoho.com", Pref: 10}}, "Zoho Mail"},
		{[]DNSMX{{Host: "mail.protection.outlook.com", Pref: 10}}, "Microsoft 365"},
		{[]DNSMX{{Host: "mail.example.com", Pref: 10}}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		got := detectEmailProvider(c.mx, nil)
		if got != c.want {
			t.Errorf("detectEmailProvider(%v) = %q, want %q", c.mx, got, c.want)
		}
	}
}

// TestDetectDNSProvider validates the heuristic against known NS strings.
func TestDetectDNSProvider(t *testing.T) {
	cases := []struct {
		ns   []string
		want string
	}{
		{[]string{"ns1.cloudflare.com", "ns2.cloudflare.com"}, "Cloudflare"},
		{[]string{"ns-721.awsdns-26.net", "ns-1390.awsdns-45.org"}, "Amazon Route 53"},
		{[]string{"ns01.domaincontrol.com"}, "GoDaddy"},
		{[]string{"ns1.digitalocean.com"}, "DigitalOcean"},
		{[]string{"ns.unknown-provider.io"}, ""},
		{nil, ""},
	}
	for _, c := range cases {
		got := detectDNSProvider(c.ns)
		if got != c.want {
			t.Errorf("detectDNSProvider(%v) = %q, want %q", c.ns, got, c.want)
		}
	}
}

// TestRootDomain validates the eTLD+1 heuristic.
func TestRootDomain(t *testing.T) {
	cases := map[string]string{
		"example.com":       "example.com",
		"www.example.com":   "example.com",
		"a.b.c.example.io":  "example.io",
		"localhost":         "localhost",
		"sub.example.co.uk": "co.uk", // intentionally over-trims (no PSL)
		"":                  "",
		"single":            "single",
	}
	for in, want := range cases {
		got := rootDomain(in)
		if got != want {
			t.Errorf("rootDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestExtractHost validates URL hostname extraction.
func TestExtractHost(t *testing.T) {
	cases := map[string]string{
		"https://example.com":       "example.com",
		"https://example.com:443":   "example.com",
		"https://sub.example.com/x": "sub.example.com",
		"not a url":                 "",
	}
	for in, want := range cases {
		got := extractHost(in)
		if got != want {
			t.Errorf("extractHost(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestComputePerfMetrics validates the perf aggregator.
func TestComputePerfMetrics(t *testing.T) {
	html := `<!DOCTYPE html>
<html>
<head>
  <title>Test</title>
  <script>console.log("inline");</script>
  <script src="a.js"></script>
  <link rel="stylesheet" href="x.css">
</head>
<body>
  <h1>Title</h1>
  <h2>Subtitle</h2>
  <a href="/foo">foo</a>
  <a href="/bar">bar</a>
  <img src="logo.png">
</body>
</html>`
	scripts := []string{"https://example.com/a.js", "https://example.com/b.js"}
	css := []string{"https://example.com/x.css"}
	meta := map[string]string{"description": "test"}
	dur := 1500 * time.Millisecond

	p := computePerfMetrics(html, scripts, css, int64(len(html)), dur, dur, meta)
	if p.ScriptCount != 2 {
		t.Errorf("ScriptCount = %d, want 2", p.ScriptCount)
	}
	if p.StylesheetCount != 1 {
		t.Errorf("StylesheetCount = %d, want 1", p.StylesheetCount)
	}
	if p.HeadingsCount != 2 {
		t.Errorf("HeadingsCount = %d, want 2 (h1+h2)", p.HeadingsCount)
	}
	if p.ImagesCount != 1 {
		t.Errorf("ImagesCount = %d, want 1", p.ImagesCount)
	}
	if p.LinksCount < 2 {
		t.Errorf("LinksCount = %d, want >=2", p.LinksCount)
	}
	if p.HTMLSizeBytes != int64(len(html)) {
		t.Errorf("HTMLSizeBytes = %d, want %d", p.HTMLSizeBytes, len(html))
	}
	if p.InlineScriptBytes == 0 {
		t.Errorf("InlineScriptBytes = 0, want > 0")
	}
	if p.ExternalScriptBytes == 0 {
		t.Errorf("ExternalScriptBytes = 0, want > 0 (a.js src in script tag)")
	}
	if p.TotalDuration != dur.Milliseconds() {
		t.Errorf("TotalDuration = %v, want %v", p.TotalDuration, dur.Milliseconds())
	}
}

// TestQuerySSL_Live runs against a real TLS endpoint.
func TestQuerySSL_Live(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live SSL test in short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	info := QuerySSL(ctx, "example.com")
	if info.Error != "" {
		t.Skipf("SSL probe unavailable: %s", info.Error)
	}
	if info.Issuer == "" {
		t.Errorf("expected Issuer to be set, got empty")
	}
	if info.DaysUntilExpiry <= 0 {
		t.Errorf("expected DaysUntilExpiry > 0, got %d", info.DaysUntilExpiry)
	}
	if !strings.HasPrefix(info.TLSVersion, "TLS 1.") {
		t.Errorf("expected TLS 1.x, got %q", info.TLSVersion)
	}
	t.Logf("example.com SSL: issuer=%s expires_in=%dd TLS=%s SANs=%d",
		info.Issuer, info.DaysUntilExpiry, info.TLSVersion, info.SANCount)
}
