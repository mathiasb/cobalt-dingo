# Problem Analysis — reading the live company's financial data

**Status:** analysis complete, execution not started.
**Date:** 2026-09-08.
**Decisions taken here:** [ADR-0001](adr/0001-read-only-live-financial-data.md),
[ADR-0002](adr/0002-ingest-files-not-vendor-integrations.md) — both `proposed`,
pending Mathias.

This document is the entry point for anyone — human or agent — picking up work
on live-data ingestion. It states the problem, what is reachable and what is
not, the phase order and the success criterion for each phase. Read it before
the issues; the issues assume it.

---

## Problem statement

cobalt-dingo has only ever seen Fortnox sandbox data. The company's actual money
moves through six systems, and no single one of them knows the whole picture:
Fortnox holds what has been booked, Gmail holds the receipts, SEB holds what the
bank actually did, SEB Kort and Mynt hold card spend, Avanza holds the portfolio,
and Kivra delivers official mail. Answering "what did this company spend, on
what, and is it all in the books?" today requires opening six interfaces by hand.

The goal is one queryable, read-only view over all of them.

## Context — why now

The coo-agent merge (`infra` ADR-0021, 2026-09-06) brought receipt collection,
IMAP plumbing, Swedish VAT tables and the Mynt reconciliation skill into this
repo. The pieces to do this exist for the first time. What is missing is a live
Fortnox connection (issue #50, open since the coo-agent era) and any source of
truth independent of Fortnox.

## The reframe

This reads as six integrations. It is one problem: **get every krona that moved
into one queryable store, with no write path anywhere.** The sources differ only
in how hostile their access is — and, per ADR-0002, four of six are not API
problems at all but file-parsing problems.

## Stakeholders

| Stakeholder | Role | Primary concern |
|---|---|---|
| Mathias | Owner, sole user, decision maker | Nothing falls through the cracks; no manual admin; the books are right |
| Skatteverket / revisor | Indirect, downstream | The books are accurate and traceable to source documents |
| Future dev team (human or agent) | Maintainer | Can pick up the work from docs and issues without archaeology |
| cobalt-dingo the product | Downstream consumer | Everything proven here is a candidate feature for the SaaS track (#46) |

## Source access matrix

The ladder, easiest to hardest. This is the core reference.

| Source | Path | Why | Effort | Risk |
|---|---|---|---|---|
| **Fortnox (booked)** | Existing API adapter, `Mode=production` | Already built. Read-only by default; needs live OAuth only (#50) | XS | low |
| **Gmail ×N** | IMAP + per-account App Password | Estate decision 2026-06-19: OAuth's 7-day refresh expiry in Testing status breaks unattended agents weekly | S | low |
| **Mynt** | Read via Fortnox — native integration books it there | Verify before building any Mynt client | S | low |
| **SEB Kort Corporate** | Statement file → ingest channel | No SME API exists | M | med |
| **SEB bank** | camt.053 / CSV export → ingest channel | No API for an enskild firma; commercial gate, not regulatory (ADR-0002 Finding 2) | M | med |
| **Avanza** | Manual CSV / Årsbesked export → ingest channel | No official API. Unofficial one rejected — see ADR-0002 | M | med |
| **Kivra** | **Not reachable.** Product-track item | Read API is "Hämta E-försändelser", gated to signed Partners with negotiated pricing (ADR-0002 Finding 3) | — | blocked |

### The two findings that shaped everything

1. **Fortnox API v3 has 233 paths and zero bank-transaction endpoints.** Only
   booked material is exposed. A system reading only Fortnox is checking Fortnox
   against Fortnox and can never catch a charge that hit the bank and never
   reached the books. An independent source is structurally required, not
   optional.
2. **Everything without an API produces a file, and files arrive by mail.**
   `internal/receipts` already collects, routes and delivers mail. Four sources
   collapse onto plumbing that already exists.

## Functional requirements

- [ ] Read the live company's Fortnox data without any possibility of writing to it
- [ ] Collect receipts and statements from several Gmail accounts unattended
- [ ] Ingest a bank statement and turn it into normalised transactions
- [ ] Show card spend (Mynt, SEB Kort) against the receipts that support it
- [ ] Flag spend that reached the bank but never reached the books
- [ ] Flag recurring spend and its annualised cost (issue #59)

## Non-functional requirements

- **Safety:** no write reaches the live company. Asserted by test, not convention (ADR-0001)
- **Mail integrity:** reading a mailbox must not mutate it — no message marked `\Seen` that was not processed
- **Secrets:** every credential in 1Password, synced by ESO. None in the repo, none in a transcript
- **Auditability:** every transaction traceable to the file or API call it came from
- **Cadence:** ingestion runs unattended; a source that goes quiet raises a signal rather than failing silently

## Scope

**In scope:** Fortnox production read, multi-account Gmail, statement parsing for
SEB / SEB Kort / Avanza, Mynt via Fortnox, cross-source reconciliation with a
manual exception lane.

**Out of scope, deliberately:**

| Not doing | Why |
|---|---|
| Any write to live Fortnox | ADR-0001. Requires a superseding ADR, not a config change |
| Unofficial or scraped Avanza API | ADR-0002. Terms breach, brittle, credentials in an agent |
| PSD2 AISP licence | Not needed for own-company access, and not the blocker anyway |
| Kivra retrieval | Partner-gated. Belongs to the SaaS track (#46) |
| Multi-tenant | Issue #46, separate track |
| Automatic anything | Detect-only. Cancelling and booking stay human-gated |

## Assumptions requiring validation

| # | Assumption | How to validate | When |
|---|---|---|---|
| A1 | Fortnox Bankkoppling is active and account 1930 carries current transactions | Pull `/3/sie/4`, inspect 1930, compare count and latest date against SEB's internet bank | Phase 0 |
| A2 | Mynt spend is already booked into Fortnox by its native integration | Look for Mynt-originated vouchers/supplier invoices before writing any Mynt client | Phase 2 |
| A3 | SEB's export for this account tier includes camt.053, not only CSV | Check the export options in SEB's internet bank | Before Phase 3 |
| A4 | SEB Kort provides a structured statement export, not only PDF | Check the statement delivery options | Before Phase 3 |
| A5 | A manual export cadence is sustainable | Observed over two months. Trigger recorded in ADR-0002 | Phase 3+ |

**A1 is the highest-value cheap check in the whole plan.** If 1930 is populated
and current, Fortnox covers Phases 0–2 for free and camt.053 waits for Phase 4.
If it is empty or stale, Phase 3 moves up.

## Phases

Each phase is one tracker issue. Later phases depend on earlier ones.

### Phase 0 — Fortnox production, read-only

Live production OAuth (#50) with tokens in postgres via the CAS write-back that
already works in sandbox. Guarded by ADR-0001's wiring test.

**Prerequisite:** the wiring test needs the adapter set built by something other
than `main()`. That is issue **#10** (extract wiring from fat `main()`), which is
therefore a blocker for the *test*, not for the OAuth.

**Done when:** `task fortnox:check:production` returns the real company's
information and a non-zero voucher count; a POST attempt is refused with
`ErrReadOnlyClient`; the wiring test is green; A1 is answered in writing.

### Phase 1 — Multi-account Gmail, read-only

App Password per account → 1Password → ESO → cluster. Completes the routing rules
(#56) and the collector stubs (#48).

**Known footgun, fix before pointing at live mail:** an IMAP `FETCH BODY[]`
without `.PEEK` sets `\Seen` on everything fetched, not only what you process.
This already bit coo-agent (#24), caught on 2026-06-22.

**Done when:** the collector reads every configured account, emits a dry-run
classification report, and zero messages changed flags — verified by comparing
unread counts before and after.

### Phase 2 — Ledger view

Read `/3/vouchers`, `/3/supplierinvoices`, `/3/sie/4`. Answer A2.

**Done when:** the `mynt-reconcile` three-bucket report (matched / possible /
missing receipt) runs against real data.

### Phase 3 — Statement ingestion

Build `internal/statements` with the `Parse` port and the four preservation-
strategy items from ADR-0002 (port, camt.053-shaped schema, idempotency key,
provenance). Adapters: camt.053, CSV, PDF last.

**Done when:** one month of real SEB statement data parses and the transactions
sum to the statement's closing balance. That sum is the test — it is the only
check that catches a parser silently dropping rows.

### Phase 4 — Cross-source reconciliation

Match bank line ↔ voucher ↔ receipt. Build the manual exception lane in from the
start: FX bank fees and FX gain/loss never auto-match.

**Done when:** a period reconciles end to end, and every unmatched item is in a
named exception category rather than an "unknown" bucket.

## Risks

| Risk | Impact | Likelihood | Mitigation |
|---|---|---|---|
| A write reaches the live company | **High** | Low | ADR-0001: three layers plus a wiring test |
| Manual export cadence lapses | Med | **High** | Scheduler surfaces "no statement in N days" (#52); ADR-0002 resolution trigger |
| PDF parser silently drops or misreads rows | **High** | Med | Balance-sum test; prefer structured export; A4 |
| IMAP run marks real mail as read | Med | Med | `.PEEK`; verify unread counts before/after |
| Live company data leaks to a cloud model | **High** | Med | Egress containment is `infra` ADR-0014, not this work. Named so it is not assumed handled |
| Double-counted transactions after a source switch | **High** | Med | Idempotency key, ADR-0002 preservation item 3 |

## Open questions

1. **Which mailbox is the ingest channel?** A dedicated address, or a label in an
   existing account? Affects Phase 1 routing and Phase 3 delivery. Unanswered.
2. **Where do parsed transactions live?** Postgres alongside the token store, or
   recomputed per run? Affects whether Phase 4 can look back across periods.
3. **Does the Avanza portfolio belong here at all?** It is wealth, not
   bookkeeping. Included because Mathias asked; the value is unproven until
   Phase 3 exists.

## What this does not solve

A read-only system that reads correctly can still be wrong about what it reads.
Nothing here validates that Fortnox's booked data is itself correct — only that
it matches the bank. Reconciliation catches omissions and duplicates. It does not
catch a correctly-booked transaction posted to the wrong account.
