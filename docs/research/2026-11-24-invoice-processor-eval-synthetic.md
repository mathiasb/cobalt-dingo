# Invoice processor: first evaluation on synthetic data (bet #142)

Date: 2026-11-24. Tracker: #142 (bet), #144 (proposer), #145 (extraction).
Reproduce: `uv run scripts/gen-synthetic-invoices.py && go run ./cmd/extract-eval`

## What was measured

| Stage | Result |
|---|---|
| Held-out booking proposal, 146 synthetic past bookings, latest 20% by date | top-1 96.6% (28/29), top-3 100% (29/29), 0 unseen suppliers |
| Field extraction, `qwen2.5vl:7b` on iguana (Ollama), 36 images | 100% on each of 7 fields (252/252), mean 3.4 s per image |
| End to end: image to supplier read to proposed account vs account booked | top-1 100% (36/36), top-3 100% (36/36), 0 unseen suppliers |

## What this does NOT show

- **The data is synthetic and clean.** Twelve fictional suppliers, four template
  layouts, rendered text, no scans, skew, stamps, handwriting or multi-page
  documents. 100% field accuracy is a statement about this set, not about real
  invoices. The honest expectation is that real documents score lower.
- **The end-to-end 100% is partly built in.** Each supplier is booked to its main
  account about 92% of the time, and the truth for an image is that main account.
  A retrieval proposer that votes for the majority will match it by construction.
  The 96.6% held-out figure, which includes the 8% off-pattern bookings, is the
  more honest number, and it rests on one miss in 29.
- **No real customer history has been used.** Production history is readable only
  in-cluster and none has been supplied. Accuracy on Definitely Mabe's own
  bookings is NOT claimed. The bet's outcome (a) is therefore unmet until it runs
  on real history.
- **Real voucher text is unverified.** The proposer matches on the voucher
  description. If Fortnox supplier-invoice vouchers carry "Leverantörsfaktura 123"
  instead of the supplier name, the match will fail and the supplier name must come
  from the supplier invoice record instead. Not yet checked against real data.
- Accounts are proposed, not VAT code or cost centre accuracy; those were not scored.
- One model, one run, temperature 0. No comparison with other iguana models
  (`gemma4:31b` is a candidate) and no latency test under load.

## Next, in order of what de-risks the bet

1. Run on real history: supply the voucher cache read in-cluster, or a SIE4 export.
2. Test on a handful of real invoice scans (even three) to see how far 100% drops.
3. Compare `gemma4:31b` against `qwen2.5vl:7b` on the harder real scans.
