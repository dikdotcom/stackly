package scanner

import (
	"sort"
	"strings"
	"testing"
)

func TestExtractEmails_JSONLD(t *testing.T) {
	tests := []struct {
		name string
		html string
		want []string
	}{
		{
			name: "organization with email",
			html: `<html><head>
				<script type="application/ld+json">
				{"@context":"https://schema.org","@type":"Organization","name":"Acme","email":"hello@acme.com"}
				</script>
			</head><body></body></html>`,
			want: []string{"hello@acme.com"},
		},
		{
			name: "contactpoint nested",
			html: `<html><head>
				<script type="application/ld+json">
				{
					"@type": "Organization",
					"contactPoint": {
						"@type": "ContactPoint",
						"email": "support@acme.com",
						"telephone": "+1-555-0100"
					}
				}
				</script>
			</head><body></body></html>`,
			want: []string{"support@acme.com"},
		},
		{
			name: "graph array",
			html: `<html><head>
				<script type="application/ld+json">
				{
					"@graph": [
						{"@type":"Person","name":"Jane","email":"jane@acme.com"},
						{"@type":"Person","name":"Joe","email":"joe@acme.com"}
					]
				}
				</script>
			</head><body></body></html>`,
			want: []string{"jane@acme.com", "joe@acme.com"},
		},
		{
			name: "malformed JSON-LD skipped",
			html: `<html><head>
				<script type="application/ld+json">{broken json</script>
			</head><body><a href="mailto:real@site.com">x</a></body></html>`,
			want: []string{"real@site.com"},
		},
		{
			name: "email as array",
			html: `<html><head>
				<script type="application/ld+json">
				{"@type":"Organization","email":["first@acme.com","second@acme.com"]}
				</script>
			</head><body></body></html>`,
			want: []string{"first@acme.com", "second@acme.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEmails(tt.html)
			sort.Strings(got)
			if !equalSlices(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractEmails_DataAttrs(t *testing.T) {
	tests := []struct {
		name string
		html string
		want []string
	}{
		{
			name: "data-email bare",
			html: `<a data-email="contact@site.com">x</a>`,
			want: []string{"contact@site.com"},
		},
		{
			name: "data-mail",
			html: `<span data-mail="info@acme.io">x</span>`,
			want: []string{"info@acme.io"},
		},
		{
			name: "data-contact with mailto",
			html: `<div data-contact="mailto:sales@acme.io">x</div>`,
			want: []string{"sales@acme.io"},
		},
		{
			name: "data-company-email",
			html: `<footer data-company-email="corp@bigco.com"></footer>`,
			want: []string{"corp@bigco.com"},
		},
		{
			name: "multiple data attrs",
			html: `<div>
				<a data-support-email="help@site.com">help</a>
				<a data-sales-email="buy@site.com">sales</a>
			</div>`,
			want: []string{"buy@site.com", "help@site.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEmails(tt.html)
			sort.Strings(got)
			if !equalSlices(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractEmails_SSRData(t *testing.T) {
	tests := []struct {
		name string
		html string
		want []string
	}{
		{
			name: "nextjs data",
			html: `<html><body>
				<script id="__NEXT_DATA__" type="application/json">
				{"props":{"pageProps":{"contact":{"email":"admin@nextsite.com"}}}}
				</script>
			</body></html>`,
			want: []string{"admin@nextsite.com"},
		},
		{
			name: "nextjs with mailto in HTML",
			html: `<html><body>
				<a href="mailto:foo@bar.com">x</a>
				<script id="__NEXT_DATA__" type="application/json">
				{"props":{"pageProps":{"author":"jane@blog.io"}}}
				</script>
			</body></html>`,
			want: []string{"foo@bar.com", "jane@blog.io"},
		},
		{
			name: "nuxt with assignment",
			html: `<html><body>
				<script>window.__NUXT__={"data":[{"email":"hi@nuxtsite.com"}]}</script>
			</body></html>`,
			want: []string{"hi@nuxtsite.com"},
		},
		{
			name: "redux state",
			html: `<html><body>
				<script id="__REDUX_STATE__" type="application/json">
				{"user":{"profile":{"contact":"redux@app.com"}}}
				</script>
			</body></html>`,
			want: []string{"redux@app.com"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEmails(tt.html)
			sort.Strings(got)
			if !equalSlices(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractEmails_CapAt20(t *testing.T) {
	// Build HTML with 30 unique emails
	var sb strings.Builder
	for i := 0; i < 30; i++ {
		sb.WriteString("<a href=\"mailto:user")
		sb.WriteString(itoa(i))
		sb.WriteString("@site.com\">x</a> ")
	}
	got := ExtractEmails(sb.String())
	if len(got) > 20 {
		t.Errorf("expected cap at 20, got %d", len(got))
	}
}

func TestExtractEmails_DedupesAcrossExtractors(t *testing.T) {
	html := `<html><head>
		<script type="application/ld+json">
		{"email":"dup@site.com"}
		</script>
	</head><body>
		<a href="mailto:dup@site.com">x</a>
		<div data-email="dup@site.com">y</div>
	</body></html>`
	got := ExtractEmails(html)
	if len(got) != 1 {
		t.Errorf("expected 1 deduped email, got %d: %v", len(got), got)
	}
	if got[0] != "dup@site.com" {
		t.Errorf("got %q, want dup@site.com", got[0])
	}
}

// Helper for tests
func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
