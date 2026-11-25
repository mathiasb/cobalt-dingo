#!/usr/bin/env python3
"""Fetch Riksbanken's published daily exchange rates into the local file the demo reads.

    python3 scripts/fetch-riksbank-rates.py OUT.json [FROM [TO [CUR ...]]]

Defaults: the last 120 days, ending today, for EUR USD DKK NOK GBP CHF.

The demo never calls Riksbanken itself (a lookup keyed on an invoice's currency
and date would send a little of the invoice off the network). Run this on a
schedule or by hand, then restart the demo. Rates are Riksbanken's daily rates
(series SEK<CUR>PMI), SEK per ONE unit of the currency, written as exact decimal
strings and never as floats. The file's loader (internal/fx) refuses anything
implausible, so a bad fetch fails at startup instead of mispricing an entry.

Public data only: no credentials, no invoice data.
"""
import json
import sys
import time
import urllib.error
import urllib.request
from datetime import date, timedelta
from pathlib import Path

API = "https://api.riksbank.se/swea/v1/Observations"
DEFAULT = ["EUR", "USD", "DKK", "NOK", "GBP", "CHF"]


def fetch(cur: str, start: str, end: str) -> list[dict]:
    url = f"{API}/SEK{cur}PMI/{start}/{end}"
    for attempt in range(1, 7):
        try:
            with urllib.request.urlopen(url, timeout=30) as r:
                # parse_float=str keeps the exact decimal text, never a binary float.
                rows = json.loads(r.read().decode("utf-8"), parse_float=str, parse_int=str)
            break
        except urllib.error.HTTPError as e:
            # The API limits requests per minute: wait as long as it asks, then retry.
            if e.code != 429 or attempt == 6:
                raise
            wait = int(e.headers.get("Retry-After", "0") or 0) or 15 * attempt
            print(f"{cur}: rate limited, waiting {wait}s (attempt {attempt})", file=sys.stderr)
            time.sleep(wait)
    out = [{"date": row["date"], "rate": str(row["value"])} for row in rows if row.get("value") not in (None, "")]
    out.sort(key=lambda o: o["date"])
    return out


def main(argv: list[str]) -> int:
    if not argv:
        print(__doc__)
        return 2
    out = Path(argv[0])
    end = argv[2] if len(argv) > 2 else date.today().isoformat()
    start = argv[1] if len(argv) > 1 else (date.today() - timedelta(days=120)).isoformat()
    currencies = argv[3:] or DEFAULT
    table = {}
    for cur in currencies:
        rows = fetch(cur, start, end)
        if not rows:
            print(f"no rates returned for {cur} between {start} and {end}", file=sys.stderr)
            return 1
        table[cur] = rows
        time.sleep(2)  # be gentle with a free public API
    doc = {
        "source": "Riksbanken daily rates (SWEA series SEK<CUR>PMI), api.riksbank.se",
        "fetched": date.today().isoformat(),
        "currencies": table,
    }
    tmp = out.with_suffix(out.suffix + ".tmp")
    tmp.write_text(json.dumps(doc, indent=1) + "\n")
    tmp.replace(out)
    n = sum(len(v) for v in table.values())
    print(f"wrote {n} rates for {', '.join(table)} ({start} to {end}) to {out}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
