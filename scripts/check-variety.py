# /// script
# requires-python = ">=3.10"
# dependencies = ["pillow", "matplotlib", "reportlab", "python-docx", "pypdf"]
# ///
"""Checks the acceptance criteria of #160 that concern the generated set.

    uv run scripts/check-variety.py

Generates twice into temporary directories and asserts: the same seed gives a byte-identical
truth.json, at least 150 documents, all four formats, at least 8 layouts, credit notes,
more than one currency, text PDFs have extractable text and scans have none, every UBL
file is well-formed XML whose payable amount matches the truth, and each DOCX opens.
Exits non-zero on the first failed assertion, printing which.
"""
import json
import subprocess
import sys
import tempfile
import xml.etree.ElementTree as ET
from pathlib import Path

from docx import Document
from pypdf import PdfReader

HERE = Path(__file__).resolve().parent


def gen(out: Path, seed: str) -> list[dict]:
    subprocess.run([sys.executable, str(HERE / "gen-variety.py"), str(out), seed], check=True, capture_output=True)
    return json.loads((out / "truth.json").read_text())


def fail(msg: str):
    print("FAIL", msg)
    sys.exit(1)


with tempfile.TemporaryDirectory() as a, tempfile.TemporaryDirectory() as b:
    ta, tb = gen(Path(a), "7"), gen(Path(b), "7")
    if (Path(a) / "truth.json").read_bytes() != (Path(b) / "truth.json").read_bytes():
        fail("same seed gave different truth.json")
    if len(ta) < 150:
        fail(f"only {len(ta)} documents")
    fmts = {r["format"] for r in ta}
    if fmts != {"pdf_text", "pdf_scan", "docx", "ubl"}:
        fail(f"formats {fmts}")
    if len({r["layout"] for r in ta}) < 8:
        fail("fewer than 8 layouts")
    if not any(r["kind"] == "credit" for r in ta):
        fail("no credit note")
    if len({r["currency"] for r in ta}) < 4:
        fail("fewer than 4 currencies")
    for r in ta:
        p = Path(a) / r["file"]
        if r["format"] in ("pdf_text", "pdf_scan"):
            text = "".join(pg.extract_text() or "" for pg in PdfReader(str(p)).pages).strip()
            if r["format"] == "pdf_text" and len(text) < 100:
                fail(f"{r['file']} has no usable text layer")
            if r["format"] == "pdf_scan" and text:
                fail(f"{r['file']} should be image-only but has text")
        if r["format"] == "docx":
            Document(str(p))
        if r["format"] == "ubl":
            root = ET.parse(p).getroot()
            pay = root.find(".//{*}LegalMonetaryTotal/{*}PayableAmount")
            if pay is None or abs(float(pay.text)) != abs(float(r["total"])):
                fail(f"{r['file']} payable amount does not match truth")
            if (root.tag.endswith("CreditNote")) != (r["kind"] == "credit"):
                fail(f"{r['file']} document type does not match kind")
print(f"OK {len(ta)} documents, {len({r['layout'] for r in ta})} layouts, formats {sorted(fmts)}")
