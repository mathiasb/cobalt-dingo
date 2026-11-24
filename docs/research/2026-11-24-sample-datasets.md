# Public invoice and receipt datasets for the invoice processor (bet #142)

Found 2026-11-24 through the Hugging Face hub. Public data only: no client or
prospect document is involved, so a local model is a rule, not a legal need.
Images are NOT committed; fetch them into a scratch directory.

## Candidates

| Dataset | What | Size | License | Fit |
|---|---|---|---|---|
| `katanaml-org/invoices-donut-data-v1` | English invoices with header, line items, totals as JSON ground truth. Derived from Mendeley Data "Samples of electronic invoices" (Kozłowski and Weichbroth, 2021) | 501 (26 test) | MIT | **Best for extraction.** Real layouts, generated content, US dollars, MM/DD/YYYY. |
| `mathieu1256/FATURA2-invoices` | 10,000 invoices from 50 templates plus coloured variants, field annotations (arXiv 2311.11856) | 10k+ | CC-BY-4.0 | Layout variety at scale. Not inspected yet. |
| `Voxel51/scanned_receipts` and `Voxel51/consolidated_receipt_dataset` | Scanned receipts (SROIE, arXiv 2103.10213) with company, date, address, total | 712 / 800 | CC-BY-4.0 | Receipts with real scan noise. FiftyOne format, not inspected yet. |
| `mychen76/invoices-and-receipts_ocr_v1` | Mixed invoices and receipts with OCR | 1k-10k | not stated | Check the licence before any use. |
| `albertobarnabo/synthetic-receipts-ocr` | Thermal receipts, clean and photo-degraded twins, US/UK/DE/IT/FR | 32k | Apache-2.0 | Tests degradation; no Swedish. |

Not Swedish and not labelled with a booking account: none of these can test the
proposer. They test extraction only. Account accuracy still needs real history.

## First probe on real-layout invoices (not the synthetic set)

`katanaml-org/invoices-donut-data-v1`, test split, first 10 invoices, `qwen2.5vl:7b`
on iguana, temperature 0, mean 10.6 s per image. Compared against the dataset's
ground truth.

| Field | Correct |
|---|---|
| supplier (name prefix found in the ground-truth seller) | 10/10 |
| invoice number | 10/10 |
| invoice date | 9/10 |
| total (gross) | 10/10 |
| VAT amount | 10/10 |

Small sample, one model, loose supplier match, content generated rather than real.
It says the model reads real layouts well; it does not say 98% in production.

## Full run: 74 real-layout invoices (committed tool, not a script)

`go run ./cmd/extract-eval -dir <prepared dir>` on katanaml test plus validation
(76 rows, 2 skipped for a different annotation shape), `qwen2.5vl:7b` on iguana,
temperature 0, mean 10.4 s per image. All 74 images were read; none failed to parse.

| Field | Correct |
|---|---|
| supplier (word-boundary prefix of seller) | 97.3% (72/74) |
| invoice number | 100% (74/74) |
| **invoice date** | **79.7% (59/74)** |
| currency | 100% (71/71) |
| **total (gross)** | **90.5% (67/74)** |
| VAT amount | 100% (74/74) |
| all labelled fields | 94.6% (417/441) |

The earlier 10-image probe (98%) was optimistic: 10 images was too few and the
first ten happened to be easy.

### Failure mode 1: day/month order (all date misses checked)

The dataset writes dates MM/DD/YYYY. For a date such as `06/02/2015` the model
answers 2015-02-06 (6 February); the truth is 2 June. Every date miss I inspected
(6 of 15) was a swap, and only dates with both numbers 12 or below can swap. This
is an ambiguity in the document, not a reading error, and it matters here: Swedish
invoices mostly use ISO dates, but invoices from US suppliers do not, and a swapped
date puts a booking in the wrong period. Fix direction: ask for the date as printed,
normalise in code with an explicit locale, and flag any date that is ambiguous.

### Failure mode 2: wrong totals that look plausible (3 of 6 totals inspected)

- dropped digit: 157929.37 read for 1579929.37
- transposed digits: 8095.77 read for 8905.77
- a different figure: 286.02 read for 314.62

A wrong total posted to the books is worse than a refusal. Fix direction: read net,
VAT and gross and the line sum, and require net + VAT = gross; where that fails,
mark the proposal "do not trust" instead of showing it. That check costs nothing
and would catch all three here only if the model's other amounts are right, which
is untested.

## A defect this found earlier

All 10 invoices state no due date. `extract.Client` required one and would have
rejected every invoice. Fixed test-first: the due date is optional, and a due date
that is present must still be a valid date.

## Reproduce

```
curl -L -o test.parquet \
  "https://huggingface.co/datasets/katanaml-org/invoices-donut-data-v1/resolve/refs%2Fconvert%2Fparquet/default/test/0000.parquet"
uv run --with pyarrow python   # read the `image` and `ground_truth` columns
```

## Next

1. ~~Port the probe into `cmd/extract-eval`~~ done: `scripts/prep-dataset.py` plus the
   committed `cmd/extract-eval`.
2. FATURA2 needs a tag-id to field mapping (it ships per-word tags, not field values).
   The Voxel51 SROIE parquet has images only, no labels; it would have to come through
   FiftyOne. Neither has been run.
3. A few real Swedish invoices are still the number that matters.
