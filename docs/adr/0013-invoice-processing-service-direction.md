---
adr:           "0013"
title:         "Invoice processing is a propose-only service that learns per tenant; invoices get their own document port"
status:        proposed
date:          2026-11-25
deciders:      Mathias
supersedes:    null
verify:        null
---

# ADR-0013 — Invoice processing is a propose-only service that learns per tenant; invoices get their own document port

## Status

**Proposed 2026-11-25.** Not accepted. No `verify:` script exists; see Validation. Reference design and review record: `docs/design/invoice-processing-service.md`.

## Context

Bet #142 (placed 2026-11-24) builds an invoice processor that proposes accounts payable entries learned from a customer's own bookings. A demo exists and runs on synthetic and public data (`internal/{extract,booking,process,apentry,demo}`); nothing has been measured on real history. ADR-0001 keeps live books read-only; ADR-0002 defines a file-ingest seam that is shaped for bank statements; ADR-0007 makes approval a human act; ADR-0009 pauses the venture framing but keeps `TenantID` and the ports.

## Decision

1. **The service proposes, and never posts.** Output rungs in scope are L0 (on-screen, CSV) and L1 (a SIE4 file the user imports). L2 (an unbooked ERP draft) and L3 (booked entries) are **not** decided here and each needs a successor to ADR-0001.
2. **Invoices get their own document type and port** with per-field provenance. ADR-0002's statement parser is left as it is; this ADR does not amend ADR-0002's acceptance, it adds a sibling seam.
3. **Tax logic is deterministic.** A versioned rule table admits only (account, VAT) pairs; a model classifies the account within what the rules admit. Ambiguous pairs stay in human review.
4. **Learning is retrieval over the tenant's own history and corrections**, per `TenantID`, with every proposal citing supporting history. No cross-tenant learning.
5. **Evidence before automation, per supplier**, from a time-split backtest on imported history.
6. **Three spikes precede further build**: a read-only Fortnox probe on the company's own books, a text-layer versus vision comparison on real Swedish invoices, and a day-one backtest on real history.

## Consequences

- Cheap to start: most of L0 exists. The cost is in the supplier-resolution, rules and L1 work, none of which is built.
- The service is only as good as the tenant's history; a tenant with none gets no proposals, by design.
- Reverse charge, import and mixed-rate invoices are refused until a tax-domain scope is decided (#152).
- Real accuracy is unknown until spike 1; the 96.6% and 99.3% figures are synthetic or public data and are not a calibration.
- Nothing here lifts ADR-0009's pause: no buyer, pricing or multi-tenant onboarding work.

## Validation

Working if, after the three spikes, held-out accuracy on the company's own history is measured and reported per supplier, and no output path writes to a ledger. Failed if a rung above L1 is built without its own ADR, or a model output decides a VAT treatment. There is no executable `verify:` yet; the candidate is an assertion that no package outside the read-only Fortnox adapter can issue a non-GET (#109), extended to any new sink.

## Revisit

When spike 1 reports, or when a tenant other than the operator exists, whichever comes first.
