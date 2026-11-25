# Real-layout demo samples (2026-11-25)

Mathias saw the demo page and said the invoices "all look the same". The generated set (`scripts/gen-variety.py`)
uses one renderer, so they do. This note records the third-party samples tried instead.

## What was installed (samples 201-210, minus two)

Source: **FATURA2**, `mathieu1256/FATURA2-invoices` on Hugging Face, **CC-BY-4.0**. Attribution: Limam et al.,
"FATURA: A Multi-Layout Invoice Image Dataset for Document Analysis and Understanding", 2023, arXiv 2311.11856
(data on Zenodo, record 10371464). 10 000 JPEG invoices from 50 templates; names, addresses and amounts are
generated. English, USD or EUR, no Swedish content, and the dates are random (1990s to 2020s).

Ten images with visibly different templates were taken from the test split (ids 120, 200, 260, 330, 400, 470, 820,
1030, 1240, 1380), converted to PNG and installed as `201`-`210`. The demo host's `history.json` got 39
**synthetic** past bookings (series C) for the five supplier names they contain, so a booking can be proposed;
the real supplier names are not in any real books. The demo page does not say the history is synthetic: say so when presenting.

## Results through the live demo (qwen2.5vl:7b)

| Sample | Outcome |
|---|---|
| 201, 204, 205, 207, 209, 210 | Read correctly (supplier, number, date, net, VAT, total); booking proposed |
| 203 | Invoice number read as a date; flagged "do not trust" (guard worked) |
| 208 | A discount line read as VAT; flagged "do not trust" |
| 202 | No VAT line, model returned an empty `vat_amount`, request refused (502). Kept strict on purpose |
| 206 | **Wrong and not flagged**: a discount (17.54) read as VAT, total invented as 1567.78 (printed 1550.24). Moved out of the demo set |

Six of ten clean, two flagged, one refused, one silently wrong. That is a worse rate than on the generated set and it
is the honest one for unfamiliar layouts. **Open problem:** a discount row can be read as VAT and the reconciliation
(net + VAT = total) still passes when the model also invents the total. Tracked in the issue filed with this note.

## Found by running them

`09-Feb-2000` (day, month name, year, with dashes) was refused as "not a date". Fixed in `internal/extract/date.go`
with tests; this format is common on real invoices.

## Tried and rejected

`bks-lab/xrechnung-invoice-fields` (Apache-2.0), PDFs `01.01a` `01.03a` `01.06a` `01.09a`: official XRechnung test-suite
documents with placeholder content (`[Seller name]`) and multiple pages, so they are not useful as demo material. Their XML is
**CII**, which we do not parse (#161). Not installed.

Samples that were moved out of the live set are kept on the demo host in `~/invoice-demo-data/invoices-held-out/`.

## Update: second read of the total (#167)

The broad prompt reads a discount as VAT and can invent a matching total. A narrow second question ("what is the
final amount to pay, as printed") gave the printed total on every sample tried. `Client.Extract` now asks it after
the first read and, if the two totals differ or the second cannot be parsed, the invoice is not reconciled: it is
flagged and no entry is proposed. Cost: one more model call per image (2-4 s).

Re-run on the live demo: 206 (the silent failure) is now flagged. 204 turned out to be wrong too: the invoice prints
TOTAL 706.20, the first read said 748.48, and I had counted it correct by mistake; it is now flagged as well. 201, 205,
207 and 210 are unchanged. The text path (PDF, DOCX) gets the same second read, with the document fenced and a fresh fence token; seven
text samples re-run with no change. Not covered: 202 (no VAT line) is still refused.
