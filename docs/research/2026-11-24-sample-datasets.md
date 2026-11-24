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

## A defect this found

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

1. Port the probe into `cmd/extract-eval` as a second dataset adapter, so the numbers
   come from the committed tool and not a throwaway script.
2. Run FATURA2 and the SROIE receipts for layout and scan-noise variety.
3. A few real Swedish invoices are still the number that matters.
