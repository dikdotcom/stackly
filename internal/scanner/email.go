package scanner

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// ExtractEmails scans HTML for contact email addresses. It checks:
//   - mailto: links in href attributes
//   - plain text patterns in body
//   - data-* attributes (data-email, data-mail, data-contact, etc.)
//   - JSON-LD blocks (ContactPoint.email, Organization.email, Person.email, etc.)
//   - SSR hydration data (window.__NEXT_DATA__, __NUXT__, __INITIAL_STATE__)
//
// All results are deduped, lowercased, sorted, and filtered against
// noise patterns (image assets, placeholders). Max 20 emails returned.
func ExtractEmails(html string) []string {
	seen := make(map[string]bool)

	addEmails := func(emails []string) {
		for _, e := range emails {
			e = strings.ToLower(strings.TrimSpace(e))
			if isValidEmail(e) && !isEmailNoise(e) {
				seen[e] = true
			}
		}
	}

	addEmails(extractMailtoEmails(html))
	addEmails(extractPlaintextEmails(html))
	addEmails(extractDataAttrEmails(html))
	addEmails(extractJSONLDEmails(html))
	addEmails(extractSSRDataEmails(html))

	out := make([]string, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Strings(out)

	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

// extractMailtoEmails finds emails in mailto: links
var mailtoRe = regexp.MustCompile(`(?i)mailto:\s*([a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,})`)

func extractMailtoEmails(html string) []string {
	matches := mailtoRe.FindAllStringSubmatch(html, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// extractPlaintextEmails finds email-like patterns in plain text
// (case-insensitive). The lookbehind/lookahead are NOT used because
// Go regexp (RE2) does not support them; we filter post-match instead.
var plaintextEmailRe = regexp.MustCompile(`([a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,})`)

func extractPlaintextEmails(html string) []string {
	matches := plaintextEmailRe.FindAllString(html, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m)
	}
	return out
}

// extractDataAttrEmails finds emails in data-* attributes like:
// data-email, data-mail, data-contact, data-user-email, data-company-email,
// data-support-email, data-sales-email. The attribute value can be a bare
// email or a mailto: URI.
var dataAttrEmailRe = regexp.MustCompile(
	`(?i)data-(?:email|mail|contact|user[-_]?email|company[-_]?email|support[-_]?email|sales[-_]?email|admin[-_]?email|info[-_]?email)` +
		`\s*=\s*["']([^"']+)["']`,
)

func extractDataAttrEmails(html string) []string {
	matches := dataAttrEmailRe.FindAllStringSubmatch(html, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		val := strings.TrimSpace(m[1])
		// Strip mailto: prefix if present
		val = strings.TrimPrefix(val, "mailto:")
		val = strings.TrimPrefix(val, "MAILTO:")
		// Bare email case
		if strings.Contains(val, "@") {
			out = append(out, val)
		}
	}
	return out
}

// extractJSONLDEmails finds emails inside <script type="application/ld+json">
// blocks. Walks the JSON tree looking for keys named "email" or "ContactPoint"
// (which itself contains an email sub-field). Handles nested @graph arrays.
var jsonLDRe = regexp.MustCompile(`(?s)<script[^>]*type=["']application/ld\+json["'][^>]*>(.*?)</script>`)

func extractJSONLDEmails(html string) []string {
	blocks := jsonLDRe.FindAllStringSubmatch(html, -1)
	var out []string
	for _, b := range blocks {
		raw := strings.TrimSpace(b[1])
		if raw == "" {
			continue
		}
		var data interface{}
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			continue // malformed JSON-LD — skip silently
		}
		out = append(out, walkJSONLDForEmails(data)...)
	}
	return out
}

// walkJSONLDForEmails recursively walks a JSON-LD structure and returns
// all string values that look like emails. Schema.org Organization,
// Person, ContactPoint all use "email" as a top-level field, so we just
// look for any string that contains "@" when we hit an email-shaped key.
func walkJSONLDForEmails(v interface{}) []string {
	var out []string
	switch x := v.(type) {
	case map[string]interface{}:
		for k, val := range x {
			lk := strings.ToLower(k)
			// Direct email field
			if lk == "email" {
				switch e := val.(type) {
				case string:
					if strings.Contains(e, "@") {
						out = append(out, e)
					}
				case []interface{}:
					for _, item := range e {
						if s, ok := item.(string); ok && strings.Contains(s, "@") {
							out = append(out, s)
						}
					}
				}
				continue
			}
			// Recurse into nested objects/arrays
			out = append(out, walkJSONLDForEmails(val)...)
		}
	case []interface{}:
		for _, item := range x {
			out = append(out, walkJSONLDForEmails(item)...)
		}
	}
	return out
}

// extractSSRDataEmails finds emails inside Next.js / Nuxt / generic SSR
// hydration scripts. These frameworks embed the server-rendered data
// as JSON inside <script> tags so client-side hydration can use it.
// Emails buried in this data (e.g. author profiles, contact info) are
// not visible in the rendered DOM via simple text scan, so we parse
// the JSON itself.
var ssrScriptRe = regexp.MustCompile(
	`(?s)<script[^>]*\bid=["'](?:__NEXT_DATA__|__NUXT__|__INITIAL_STATE__|__APOLLO_STATE__|__REDUX_STATE__)["'][^>]*>(.*?)</script>`,
)

func extractSSRDataEmails(html string) []string {
	blocks := ssrScriptRe.FindAllStringSubmatch(html, -1)
	var out []string
	for _, b := range blocks {
		raw := strings.TrimSpace(b[1])
		if raw == "" {
			continue
		}
		// Nuxt's __NUXT__ is often a JS expression like:
		//   window.__NUXT__={serverRendered:true,data:[{contact:"a@b.c"}]}
		// not pure JSON. We attempt strict JSON first, then a loose
		// fallback that strips the assignment wrapper.
		var data interface{}
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			loose := stripJSAssignment(raw)
			if err2 := json.Unmarshal([]byte(loose), &data); err2 != nil {
				continue
			}
		}
		out = append(out, walkJSONLDForEmails(data)...)
	}
	return out
}

// stripJSAssignment removes the "window.__FOO__=" or "var __FOO__ ="
// prefix and trailing semicolon from an embedded SSR script body.
// Returns the JSON-ish portion best-effort; not guaranteed to be valid.
func stripJSAssignment(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "="); i != -1 && i < 60 {
		s = strings.TrimSpace(s[i+1:])
	}
	s = strings.TrimSuffix(s, ";")
	return s
}

// isValidEmail does a quick syntactic sanity check. The regex above
// already enforces basic shape, so this is mostly a length check.
func isValidEmail(e string) bool {
	if len(e) < 5 || len(e) > 254 {
		return false
	}
	at := strings.Index(e, "@")
	if at <= 0 || at == len(e)-1 {
		return false
	}
	if !strings.Contains(e[at+1:], ".") {
		return false
	}
	return true
}

// isEmailNoise filters out false positives: image-asset suffixes,
// CSS units, common placeholders, and well-known non-contact patterns.
func isEmailNoise(e string) bool {
	lower := strings.ToLower(e)
	noise := []string{
		"@2x.png", "@3x.png", "@1x.png",
		"@1x.jpg", "@2x.jpg", "@3x.jpg",
		"@1x.webp", "@2x.webp",
		"@1x.gif", "@2x.gif",
		"example.com", "example.org", "example.net",
		"yourdomain.com", "domain.com", "company.com",
		"email.com", "wixpress.com", "sentry.io",
		"cloudflare.com", "schema.org",
	}
	for _, n := range noise {
		if strings.Contains(lower, n) {
			return true
		}
	}
	// Reject file extensions in domain (image@2x.png, font@bold.woff)
	if strings.Contains(e[atIdx(e)+1:], ".png") ||
		strings.Contains(e[atIdx(e)+1:], ".jpg") ||
		strings.Contains(e[atIdx(e)+1:], ".gif") ||
		strings.Contains(e[atIdx(e)+1:], ".webp") ||
		strings.Contains(e[atIdx(e)+1:], ".svg") {
		return true
	}
	return false
}

func atIdx(s string) int {
	for i, c := range s {
		if c == '@' {
			return i
		}
	}
	return -1
}
