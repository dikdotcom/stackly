package scanner

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/dikdotcom/stackly/internal/fingerprint"
)

// Result represents a detected technology
type Result struct {
	Technology fingerprint.Technology
	Confidence int
	Matches    []Match
}

// Match represents a single detection match
type Match struct {
	Type   string // html, header, script, cookie, url, js_global, css, meta
	Rule   string // the rule that matched
	Detail string // what was found
}

// ScanResult contains the full scan output
type ScanResult struct {
	URL          string
	HTML         string
	Headers      http.Header
	Scripts      []string
	CSSLinks     []string
	JSGlobals    []string
	MetaTags     map[string]string
	Cookies      []string
	Results      []Result
	Emails       []string
	DNS          *DNSInfo     `json:"dns,omitempty"`
	WHOIS        *WHOISInfo   `json:"whois,omitempty"`
	SSL          *SSLInfo     `json:"ssl,omitempty"`
	Perf         *PerfMetrics `json:"perf,omitempty"`
	ScanDuration time.Duration
	Error        error
}

// Scanner is the main detection engine
type Scanner struct {
	db        *fingerprint.Database
	userAgent string
	uaPool    *UserAgentPool
	timeout   time.Duration
	rotateUA  bool
	lastUA    string
	lastUAMu  sync.Mutex
	proxyURL  string

	// ProgressCallback is invoked with phase updates during ScanURL.
	// It runs on the scanner goroutine — implementations should not block.
	ProgressCallback func(jobID, phase string, progress int, message string, partial []Result)
}

// NewScanner creates a new scanner instance
func NewScanner(db *fingerprint.Database) *Scanner {
	return &Scanner{
		db:        db,
		userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36",
		uaPool:    NewUserAgentPool(),
		timeout:   30 * time.Second,
		rotateUA:  false,
	}
}

// SetUserAgent sets a fixed user agent string (disables rotation)
func (s *Scanner) SetUserAgent(ua string) {
	s.userAgent = ua
	s.rotateUA = false
}

// EnableUARotation enables rotating user agents per scan
func (s *Scanner) EnableUARotation() {
	s.rotateUA = true
}

// SetTimeout sets the page load timeout
func (s *Scanner) SetTimeout(d time.Duration) {
	s.timeout = d
}

// SetProxy sets a proxy URL for outgoing requests (http://host:port or socks5://host:port)
func (s *Scanner) SetProxy(url string) {
	s.proxyURL = url
}

// GetLastUserAgent returns the UA used for the most recent scan
func (s *Scanner) GetLastUserAgent() string {
	s.lastUAMu.Lock()
	defer s.lastUAMu.Unlock()
	return s.lastUA
}

// getUserAgent returns the appropriate UA based on rotation settings
func (s *Scanner) getUserAgent() string {
	var ua string
	if s.rotateUA {
		ua = s.uaPool.Random()
	} else {
		ua = s.userAgent
	}
	s.lastUAMu.Lock()
	s.lastUA = ua
	s.lastUAMu.Unlock()
	return ua
}

// ScanURL performs a full scan of the target URL.
// jobID is optional — pass "" if no live progress reporting is needed.
func (s *Scanner) ScanURL(ctx context.Context, jobID, url string) *ScanResult {
	start := time.Now()

	result := &ScanResult{
		URL: url,
	}

	// Create browser context
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserAgent(s.getUserAgent()),
		chromedp.DisableGPU,
		chromedp.NoSandbox,
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-web-security", true),
		chromedp.Flag("disable-features", "site-per-process"),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	// Set timeout
	browserCtx, cancel := context.WithTimeout(browserCtx, s.timeout)
	defer cancel()

	// Variables to collect data
	var html string
	var scripts []string
	var cssLinks []string
	var metaTags map[string]string
	var jsGlobals []string
	var cookies []string
	var headerMap map[string]interface{}

	// Listen for response headers via Network event
	chromedp.ListenTarget(browserCtx, func(ev interface{}) {
		switch ev := ev.(type) {
		case *network.EventResponseReceived:
			if ev.Type == "Document" {
				headerMap = make(map[string]interface{})
				for k, v := range ev.Response.Headers {
					if strVal, ok := v.(string); ok {
						headerMap[k] = strVal
					}
				}
			}
		}
	})

	// Navigate and collect page data
	s.emitProgress(jobID, "fetching", 20, "Opening page in headless browser", nil)
	err := chromedp.Run(browserCtx,
		// Navigate to URL
		chromedp.Navigate(url),

		// Wait for page to load
		chromedp.WaitReady("body"),

		// Get HTML content
		chromedp.OuterHTML("html", &html),

		// Get all script sources
		chromedp.Evaluate(`Array.from(document.querySelectorAll('script[src]')).map(s => s.src)`, &scripts),

		// Get all CSS links
		chromedp.Evaluate(`Array.from(document.querySelectorAll('link[rel="stylesheet"]')).map(l => l.href)`, &cssLinks),

		// Get meta tags
		chromedp.Evaluate(`
			(() => {
				const metas = {};
				document.querySelectorAll('meta').forEach(m => {
					const name = m.getAttribute('name') || m.getAttribute('property') || '';
					const content = m.getAttribute('content') || '';
					if (name) metas[name.toLowerCase()] = content;
				});
				return metas;
			})()
		`, &metaTags),

		// JS global detection via __proto__ walking + interesting-key filter
		chromedp.Evaluate(`
			(() => {
				const globals = new Set();

				// Regex of "interesting" globals — vendor SDKs, frameworks, analytics
				const interesting = /^(jQuery|\\$|React|Vue|angular|Drupal|wp|Shopify|__NEXT|__NUXT|nuxt|\\$nuxt|_gaq|gtag|dataLayer|fbq|fbevents|Intercom|CRISP|\\$crisp|Stripe|StripeCheckout|grecaptcha|__REACT|webpack|__webpack|hj|hotjar|Amplitude|mixpanel|Sentry|datadog|gtm|OneTrust|optimizely|HubSpot|hubspot|_hsq|klaviyo|mailchimp|segment|Heap|mParticle|pendo|adobe|Typekit|AdobeFonts|trustarc|recaptcha|hcaptcha|axios|Popper|Tippy|gsap|three|twttr|twitter|linkedin|pinterest|tiktok|reddit|clarity|quants|scorecard|crazyegg|rollbar|bugsnag|newrelic|raygun|appcues|fullstory|swup|lozad)/i;

				try {
					// Walk own property names of window
					const keys = Object.getOwnPropertyNames(window);
					for (let i = 0; i < keys.length && i < 1500; i++) {
						if (interesting.test(keys[i])) {
							globals.add(keys[i]);
						}
					}
				} catch(e) {}

				// Walk __proto__ of known vendor namespaces for method discovery
				const namespaces = ['Stripe', 'jQuery', 'React', 'Vue', 'angular', 'Shopify',
					'dataLayer', 'gtag', 'fbq', 'mixpanel', 'amplitude', 'Sentry',
					'OneTrust', 'HubSpot', 'klaviyo', 'mailchimp', 'segment', 'pendo',
					'Heap', 'mParticle', 'AdobeFonts', 'Typekit', 'twttr'];
				for (const ns of namespaces) {
					try {
						const obj = window[ns];
						if (obj && (typeof obj === 'object' || typeof obj === 'function')) {
							const proto = Object.getPrototypeOf(obj);
							if (proto) {
								const methods = Object.getOwnPropertyNames(proto).slice(0, 8);
								for (const m of methods) {
									if (m !== 'constructor' && m !== '__proto__' && obj[m] !== undefined) {
										globals.add(ns + '.' + m);
									}
								}
							}
						}
					} catch(e) {}
				}

				return Array.from(globals);
			})()
		`, &jsGlobals),

		// Get cookies
		chromedp.Evaluate(`document.cookie.split(';').map(c => c.trim())`, &cookies),
	)

	if err != nil {
		result.Error = err
		result.ScanDuration = time.Since(start)
		return result
	}

	// Parse headers
	headers := make(http.Header)
	if headerMap != nil {
		for k, v := range headerMap {
			if strVal, ok := v.(string); ok {
				headers.Set(k, strVal)
			}
		}
	}

	result.HTML = html
	result.Headers = headers
	result.Scripts = scripts
	result.CSSLinks = cssLinks
	result.MetaTags = metaTags
	result.JSGlobals = jsGlobals
	result.Cookies = cookies
	result.ScanDuration = time.Since(start)

	s.emitProgress(jobID, "parsing", 60, "Parsing HTML, scripts, and JS globals", nil)

	// Run detection
	result.Results = s.detect(result)

	s.emitProgress(jobID, "detecting", 90, fmt.Sprintf("Matched %d technologies", len(result.Results)), result.Results)

	// Extract contact emails from HTML (mailto: links + plain text patterns
	// + JSON-LD + data-* attrs + SSR hydration data). If nothing turns up
	// on the main page, fall back to a contact-page waterfall:
	//   1. Find candidate contact links in the main HTML
	//   2. Parallel: HTTP fetch (no JS) + chromedp deep navigation
	// The waterfall is bounded by MaxContactPages URLs and adds 0-5s
	// to scan time when no emails are found on the main page.
	result.Emails = ExtractEmails(result.HTML)
	if len(result.Emails) > 0 {
		s.emitProgress(jobID, "detecting", 95, fmt.Sprintf("Found %d email(s)", len(result.Emails)), nil)
	} else {
		s.crawlForEmails(ctx, browserCtx, jobID, result)
	}

	// Enrich with DNS + WHOIS + SSL + perf metrics. Run in parallel
	// (each has its own 5-8s timeout) and wait — this adds ~1s p99 to
	// wall-clock time but never fails the scan (errors are captured per-field).
	s.enrichResult(ctx, result)

	return result
}

// crawlForEmails is the contact-page fallback. It looks for /contact,
// /kontak, etc. links in the main HTML, then tries to extract emails
// from those pages using two strategies in parallel:
//
//   - HTTP fetch (plain net/http, 5s timeout, no JS execution)
//   - chromedp deep nav (reuses existing browserCtx, renders JS, slower)
//
// We run both in parallel and merge into result.Emails, deduped. If
// neither strategy finds anything, we surface nothing extra — the scan
// still completes normally and result.Emails is whatever ExtractEmails
// pulled from the main HTML.
//
// Bounded: at most MaxContactPages URLs tried, parallel HTTP+chrome
// per URL. 8s hard ceiling so a slow site can't stall the scan.
func (s *Scanner) crawlForEmails(ctx, browserCtx context.Context, jobID string, result *ScanResult) {
	candidates := FindContactPages(result.HTML, result.URL)
	if len(candidates) == 0 {
		return
	}
	s.emitProgress(jobID, "detecting", 95, "Searching contact pages for emails", nil)

	// Bounded timeout: 8s total for the whole waterfall.
	waterfallCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	// Try candidates in order. Stop as soon as we find at least one
	// email — no need to keep crawling if we're already populating
	// the result. We still run HTTP and chromedp in parallel for the
	// same URL so the slower one doesn't gate the faster one.
	for _, pageURL := range candidates {
		if len(result.Emails) > 0 {
			return
		}

		var wg sync.WaitGroup
		wg.Add(2)

		// HTTP fetch path
		go func(url string) {
			defer wg.Done()
			body, err := FetchContactPage(waterfallCtx, url, s.getUserAgent())
			if err != nil || body == "" {
				return
			}
			for _, e := range ExtractEmails(body) {
				mergeUnique(&result.Emails, e)
			}
		}(pageURL)

		// Chromedp deep nav path — reuse the existing browser context.
		// The browser is still alive (defer cancel runs at function end).
		go func(url string) {
			defer wg.Done()
			navCtx, navCancel := context.WithTimeout(browserCtx, 6*time.Second)
			defer navCancel()
			var newHTML string
			err := chromedp.Run(navCtx,
				chromedp.Navigate(url),
				chromedp.WaitReady("body"),
				chromedp.OuterHTML("html", &newHTML),
			)
			if err != nil || newHTML == "" {
				return
			}
			for _, e := range ExtractEmails(newHTML) {
				mergeUnique(&result.Emails, e)
			}
		}(pageURL)

		wg.Wait()
	}

	if len(result.Emails) > 0 {
		s.emitProgress(jobID, "detecting", 96, fmt.Sprintf("Found %d email(s) via contact pages", len(result.Emails)), nil)
	}
}

// mergeUnique appends e to *dst if it's not already present and not
// a noise/empty value. Keeps the slice small (cap 20 to match
// ExtractEmails' cap).
func mergeUnique(dst *[]string, e string) {
	if e == "" {
		return
	}
	for _, existing := range *dst {
		if existing == e {
			return
		}
	}
	if len(*dst) >= 20 {
		return
	}
	*dst = append(*dst, e)
}

// enrichResult fills DNS, WHOIS, SSL, Perf fields in-place. Each enrichment
// has its own timeout and never propagates errors — failures are stored as
// Error fields inside the sub-struct so the scan still completes.
func (s *Scanner) enrichResult(_ context.Context, result *ScanResult) {
	host := extractHost(result.URL)
	if host == "" {
		return
	}
	// Use a fresh background context — the parent ctx is about to be
	// cancelled by the caller's defer, which would kill our lookups.
	enrichCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		domain := rootDomain(host)
		if domain == "" {
			return
		}
		result.DNS = QueryDNS(enrichCtx, domain)
		result.WHOIS = QueryWHOIS(enrichCtx, domain)
	}()

	go func() {
		defer wg.Done()
		result.SSL = QuerySSL(enrichCtx, host)
	}()

	wg.Wait()

	// Perf metrics — derived from already-collected scan data
	result.Perf = computePerfMetrics(
		result.HTML,
		result.Scripts,
		result.CSSLinks,
		int64(len(result.HTML)),
		result.ScanDuration, // TTFB ≈ total scan duration in our setup
		result.ScanDuration,
		result.MetaTags,
	)
}

// extractHost returns hostname (without port) from a URL string.
func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// rootDomain returns the registrable domain (eTLD+1) for a host. Falls
// back to the host itself if suffix parsing fails. We don't embed the
// public suffix list — a simple "last 2 labels" heuristic handles
// the common case and any miss just queries the wrong level of RDAP.
func rootDomain(host string) string {
	host = strings.TrimSuffix(host, ".")
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return host
	}
	// Take last 2 labels (covers .com/.io/.co/etc.). Multi-segment TLDs
	// like .co.uk would over-trim, but rdap.org will just return NXDOMAIN.
	return strings.Join(parts[len(parts)-2:], ".")
}

// emitProgress invokes the progress callback if set. Safe to call with nil callback.
func (s *Scanner) emitProgress(jobID, phase string, progress int, message string, partial []Result) {
	if s.ProgressCallback == nil {
		return
	}
	s.ProgressCallback(jobID, phase, progress, message, partial)
}

// detect runs all detection methods against collected data
func (s *Scanner) detect(data *ScanResult) []Result {
	var results []Result

	for _, tech := range s.db.Technologies {
		confidence := 0
		var matches []Match

		// HTML detection
		for _, rule := range tech.Detectors.HTML {
			if s.matchPattern(data.HTML, rule.Pattern, rule.Match) {
				conf := rule.Confidence
				if conf == 0 {
					conf = 100
				}
				confidence += conf
				matches = append(matches, Match{Type: "html", Rule: rule.Pattern, Detail: "HTML match"})
			}
		}

		// Headers detection
		for _, rule := range tech.Detectors.Headers {
			headerVal := data.Headers.Get(rule.Header)
			if headerVal != "" {
				if rule.Pattern == "" || s.matchPattern(headerVal, rule.Pattern, rule.Match) {
					conf := rule.Confidence
					if conf == 0 {
						conf = 100
					}
					confidence += conf
					matches = append(matches, Match{Type: "header", Rule: rule.Header + ": " + rule.Pattern, Detail: headerVal})
				}
			}
		}

		// Scripts detection
		for _, rule := range tech.Detectors.Scripts {
			for _, script := range data.Scripts {
				if s.matchPattern(script, rule.Pattern, rule.Match) {
					conf := rule.Confidence
					if conf == 0 {
						conf = 100
					}
					confidence += conf
					matches = append(matches, Match{Type: "script", Rule: rule.Pattern, Detail: script})
					break
				}
			}
		}

		// CSS detection
		for _, rule := range tech.Detectors.CSS {
			for _, css := range data.CSSLinks {
				if s.matchPattern(css, rule.Pattern, rule.Match) {
					conf := rule.Confidence
					if conf == 0 {
						conf = 100
					}
					confidence += conf
					matches = append(matches, Match{Type: "css", Rule: rule.Pattern, Detail: css})
					break
				}
			}
		}

		// URL detection
		for _, rule := range tech.Detectors.URL {
			if s.matchPattern(data.URL, rule.Pattern, rule.Match) {
				conf := rule.Confidence
				if conf == 0 {
					conf = 100
				}
				confidence += conf
				matches = append(matches, Match{Type: "url", Rule: rule.Pattern, Detail: "URL match"})
			}
		}

		// Meta tag detection
		for _, rule := range tech.Detectors.Meta {
			name := strings.ToLower(rule.Name)
			if val, ok := data.MetaTags[name]; ok {
				if s.matchPattern(val, rule.Pattern, rule.Match) {
					conf := rule.Confidence
					if conf == 0 {
						conf = 100
					}
					confidence += conf
					matches = append(matches, Match{Type: "meta", Rule: rule.Name + "=" + rule.Pattern, Detail: val})
				}
			}
		}

		// JS globals detection
		for _, rule := range tech.Detectors.JSGlobals {
			for _, global := range data.JSGlobals {
				if s.matchPattern(global, rule.Pattern, rule.Match) {
					conf := rule.Confidence
					if conf == 0 {
						conf = 100
					}
					confidence += conf
					matches = append(matches, Match{Type: "js_global", Rule: rule.Pattern, Detail: global})
					break
				}
			}
		}

		// Cookies detection
		for _, rule := range tech.Detectors.Cookies {
			for _, cookie := range data.Cookies {
				if s.matchPattern(cookie, rule.Pattern, rule.Match) {
					conf := rule.Confidence
					if conf == 0 {
						conf = 100
					}
					confidence += conf
					matches = append(matches, Match{Type: "cookie", Rule: rule.Pattern, Detail: cookie})
					break
				}
			}
		}

		// Add to results if any matches
		if confidence > 0 {
			results = append(results, Result{
				Technology: tech,
				Confidence: confidence,
				Matches:    matches,
			})
		}
	}

	return results
}

// matchPattern checks if a string matches a pattern
func (s *Scanner) matchPattern(input, pattern, matchType string) bool {
	if pattern == "" {
		return input != ""
	}

	switch matchType {
	case "exact":
		return input == pattern
	case "regex":
		matched, _ := regexp.MatchString(pattern, input)
		return matched
	case "exists":
		return input != ""
	default: // contains
		return strings.Contains(input, pattern)
	}
}

// GetImplied returns implied technologies based on detections
func GetImplied(results []Result) []string {
	detected := make(map[string]bool)
	for _, r := range results {
		detected[r.Technology.Slug] = true
	}

	var implied []string
	for _, r := range results {
		for _, imp := range r.Technology.Implies {
			if !detected[imp] {
				detected[imp] = true
				implied = append(implied, imp)
			}
		}
	}
	return implied
}

// FormatResults returns a human-readable string of results
func FormatResults(results []Result) string {
	if len(results) == 0 {
		return "No technologies detected."
	}

	var sb strings.Builder
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("%-20s %s\n", r.Technology.Name, r.Technology.Category))
		for _, m := range r.Matches {
			sb.WriteString(fmt.Sprintf("  [%s] %s\n", m.Type, m.Rule))
		}
	}
	return sb.String()
}
