#!/usr/bin/env python3
"""Vendor the Fortnox developer-portal guides as markdown.

Why this exists. Five of the six Fortnox mistakes in this repo's history came
from the DEVELOPER PORTAL rather than the API reference: service accounts,
the licence step, redirect-URI cardinality, Integrationer vs Testmiljoer, and
one integration serving both test and real companies. All five are documented
on fortnox.se/developer, and none of them is in the OpenAPI spec.

Why not a crawler. Measured 2026-09-14: these pages are server-rendered, so a
plain GET returns the full text including the scopes table. crawl4ai
(infra#187) is for genuinely JS-rendered sources — apps.fortnox.se/apidocs is
one, which is why its spec download link is not reachable this way.

Vendored and committed, same reasoning as docs/vendor/fortnox-openapi.json:
it pins what we read, and a diff on a later run is the cheapest change
detector for vendor documentation that has no changelog.

Usage: scripts/fetch-fortnox-guides.py [--out DIR]
"""

import argparse
import html
import os
import re
import sys
import time
import urllib.error
import urllib.request

BASE = "https://www.fortnox.se"

# Enumerated rather than crawled: the corpus is small, fixed, and a link
# crawler on a marketing site wanders into the whole of fortnox.se.
PAGES = [
    "/developer/faq",
    "/developer/checklist",
    "/developer/developer-portal",
    "/developer/guides-and-good-to-know/best-practices",
    "/developer/guides-and-good-to-know/delete-values",
    "/developer/guides-and-good-to-know/errors",
    "/developer/guides-and-good-to-know/formats-and-encoding",
    "/developer/guides-and-good-to-know/header-fields",
    "/developer/guides-and-good-to-know/legal-and-change-policies",
    "/developer/guides-and-good-to-know/parameters",
    "/developer/guides-and-good-to-know/pricing-models",
    "/developer/guides-and-good-to-know/rate-limits-for-fortnox-api",
    "/developer/guides-and-good-to-know/responses",
    "/developer/guides-and-good-to-know/scopes",
    "/developer/guides-and-good-to-know/websockets",
    "/developer/guides-and-good-to-know/warehouse-resource-specific-fields",
]

# One request every two seconds. Their docs host returned 429 during this
# session's manual reading, and a vendor whose documentation we depend on is
# not somewhere to be impolite.
DELAY_SECONDS = 2


def fetch(path):
    req = urllib.request.Request(BASE + path, headers={"User-Agent": "cobalt-dingo-docs/1.0"})
    with urllib.request.urlopen(req, timeout=30) as resp:
        return resp.read().decode("utf-8", "replace")


def to_markdown(raw):
    """Extract <main> and reduce it to readable text.

    Deliberately crude. A faithful HTML-to-markdown conversion needs a
    dependency, and the purpose here is retrieval and diffing, not rendering.
    Tables are the one structure worth keeping legible, because the scopes
    table is the single most load-bearing page in the corpus.
    """
    m = re.search(r"(?is)<main[^>]*>(.*?)</main>", raw)
    body = m.group(1) if m else raw

    body = re.sub(r"(?is)<(script|style|svg|noscript)[^>]*>.*?</\1>", " ", body)
    body = re.sub(r"(?is)<h([1-6])[^>]*>(.*?)</h\1>",
                  lambda mo: "\n\n" + "#" * int(mo.group(1)) + " " + mo.group(2) + "\n", body)
    body = re.sub(r"(?is)</(p|div|section|tr|ul|ol)>", "\n", body)
    body = re.sub(r"(?is)<li[^>]*>", "\n- ", body)
    body = re.sub(r"(?is)</t[dh]>", " | ", body)
    body = re.sub(r"(?s)<[^>]+>", "", body)
    body = html.unescape(body)

    lines = [re.sub(r"[ \t]+", " ", ln).strip() for ln in body.splitlines()]
    out, blank = [], 0
    for ln in lines:
        if ln:
            blank = 0
            out.append(ln)
        else:
            blank += 1
            if blank == 1:
                out.append("")
    return "\n".join(out).strip() + "\n"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=os.path.join(os.path.dirname(__file__), "..", "docs", "vendor", "fortnox-guides"))
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)

    failures = []
    for i, path in enumerate(PAGES):
        if i:
            time.sleep(DELAY_SECONDS)
        name = path.strip("/").replace("/", "__") + ".md"
        try:
            text = to_markdown(fetch(path))
        except (urllib.error.HTTPError, urllib.error.URLError, TimeoutError) as e:
            # Report and carry on: a partial vault is useful, and a page that
            #404s tells us the vendor moved it, which is itself the signal.
            failures.append(f"{path}: {e}")
            print(f"  FAIL {path}: {e}", file=sys.stderr)
            continue
        header = f"<!-- vendored from {BASE}{path} by scripts/fetch-fortnox-guides.py -->\n\n"
        with open(os.path.join(args.out, name), "w") as f:
            f.write(header + text)
        print(f"  ok   {path}  ({len(text)} chars)")

    if failures:
        print(f"\n{len(failures)} page(s) failed:", file=sys.stderr)
        for f in failures:
            print("  " + f, file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
