package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFindContactPages(t *testing.T) {
	tests := []struct {
		name    string
		html    string
		baseURL string
		want    []string
	}{
		{
			name:    "empty",
			html:    "",
			baseURL: "https://example.com/",
			want:    nil,
		},
		{
			name: "text match contact",
			html: `<a href="/contact">Contact us</a>`,
			baseURL: "https://example.com/",
			want:    []string{"https://example.com/contact"},
		},
		{
			name: "href fragment contact",
			html: `<a href="/about/contact">x</a>`,
			baseURL: "https://example.com/",
			want:    []string{"https://example.com/about/contact"},
		},
		{
			name: "indonesian kontak",
			html: `<a href="/kontak">Hubungi Kami</a>`,
			baseURL: "https://example.go.id/",
			want:    []string{"https://example.go.id/kontak"},
		},
		{
			name: "tentang-kami",
			html: `<a href="/tentang-kami">Tentang Kami</a>`,
			baseURL: "https://example.go.id/",
			want:    []string{"https://example.go.id/tentang-kami"},
		},
		{
			name: "absolute external skipped",
			html: `<a href="https://other.com/contact">x</a>`,
			baseURL: "https://example.com/",
			want:    nil,
		},
		{
			name: "mailto skipped",
			html: `<a href="mailto:hi@x.com">x</a>`,
			baseURL: "https://example.com/",
			want:    nil,
		},
		{
			name: "tel skipped",
			html: `<a href="tel:+62123">x</a>`,
			baseURL: "https://example.com/",
			want:    nil,
		},
		{
			name: "unrelated link ignored",
			html: `<a href="/products">Our Products</a>
			        <a href="/about">About</a>`,
			baseURL: "https://example.com/",
			want:    []string{"https://example.com/about"},
		},
		{
			name: "capped at MaxContactPages",
			html: `<a href="/contact">x</a>
			        <a href="/about">y</a>
			        <a href="/team">z</a>
			        <a href="/staff">w</a>
			        <a href="/support">v</a>`,
			baseURL: "https://example.com/",
			want:    []string{"https://example.com/contact", "https://example.com/about"},
		},
		{
			name: "ranking href fragment wins over text",
			html: `<a href="/products/contact-info">View</a>
			        <a href="/about">Read about us</a>`,
			baseURL: "https://example.com/",
			// "about" link scores 2 (text "about") + 5 (href "/about") = 7
			// "products/contact-info" scores 0 (text "view") + 5 (href "/contact") = 5
			// about wins
			want: []string{"https://example.com/about", "https://example.com/products/contact-info"},
		},
		{
			name: "base url with path",
			html: `<a href="/contact">x</a>`,
			baseURL: "https://example.com/some/page",
			want:    []string{"https://example.com/contact"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindContactPages(tt.html, tt.baseURL)
			if !equalSlices(got, tt.want) {
				t.Errorf("FindContactPages() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFetchContactPage(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
			w.Write([]byte("<html><body>contact@a.com</body></html>"))
		}))
		defer srv.Close()

		body, err := FetchContactPage(context.Background(), srv.URL+"/contact", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(body, "contact@a.com") {
			t.Errorf("expected body to contain contact@a.com, got %q", body)
		}
	})

	t.Run("404 returns error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(404)
		}))
		defer srv.Close()

		_, err := FetchContactPage(context.Background(), srv.URL+"/missing", "")
		if err == nil {
			t.Errorf("expected error on 404, got nil")
		}
	})

	t.Run("empty url returns empty", func(t *testing.T) {
		body, err := FetchContactPage(context.Background(), "", "")
		if err != nil {
			t.Errorf("unexpected error for empty url: %v", err)
		}
		if body != "" {
			t.Errorf("expected empty body, got %q", body)
		}
	})

	t.Run("respects context cancel", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// hang
			<-r.Context().Done()
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		_, err := FetchContactPage(ctx, srv.URL+"/hang", "")
		if err == nil {
			t.Errorf("expected error on cancelled context, got nil")
		}
	})
}
