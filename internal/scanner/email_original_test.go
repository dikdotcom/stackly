package scanner

import (
	"sort"
	"testing"
)

// TestExtractEmails_OriginalCases locks in the behavior of the
// pre-JSON-LD-extractor version. These cases cover plain text,
// mailto, image-asset noise, and placeholder filtering.
func TestExtractEmails_OriginalCases(t *testing.T) {
	tests := []struct {
		name string
		html string
		want []string
	}{
		{
			name: "empty",
			html: "",
			want: nil,
		},
		{
			name: "no emails",
			html: `<html><body><p>Hello world</p></body></html>`,
			want: nil,
		},
		{
			name: "single mailto",
			html: `<a href="mailto:hello@example.com">Contact</a>`,
			// hello@example.com is in noise list (example.com placeholder)
			want: nil,
		},
		{
			name: "single plaintext",
			html: `<p>Reach me at jane.doe@company.io for inquiries.</p>`,
			want: []string{"jane.doe@company.io"},
		},
		{
			name: "multiple mixed",
			html: `<a href="mailto:support@acme.com">Support</a>
			        <p>Press: press@acme.com</p>
			        <a href="mailto:sales@acme.com">Sales</a>`,
			want: []string{"press@acme.com", "sales@acme.com", "support@acme.com"},
		},
		{
			name: "filters srcset 2x noise",
			html: `<img srcset="foo@2x.png 2x, foo.png 1x">
			        <p>Real: real@site.com</p>`,
			want: []string{"real@site.com"},
		},
		{
			name: "filters placeholder",
			html: `<a href="mailto:you@example.com">Test</a>
			        <a href="mailto:real@site.com">Real</a>`,
			want: []string{"real@site.com"},
		},
		{
			name: "dedupes",
			html: `<a href="mailto:foo@bar.com">x</a>
			        <p>foo@bar.com</p>
			        <a href="mailto:foo@bar.com">y</a>`,
			want: []string{"foo@bar.com"},
		},
		{
			name: "lowercases",
			html: `<a href="mailto:Hello@Example.COM">x</a>`,
			// example.com is in noise list
			want: nil,
		},
		{
			name: "filters image asset",
			html: `<img src="icon@2x.png">`,
			want: nil,
		},
		{
			name: "accepts common forms",
			html: `<a href="mailto:user.name+tag@sub.example.co.uk">x</a>
			        <p>_underscore@test.io</p>`,
			// user.name+tag@sub.example.co.uk — example. is a substring of
			// example.com? No: example.co.uk contains "example." but not
			// "example.com". So it should pass noise filter.
			// Actually wait: "example.com" is in noise as substring. Does
			// "example.co.uk" contain "example.com"? No, it contains
			// "example.co" and "e.co.uk" but not "example.com". Pass.
			want: []string{"_underscore@test.io", "user.name+tag@sub.example.co.uk"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractEmails(tt.html)
			sort.Strings(got)
			if !equalSlices(got, tt.want) {
				t.Errorf("ExtractEmails() = %v, want %v", got, tt.want)
			}
		})
	}
}
