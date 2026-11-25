# /// script
# requires-python = ">=3.10"
# dependencies = ["pillow", "matplotlib", "reportlab", "python-docx"]
# ///
"""A larger, more varied, fully fictional invoice set in several formats (slice #160, bet #142).

    uv run scripts/gen-variety.py [outdir] [seed]

One invoice model is rendered into: a PDF with a text layer (many layouts), an
image-only PDF (a "scan": no text layer), a DOCX, and a Peppol BIS 3.0 UBL XML
(Invoice and CreditNote). Written under outdir (default testdata/variety):

  files/NNN.<ext>   the documents
  truth.json        ground truth per file: format, layout, kind (invoice|credit), supplier,
                    number, dates, currency, net, vat, total (signed: credit notes are
                    negative), expected_account, and new_supplier

Variety on purpose: Swedish and English labels, three number styles, five date styles,
single and mixed VAT rates, credit notes, rounding lines (öresavrundning), reverse
charge, multi-page, distractor numbers and dates (order, customer number, bankgiro,
the customer's own address block), and SEK/EUR/USD/DKK/NOK.

Everything is fictional. No real company, person or number. The same seed gives a
byte-identical truth.json. reportlab, python-docx, pillow and matplotlib (for its
bundled DejaVu fonts, which carry å ä ö) are throwaway tooling pulled in by uv.
"""
import importlib.util
import json
import random
import sys
import zipfile  # noqa: F401  (python-docx writes a zip; imported to make the dependency explicit)
from datetime import date, datetime, timedelta
from pathlib import Path
from xml.sax.saxutils import escape

import matplotlib
from docx import Document
from reportlab import rl_config
from reportlab.lib import colors
from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import ParagraphStyle
from reportlab.lib.units import mm
from reportlab.pdfbase import pdfmetrics
from reportlab.pdfbase.ttfonts import TTFont
from reportlab.platypus import PageBreak, Paragraph, SimpleDocTemplate, Spacer, Table, TableStyle

rl_config.invariant = 1  # no creation date or random id in PDFs

HERE = Path(__file__).resolve().parent
_spec = importlib.util.spec_from_file_location("gen_synth", HERE / "gen-synthetic-invoices.py")
synth = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(synth)

FONTS = Path(matplotlib.get_data_path()) / "fonts" / "ttf"
pdfmetrics.registerFont(TTFont("DejaVu", str(FONTS / "DejaVuSans.ttf")))
pdfmetrics.registerFont(TTFont("DejaVu-Bold", str(FONTS / "DejaVuSans-Bold.ttf")))

# name, address, country, currency, rates, main_account, alt_account, id_line, lang, net_lo, net_hi, tax_id, new
EXTRA = [
    ("Gotland Fika & Mat AB", "Strandgatan 4, 621 56 Visby", "SE", "SEK", [12], 5831, 5830, "Org.nr 556890-1234", "sv", 120, 1800, "SE556890123401", True),
    ("Östersunds Hotell AB", "Prästgatan 11, 831 30 Östersund", "SE", "SEK", [12, 25], 5831, 5830, "Org.nr 556901-2345", "sv", 900, 7800, "SE556901234501", True),
    ("Norrlands Bilservice AB", "Verkstadsvägen 7, 903 47 Umeå", "SE", "SEK", [25], 5611, 5612, "Org.nr 556012-3456", "sv", 400, 9000, "SE556012345601", True),
    ("Bergsjö Tryckeri AB", "Industrigatan 21, 824 30 Hudiksvall", "SE", "SEK", [25], 5910, 5460, "Org.nr 556123-4560", "sv", 300, 5200, "SE556123456001", True),
    ("Copenhagen Design ApS", "Vesterbrogade 40, 1620 København", "DK", "DKK", [0], 6550, 6540, "CVR 12345678", "en", 800, 9000, "DK12345678", True),
    ("Oslo Systems AS", "Karl Johans gate 9, 0154 Oslo", "NO", "NOK", [0], 6540, 5420, "Org.nr 912 345 678", "en", 700, 8000, "NO912345678MVA", True),
    ("Acme Cloud Inc", "500 Howard St, San Francisco, CA 94105", "US", "USD", [0], 5420, 6540, "EIN 12-3456789", "en", 20, 900, "US123456789", True),
    ("Tallinn Logistics OÜ", "Narva mnt 5, 10117 Tallinn", "EE", "EUR", [0], 5710, 5700, "Reg. 12345678", "en", 100, 2400, "EE123456789", True),
]
SUPS = []
for s in synth.SUPPLIERS:  # reuse the demo's suppliers so their booking history applies
    name, addr, cty, cur, rate, main, alt, idline, lang, _layout, lo, hi = s
    SUPS.append((name, addr, cty, cur, [rate], main, alt, idline, lang, lo, hi, "SE" + "".join(c for c in idline if c.isdigit()) + "01" if cty == "SE" else idline.split()[-1], False))
SUPS += EXTRA

CUSTOMER = ("Definitely Mabe AB", "Drottninggatan 99, 111 60 Stockholm", "Org.nr 559900-0001", "SE559900000101")
BANK = {"SE": "Bankgiro 123-4567", "DE": "IBAN DE89 3704 0044 0532 0130 00", "IE": "IBAN IE29 AIBK 9311 5212 3456 78",
        "CH": "IBAN CH93 0076 2011 6238 5295 7", "FI": "IBAN FI21 1234 5600 0007 85", "DK": "IBAN DK50 0040 0440 1162 43",
        "NO": "IBAN NO93 8601 1117 947", "US": "ACH routing 021000021", "EE": "IBAN EE38 2200 2210 2014 5685"}

LBL = {
    "sv": [dict(title="Faktura", no="Fakturanummer", date="Fakturadatum", due="Förfallodatum", net="Summa exkl. moms", vat="Moms", total="Att betala", desc="Beskrivning", qty="Antal", unit="À-pris", amt="Belopp", rate="Moms %", cust="Fakturaadress", ref="Er referens", order="Ordernummer", terms="Betalningsvillkor 30 dagar", rnd="Öresavrundning", credit="Kreditfaktura"),
           dict(title="Faktura", no="Faktura nr", date="Datum", due="Förfaller", net="Netto", vat="Moms", total="Summa att betala", desc="Artikel", qty="Ant.", unit="Pris", amt="Summa", rate="Moms", cust="Kund", ref="Vår referens", order="Ert ordernr", terms="30 dagar netto", rnd="Avrundning", credit="Kreditnota")],
    "en": [dict(title="Invoice", no="Invoice no.", date="Invoice date", due="Due date", net="Subtotal", vat="VAT", total="Total due", desc="Description", qty="Qty", unit="Unit price", amt="Amount", rate="VAT %", cust="Bill to", ref="Your reference", order="Order no.", terms="Payment terms: 30 days", rnd="Rounding", credit="Credit note"),
           dict(title="Tax invoice", no="Number", date="Issued", due="Payable by", net="Net total", vat="Tax", total="Amount payable", desc="Item", qty="Units", unit="Rate", amt="Total", rate="Tax %", cust="Customer", ref="Contact", order="PO", terms="Net 30", rnd="Rounding", credit="Credit memo")],
}
WORDS = {"sv": ["Konsulttjänst", "Licens", "Månadsavgift", "Resa", "Material", "Support", "Frakt", "Underhåll"],
         "en": ["Service", "License", "Monthly fee", "Fare", "Supplies", "Support", "Freight", "Maintenance"]}


def num(minor: int, style: str) -> str:
    sign = "-" if minor < 0 else ""
    whole, frac = divmod(abs(minor), 100)
    if style == "sv":
        return f"{sign}{whole:,}".replace(",", " ") + f",{frac:02d}"
    if style == "en":
        return f"{sign}{whole:,}.{frac:02d}"
    return f"{sign}{whole}.{frac:02d}"


def dt(d: date, style: int) -> str:
    return [d.isoformat(), d.strftime("%d.%m.%Y"), d.strftime("%d %b %Y"), d.strftime("%-d/%-m/%Y"), d.strftime("%d-%m-%y")][style]


def dec(v: int) -> str:
    sign = "-" if v < 0 else ""
    return f"{sign}{abs(v) // 100}.{abs(v) % 100:02d}"


def build(rng: random.Random, sup, seq: int, kind: str, multipage: bool):
    name, addr, cty, cur, rates, main, alt, idline, lang, lo, hi, taxid, new = sup
    when = date(2026, 11, rng.randint(1, 26))
    n = rng.randint(14, 22) if multipage else rng.randint(1, 4)
    sgn = -1 if kind == "credit" else 1
    lines = []
    for _ in range(n):
        rate = rng.choice(rates)
        qty = rng.choice([1, 1, 2, 3, 5, 10])
        unit = max(100, rng.randint(lo * 100, hi * 100) // (n * qty)) // 1 + rng.randint(0, 99)
        lines.append(dict(desc=rng.choice(WORDS[lang]), qty=qty, unit=unit, rate=rate, net=sgn * qty * unit))
    by_rate: dict[int, tuple[int, int]] = {}
    for ln in lines:
        base, _ = by_rate.get(ln["rate"], (0, 0))
        by_rate[ln["rate"]] = (base + ln["net"], 0)
    by_rate = {r: (b, round(b * r / 100)) for r, (b, _) in by_rate.items()}
    net = sum(b for b, _ in by_rate.values())
    vat = sum(v for _, v in by_rate.values())
    total = net + vat
    rounding = 0
    if cur == "SEK" and rng.random() < 0.3:  # öresavrundning: total to a whole krona
        rounding = round(total / 100) * 100 - total
        total += rounding
    return dict(sup=sup, kind=kind, number=f"{rng.randint(10, 99)}{when.year % 100}{seq:04d}", date=when,
                due=when + timedelta(days=30), lines=lines, by_rate=by_rate, net=net, vat=vat, rounding=rounding, total=total,
                order=f"PO-{rng.randint(1000, 9999)}", custno=f"K{rng.randint(10000, 99999)}", ref=rng.choice(["M. Berg", "A. Lind", "S. Ek"]),
                lbl=rng.choice(LBL[lang]), numstyle=rng.choice(["sv", "en", "plain"] if lang == "en" else ["sv", "sv", "plain"]),
                datestyle=rng.randint(0, 4) if lang == "en" else rng.choice([0, 1, 1, 2, 4]), layout=rng.randint(0, 7))


def truth_row(inv, fn, fmt):
    s = inv["sup"]
    return {"file": fn, "format": fmt, "layout": inv["layout"], "kind": inv["kind"], "supplier": s[0], "invoice_number": inv["number"],
            "invoice_date": inv["date"].isoformat(), "due_date": inv["due"].isoformat(), "currency": s[3],
            "net": dec(inv["net"]), "vat_amount": dec(inv["vat"]), "total": dec(inv["total"]),
            "expected_account": s[5], "new_supplier": s[12]}


# ---------- PDF with a text layer ----------
def render_pdf(path: Path, inv):
    s, L = inv["sup"], inv["lbl"]
    name, addr, cty, cur, rates, _m, _a, idline, lang, _lo, _hi, taxid, _new = s
    lay = inv["layout"]
    ns = inv["numstyle"]
    dsty = inv["datestyle"]
    ink = [colors.HexColor(c) for c in ("#0a3c64", "#222222", "#7a1f1f", "#1f5f3a")][lay % 4]
    st = lambda size=9, bold=False, align=0: ParagraphStyle("x", fontName="DejaVu-Bold" if bold else "DejaVu", fontSize=size, leading=size + 3, alignment=align)
    money = lambda v: f"{num(v, ns)} {cur}"
    story = []
    head = [Paragraph(f"<b>{escape(name)}</b>", st(15, True)), Paragraph(escape(addr), st()), Paragraph(escape(idline), st()), Paragraph(escape(taxid), st())]
    title = L["credit"] if inv["kind"] == "credit" else L["title"]
    meta = [[f"{L['no']}:", inv["number"]], [f"{L['date']}:", dt(inv["date"], dsty)], [f"{L['due']}:", dt(inv["due"], dsty)], [f"{L['order']}:", inv["order"]], [f"{L['ref']}:", inv["ref"]]]
    cust = [Paragraph(f"<b>{L['cust']}</b>", st(8, True)), Paragraph(CUSTOMER[0], st()), Paragraph(CUSTOMER[1], st()), Paragraph(f"{CUSTOMER[2]} · {inv['custno']}", st())]
    mt = Table(meta, colWidths=[32 * mm, 38 * mm])
    mt.setStyle(TableStyle([("FONT", (0, 0), (-1, -1), "DejaVu", 9)]))
    if lay % 2 == 0:
        top = Table([[head, [Paragraph(title.upper(), st(18, True, 2)), mt]]], colWidths=[95 * mm, 80 * mm])
    else:
        top = Table([[[Paragraph(title.upper(), st(18, True))] + head, mt]], colWidths=[95 * mm, 80 * mm])
    story += [top, Spacer(1, 6 * mm), Table([[cust]], colWidths=[90 * mm]), Spacer(1, 6 * mm)]
    wide = lay in (2, 3, 6)
    hdr = [L["desc"], L["qty"], L["unit"], L["rate"], L["amt"]] if wide else [L["desc"], L["amt"]]
    rows = [hdr]
    for ln in inv["lines"]:
        rows.append([ln["desc"], str(ln["qty"]), money(ln["unit"]), f"{ln['rate']}%", money(ln["net"])] if wide else [ln["desc"], money(ln["net"])])
    cw = [70 * mm, 14 * mm, 28 * mm, 14 * mm, 30 * mm] if wide else [120 * mm, 40 * mm]
    t = Table(rows, colWidths=cw, repeatRows=1)
    t.setStyle(TableStyle([("FONT", (0, 0), (-1, -1), "DejaVu", 8.5), ("FONT", (0, 0), (-1, 0), "DejaVu-Bold", 8.5), ("LINEBELOW", (0, 0), (-1, 0), 0.8, ink),
                           ("ALIGN", (1 if not wide else 1, 0), (-1, -1), "RIGHT"), ("TEXTCOLOR", (0, 0), (-1, 0), ink)]))
    story += [t, Spacer(1, 5 * mm)]
    tot = [[L["net"], money(inv["net"])]]
    for r, (b, v) in sorted(inv["by_rate"].items()):
        tot.append([f"{L['vat']} {r}%" + (f" ({money(b)})" if len(inv["by_rate"]) > 1 else ""), money(v)])
    if inv["rounding"]:
        tot.append([L["rnd"], money(inv["rounding"])])
    tot.append([L["total"], money(inv["total"])])
    tt = Table(tot, colWidths=[90 * mm, 40 * mm], hAlign="RIGHT")
    tt.setStyle(TableStyle([("FONT", (0, 0), (-1, -1), "DejaVu", 9), ("FONT", (0, -1), (-1, -1), "DejaVu-Bold", 11), ("ALIGN", (1, 0), (1, -1), "RIGHT"), ("LINEABOVE", (0, -1), (-1, -1), 0.8, ink)]))
    story += [tt, Spacer(1, 8 * mm)]
    foot = [L["terms"], BANK.get(s[2], ""), f"{L['order']} {inv['order']}"]
    if cur != "SEK" and rates == [0]:
        foot.append("Reverse charge / Omvänd skattskyldighet")
    story += [Paragraph(escape(" · ".join(f for f in foot if f)), st(8))]
    SimpleDocTemplate(str(path), pagesize=A4, leftMargin=18 * mm, rightMargin=18 * mm, topMargin=16 * mm, bottomMargin=16 * mm, title=f"{title} {inv['number']}", author=name).build(story)


# ---------- image-only PDF (a scan) ----------
def render_scan(path: Path, inv):
    from PIL import Image
    from reportlab.lib.utils import ImageReader
    from reportlab.pdfgen import canvas

    s = inv["sup"]
    sup_tuple = (s[0], s[1], s[2], s[3], s[4][0] if s[4] else 0, s[5], s[6], s[7], s[8], inv["layout"] % 4, 0, 0)
    one = dict(number=inv["number"], date=inv["date"], due=inv["due"], lines=[(ln["desc"], ln["net"]) for ln in inv["lines"]], net=inv["net"], vat=inv["vat"], total=inv["total"])
    png = path.with_name(path.stem + ".tmp.png")
    synth.render(png, sup_tuple, one)
    img = Image.open(png).convert("RGB")
    img = img.rotate(random.Random(inv["number"]).uniform(-0.6, 0.6), expand=True, fillcolor="white")
    c = canvas.Canvas(str(path), pagesize=A4, invariant=1)
    c.drawImage(ImageReader(img), 0, 0, width=A4[0], height=A4[1])
    c.save()
    png.unlink()


# ---------- DOCX ----------
def render_docx(path: Path, inv):
    s, L = inv["sup"], inv["lbl"]
    name, addr, _cty, cur, rates, _m, _a, idline, _lang, *_ = s
    ns, dsty = inv["numstyle"], inv["datestyle"]
    money = lambda v: f"{num(v, ns)} {cur}"
    d = Document()
    cp = d.core_properties
    cp.author, cp.created, cp.modified, cp.last_modified_by = name, datetime(2026, 11, 1), datetime(2026, 11, 1), name
    d.add_heading(L["credit"] if inv["kind"] == "credit" else L["title"], 0)
    p = d.add_paragraph()
    p.add_run(name).bold = True
    p.add_run(f"\n{addr}\n{idline}")
    if inv["layout"] % 2:
        mt = d.add_table(rows=0, cols=2)
        for k, v in ((L["no"], inv["number"]), (L["date"], dt(inv["date"], dsty)), (L["due"], dt(inv["due"], dsty)), (L["order"], inv["order"])):
            r = mt.add_row().cells
            r[0].text, r[1].text = k, v
    else:
        d.add_paragraph(f"{L['no']}: {inv['number']}\n{L['date']}: {dt(inv['date'], dsty)}\n{L['due']}: {dt(inv['due'], dsty)}\n{L['order']}: {inv['order']}")
    d.add_paragraph(f"{L['cust']}: {CUSTOMER[0]}, {CUSTOMER[1]} ({inv['custno']})")
    t = d.add_table(rows=1, cols=4)
    t.style = "Table Grid"
    for i, h in enumerate((L["desc"], L["qty"], L["unit"], L["amt"])):
        t.rows[0].cells[i].text = h
    for ln in inv["lines"]:
        r = t.add_row().cells
        r[0].text, r[1].text, r[2].text, r[3].text = ln["desc"], str(ln["qty"]), money(ln["unit"]), money(ln["net"])
    d.add_paragraph()
    d.add_paragraph(f"{L['net']}: {money(inv['net'])}")
    for r, (_b, v) in sorted(inv["by_rate"].items()):
        d.add_paragraph(f"{L['vat']} {r}%: {money(v)}")
    if inv["rounding"]:
        d.add_paragraph(f"{L['rnd']}: {money(inv['rounding'])}")
    d.add_paragraph().add_run(f"{L['total']}: {money(inv['total'])}").bold = True
    d.add_paragraph(f"{L['terms']} · {BANK.get(s[2], '')}")
    d.save(str(path))


# ---------- Peppol BIS Billing 3.0 UBL ----------
def render_ubl(path: Path, inv):
    s = inv["sup"]
    name, addr, cty, cur, _r, _m, _a, idline, *_ , taxid, _new = s
    credit = inv["kind"] == "credit"
    root = "CreditNote" if credit else "Invoice"
    ns = "urn:oasis:names:specification:ubl:schema:xsd:" + root + "-2"
    a = lambda v: f"{abs(v) // 100}.{abs(v) % 100:02d}"
    # In UBL amounts are positive; the document type says whether it is a credit note.
    out = [f'<?xml version="1.0" encoding="UTF-8"?>\n<{root} xmlns="{ns}" xmlns:cac="urn:oasis:names:specification:ubl:schema:xsd:CommonAggregateComponents-2" xmlns:cbc="urn:oasis:names:specification:ubl:schema:xsd:CommonBasicComponents-2">',
           "<cbc:CustomizationID>urn:cen.eu:en16931:2017#compliant#urn:fdc:peppol.eu:2017:poid:billing:3.0</cbc:CustomizationID>",
           "<cbc:ProfileID>urn:fdc:peppol.eu:2017:poid:billing:01:1.0</cbc:ProfileID>",
           f"<cbc:ID>{escape(inv['number'])}</cbc:ID><cbc:IssueDate>{inv['date'].isoformat()}</cbc:IssueDate>"]
    if not credit:
        out.append(f"<cbc:DueDate>{inv['due'].isoformat()}</cbc:DueDate>")
    out.append(f"<cbc:{'CreditNoteTypeCode>381</cbc:CreditNoteTypeCode>' if credit else 'InvoiceTypeCode>380</cbc:InvoiceTypeCode>'}")
    out.append(f"<cbc:DocumentCurrencyCode>{cur}</cbc:DocumentCurrencyCode><cbc:BuyerReference>{escape(inv['order'])}</cbc:BuyerReference>")
    out.append(f'<cac:AccountingSupplierParty><cac:Party><cac:PartyName><cbc:Name>{escape(name)}</cbc:Name></cac:PartyName><cac:PostalAddress><cbc:StreetName>{escape(addr)}</cbc:StreetName><cac:Country><cbc:IdentificationCode>{cty}</cbc:IdentificationCode></cac:Country></cac:PostalAddress>'
               f'<cac:PartyTaxScheme><cbc:CompanyID>{escape(taxid)}</cbc:CompanyID><cac:TaxScheme><cbc:ID>VAT</cbc:ID></cac:TaxScheme></cac:PartyTaxScheme><cac:PartyLegalEntity><cbc:RegistrationName>{escape(name)}</cbc:RegistrationName></cac:PartyLegalEntity></cac:Party></cac:AccountingSupplierParty>')
    out.append(f'<cac:AccountingCustomerParty><cac:Party><cac:PartyName><cbc:Name>{escape(CUSTOMER[0])}</cbc:Name></cac:PartyName><cac:PartyTaxScheme><cbc:CompanyID>{CUSTOMER[3]}</cbc:CompanyID><cac:TaxScheme><cbc:ID>VAT</cbc:ID></cac:TaxScheme></cac:PartyTaxScheme><cac:PartyLegalEntity><cbc:RegistrationName>{escape(CUSTOMER[0])}</cbc:RegistrationName></cac:PartyLegalEntity></cac:Party></cac:AccountingCustomerParty>')
    out.append(f'<cac:TaxTotal><cbc:TaxAmount currencyID="{cur}">{a(inv["vat"])}</cbc:TaxAmount>')
    for r, (b, v) in sorted(inv["by_rate"].items()):
        out.append(f'<cac:TaxSubtotal><cbc:TaxableAmount currencyID="{cur}">{a(b)}</cbc:TaxableAmount><cbc:TaxAmount currencyID="{cur}">{a(v)}</cbc:TaxAmount><cac:TaxCategory><cbc:ID>{"S" if r else "AE"}</cbc:ID><cbc:Percent>{r}</cbc:Percent><cac:TaxScheme><cbc:ID>VAT</cbc:ID></cac:TaxScheme></cac:TaxCategory></cac:TaxSubtotal>')
    out.append("</cac:TaxTotal>")
    tot = f'<cac:LegalMonetaryTotal><cbc:LineExtensionAmount currencyID="{cur}">{a(inv["net"])}</cbc:LineExtensionAmount><cbc:TaxExclusiveAmount currencyID="{cur}">{a(inv["net"])}</cbc:TaxExclusiveAmount><cbc:TaxInclusiveAmount currencyID="{cur}">{a(inv["net"] + inv["vat"])}</cbc:TaxInclusiveAmount>'
    if inv["rounding"]:
        tot += f'<cbc:PayableRoundingAmount currencyID="{cur}">{dec(inv["rounding"] * (-1 if credit else 1))}</cbc:PayableRoundingAmount>'
    tot += f'<cbc:PayableAmount currencyID="{cur}">{a(inv["total"])}</cbc:PayableAmount></cac:LegalMonetaryTotal>'
    out.append(tot)
    tag = "CreditNoteLine" if credit else "InvoiceLine"
    qtag = "CreditedQuantity" if credit else "InvoicedQuantity"
    for i, ln in enumerate(inv["lines"], 1):
        out.append(f'<cac:{tag}><cbc:ID>{i}</cbc:ID><cbc:{qtag} unitCode="C62">{ln["qty"]}</cbc:{qtag}><cbc:LineExtensionAmount currencyID="{cur}">{a(ln["net"])}</cbc:LineExtensionAmount><cac:Item><cbc:Name>{escape(ln["desc"])}</cbc:Name><cac:ClassifiedTaxCategory><cbc:ID>{"S" if ln["rate"] else "AE"}</cbc:ID><cbc:Percent>{ln["rate"]}</cbc:Percent><cac:TaxScheme><cbc:ID>VAT</cbc:ID></cac:TaxScheme></cac:ClassifiedTaxCategory></cac:Item><cac:Price><cbc:PriceAmount currencyID="{cur}">{a(ln["unit"])}</cbc:PriceAmount></cac:Price></cac:{tag}>')
    out.append(f"</{root}>")
    path.write_text("".join(out), encoding="utf-8")


PLAN = [("pdf", 60, render_pdf), ("scan.pdf", 20, render_scan), ("docx", 40, render_docx), ("xml", 40, render_ubl)]


def main():
    out = Path(sys.argv[1] if len(sys.argv) > 1 else "testdata/variety")
    seed = int(sys.argv[2]) if len(sys.argv) > 2 else 20261125
    rng = random.Random(seed)
    (out / "files").mkdir(parents=True, exist_ok=True)
    truth, idx = [], 1
    for ext, count, fn in PLAN:
        for k in range(count):
            sup = SUPS[rng.randrange(len(SUPS))]
            kind = "credit" if (rng.random() < 0.1 and ext != "scan.pdf") else "invoice"
            inv = build(rng, sup, idx, kind, multipage=(k % 9 == 4 and ext in ("pdf", "docx")))
            name = f"files/{idx:03d}.{ext}"
            fn(out / name, inv)
            row = truth_row(inv, name, {"pdf": "pdf_text", "scan.pdf": "pdf_scan", "docx": "docx", "xml": "ubl"}[ext])
            truth.append(row)
            idx += 1
    (out / "truth.json").write_text(json.dumps(truth, ensure_ascii=False, indent=1) + "\n")
    kinds = {}
    for r in truth:
        kinds[r["format"]] = kinds.get(r["format"], 0) + 1
    print(f"wrote {len(truth)} documents to {out}: {kinds}")


if __name__ == "__main__":
    main()
