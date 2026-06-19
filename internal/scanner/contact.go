package scanner

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// MaxContactPages caps how many contact pages we'll try to fetch per
// scan. Most sites have a single /contact or /kontak page; anything
// beyond 2 is almost always noise.
const MaxContactPages = 2

// contactKeywords are the words that signal a link might be a contact
// page. We match either in the link's text content or in its href.
// Multi-language so we catch sites that localize (e.g. "kontak" in ID,
// "联系我们" not supported because we ASCII-match only).
var contactKeywords = []string{
	"contact", "kontak", "hubungi", "tentang", "about",
	"team", "staff", "author", "profil", "profile",
	"get in touch", "reach us", "write to us",
	"customer service", "support", "help",
	"redaksi", "kotak pos", "kotakpos",
}

// contactHrefFragments are path segments that strongly suggest a
// contact page even if the link text doesn't match. We accept both
// English and Indonesian ("kontak" / "hubungi" / "tentang-kami" / "redaksi").
var contactHrefFragments = []string{
	"/contact", "/kontak", "/hubungi", "/tentang",
	"/about", "/team", "/staff", "/author", "/profile",
	"/customer-service", "/support", "/help",
	"contact-us", "contact.php", "contact.html",
	"kontak-kami", "hubungi-kami", "tentang-kami",
	"/redaksi", "/kotak-pos", "/kotakpos", "/karir",
}

// FindContactPages extracts candidate contact page URLs from HTML.
// Returns up to MaxContactPages absolute URLs, same-origin only,
// de-duplicated and ranked by signal strength (href match > text match).
func FindContactPages(html, baseURL string) []string {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}

	// Match <a href="...">label</a>. The href regex is permissive
	// on purpose: real-world HTML is full of attribute ordering and
	// quoting variations.
	linkRe := regexp.MustCompile(`(?is)<a\s+[^>]*?href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
	matches := linkRe.FindAllStringSubmatch(html, -1)

	type candidate struct {
		href  string
		score int
	}
	seen := make(map[string]bool)
	var candidates []candidate

	for _, m := range matches {
		rawHref := strings.TrimSpace(m[1])
		text := stripTags(strings.ToLower(m[2]))
		hrefLower := strings.ToLower(rawHref)

		// Skip non-HTTP(S) (mailto:, tel:, javascript:, #)
		if !strings.HasPrefix(rawHref, "http://") &&
			!strings.HasPrefix(rawHref, "https://") &&
			!strings.HasPrefix(rawHref, "/") &&
			!strings.HasPrefix(rawHref, "./") &&
			!strings.HasPrefix(rawHref, "../") {
			continue
		}

		score := 0
		for _, kw := range contactKeywords {
			if strings.Contains(text, kw) {
				score += 2
				break
			}
		}
		for _, frag := range contactHrefFragments {
			if strings.Contains(hrefLower, frag) {
				score += 5
				break
			}
		}
		if score == 0 {
			continue
		}

		// Resolve relative to absolute
		abs, err := base.Parse(rawHref)
		if err != nil {
			continue
		}
		// Same-origin only
		if abs.Host != base.Host {
			continue
		}
		// Strip fragment for dedup
		abs.Fragment = ""
		key := abs.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		candidates = append(candidates, candidate{href: key, score: score})
	}

	// Sort by score desc, take top N
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	if len(candidates) > MaxContactPages {
		candidates = candidates[:MaxContactPages]
	}

	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.href)
	}
	return out
}

// FetchContactPage does a plain HTTP GET (no JS) of the given URL.
// Used as the first fallback after the main page scan returns no
// emails. 5s timeout, 2 MB body cap, returns the HTML body or an
// error. Caller is responsible for not hammering — we fetch at most
// MaxContactPages URLs per scan.
func FetchContactPage(ctx context.Context, pageURL, userAgent string) (string, error) {
	if pageURL == "" {
		return "", nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", pageURL, nil)
	if err != nil {
		return "", err
	}
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,id;q=0.8")

	client := &http.Client{
		Timeout: 5 * time.Second,
		// Don't follow redirects to private/loopback addresses
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &contactFetchError{URL: pageURL, Status: resp.StatusCode}
	}

	// Cap body at 2 MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

type contactFetchError struct {
	URL    string
	Status int
}

func (e *contactFetchError) Error() string {
	return "fetch " + e.URL + ": HTTP " + http.StatusText(e.Status)
}

// stripTags removes HTML tags from a string. Used to extract link
// text content for keyword matching. Naive but good enough for
// contact-link detection where the text is usually plain words.
func stripTags(s string) string {
	re := regexp.MustCompile(`<[^>]*>`)
	s = re.ReplaceAllString(s, " ")
	// Collapse whitespace
	s = strings.Join(strings.Fields(s), " ")
	return s
}
