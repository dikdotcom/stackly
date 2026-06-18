#!/usr/bin/env python3
"""
Expand fingerprints.json with Wappalyzer-style categories:
- tag-managers (move GTM, Segment from analytics; add Tealium, Adobe Launch)
- cookie-compliance (OneTrust, Cookiebot, CookieYes, Iubenda, TrustArc)
- performance (Boomerang, NitroPack, Speed Curve, New Relic Browser)
- advertising (consolidate marketing + retargeting)
- seo (Yoast, etc.)
- miscellaneous (consolidate api, payment, security catch-alls)
"""

import json
import pathlib

DATA = pathlib.Path(__file__).parent.parent / "data" / "fingerprints.json"

# Techs to relocate (name -> new category)
RELOCATE = {
    "Google Tag Manager": "tag-managers",
    "Segment": "tag-managers",
    "Yoast SEO": "seo",
}

# Category merge: marketing + retargeting → advertising
MERGE_FROM_TO = {
    "marketing": "advertising",
    "retargeting": "advertising",
}

# New fingerprints to add
NEW_TECHS = [
    # Tag managers
    {
        "slug": "tealium", "name": "Tealium", "website": "https://tealium.com",
        "category": "tag-managers", "icon": "tealium.svg",
        "detectors": {
            "scripts": [{"pattern": "tags.tiqcdn.com"}, {"pattern": "tealiumiq.com"}],
        },
    },
    {
        "slug": "adobe-launch", "name": "Adobe Experience Platform Launch", "website": "https://business.adobe.com/products/experience-platform/launch.html",
        "category": "tag-managers", "icon": "adobe.svg",
        "detectors": {
            "scripts": [{"pattern": "assets.adobedtm.com"}, {"pattern": "launch-"}],
            "html": [{"pattern": "assets.adobedtm.com"}],
        },
    },
    # Cookie compliance
    {
        "slug": "onetrust", "name": "OneTrust", "website": "https://onetrust.com",
        "category": "cookie-compliance", "icon": "onetrust.svg",
        "detectors": {
            "scripts": [{"pattern": "cdn.cookielaw.org"}, {"pattern": "optanon.blob.core"}],
            "html": [{"pattern": "cookielaw.org"}, {"pattern": "optanon.blob.core"}],
            "cookies": [{"pattern": "OptanonConsent"}, {"pattern": "OptanonAlertBoxClosed"}],
        },
    },
    {
        "slug": "cookiebot", "name": "Cookiebot", "website": "https://cookiebot.com",
        "category": "cookie-compliance", "icon": "cookiebot.svg",
        "detectors": {
            "scripts": [{"pattern": "consent.cookiebot.com"}, {"pattern": "cookiebot.com/uc.js"}],
            "html": [{"pattern": "CookieConsent"}],
            "cookies": [{"pattern": "CookieConsent"}],
        },
    },
    {
        "slug": "cookieyes", "name": "CookieYes", "website": "https://cookieyes.com",
        "category": "cookie-compliance", "icon": "cookieyes.svg",
        "detectors": {
            "scripts": [{"pattern": "cdn-cookieyes.com"}, {"pattern": "cookieyes.com"}],
            "html": [{"pattern": "cookie-law-info-bar"}, {"pattern": "cookieyes"}],
            "cookies": [{"pattern": "cookieyes-consent"}, {"pattern": "cky-consent"}],
        },
    },
    {
        "slug": "iubenda", "name": "Iubenda", "website": "https://iubenda.com",
        "category": "cookie-compliance", "icon": "iubenda.svg",
        "detectors": {
            "scripts": [{"pattern": "cdn.iubenda.com"}, {"pattern": "iubenda.com/cs.js"}],
            "html": [{"pattern": "iubenda.com"}],
        },
    },
    {
        "slug": "trustarc", "name": "TrustArc", "website": "https://trustarc.com",
        "category": "cookie-compliance", "icon": "trustarc.svg",
        "detectors": {
            "scripts": [{"pattern": "consent.trustarc.com"}, {"pattern": "trustarc.com"}],
            "html": [{"pattern": "trustarc.com"}],
        },
    },
    # Performance / RUM
    {
        "slug": "boomerang", "name": "Boomerang", "website": "https://akamai.com/boomerang",
        "category": "performance", "icon": "boomerang.svg",
        "detectors": {
            "scripts": [{"pattern": "akamaihd.net/boomerang"}, {"pattern": "/boomerang/"}],
            "js_globals": [{"pattern": "BOOMR"}, {"pattern": "BOOMR_mkt"}],
        },
    },
    {
        "slug": "nitropack", "name": "NitroPack", "website": "https://nitropack.io",
        "category": "performance", "icon": "nitropack.svg",
        "detectors": {
            "scripts": [{"pattern": "nitropack.io"}, {"pattern": "nitropack.com"}],
            "html": [{"pattern": "nitropack"}],
            "headers": [{"header": "x-nitro-cache", "pattern": ""}],
        },
    },
    {
        "slug": "speedcurve", "name": "SpeedCurve", "website": "https://speedcurve.com",
        "category": "performance", "icon": "speedcurve.svg",
        "detectors": {
            "scripts": [{"pattern": "speedcurve.com"}, {"pattern": "lux.speedcurve.com"}],
            "html": [{"pattern": "speedcurve"}],
        },
    },
    {
        "slug": "new-relic-browser", "name": "New Relic Browser", "website": "https://newrelic.com/platform/browser-monitoring",
        "category": "performance", "icon": "newrelic.svg",
        "detectors": {
            "scripts": [{"pattern": "js-agent.newrelic.com"}, {"pattern": "nr-data.net"}],
            "js_globals": [{"pattern": "NREUM"}, {"pattern": "newrelic"}],
        },
    },
    # SEO
    {
        "slug": "ahrefs", "name": "Ahrefs", "website": "https://ahrefs.com",
        "category": "seo", "icon": "ahrefs.svg",
        "detectors": {
            "html": [{"pattern": "ahrefs.com/siteaudit"}, {"pattern": "ahrefs-site-verification"}],
            "headers": [{"header": "x-ahrefs-site-verification", "pattern": ""}],
        },
    },
    {
        "slug": "semrush", "name": "Semrush", "website": "https://semrush.com",
        "category": "seo", "icon": "semrush.svg",
        "detectors": {
            "html": [{"pattern": "semrush.com/sa/"}, {"pattern": "semrush-verification"}],
            "headers": [{"header": "x-semrush-verification", "pattern": ""}],
        },
    },
    # Miscellaneous additions (misc catch-all)
    {
        "slug": "google-maps", "name": "Google Maps", "website": "https://developers.google.com/maps",
        "category": "maps", "icon": "googlemaps.svg",
        "detectors": {
            "scripts": [{"pattern": "maps.googleapis.com"}, {"pattern": "maps.google.com/maps/api"}],
            "html": [{"pattern": "google.com/maps/embed"}, {"pattern": "google.com/maps"}],
        },
    },
]


def main():
    data = json.loads(DATA.read_text())
    techs = data["technologies"]

    existing_names = {t["name"] for t in techs}

    # 1. Apply relocations
    relocated = 0
    for t in techs:
        if t["name"] in RELOCATE:
            t["category"] = RELOCATE[t["name"]]
            relocated += 1

    # 2. Apply category merges (marketing + retargeting → advertising)
    merged = 0
    for t in techs:
        if t["category"] in MERGE_FROM_TO:
            t["category"] = MERGE_FROM_TO[t["category"]]
            merged += 1

    # 3. Add new techs (skip if already exists)
    added = 0
    for nt in NEW_TECHS:
        if nt["name"] in existing_names:
            continue
        techs.append(nt)
        existing_names.add(nt["name"])
        added += 1

    # 4. Bump version + update meta
    data["version"] = "1.2.0"
    if "_meta" not in data:
        data["_meta"] = {}
    data["_meta"]["wappalyzer_style_categories"] = sorted({t["category"] for t in techs})

    DATA.write_text(json.dumps(data, indent=2) + "\n")

    # Summary
    print(f"Relocated: {relocated} techs")
    print(f"Merged: {merged} techs into 'advertising'")
    print(f"Added: {added} new techs")
    print(f"Total: {len(techs)} techs")
    print()
    from collections import Counter
    counts = Counter(t["category"] for t in techs)
    print("Category breakdown:")
    for cat, n in sorted(counts.items(), key=lambda x: -x[1]):
        print(f"  {cat:24s} {n:3d}")


if __name__ == "__main__":
    main()