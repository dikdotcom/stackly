package report

import (
	"strings"
	"testing"
	"time"

	"github.com/dikdotcom/stackly/internal/fingerprint"
	"github.com/dikdotcom/stackly/internal/scanner"
)

func TestRender_BasicShape(t *testing.T) {
	r := &scanner.ScanResult{
		URL:          "https://example.com/",
		Results:      []scanner.Result{{Technology: fingerprint.Technology{Name: "Cloudflare", Category: "cdn"}, Confidence: 100}},
		ScanDuration: 500 * time.Millisecond,
	}
	html := Render(Data{Result: r, JobID: "abc123", Generated: time.Now()})

	for _, want := range []string{
		"<!DOCTYPE html>",
		"https://example.com/",
		"abc123",
		"Cloudflare",
		"Stackly",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRender_EscapesXSS(t *testing.T) {
	// Malicious URL — must not inject scripts.
	r := &scanner.ScanResult{
		URL: `https://example.com/"><script>alert(1)</script>`,
	}
	html := Render(Data{Result: r, Generated: time.Now()})

	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("XSS payload not escaped")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("expected escaped &lt;script&gt;")
	}
}

func TestRender_NilResult(t *testing.T) {
	html := Render(Data{Result: nil, Generated: time.Now()})
	if !strings.Contains(html, "Report unavailable") {
		t.Errorf("nil result should produce error page, got: %s", html[:min(200, len(html))])
	}
}

func TestRender_EmptyTechList(t *testing.T) {
	r := &scanner.ScanResult{URL: "https://x.com"}
	html := Render(Data{Result: r, Generated: time.Now()})
	if !strings.Contains(html, "No technologies detected") {
		t.Error("expected empty-state message")
	}
}

func TestRender_EnrichmentCards(t *testing.T) {
	r := &scanner.ScanResult{
		URL: "https://x.com",
		DNS: &scanner.DNSInfo{
			DNSProvider: "Cloudflare",
			A:           []string{"1.2.3.4"},
			NS:          []string{"ns1.example.com"},
		},
		SSL: &scanner.SSLInfo{
			Issuer:          "Let's Encrypt",
			TLSVersion:      "TLS 1.3",
			DaysUntilExpiry: 45,
		},
		Perf: &scanner.PerfMetrics{
			TTFB:          120,
			HTMLSizeBytes: 1234,
		},
	}
	html := Render(Data{Result: r, Generated: time.Now()})

	for _, want := range []string{
		"Cloudflare", "1.2.3.4", "Let&#39;s Encrypt", "TLS 1.3",
		"120 ms", "1.2 KB", // humanBytes output
	} {
		if !strings.Contains(html, want) {
			t.Errorf("enrichment card missing %q", want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
