# Tech Stack Detector - Fingerprint Format

## JSON Schema

Each technology is defined with detection rules across 4 pillars:

```json
{
  "slug": "wordpress",
  "name": "WordPress",
  "website": "https://wordpress.org",
  "category": "cms",
  "icon": "wordpress.svg",
  "detectors": {
    "html": [...],
    "headers": [...],
    "scripts": [...],
    "cookies": [...],
    "url": [...],
    "js_globals": [...],
    "css": [...],
    "meta": [...]
  },
  "implies": [...],
  "excludes": [...]
}
```

## Detector Types

### 1. HTML (string/regex match on raw HTML)
```json
"html": [
  {"pattern": "<meta name=\"generator\" content=\"WordPress\""},
  {"pattern": "/wp-content/", "match": "contains"},
  {"pattern": "/wp-includes/", "match": "contains"}
]
```

### 2. Headers (HTTP response headers)
```json
"headers": [
  {"header": "X-Powered-By", "pattern": "PHP/"},
  {"header": "Server", "pattern": "nginx"},
  {"header": "Set-Cookie", "pattern": "PHPSESSID"}
]
```

### 3. Scripts (script/link src URLs)
```json
"scripts": [
  {"pattern": "wp-content/themes/"},
  {"pattern": "wp-includes/js/"}
]
```

### 4. Cookies (Set-Cookie values)
```json
"cookies": [
  {"pattern": "wordpress_logged_in"},
  {"pattern": "wp-settings-"}
]
```

### 5. URL (URL structure patterns)
```json
"url": [
  {"pattern": "/wp-admin/"},
  {"pattern": "/wp-login.php"}
]
```

### 6. JS Globals (window object keys)
```json
"js_globals": [
  {"pattern": "wp"},
  {"pattern": "jQuery"}
]
```

### 7. Meta (HTML meta tags)
```json
"meta": [
  {"name": "generator", "pattern": "WordPress"},
  {"name": "generator", "pattern": "Ghost"}
]
```

### 8. CSS (stylesheet URLs or inline styles)
```json
"css": [
  {"pattern": "wp-content/themes/"},
  {"pattern": "elementor"}
]
```

## Match Types

- `contains` (default): substring match
- `regex`: regex pattern match
- `exact`: exact string match
- `exists`: just check if header/meta/cookie exists

## Confidence

Each detector can have optional `confidence` (default: 100):
```json
"html": [
  {"pattern": "WordPress", "confidence": 100},
  {"pattern": "/wp-content/", "confidence": 50}
]
```

If ANY detector matches, technology is detected. Confidence is summed from all matches.

## Implies & Excludes

- `implies`: if WordPress detected, implies PHP + MySQL
- `excludes`: if React detected, excludes jQuery (usually)

## Categories

- cms, framework, analytics, advertising, widgets, hosting, 
- javascript, css, fonts, servers, databases, caching, 
- security, analytics, marketing, social, video, audio, 
- ecommerce, crm, helpdesk, cdn, dns, payment
