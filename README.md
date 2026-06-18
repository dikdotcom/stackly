# Stackly

Tech stack detection platform — Wappalyzer alternative built in Go with its own fingerprint database.

> Built because Wappalyzer's dataset is AGPL and we wanted something we could deploy, extend, and ship to clients without licensing surprises.

## Features

- **Detection engine**: 87 technologies, 8 detection methods (HTML, headers, scripts, CSS, meta tags, JS globals, cookies, URL patterns)
- **Stripe + PayPal detection** including JS-bundled sites via `pk_live_*`, `pk_test_*`, `data-stripe-*` attribute sniffing
- **JS global `__proto__` walking** discovers vendor SDK methods like `Stripe.elements`, `gtag.apply` even when minified
- **User-Agent rotation** across 14 real browser UAs (Chrome/Firefox/Safari/Edge, Win/Mac/Linux) — bypasses naive bot detection
- **Persistent cache** with 24h TTL and disk persistence (`data/cache.json`)
- **Auth**: API keys, HTTP Basic, JWT (HS256) — per-user rate limits
- **Multi-user** with usage tracking (`data/users.json`)
- **OpenAPI 3.0 spec** at `/api/docs` + Swagger UI at `/api/docs/ui`
- **Chrome extension** in `extension/` for in-browser scans
- **Docker** multi-stage build using `chromedp/headless-shell`

## Quick Start

### Local

```bash
go build -o bin/stackly-server ./cmd/server
go build -o bin/stackly-cli ./cmd/techstack

./bin/stackly-server -port=8890
# Open http://localhost:8890
```

### Docker

```bash
docker compose up -d
# Open http://localhost:8890
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

data/
  fingerprints.json   87 technologies, 17 categories
  users.json          User store (auto-created from env)
  cache.json          Persistent scan cache

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
STACKLY_API_KEYS=***  ./bin/stackly-server
```

## License

MIT

## Credits

- Inspired by [Wappalyzer](https://www.wappalyzer.com/) but with our own (non-AGPL) fingerprint database
- Built with [chromedp](https://github.com/chromedp/chromedp) for browser automation
- JWT via [golang-jwt/jwt](https://github.com/golang-jwt/jwt)
- Runs on [chromedp/headless-shell](https://github.com/chromedp/docker-headless-shell) in Docker