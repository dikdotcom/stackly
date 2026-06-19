# Stackly

[![CI](https://github.com/dikdotcom/stackly/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/dikdotcom/stackly/actions/workflows/ci.yml)
[![GitHub Pages](https://github.com/dikdotcom/stackly/actions/workflows/pages.yml/badge.svg)](https://github.com/dikdotcom/stackly/actions/workflows/pages.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-22C55E.svg)](https://github.com/dikdotcom/stackly/blob/main/LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.22%20%7C%201.23-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Private](https://img.shields.io/badge/repo-private-lightgrey)](https://github.com/dikdotcom/stackly)

Multi-tenant tech stack detection platform built in Go — headless browser scanning with its own fingerprint database, top-level category grouping, and contact email extraction.

> Built for production deployments where fingerprint detection needs to be auditable, extensible, and license-clean. The dataset is hand-written and shipped as plain JSON that ops can review.

## Features

- **Detection engine**: 133 technologies, 23 categories, 8 detection methods (HTML, headers, scripts, CSS, meta tags, JS globals, cookies, URL patterns)
- **Top-level category grouping**: 18 groups (advertising, tag-managers, cookie-compliance, performance, seo, maps, etc.) with priority ordering
- **Contact email extraction**: mailto links, plaintext patterns, JSON-LD (`Organization.email`, `ContactPoint.email`), `data-*` attributes, and SSR hydration data (`__NEXT_DATA__`, `__NUXT__`)
- **Contact page waterfall**: when the main page has no email, falls back to fetching `/contact`, `/kontak`, `/redaksi`, `/hubungi` via parallel HTTP + chromedp deep nav
- **Stripe + PayPal detection** including JS-bundled sites via `pk_live_*`, `pk_test_*`, `data-stripe-*` attribute sniffing
- **JS global `__proto__` walking** discovers vendor SDK methods like `Stripe.elements`, `gtag.apply` even when minified
- **User-Agent rotation** across 14 real browser UAs (Chrome/Firefox/Safari/Edge, Win/Mac/Linux) — bypasses naive bot detection
- **Persistent cache** with 24h TTL and disk persistence (`data/cache.json`)
- **Auth**: API keys, HTTP Basic, JWT (HS256) — per-user rate limits
- **Multi-user** with usage tracking (`data/users.json`)
- **OpenAPI 3.0 spec** at `/api/docs` + Swagger UI at `/api/docs/ui`
- **Chrome extension** in `extension/` for in-browser scans
- **Docker** multi-stage build using `chromedp/headless-shell`

## Tutorial

Step-by-step guide for setting up Stackly from a fresh clone. Assumes basic familiarity with Go and the command line.

### 1. Prerequisites

| Tool | Version | Why |
|---|---|---|
| **Go** | 1.22 or 1.23 | Compiles the server + CLI |
| **Chrome / Chromium** | 90+ | Needed by chromedp for page rendering |
| **Git** | any | Clone the repo |

The scanner auto-detects Chrome. Set `CHROME_BIN` env var to override. On Linux without a system Chrome:

```bash
# Debian/Ubuntu
sudo apt install -y chromium-browser

# Or use the bundled headless-shell (recommended for servers/Docker)
# Dockerfile uses chromedp/headless-shell — works without a desktop browser
```

### 2. Clone & Build

```bash
git clone https://github.com/dikdotcom/stackly.git
cd stackly

# Build the server
go build -o bin/stackly-server ./cmd/server

# Build the CLI
go build -o bin/stackly-cli ./cmd/techstack

# Verify
./bin/stackly-server --help
```

You should see `🚀 Stackly — Tech Stack Detector` and a list of flags.

### 3. Configure Authentication (optional)

Without auth, the API is open. For local development that's fine. For anything exposed to a network, set auth.

**Option A: HTTP Basic auth** (simplest for personal use)

```bash
export STACKLY_BASIC_USERS="andika:your-password"
./bin/stackly-server -port=8890
```

**Option B: API keys with tiers**

```bash
export STACKLY_API_KEYS=*** k_admin_zZz:admin"
./bin/stackly-server -port=8890
```

**Option C: JWT**

```bash
export STACKLY_JWT_SECRET=*** y-stackly-server -port=8890
```

You can combine all three — they're checked in order: API key → Basic → JWT.

### 4. Run Your First Scan

```bash
# Start the server
./bin/stackly-server -port=8890

# In another terminal, check health
curl http://localhost:8890/api/health
# {"status":"ok",...}

# Submit a scan (no auth if STACKLY_BASIC_USERS not set)
curl -X POST http://localhost:8890/api/scan \
  -H "Content-Type: application/json" \
  -d '{"url":"https://react.dev"}'

# Returns: {"id":"abc123...","status":"pending","url":"https://react.dev"}

# Get results
curl http://localhost:8890/api/results/abc123
```

With auth, add the header:

```bash
curl -u "andika:your-password" http://localhost:8890/api/scan ...
# OR
curl -H "X-API-Key: ***" http://localhost:8890/api/scan ...
```

### 5. Open the Web UI

Navigate to `http://localhost:8890` in your browser. You should see:

- The Stackly header with quota widget
- A URL input to submit scans
- Category groups (advertising, analytics, frameworks, etc.)
- Result cards with detected technologies, confidence badges, and detail

If auth is enabled, the UI auto-pops a sign-in modal on first scan attempt.

### 6. Build the Chrome Extension Zip (optional)

The web UI offers a "Download Chrome Extension" button that serves `web/extension/stackly-chrome-extension.zip`. Since this is a build artifact (regenerated from `extension/`), it's not in the repo. Build it:

```bash
cd extension
zip -r ../web/extension/stackly-chrome-extension.zip .
cd ..

# Now the download works at /extension/stackly-chrome-extension.zip
```

Or load the extension unpacked directly:

1. Open `chrome://extensions/` in Chrome
2. Enable **Developer mode** (top right)
3. Click **Load unpacked**
4. Select the `extension/` directory

### 7. Run Tests

```bash
# All tests
go test ./...

# With race detection
go test -race ./...

# Specific package
go test ./internal/scanner/

# Verbose
go test -v ./internal/scanner/ -run TestExtractEmails
```

Tests run offline except for `TestQueryDNS_Live` and `TestQuerySSL_Live` which hit `example.com`. Skip those with `-short`:

```bash
go test -short ./...
```

### 8. Add Custom Fingerprints

The fingerprint DB lives at `data/fingerprints.json`. Each technology entry has this shape:

```json
{
  "slug": "my-cms",
  "name": "My CMS",
  "website": "https://mycms.com",
  "category": "cms",
  "icon": "my-cms.svg",
  "detectors": {
    "html":       [{"pattern": "powered by my-cms"}],
    "headers":    [{"name": "X-Powered-By", "pattern": "MyCMS"}],
    "scripts":    [{"pattern": "/my-cms/"}],
    "meta":       [{"name": "generator", "pattern": "MyCMS"}],
    "js_globals": ["MyCMS"],
    "cookies":    ["mycms_session"],
    "css":        [{"pattern": ".my-cms-class"}],
    "url":        [{"pattern": "/wp-content/plugins/my-cms/"}]
  }
}
```

8 detection vectors, each with a pattern. A technology is detected when ANY pattern matches. Confidence is the count of distinct vectors that matched (max ~200).

After editing, restart the server — the DB loads at startup from `data/fingerprints.json`.

For bulk additions, use `scripts/expand-fingerprints.py` as a template — it processes a list of new fingerprints and merges them into the DB.

### 9. Production Deployment

The simplest path is Docker:

```bash
docker compose up -d
```

This uses the multi-stage Dockerfile which:
- Compiles Go with CGO disabled
- Bundles `chromedp/headless-shell` (~120MB)
- Exposes port 8890
- Mounts `stackly-data` volume for `data/` persistence

For custom VPS deploys (Hetzner, Tencent, DigitalOcean):

```bash
# On the VPS
git clone https://github.com/dikdotcom/stackly.git /opt/stackly
cd /opt/stackly
go build -o bin/stackly-server ./cmd/server

# Run with systemd
sudo tee /etc/systemd/system/stackly.service <<EOF
[Unit]
Description=Stackly
After=network.target

[Service]
Type=simple
User=stackly
WorkingDirectory=/opt/stackly
Environment=STACKLY_BASIC_USERS=youruser:yourpass
ExecStart=/opt/stackly/bin/stackly-server -port=8890
Restart=always

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl enable --now stackly
```

Remember to open port 8890 in your cloud provider's Security Group / firewall.

### 10. Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `failed to find chrome binary` | No Chrome installed | Install chromium or set `CHROME_BIN` |
| `auth required` on every scan | Auth enabled but no env vars set | Set `STACKLY_NO_AUTH=*** for open mode (dev only) |
| `chromedp: unhandled event EventAdoptedStyleSheetsModified` | Non-fatal warning | Safe to ignore — known chromedp limitation |
| Scan timeout | Slow target site | Increase with `-timeout=60s` CLI flag |
| 0 emails detected | Site uses contact forms, not raw emails | Expected behavior — see [Contact email extraction](#features) |

## Quick Start

For the impatient:

```bash
go build -o bin/stackly-server ./cmd/server
./bin/stackly-server -port=8890
# Open http://localhost:8890
```

Or with Docker:

```bash
docker compose up -d
```

### API

```bash
# Health (no auth)
curl http://localhost:8890/api/health

# Scan (with API key)
curl -X POST http://localhost:8890/api/scan \
  -H "X-API-Key: *** \
  -H "Content-Type: application/json" \
  -d '{"url":"https://react.dev"}'

# Get result
curl http://localhost:8890/api/results/<id>

# API docs
open http://localhost:8890/api/docs/ui
```

## Configuration

All config via env vars (or CLI flags). See `.env.example`.

| Var | Default | Description |
|---|---|---|
| `PORT` | 8890 | HTTP port |
| `STACKLY_API_KEYS` | — | Comma-separated `key[:tier]` pairs. Tiers: free (60/min), pro (600/min), admin (10000/min) |
| `STACKLY_BASIC_USERS` | — | Comma-separated `user:pass` for HTTP Basic |
| `STACKLY_JWT_SECRET` | — | HS256 shared secret — JWTs with this signature are accepted |
| `STACKLY_RATE_LIMIT` | 60 | Per-IP fallback rate limit (per minute) |
| `CACHE_TTL` | 24h | Cache duration |
| `CACHE_PATH` | `data/cache.json` | Cache persistence path |
| `UA_ROTATE` | true | Rotate User-Agent per scan |
| `PROXY_URL` | — | HTTP or SOCKS5 proxy |

## Auth Tiers

| Tier | Rate limit | Notes |
|---|---|---|
| free | 60/min | Default for plain keys |
| basic | 120/min | HTTP Basic users |
| pro | 600/min | Higher limits |
| admin | 10000/min | Can list all users (`/api/auth/users`) |

Format: `STACKLY_API_KEYS=sk_free_xxx,sk_pro_yyy:pro,sk_admin_zzz:admin`

## Endpoints

| Method | Path | Auth | Purpose |
|---|---|---|---|
| GET | `/api/health` | — | Health check |
| POST | `/api/scan` | ✓ | Submit URL (sync via `?wait=true`) |
| GET | `/api/results/:id` | ✓ | Get scan results (`?compact=1` for flat) |
| GET | `/api/jobs` | ✓ | List recent jobs |
| GET | `/api/stats` | ✓ | Queue + cache stats |
| GET | `/api/cache/stats` | ✓ | Cache stats |
| POST | `/api/cache/clear` | ✓ | Clear cache |
| GET | `/api/auth/stats` | ✓ | Auth + rate-limit stats |
| GET | `/api/auth/usage` | ✓ | Your usage |
| GET | `/api/auth/users` | admin | List all users |
| GET | `/api/docs` | — | OpenAPI YAML |
| GET | `/api/docs/ui` | — | Swagger UI |

## Chrome Extension

```
extension/
├── manifest.json
├── popup.html
├── popup.js
├── background.js
└── icons/
```

Install:
1. `chrome://extensions/` → Developer mode
2. Load unpacked → select `extension/`
3. Click extension icon → set API URL + key

To enable the in-UI download button, build the zip artifact first — see [Tutorial §6](#6-build-the-chrome-extension-zip-optional).

## Architecture

```
cmd/
  server/      HTTP API server
  techstack/   CLI tool (./stackly-cli -url=... -json)

internal/
  api/         HTTP routes + middleware
  auth/        Multi-user auth (API key + Basic + JWT)
  cache/       In-memory + disk cache
  fingerprint/ JSON DB loader
  queue/       Job queue + worker pool
  scanner/     Detection engine + UA pool
  scheduler/   Scheduled scans + webhook dispatch
  ws/          WebSocket job progress
  report/      HTML + PDF report generation
  metrics/     Per-tier counters

data/
  fingerprints.json   133 technologies, 23 categories (v1.2.0)
  users.json          User store (auto-created from env, gitignored)
  cache.json          Persistent scan cache (gitignored)

web/
  index.html     SPA shell
  static/app.js  Frontend logic

extension/       Chrome MV3 extension
docs/openapi.yaml OpenAPI 3.0 spec
```

## Development

```bash
# Run tests
go test ./...

# Lint
gofmt -l .
go vet ./...

# Build Docker
docker build -t stackly:test .

# Run locally with auth
STACKLY_API_KEYS=*** /bin/stackly-server

# Add a fingerprint
$EDITOR data/fingerprints.json  # add new entry, restart server

# Regenerate Chrome extension zip
cd extension && zip -r ../web/extension/stackly-chrome-extension.zip . && cd ..
```

## Project Links

- 🌐 **Landing page**: <https://dikdotcom.github.io/stackly/>
- 📦 **Repository**: <https://github.com/dikdotcom/stackly>
- 📖 **API docs**: [`docs/openapi.yaml`](docs/openapi.yaml)
- 🐛 **Issues**: <https://github.com/dikdotcom/stackly/issues>

## License

MIT

## Credits

- Built with [chromedp](https://github.com/chromedp/chromedp) for browser automation
- JWT via [golang-jwt/jwt](https://github.com/golang-jwt/jwt)
- Runs on [chromedp/headless-shell](https://github.com/chromedp/docker-headless-shell) in Docker
- Icons via [devicon](https://devicon.dev/)