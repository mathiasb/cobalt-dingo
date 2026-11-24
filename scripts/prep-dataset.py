# /// script
# requires-python = ">=3.10"
# dependencies = ["pyarrow"]
# ///
"""Convert a public Hugging Face invoice dataset into the extract-eval layout.

    uv run scripts/prep-dataset.py katanaml OUTDIR [SPLIT...]

Writes OUTDIR/invoices/NNN.png and OUTDIR/truth.json (extract.Truth records).
There is no history.json, so extract-eval skips the booking stages. Fields the
dataset does not label are left empty and are not scored. OUTDIR must be a
fresh scratch directory: downloaded files are untrusted data, so nothing here
is executed and nothing is written next to the code.

katanaml: katanaml-org/invoices-donut-data-v1 (MIT), splits test, validation, train.
Seller is name plus address in one string; the scorer accepts a word-boundary
prefix. Amounts are "$ 1.234,56"-style with a comma as the decimal mark.
"""
import json
import re
import sys
import urllib.request
from pathlib import Path

import pyarrow.parquet as pq

KATANAML = "https://huggingface.co/datasets/katanaml-org/invoices-donut-data-v1/resolve/refs%2Fconvert%2Fparquet/default/{split}/0000.parquet"


def dec(s):
    """'$ 1.234,56' or '889,20' -> '1234.56' / '889.20'; '' if unreadable."""
    if s is None:
        return ""
    s = re.sub(r"[^\d,.]", "", str(s))
    if not s:
        return ""
    if "," in s and "." in s:
        # the last separator is the decimal mark
        dm = "," if s.rfind(",") > s.rfind(".") else "."
        ts = "." if dm == "," else ","
        s = s.replace(ts, "").replace(dm, ".")
    elif "," in s:
        s = s.replace(",", ".")
    whole, _, frac = s.partition(".")
    return f"{int(whole or 0)}.{(frac + '00')[:2]}" if len(frac) <= 2 else ""


def iso(d):
    m = re.match(r"(\d\d)/(\d\d)/(\d{4})$", d or "")
    return f"{m[3]}-{m[1]}-{m[2]}" if m else ""


def katanaml(out: Path, splits):
    records, n, skipped = [], 0, 0
    for split in splits:
        local = out / f"{split}.parquet"
        urllib.request.urlretrieve(KATANAML.format(split=split), local)
        for row in pq.read_table(local).to_pylist():
            gt = json.loads(row["ground_truth"])["gt_parse"]
            if "header" not in gt or "summary" not in gt:
                skipped += 1  # some rows carry a different annotation shape
                continue
            h, s = gt["header"], gt["summary"]
            n += 1
            fn = f"invoices/{n:03d}.png"
            (out / fn).write_bytes(row["image"]["bytes"])
            records.append({
                "file": fn,
                "supplier": h.get("seller", ""),
                "invoice_number": h.get("invoice_no", ""),
                "invoice_date": iso(h.get("invoice_date")),
                "due_date": "",
                "currency": "USD" if "$" in str(s.get("total_gross_worth", "")) else "",
                "total": dec(s.get("total_gross_worth")),
                "vat_amount": dec(s.get("total_vat")),
                "expected_account": 0,
            })
    if skipped:
        print(f"skipped {skipped} rows without header/summary annotations", file=sys.stderr)
    return records


def main():
    if len(sys.argv) < 3 or sys.argv[1] != "katanaml":
        sys.exit(__doc__)
    out = Path(sys.argv[2])
    if out.exists() and any(out.iterdir()):
        sys.exit(f"{out} is not empty; use a fresh scratch directory")
    (out / "invoices").mkdir(parents=True, exist_ok=True)
    recs = katanaml(out, sys.argv[3:] or ["test"])
    (out / "truth.json").write_text(json.dumps(recs, ensure_ascii=False, indent=1) + "\n")
    print(f"wrote {len(recs)} invoices to {out}")


if __name__ == "__main__":
    main()
