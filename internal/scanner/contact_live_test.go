package scanner

import (
	"fmt"
	"strings"
	"testing"
)

// TestFindContactPages_DetikHTML is a live-shaped test against a
// fragment of www.detik.com footer links (captured 2026-06-19).
// Should pick up /redaksi, /kotak-pos, /karir as contact candidates.
func TestFindContactPages_DetikHTML(t *testing.T) {
	html := `<html><body>
		<footer>
			<a href="https://www.detik.com/redaksi">Redaksi</a>
			<a href="https://www.detik.com/pedoman-media">Pedoman Media Siber</a>
			<a href="https://www.detik.com/karir">Karir</a>
			<a href="https://www.detik.com/kotak-pos">Kotak Pos</a>
			<a href="https://www.detik.com/media-partner">Media Partner</a>
			<a href="https://www.detik.com/info-iklan">Info Iklan</a>
			<a href="https://www.detik.com/privacy-policy">Privacy Policy</a>
			<a href="https://www.detik.com/disclaimer">Disclaimer</a>
		</footer>
	</body></html>`

	got := FindContactPages(html, "https://www.detik.com/")
	t.Logf("Found %d candidates:", len(got))
	for _, u := range got {
		t.Logf("  %s", u)
	}
	if len(got) == 0 {
		t.Errorf("expected at least 1 contact candidate from Detik footer")
	}

	// Specifically check redaksi is picked up
	hasRedaksi := false
	for _, u := range got {
		if strings.Contains(u, "/redaksi") {
			hasRedaksi = true
		}
	}
	if !hasRedaksi {
		t.Errorf("expected /redaksi in candidates, got none")
	}
}

// TestFindContactPages_KontakHubungi verifies Indonesian contact keywords.
func TestFindContactPages_KontakHubungi(t *testing.T) {
	html := `<html><body>
		<a href="/kontak">Hubungi Kami</a>
		<a href="/tentang-kami">Tentang Kami</a>
		<a href="/produk">Produk</a>
	</body></html>`

	got := FindContactPages(html, "https://example.go.id/")
	fmt.Println("Found:", got)
	if len(got) < 1 {
		t.Errorf("expected at least 1 candidate")
	}
}
