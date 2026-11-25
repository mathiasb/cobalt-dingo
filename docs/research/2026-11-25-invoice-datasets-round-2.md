# Invoice datasets, round 2: PDF, e-invoice XML and real layouts (slice #160)

Searched 2026-11-25: Hugging Face hub (repo search and card metadata), Kaggle (via web search, no
login), Zenodo, GitHub and the e-invoicing standards bodies. Round 1 is
`2026-11-24-sample-datasets.md` (katanaml, FATURA2, SROIE, mychen76, synthetic receipts); this adds
what that round did not cover: formats other than images, e-invoice XML, Swedish material, and sets
built to benchmark extraction.

**What was and was not verified.** `[V]` = read from the hub's listing or card by a tool today.
`[W]` = from a web search snippet only. Nothing was downloaded, no licence text was read in full, and
no document was opened. Kaggle needs a login, so Kaggle entries are `[W]`. Licences in this table are
what the uploader declared; for a mirror of third-party documents that is a claim, not clearance.

**The finding that matters.** No public set has all three of: Swedish suppliers, real layouts, and a
booked BAS account per invoice. Extraction can be tested on public data. The booking proposer cannot,
and only a real company's history can (spike 1, #155). That has not changed since round 1.

## Ranked for what we are building

| # | Source | What | Why it is useful here | Licence (declared) | Caution |
|---|---|---|---|---|---|
| 1 | **Peppol BIS Billing 3.0 official examples** `[W]` docs.peppol.eu/poacc/billing/3.0/bis (zip) | The reference UBL invoices and credit notes | Exactly our UBL parser's input, and the standard Sweden uses (BIS Billing 3 is the Swedish CIUS) | check on the page | Tiny; not layout variety |
| 2 | **SKR Swedish BIS Billing 3 example** `[W]` extra.skr.se (Räntefaktura / Samlingsfaktura XML) | A Swedish UBL file | The only Swedish e-invoice found | check on the page | One file; collective and interest invoices are unusual shapes |
| 3 | **KoSIT XRechnung testsuite** `[W]` github.com/itplr-kosit/xrechnung-testsuite | Official German e-invoices in **UBL and CII** | Many edge cases, credit notes, mixed VAT; free; both syntaxes | not found in the snippet: read the repo's LICENSE | German content |
| 4 | **bks-lab/xrechnung-invoice-fields** `[V]` | 34 of those XRechnung invoices rendered to 118 page images, 12 gold fields each, gold taken from the XML | The same invoice as XML **and** as page images: measures the vision path against exact truth, and cross-checks image vs parser | Apache-2.0 | 34 invoices; German |
| 5 | **DocILE (Rossum)** `[W]` github.com/rossumai/docile | 6,680 manually annotated **real business documents** (PDF) from public sources, 55 field classes, line items; 100k synthetic; ~1M unlabelled | The best real-layout source; real PDFs with text layers and scans | needs a registration token from docile.rossum.ai; dataset terms not confirmed (the repo's MIT is for code) | Registering shares an email with a vendor: Mathias's call. Read the terms first |
| 6 | **sinabis ZUGFeRD / XRechnung 1k, 10k, 100k, 1M** `[V]` | Machine-generated German e-invoices, XML and documents | Scale for the XML path and a PDF with embedded XML | CC-BY-4.0 | Synthetic, probably one fictional issuer (not checked): volume, not variety |
| 7 | **drew-ipp/invoice-extraction-benchmark** `[V]` (GitHub canonical: DrewKraken/invoice-extraction-benchmark) | 181 synthetic documents with answer keys and a scorer | Built as an extraction benchmark; documents, not only images | CC-BY-4.0 | Synthetic English |
| 8 | **hyperbots-inc/synthetic-invoice-extraction** `[V]` | 200 synthetic invoices as HTML, OCR-style text and gold labels | Tests the **text path** without a converter | CC-BY-4.0 | English; HTML not PDF |
| 9 | **osolmaz/invoice-fixture** `[V]` | 17 German PDFs and images of one invented person, one scan with no text layer | A small realistic folder incl. a scan; good for the refusal path | not declared | 17 files |
| 10 | **jngb-labs/InvoiceBenchmark** `[V]` | 200 synthetic invoices with cent-perfect truth, built to test reading numbers | Stress test for amounts and the net+VAT=total check | MIT | English |
| 11 | **Kaggle** `[W]`: devp1866 *High-Quality OCR-Ready Invoice PDFs* (100 synthetic PDFs with JSON); miadul *Multi-Document Dataset* (PDFs, images, labels, text); senju14 *Invoice-OCR*; ryanznie *SROIE v2 with labels* (973 receipts); nikitpatel *Invoice NER* (text only) | Mostly synthetic or receipts | PDF-with-truth sets to try the text path | not confirmed | Indian and generic templates; login needed |
| 12 | **Voxel51/high-quality-invoice-images-for-ocr** `[V]` | 8,181 synthetic invoice images | Volume for the vision path | ODbL | Synthetic English images |
| 13 | **FATURA2 / mathieu1256** `[V]` | 10,000 invoices, 50 templates | Template variety | CC-BY-4.0 | Tag-to-field mapping needed (round 1) |
| 14 | **chainyo/rvl-cdip-invoice** `[V]` | Invoices from RVL-CDIP, grayscale scans | Real scan noise | "other" | No field labels; old US documents |

## Use with care or not at all

- **AlroWilde/invoice-checkmark-annotations** `[V]` declares ~200 **real Swedish** invoices (plus
  Ukrainian and Chinese), Apache-2.0. It is the only real Swedish material found. But: they are real
  third-party business documents of unknown origin; an uploader's licence cannot clear the rights or
  the personal data of the people and companies on them; the hub's viewer fails to build the dataset, so
  its content was not inspected; and the annotation is a human checkmark drawn on the image, not field
  labels. Do not put it in the demo or in the repo. If it is looked at, do it locally, once, for reading
  quality only, after reading the card.
- **laterrr/belege-de-invoices-sample** `[V]`: 40 German synthetic invoices and credit notes, licence
  "other", the full set sold. Skip.
- **Round-1 reminder:** `mychen76/invoices-and-receipts_ocr_v1` has no stated licence.
- Non-Latin sets (Arabic, Persian, Thai, Japanese, Vietnamese, Russian, Indian GST) are irrelevant to a
  Swedish buyer.

## What this changes

1. **We parse only UBL.** XRechnung and ZUGFeRD / Factur-X are very often **CII** (UN/CEFACT Cross
   Industry Invoice), and a ZUGFeRD PDF carries a CII file inside it. A CII parser, and reading the
   attachment out of a PDF, are the highest-value format gaps found. Tracked as a follow-up to #160.
2. **Cheap and official now:** items 1 to 4 cost an afternoon, need no registration, and test the e-invoice
   path and the vision path against exact truth. Item 4 is the one that lets us check vision against
   parser on the same invoice.
3. **The real-layout gap** (PDF as people actually produce them) is DocILE, behind a registration and a
   terms check that are Mathias's to decide. Until then the text-PDF numbers rest on our own generator.
4. **Swedish realism** is still not available from the public web. Real Swedish invoices from the
   company's own mailbox or supplier-invoice archive are what remains (spike 2, #155).

## Not done

No dataset was downloaded. No licence was read in full. Kaggle was not logged into. DocILE and the
XRechnung testsuite were not opened. The `sinabis` issuer count and the `osolmaz` licence are unchecked.
