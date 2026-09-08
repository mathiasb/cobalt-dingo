---
adr:           0002
title:         "Financial sources arrive as parsed files on one ingest channel, not as per-vendor API clients"
status:        accepted
date:          2026-09-08
deciders:      Mathias
supersedes:    null
verify:        "scripts/assert-no-vendor-clients.sh"
---

# ADR-0002 — Financial sources arrive as parsed files on one ingest channel, not as per-vendor API clients

## Status

**Accepted by Mathias, 2026-09-08**, explicitly **as a start**, alongside
[ADR-0001](0001-read-only-live-financial-data.md).

The stated direction of travel is **towards real APIs and MCP servers**. File
parsing is not the destination. But establishing the business-value process
comes first, and getting entangled in technical dependencies before that value
is proven is the more expensive mistake — so where a source can be "hacked"
cheaply, hack it.

This reframes the ADR usefully and is worth stating plainly: the file-first
design is not merely what the vendors forced on us (Findings 2 and 3), it is
what we would choose anyway while the value is unproven. Those two arguments
happen to point the same way, which is why this decision is safe to take now.

### Where the hack is allowed, and where it is not

The distinction that keeps "hack it" from becoming the trap:

- **Acquisition may be hacked.** A file downloaded by hand, a mail forward, a
  CSV of unknown provenance — all fine. That layer is expected to be replaced.
- **The schema may not.** The four preservation items in the Decision below —
  the `Parse` port, the camt.053-shaped `Transaction`, the idempotency key, and
  source provenance — are precisely what make the acquisition layer disposable.
  Hack the input; do not hack the model the input lands in.

A hack behind a stable seam is a cheap option kept open. A hack that becomes the
schema is a migration nobody budgeted for, and it is the specific way this
approach fails.

## Context

Beyond Fortnox, the company's money moves through five systems: multiple Gmail
inboxes, an SEB bank account, an SEB Kort Corporate Limit Credit card, Mynt
corporate cards, an Avanza portfolio — and Kivra, which delivers official mail
and invoices. The obvious design is one API client per vendor. The research
below says that design is unbuildable for four of the six, and unnecessary for
the rest.

### Finding 1 — Fortnox is not a bank aggregator, and its API proves it

The vendored Fortnox OpenAPI spec (`docs/receipts/docs/fortnox-openapi.json`)
declares **233 paths and zero bank-transaction endpoints**. No `/bankaccounts`,
no `/banktransactions`. Whatever Fortnox's own Bankkoppling shows inside its web
UI, API v3 exposes only **booked** material: `/3/vouchers`,
`/3/supplierinvoices`, `/3/invoicepayments`, and a full ledger export at
`/3/sie/{Type}`.

The consequence is not a missing feature, it is a reconciliation impossibility:
booked data is by definition what has *already been reconciled by hand*. A
system reading only Fortnox is checking Fortnox against Fortnox, and can never
surface the one thing worth catching — a charge that hit the bank and never
reached the books. An independent source is structurally required.

### Finding 2 — the regulatory door is open and the commercial one is shut

A company connecting its own systems to its own banks under an ordinary
banking agreement is not exercising PSD2 third-party access rights and is not
an AISP or PISP. It needs no licence. (Brain:
`corporate-treasury-apis-sit-outside-psd2-psr-by-structure-not-explicit-carveout`
— well-supported structural inference, corroborated by market practice, *not*
settled law with an explicit statutory carve-out.)

That door leads to bilateral corporate API programmes, which banks sell to
corporate treasuries. SEB is not going to onboard a one-person enskild firma
onto host-to-host. **The blocker is commercial, not regulatory** — which matters,
because it means no amount of compliance work opens it.

### Finding 3 — Kivra's read API exists, and is contractually out of reach

Kivra's developer surface is send-only: Tenant API (letters, invoices,
lönespecifikationer, bokningar), Receipt API, Legal API. Reading a company's own
received mail is a separate product, **"Hämta E-försändelser"**, and its terms
gate it to *Partners*:

> "Priser för Hämta E-försändelser och betalningsvillkor anges i ett separat
> avtal med Kivra."

> "För att Partnern ska kunna Hämta E-försändelser från en Användare krävs att
> Användaren har aktiverat integrationen till Avsändaren i Kivra."

So it requires a signed partner agreement with negotiated pricing, the end user
must activate the integration, and the Partner must independently contract with
that user; both parties are separate data processors under GDPR. Same shape as
SEB: a commercial gate, not a technical one.

This is genuinely interesting for the **product**, not for the plumbing. Kivra's
own terms describe the use case as retrieval "for example for accounting" — which
is precisely what a multi-tenant cobalt-dingo would be. It belongs to the SaaS
track (issue #46), not to getting one company's data in.

### Finding 4 — the pipe already exists

`internal/receipts` already implements IMAP collection, rule-based routing and
delivery (imported from coo-agent under `infra` ADR-0021). Every source that has
no API produces a file — camt.053, CSV, PDF — and every one of those files can
arrive by mail. Four of six sources collapse onto plumbing that is already
written.

## Decision

**A financial source is integrated by parsing the file it produces, delivered to
one ingest channel, behind a single port. Per-vendor API clients are added only
where an official, self-serve API genuinely exists.**

```go
// one seam for every non-API source
Parse(source Source, r io.Reader) ([]Transaction, error)
```

Adapters: camt.053 XML (SEB), CSV (SEB Kort, Avanza), PDF (last resort). Fortnox
and Mynt keep API/native paths because they actually have them — Mynt's spend
reaches Fortnox through its native integration and is readable via the existing
Fortnox adapter, so **verify that before building any Mynt client**.

**Explicitly rejected: unofficial and scraped APIs.** Avanza has no official API;
the reverse-engineered TOTP+websocket client would breach their terms, break on
any change, and put brokerage credentials inside an unattended agent. The cost of
a manual export is a cadence delay. The cost of a scraper is an outage in the
one system that is supposed to be telling you the truth about your money.

### The tension, preserved

Manual export versus a licensed aggregator is *not* resolved by this ADR, and
pretending otherwise would be the anti-pattern.

- **Manual export** — assumes a monthly-ish cadence is good enough for
  bookkeeping and reconciliation. Costs Mathias a recurring manual step, and the
  data is stale between exports. Zero cost, zero vendor, no consent surface.
- **Licensed AISP aggregator** (Tink, Enable Banking, GoCardless) — assumes the
  staleness will actually bite. Costs a monthly fee, a third party holding bank
  consent, and a vendor dependency sitting in the money path.

**What we do not know:** whether the manual cadence is sustainable in practice.
Nobody has run it yet, so any answer today is a guess.

**Provisional decision: manual export**, and the honest reason is that it is the
cheaper mistake, not that it is better. Reversing from manual to an aggregator
costs one new adapter behind an interface that already exists. Reversing from an
aggregator costs a contract exit and a consent migration. Neither option is
immune to the unknown, so per the `adr` skill the tiebreak is reversibility, and
this is it.

**Preservation strategy** — what gets built now so both stay reachable:

1. **The `Parse` port itself.** An aggregator becomes one more implementation
   returning the same `[]Transaction`; nothing downstream learns where the data
   came from.
2. **A normalised `Transaction` shaped on camt.053's field set**, not on any CSV's.
   camt.053 is the ISO 20022 statement standard and every aggregator emits
   something mappable onto it. Shaping on SEB's CSV export would silently make
   that CSV the schema and force a migration later.
3. **A per-transaction idempotency key** (account + booking date + amount +
   bank reference). Both paths will re-deliver overlapping ranges; without this,
   switching sources double-counts, and double-counted money is the failure that
   destroys trust in the whole system.
4. **Source provenance on every transaction.** A mixed-source period must remain
   auditable back to the file or API call it came from.

**Resolution triggers**, one per unknown:

- Manual export is skipped **two months running** → the cadence is not
  sustainable, price the aggregator.
- A reconciliation break is found that is **more than 30 days old** → staleness
  has become expensive, which is the aggregator's actual value proposition.
- SEB offers this company a corporate API on commercially sane terms → both
  options above lose to the direct bilateral path from Finding 2.
- Kivra opens self-serve retrieval, or cobalt-dingo becomes a Kivra Partner for
  the SaaS track → Kivra moves from Finding 3 to a real adapter.
- **The business value is demonstrated** — the reconciliation report has surfaced
  at least one real discrepancy that would otherwise have been missed, and a
  period has been closed using it. At that point the direction of travel in
  Status applies and it is worth paying for real API and MCP integrations,
  because there is now something concrete to protect. Before that, an API
  integration is a technical dependency bought on speculation.

## Consequences

**Positive.** One parsing seam instead of six vendor clients. No credential of
Mathias's ends up inside an unattended agent for any source that lacks a real
API. New sources are a new `Parse` implementation plus routing rules. The
reconciliation-impossibility in Finding 1 is structurally solved, because the
bank file is independent of Fortnox by construction.

**Negative / watch.**

- **Manual steps rot.** The whole design leans on Mathias exporting files. The
  first resolution trigger above exists precisely because that is the most
  likely thing to fail, and it must be *observed*, not remembered — the scheduler
  (issue #52) should surface "no SEB statement ingested in N days" as a signal.
- **PDF parsing is a trap** and is listed last deliberately. If SEB Kort offers
  any structured export, take it; a PDF layout change is a silent data-quality
  failure, and the brain already records that a parser built from one fixture
  misses live variants (`scrape-parser-fix-verify-against-live-sample-immediately`).
- **camt.053 as the schema anchor is a bet.** If SEB's export turns out to be
  CSV-only for this account tier, item 2 of the preservation strategy is the
  first thing to re-examine — before writing the mapping, not after.
- **FX will not reconcile cleanly and this is not a bug.** Bank fees and FX
  gain/loss never auto-match in a foreign-currency AP flow (brain:
  `ap-automation-cannot-auto-reconcile-fx-fees-and-gainloss`). The manual
  exception lane is part of the design from the start, not a v2 patch.

## Validation

`verify: scripts/assert-no-vendor-clients.sh` — asserts the end state that no
unofficial vendor integration has crept in: no outbound HTTP host belonging to
Avanza, SEB or Kivra appears in the non-test source tree. This is the same
assertion shape that `infra` ADR-0020 recorded as working for
`openbanking-api`'s "never call a bank" clause.

It asserts on **behaviour — a host in a request path — never on vocabulary**.
ADR-0020 records a false positive where a "no kanban integration" assertion went
red by matching source comments explaining why there was no kanban integration.
This document names all three vendors repeatedly and would trip exactly that
trap; `docs/` is therefore out of the assertion's scope by construction.

**RED today**, because no ingest path exists at all yet — the script fails its
own precondition that `internal/statements` is present, per ADR-0020
constraint 4 (a missing precondition is red, not "nothing to check").
