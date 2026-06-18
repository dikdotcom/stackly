#!/usr/bin/env python3
"""Expand Stackly fingerprints to Wappalyzer-level coverage.
Adds ~35 techs focused on marketing, retargeting, live-chat, analytics, CDN, framework.
Re-categorizes: HubSpot (analytics->marketing), Facebook Pixel (analytics->retargeting),
Intercom/Crisp (helpdesk->live-chat).
"""
import json
import sys

PATH = "data/fingerprints.json"

# ---- New fingerprints (35 total) ----
# Each: (slug, name, website, category, icon, detectors)
NEW = [
    # ===== Marketing (9 new) =====
    ("hubspot", "HubSpot", "https://hubspot.com", "marketing", "hubspot.svg", {
        "html": ["js.hsforms.net", "hubspot.com", "hsadspixel.net"],
        "scripts": ["js.hsforms.net", "hs-scripts.com", "hs-analytics.net"],
        "js_globals": ["_hsq", "hbspt"],
        "cookies": ["hubspotutk", "__hssc", "__hssrc"],
    }),
    ("activecampaign", "ActiveCampaign", "https://activecampaign.com", "marketing", "activecampaign.svg", {
        "html": ["activehosted.com", "trackcmp.net"],
        "scripts": ["trackcmp.net", "activehosted.com"],
    }),
    ("convertkit", "ConvertKit", "https://convertkit.com", "marketing", "convertkit.svg", {
        "html": ["convertkit.com", "ck.page"],
        "scripts": ["convertkit.com"],
    }),
    ("brevo", "Brevo", "https://brevo.com", "marketing", "brevo.svg", {
        "html": ["sib-cdn.com", "sendinblue.com", "brevo.com", "sibautomation.com"],
        "scripts": ["sib-cdn.com", "sendinblue.com"],
        "cookies": ["sib_cuid"],
    }),
    ("sendgrid", "Sendgrid", "https://sendgrid.com", "marketing", "sendgrid.svg", {
        "html": ["sendgrid.net", "sendgrid.com"],
        "scripts": ["sendgrid.com"],
    }),
    ("mailgun", "Mailgun", "https://mailgun.com", "marketing", "mailgun.svg", {
        "html": ["mailgun.com", "mgcdn.com"],
        "scripts": ["mailgun.com"],
    }),
    ("marketo", "Marketo", "https://marketo.com", "marketing", "marketo.svg", {
        "html": ["marketo.com", "munchkin.js", "marketo.net"],
        "scripts": ["munchkin.js", "marketo.com"],
        "js_globals": ["Munchkin"],
    }),
    ("omnisend", "Omnisend", "https://omnisend.com", "marketing", "omnisend.svg", {
        "html": ["omnisend.com", "omnisnippet"],
        "scripts": ["omnisend.com"],
        "cookies": ["omnisendAccountID"],
    }),
    ("sumo", "Sumo", "https://sumo.com", "marketing", "sumo.svg", {
        "html": ["sumo.com", "load.sumo.com"],
        "scripts": ["load.sumo.com"],
        "js_globals": ["sumo"],
    }),

    # ===== Retargeting (7 new) =====
    ("facebook-pixel", "Facebook Pixel", "https://facebook.com/business/tools/meta-pixel", "retargeting", "facebook.svg", {
        "html": ["connect.facebook.net/en_US/fbevents.js", "fbq('init'", "fbq('track'"],
        "scripts": ["connect.facebook.net", "fbevents.js"],
        "js_globals": ["fbq", "_fbq"],
    }),
    ("tiktok-pixel", "TikTok Pixel", "https://ads.tiktok.com", "retargeting", "tiktok.svg", {
        "html": ["analytics.tiktok.com", "ttq.load"],
        "scripts": ["analytics.tiktok.com"],
        "js_globals": ["ttq", "_ttq"],
    }),
    ("pinterest-tag", "Pinterest Tag", "https://business.pinterest.com", "retargeting", "pinterest.svg", {
        "html": ["s.pinimg.com/ct", "pintrk"],
        "scripts": ["s.pinimg.com/ct"],
        "js_globals": ["pintrk", "_pinterest_loaded"],
    }),
    ("linkedin-insight", "LinkedIn Insight", "https://linkedin.com", "retargeting", "linkedin.svg", {
        "html": ["snap.licdn.com/li.lms-analytics", "_linkedin_partner_id"],
        "scripts": ["snap.licdn.com"],
        "js_globals": ["_linkedin_partner_id", "lintrk"],
    }),
    ("bing-ads", "Microsoft Ads", "https://ads.microsoft.com", "retargeting", "bing-ads.svg", {
        "html": ["bat.bing.com", "uetq"],
        "scripts": ["bat.bing.com"],
        "js_globals": ["uetq", "UET"],
    }),
    ("google-ads", "Google Ads", "https://ads.google.com", "retargeting", "google-ads.svg", {
        "html": ["googleadservices.com", "AW-", "google_conversion_id"],
        "scripts": ["googleadservices.com"],
        "js_globals": ["gtag", "google_tag_data"],
    }),
    ("taboola", "Taboola", "https://taboola.com", "retargeting", "taboola.svg", {
        "html": ["cdn.taboola.com", "taboola"],
        "scripts": ["cdn.taboola.com"],
        "js_globals": ["_taboola"],
    }),

    # ===== Live Chat (6 new) =====
    ("intercom", "Intercom", "https://intercom.com", "live-chat", "intercom.svg", {
        "scripts": ["widget.intercom.io", "intercom.io/widget"],
        "js_globals": ["Intercom", "intercomSettings"],
        "cookies": ["intercom-id"],
    }),
    ("crisp", "Crisp", "https://crisp.chat", "live-chat", "crisp.svg", {
        "scripts": ["client.crisp.chat"],
        "js_globals": ["CRISP_WEBSITE_ID", "$crisp", "Crisp"],
    }),
    ("tawkto", "Tawk.to", "https://tawk.to", "live-chat", "tawkto.svg", {
        "scripts": ["embed.tawk.to"],
        "js_globals": ["Tawk_API", "Tawk"],
    }),
    ("drift", "Drift", "https://drift.com", "live-chat", "drift.svg", {
        "scripts": ["js.driftt.com", "widget.drift.com"],
        "js_globals": ["drift", "Drift"],
    }),
    ("zopim", "Zendesk Chat", "https://zendesk.com/chat", "live-chat", "zendesk.svg", {
        "scripts": ["v2.zopim.com", "static.zdassets.com"],
        "js_globals": ["$zopim", "zE"],
    }),
    ("tidio", "Tidio", "https://tidio.com", "live-chat", "tidio.svg", {
        "scripts": ["code.tidio.co"],
        "js_globals": ["tidio", "Tidio"],
    }),

    # ===== Analytics (8 new) =====
    ("plausible", "Plausible", "https://plausible.io", "analytics", "plausible.svg", {
        "scripts": ["plausible.io/js", "plausible.js"],
        "js_globals": ["plausible"],
    }),
    ("matomo", "Matomo", "https://matomo.org", "analytics", "matomo.svg", {
        "scripts": ["matomo.js", "piwik.js", "matomo.php"],
        "js_globals": ["_paq", "Matomo"],
        "cookies": ["_pk_id", "_pk_ses"],
    }),
    ("fathom", "Fathom", "https://usefathom.com", "analytics", "fathom.svg", {
        "scripts": ["cdn.usefathom.com"],
        "js_globals": ["fathom"],
    }),
    ("heap", "Heap", "https://heap.io", "analytics", "heap.svg", {
        "scripts": ["cdn.heapanalytics.com", "heapanalytics.com"],
        "js_globals": ["heap", "Heap"],
    }),
    ("fullstory", "FullStory", "https://fullstory.com", "analytics", "fullstory.svg", {
        "scripts": ["edge.fullstory.com", "fullstory.com/s/fs.js"],
        "js_globals": ["FS", "_fs_namespace"],
    }),
    ("mouseflow", "Mouseflow", "https://mouseflow.com", "analytics", "mouseflow.svg", {
        "scripts": ["mouseflow.com", "cdn.mouseflow.com"],
        "js_globals": ["_mfq"],
        "cookies": ["mf_"],
    }),
    ("logrocket", "LogRocket", "https://logrocket.com", "analytics", "logrocket.svg", {
        "scripts": ["cdn.logrocket.io", "cdn.lr-in.com", "cdn.lr-in-prod.com"],
        "js_globals": ["LogRocket"],
    }),
    ("clarity", "Microsoft Clarity", "https://clarity.microsoft.com", "analytics", "clarity.svg", {
        "scripts": ["clarity.ms"],
        "js_globals": ["clarity"],
        "cookies": ["_clck", "_clsk"],
    }),

    # ===== CDN (4 new) =====
    ("jsdelivr", "jsDelivr", "https://jsdelivr.com", "cdn", "jsdelivr.svg", {
        "scripts": ["cdn.jsdelivr.net"],
    }),
    ("unpkg", "unpkg", "https://unpkg.com", "cdn", "unpkg.svg", {
        "scripts": ["unpkg.com"],
    }),
    ("akamai", "Akamai", "https://akamai.com", "cdn", "akamai.svg", {
        "headers": ["x-akamai-", "akamai-"],
    }),
    ("keycdn", "KeyCDN", "https://keycdn.com", "cdn", "keycdn.svg", {
        "headers": ["x-keycdn", "keycdn-"],
    }),

    # ===== Framework (4 new SSG) =====
    ("gatsby", "Gatsby", "https://gatsbyjs.com", "framework", "gatsby.svg", {
        "html": ["___gatsby"],
        "js_globals": ["___gatsby", "Gatsby"],
        "meta": [{"name": "generator", "pattern": "Gatsby"}],
    }),
    ("hugo", "Hugo", "https://gohugo.io", "framework", "hugo.svg", {
        "meta": [{"name": "generator", "pattern": "Hugo"}],
    }),
    ("jekyll", "Jekyll", "https://jekyllrb.com", "framework", "jekyll.svg", {
        "meta": [{"name": "generator", "pattern": "Jekyll"}],
        "html": ["/_includes/", "jekyll"],
    }),
    ("strapi", "Strapi", "https://strapi.io", "framework", "strapi.svg", {
        "headers": ["x-powered-by", "strapi"],
        "html": ["strapi"],
    }),
]


def detectors_from_simple(spec: dict) -> dict:
    """Convert {'html': ['a', 'b'], 'headers': ['x-foo']} to detector format."""
    out = {}
    for kind, items in spec.items():
        if kind in ("headers", "meta"):
            out[kind] = [{"pattern": p} for p in items]
        else:
            out[kind] = [{"pattern": p} for p in items]
    return out


def build_fingerprint(slug, name, website, category, icon, det_spec):
    return {
        "slug": slug,
        "name": name,
        "website": website,
        "category": category,
        "icon": icon,
        "detectors": detectors_from_simple(det_spec),
    }


def main():
    with open(PATH) as f:
        data = json.load(f)

    # Bump version
    data["version"] = "1.1.0"
    data["updated"] = "2026-06-18"

    existing_slugs = {t["slug"] for t in data["technologies"]}

    # Remove old HubSpot/Facebook Pixel/Intercom/Crisp so we can re-add with new category
    new_cat_overrides = {"hubspot", "facebook-pixel", "intercom", "crisp"}
    data["technologies"] = [t for t in data["technologies"] if t["slug"] not in new_cat_overrides]
    print(f"Removed {len(new_cat_overrides)} techs for re-categorization")

    added = 0
    skipped = 0
    for entry in NEW:
        slug = entry[0]
        if slug in existing_slugs and slug not in new_cat_overrides:
            print(f"SKIP (already exists): {slug}")
            skipped += 1
            continue
        fp = build_fingerprint(*entry)
        data["technologies"].append(fp)
        added += 1
        print(f"+ {fp['category']:14s} {fp['slug']:24s} {fp['name']}")

    with open(PATH, "w") as f:
        json.dump(data, f, indent=2, ensure_ascii=False)

    print()
    print(f"Added: {added}, Skipped: {skipped}")
    print(f"Total techs: {len(data['technologies'])}")
    cats = {}
    for t in data["technologies"]:
        c = t.get("category", "other")
        cats[c] = cats.get(c, 0) + 1
    print("Category breakdown:")
    for c, n in sorted(cats.items(), key=lambda x: -x[1]):
        print(f"  {c:14s} {n}")


if __name__ == "__main__":
    main()