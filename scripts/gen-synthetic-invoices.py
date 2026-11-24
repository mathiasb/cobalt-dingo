# /// script
# requires-python = ">=3.10"
# dependencies = ["pillow", "matplotlib"]
# ///
"""Deterministic synthetic invoices for the invoice-processor demo (bet #142).

    uv run scripts/gen-synthetic-invoices.py [outdir] [seed]

Writes, under outdir (default testdata/synthetic):
  invoices/NNN.png   rendered invoice images, four layouts, Swedish and English labels
  truth.json         ground truth per image, including the account its supplier is
                     normally booked to (for the end-to-end check)
  history.json       ~two years of past supplier-invoice bookings as domain.Voucher JSON

Everything is fictional. No real supplier, customer or amount appears. Same
seed gives byte-identical truth.json and history.json. Pillow and matplotlib
(only for its bundled DejaVu fonts, which carry å ä ö) are throwaway tooling
pulled in by uv; they are not dependencies of the Go module.
"""
import json
import random
import sys
from datetime import date, timedelta
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

# name, address, country, currency, vat_rate, main_account, alt_account, id_line, lang, layout, net_lo, net_hi
SUPPLIERS = [
    ("Nordkloud AB", "Storgatan 12, 111 22 Stockholm", "SE", "SEK", 25, 6540, 5420, "Org.nr 556123-4567", "sv", 0, 800, 4200),
    ("Fjällresor AB", "Kungsgatan 3, 411 19 Göteborg", "SE", "SEK", 6, 5810, 5800, "Org.nr 556234-5678", "sv", 1, 900, 6500),
    ("Kontorsbolaget i Malmö AB", "Hamngatan 8, 211 22 Malmö", "SE", "SEK", 25, 5460, 5410, "Org.nr 556345-6789", "sv", 2, 150, 2400),
    ("Svea Programvara AB", "Vasagatan 20, 111 20 Stockholm", "SE", "SEK", 25, 5420, 6540, "Org.nr 556456-7890", "sv", 0, 300, 3000),
    ("Parkera Smart AB", "Östra Hamngatan 5, 411 10 Göteborg", "SE", "SEK", 25, 5800, 5810, "Org.nr 556567-8901", "sv", 3, 40, 400),
    ("Lund Konsult HB", "Bruksgatan 2, 222 22 Lund", "SE", "SEK", 25, 6550, 6550, "Org.nr 969123-4567", "sv", 1, 5000, 24000),
    ("Stockholms Elverk AB", "Energivägen 1, 117 43 Stockholm", "SE", "SEK", 25, 5020, 5020, "Org.nr 556678-9012", "sv", 2, 600, 1800),
    ("Telefonia Sverige AB", "Telegatan 9, 126 25 Hägersten", "SE", "SEK", 25, 6212, 6230, "Org.nr 556789-0123", "sv", 3, 250, 700),
    ("Berlin Cloudworks GmbH", "Kreuzbergstr. 14, 10965 Berlin", "DE", "EUR", 0, 6540, 6540, "VAT ID DE812000111", "en", 0, 10, 220),
    ("Dublin Software Ltd", "12 Grand Canal Dock, Dublin 2", "IE", "EUR", 0, 5420, 5420, "VAT ID IE6388047V", "en", 1, 20, 480),
    ("Alpine Rail AG", "Bahnhofplatz 4, 8001 Zürich", "CH", "EUR", 0, 5810, 5810, "VAT ID CHE-123.456.789", "en", 2, 60, 900),
    ("Helsinki Print Oy", "Mannerheimintie 7, 00100 Helsinki", "FI", "EUR", 0, 5460, 5460, "VAT ID FI12345678", "en", 3, 30, 300),
]

HISTORY_START = date(2025, 1, 6)
HISTORY_END = date(2026, 10, 30)
INVOICE_MONTH = (2026, 11)  # the "future" the images are drawn from
PER_SUPPLIER_IMAGES = 3

LABELS = {
    "sv": dict(title="Faktura", no="Fakturanummer", date="Fakturadatum", due="Förfallodatum",
               net="Summa exkl. moms", vat="Moms", total="Att betala"),
    "en": dict(title="Invoice", no="Invoice no.", date="Invoice date", due="Due date",
               net="Net total", vat="VAT", total="Total"),
}


def money(minor: int, cur: str, lang: str) -> str:
    whole, frac = divmod(minor, 100)
    if lang == "sv":
        return f"{whole:,}".replace(",", " ") + f",{frac:02d} {'kr' if cur == 'SEK' else cur}"
    return f"{whole:,}.{frac:02d} {cur}"


def fmt_date(d: date, layout: int) -> str:
    if layout == 2:
        return d.strftime("%d.%m.%Y")
    if layout == 3:
        return d.strftime("%d %b %Y")
    return d.isoformat()


def font(size: int, bold: bool = False):
    import matplotlib

    name = "DejaVuSans-Bold.ttf" if bold else "DejaVuSans.ttf"
    return ImageFont.truetype(str(Path(matplotlib.get_data_path()) / "fonts" / "ttf" / name), size)


def render(path: Path, sup, inv):
    name, addr, _c, cur, rate, _m, _a, idline, lang, layout, *_ = sup
    L = LABELS[lang]
    w, h = 900, 1120
    img = Image.new("RGB", (w, h), "white")
    d = ImageDraw.Draw(img)
    big, mid, small = font(32, True), font(20), font(17)
    d.text((50, 40), name, fill=(10, 60, 100), font=big)
    d.text((50, 88), addr, fill="black", font=small)
    d.text((50, 114), idline, fill="black", font=small)
    ox = 520 if layout in (1, 3) else 50
    oy = 215
    d.text((50, 160), L["title"].upper(), fill=(10, 60, 100), font=big)
    rows = [(L["no"], inv["number"]), (L["date"], fmt_date(inv["date"], layout)), (L["due"], fmt_date(inv["due"], layout))]
    for i, (k, v) in enumerate(rows):
        d.text((ox, oy + i * 34), f"{k}:", fill="black", font=mid)
        d.text((ox + 200, oy + i * 34), v, fill="black", font=mid)
    ty = 360
    d.line((50, ty, w - 50, ty), fill="black", width=2)
    d.text((50, ty + 10), "Beskrivning" if lang == "sv" else "Description", fill="black", font=mid)
    d.text((w - 250, ty + 10), "Belopp" if lang == "sv" else "Amount", fill="black", font=mid)
    d.line((50, ty + 46, w - 50, ty + 46), fill="black", width=1)
    for i, (desc, amt) in enumerate(inv["lines"]):
        y = ty + 62 + i * 36
        d.text((50, y), desc, fill="black", font=small)
        d.text((w - 250, y), money(amt, cur, lang), fill="black", font=small)
    sy = ty + 62 + len(inv["lines"]) * 36 + 40
    d.line((w - 450, sy - 10, w - 50, sy - 10), fill="black", width=1)
    d.text((w - 450, sy), L["net"], fill="black", font=small)
    d.text((w - 250, sy), money(inv["net"], cur, lang), fill="black", font=small)
    d.text((w - 450, sy + 32), f"{L['vat']} {rate}%", fill="black", font=small)
    d.text((w - 250, sy + 32), money(inv["vat"], cur, lang), fill="black", font=small)
    d.text((w - 450, sy + 76), L["total"], fill="black", font=font(22, True))
    d.text((w - 250, sy + 76), money(inv["total"], cur, lang), fill="black", font=font(22, True))
    if rate == 0 and cur != "SEK":
        d.text((50, sy + 140), "Reverse charge / Omvänd skattskyldighet", fill=(90, 90, 90), font=small)
    img.save(path)


def make_invoice(rng, sup, when: date, seq: int):
    name, _a, _c, cur, rate, _m, _alt, _id, _lang, _layout, lo, hi = sup
    n_lines = rng.randint(1, 3)
    lines = [(rng.choice(["Tjänst", "Licens", "Månadsavgift", "Resa", "Material"]) if _lang == "sv"
              else rng.choice(["Service", "License", "Monthly fee", "Fare", "Supplies"]),
              rng.randint(lo * 100, hi * 100) // n_lines // 100 * 100 + rng.randint(0, 99)) for _ in range(n_lines)]
    net = sum(a for _, a in lines)
    vat = round(net * rate / 100)
    return {
        "number": f"{rng.randint(10, 99)}{when.year % 100}{seq:04d}",
        "date": when,
        "due": when + timedelta(days=30),
        "lines": lines,
        "net": net,
        "vat": vat,
        "total": net + vat,
    }


def voucher(series, number, when, supplier, account, net_sek, vat_rate):
    vat = round(net_sek * vat_rate / 100)
    z = {"MinorUnits": 0, "Currency": "SEK"}
    m = lambda v: {"MinorUnits": v, "Currency": "SEK"}
    rows = [{"Account": account, "Debit": m(net_sek), "Credit": z, "Description": "", "CostCenter": "", "Project": ""}]
    if vat:
        rows.append({"Account": 2641, "Debit": m(vat), "Credit": z, "Description": "", "CostCenter": "", "Project": ""})
    rows.append({"Account": 2440, "Debit": z, "Credit": m(net_sek + vat), "Description": "", "CostCenter": "", "Project": ""})
    return {"Series": series, "Number": number, "Description": supplier, "TransactionDate": when.isoformat(),
            "Year": when.year, "Rows": rows}


def main():
    out = Path(sys.argv[1] if len(sys.argv) > 1 else "testdata/synthetic")
    seed = int(sys.argv[2]) if len(sys.argv) > 2 else 20261124
    rng = random.Random(seed)
    (out / "invoices").mkdir(parents=True, exist_ok=True)

    # Past bookings: each supplier is usually booked to its main account and
    # occasionally to an alternative, so held-out accuracy is realistic, not 100%.
    history, num = [], 1
    d = HISTORY_START
    while d <= HISTORY_END:
        for sup in SUPPLIERS:
            name, _a, _c, cur, rate, main_acct, alt, *_rest = sup
            lo, hi = sup[10], sup[11]
            if rng.random() < 0.55:  # most suppliers bill most months
                acct = alt if rng.random() < 0.08 else main_acct
                net = rng.randint(lo, hi) * 100 * (11 if cur == "EUR" else 1)
                desc = name if rng.random() < 0.6 else f"{name} faktura {rng.randint(1000, 9999)}"
                history.append(voucher("A", num, d + timedelta(days=rng.randint(0, 25)), desc, acct, net, rate))
                num += 1
        d = (d.replace(day=1) + timedelta(days=32)).replace(day=1)
    history.sort(key=lambda v: (v["TransactionDate"], v["Number"]))
    (out / "history.json").write_text(json.dumps(history, ensure_ascii=False, indent=1) + "\n")

    truth, idx = [], 1
    for sup in SUPPLIERS:
        for k in range(PER_SUPPLIER_IMAGES):
            when = date(INVOICE_MONTH[0], INVOICE_MONTH[1], rng.randint(1, 26))
            inv = make_invoice(rng, sup, when, idx)
            fn = f"invoices/{idx:03d}.png"
            render(out / fn, sup, inv)
            cur = sup[3]
            dec = lambda v: f"{v // 100}.{v % 100:02d}"
            truth.append({
                "file": fn, "supplier": sup[0], "invoice_number": inv["number"],
                "invoice_date": inv["date"].isoformat(), "due_date": inv["due"].isoformat(),
                "currency": cur, "total": dec(inv["total"]), "vat_amount": dec(inv["vat"]),
                "expected_account": sup[5],
            })
            idx += 1
    (out / "truth.json").write_text(json.dumps(truth, ensure_ascii=False, indent=1) + "\n")
    print(f"wrote {len(truth)} invoices and {len(history)} historical vouchers to {out}")


if __name__ == "__main__":
    main()
