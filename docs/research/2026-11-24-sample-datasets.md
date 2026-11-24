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

### Failure mode 2: wrong totals (CORRECTED, see the re-run below)

The first run's totals were 90.5% (67/74). I reported three plausible-looking
misreads: a dropped digit (157929.37 for 1579929.37), transposed digits (8095.77
for 8905.77) and a different figure (286.02 for 314.62). **The first of those was
wrong.** Looking at the image (invoice 014), the printed total is "$ 157 929,37",
exactly what the model read; the dataset's own ground truth, 1579929.37, has an
extra digit. It is a label error, not a model error. The other two were real misreads
under the first prompt.

## Re-run after the #146 fixes (dates as printed, net total read, net + VAT = total)

Same 74 invoices, same model, temperature 0, mean 12.3 s per image, date order set
to month-first because this dataset is American.

| Field | First run | After |
|---|---|---|
| supplier | 97.3% | 97.3% (72/74) |
| invoice number | 100% | 100% |
| invoice date | 79.7% | 100% (74/74) |
| total | 90.5% | 98.6% (73/74); the one miss is the label error above, so effectively 100% |
| VAT amount | 100% | 100% |
| all labelled fields | 94.6% | 99.3% (438/441) |

What this does and does not show:

- **The date result depends on the order setting being right.** 32 of 74 dates were
  ambiguous (both parts 12 or below, different). Under the wrong setting each would
  be read wrongly and silently; the flag is what catches that. I did not run the
  wrong setting, so the size of the damage is an inference, not a measurement.
- **The totals improvement cannot be credited to the cross-check.** The prompt changed
  in three ways at once (dates as printed, a net total asked for, "including VAT"
  added to the total), and only the prompt-plus-code was run. Two earlier misreads
  stopped reproducing; the cause is unknown.
- **The cross-check itself was exercised on zero genuine wrong totals.** It reports
  0 of 1 caught, and that one is the label error, where net + VAT equals the printed
  total, so nothing was wrong to catch. It flagged 0 of 73 correct totals falsely.
  Its ability to catch a real misread rests on unit tests with constructed cases only.
- Supplier misses (2) are unchanged; I have not looked at them.
- One model, one run, one dataset whose content is generated.

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
