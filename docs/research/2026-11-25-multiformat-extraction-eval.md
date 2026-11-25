# Multi-format extraction, measured on the generated set (slice #160)

Date 2026-11-25. Tool: `cmd/extract-eval -dir <set> -formats ...`. Model for images and text:
`qwen2.5vl:7b` on iguana, temperature 0. Converter: `markitdown[docx,pdf]` through `uvx`.
Data: `scripts/gen-variety.py`, fully fictional, 160 documents per seed
(60 text PDF, 20 scanned PDF, 40 DOCX, 40 Peppol UBL). **These numbers describe a set we generated
ourselves, rendered by one library. They say nothing about real invoices from real suppliers**;
`2026-11-25-invoice-datasets-round-2.md` lists where real layouts could come from.

## Result, original text prompt (seed 20261125, 120 model-read files)

| Format | Files | Read | Refused | Fields right | Reconciled | Mean time |
|---|---:|---:|---:|---|---|---:|
| UBL XML (parser, no model) | 40 | 40 | 0 | 100.0% (278/278) | 40/40 | 0.0 s |
| DOCX | 40 | 40 | 0 | 100.0% (280/280) | 40/40 | 2.0 s |
| PDF with text layer | 60 | 55 | 0 | 97.7% (376/385) | 51/55 | 2.1 s |
| Scanned PDF (no text layer) | 20 | 0 | 20 | n/a | n/a | 0.4 s |

- **Scans: all 20 refused with the "scan, no text layer" message and none guessed at.** That is the
  intended behaviour, not a failure. A scan is not read at all in this slice.
- **Text PDF, the 5 files that did not parse:** each failed on `invoice_date`, the model returning a
  label cell (`Förfaller: 2026-11-15`, `Datum:`) instead of a date. They are layouts 1, 3, 5 and 7,
  where the header sits in a side-by-side table that `markitdown` flattens so a label and its value land
  on separate lines.
- **Text PDF, wrong fields among the 55 read (9 of 385):** 4 totals and 5 VAT amounts, on invoices with a
  rounding line (öresavrundning). The model returned the unrounded sum as the total, or took a rounding
  amount for VAT.
- **The net + VAT = total check caught 1 of 4 wrong totals.** The others are inside the 0.50 tolerance
  because the error is the rounding itself, so the check cannot see them. The consequence in an entry is
  a credit to 2440 that differs from the amount actually payable by under 0.50, which the entry builder
  then books to 3740. Small, but it is a real deviation from what the invoice says.
- Correct totals wrongly flagged: 3 of 91.

## An experiment that did not work

A text prompt that told the model to copy amounts, treat a rounding line as not VAT, take the total
after rounding, match each label to its own value and read the issue date, never the due date.

| Run | Read | Fields right | Reconciled |
|---|---:|---|---|
| original prompt, seed 20261125 | 55/60 | 97.7% (376/385) | 51/55 |
| tightened prompt, seed 20261125 | 54/60 | 97.4% (368/378) | 50/54 |
| tightened prompt, holdout seed 424242 | 53/60 | 97.0% (360/371) | 48/53 |

No gain on the set it was written against, so it was **reverted**, with its test. The holdout was run
with the tightened prompt only; the original prompt was not run on it, so the holdout row does not
isolate the prompt's effect. The honest reading: the failures come from the converter's table flattening
and from the model recomputing, and a longer instruction does not move either.

On the holdout, DOCX was 99.6% (279/280, 39/40 reconciled) and UBL 100% (274/274), as on the first seed.

## What would plausibly help (not tried)

1. **Convert with the layout kept** (poppler `pdftotext -layout`, or PyMuPDF), so a label stays next to
   its value. Not installed on this host; sudo is denied, so it needs an environment change or a Python
   dependency via `uv`.
2. **A second source for PDFs:** rasterise the page and read it with the vision model, and refuse when
   the two readings disagree. This is design principle 5 (every number cross-checked by a second source)
   and costs a model call per page.
3. **Read the rounding line deterministically** from the converted text and apply it in code instead of
   asking the model for the rounded total.

None of these is claimed to work. Each needs its own measurement.

## Reproduce

```
uv run scripts/gen-variety.py /tmp/variety 20261125
uv run scripts/check-variety.py
go run ./cmd/extract-eval -dir /tmp/variety -formats ubl,docx,pdf_text,pdf_scan
```
