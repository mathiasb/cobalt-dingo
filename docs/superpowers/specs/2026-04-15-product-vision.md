# cobalt-dingo: Product Vision

**Version**: 0.2.0
**Date**: 2026-04-15
**Status**: Approved
**Author**: Mathias Bergqvist

---

## One-liner

**Autonomous treasury for Nordic SMEs. The AI handles the books. The owner handles the exceptions.**

---

## Problem

A Swedish SME with foreign supplier relationships spends 4–8 hours a month on:
- Processing and coding supplier invoices to the correct GL accounts
- Scheduling and executing payments (domestic and cross-border)
- Reconciling FX differences and bank fees
- Chasing overdue customer payments

This work is almost entirely routine — the same decisions made the same way, week after week. It is either done by a part-time bookkeeper, outsourced to an accounting firm, or not done well and left to accumulate. None of these outcomes is good for the business owner.

cobalt-dingo makes that work invisible. Not faster — invisible.

---

## User

**Primary**: The business owner / founder of a Nordic SME with 5–50 employees. Has foreign supplier relationships. Uses Fortnox as their accounting system. Has no dedicated treasurer. Time is their scarcest resource.

**Interaction model**: Mobile-first. Push notification is the product surface. Conversation is the control surface. The owner never needs to open an app to do routine work — the app finds them. Most interactions take under 90 seconds.

**Not the primary user**: An internal bookkeeper or accountant. The AI replaces this role for routine work. Where an accountant is still involved (annual accounts, complex tax questions), cobalt-dingo produces clean, well-coded data that makes their job trivial.

---

## What cobalt-dingo replaces

| Today | With cobalt-dingo |
|-------|------------------|
| Bookkeeper codes each invoice manually | AI codes invoices automatically, learns from corrections |
| Owner or bookkeeper schedules payments | AI schedules based on due dates, FX rates, cash rules |
| Manual PAIN.001 or bank upload | Automatic batch assembly and PISP submission |
| FX gain/loss booked manually (or not) | Auto-posted to correct BAS account after bank confirms |
| Owner reviews every payment | Owner approves batches; eventually just reviews anomalies |
| AR follow-up in calendar reminders | Automated dunning, auto-matched inbound payments |

---

## Product layers

### Layer 1 — The intelligence layer (AI bookkeeper)

The AI watches every invoice that enters Fortnox. For each one it:

- Codes it to the correct BAS 2024 account (supplier, description, and learned company patterns)
- Determines correct VAT treatment (domestic, EU reverse charge, non-EU)
- Validates supplier banking details (IBAN checksum, BIC, domestic routing)
- Assigns cost centre where applicable
- Flags anomalies: amount outlier vs. supplier history, unfamiliar supplier, suspected duplicate, due date mismatch
- Schedules payment timing based on due date, current FX spot, and configured cash position rules

**Learning architecture — hybrid model:**

| Component | Scope | Purpose |
|-----------|-------|---------|
| Shared base model | All tenants | General Nordic accounting intelligence: BAS conventions, VAT rules, SEPA routing, supplier category patterns |
| Per-tenant fine-tune (RAG + SFT) | Single tenant | Company-specific decisions: which account this supplier maps to, cost centre rules, payment timing preferences |
| Self-correction | Single tenant | When owner overrides a decision, model learns. Override rate is the accuracy metric. |

New tenants start smart (shared base), get smarter over time (fine-tune). Every tenant's corrections improve the shared base for everyone.

### Layer 2 — The payment layer

Validated, coded invoices are assembled into standard ISO 20022 PAIN.001 batches and submitted directly to bank PSD2 APIs. cobalt-dingo holds the PISP and AISP licenses — there is no intermediary bank partner. Coverage: top ~20 Nordic and Northern European banks under a single PSD2 signature.

**Payment status**: dual-mode — webhook callbacks where banks support them, polling fallback where they don't. Both paths converge on the same reconciliation flow.

**AISP enables real-time bank data**: cobalt-dingo reads account balances and transaction history directly from banks (not via Fortnox, not via Camt file import). This powers:
- Real-time cash position (actual bank balance, not projected from Fortnox)
- Automatic AR matching: inbound transactions matched to open customer invoices
- Bank fee reconciliation from actual bank statements
- Accurate cash flow forecasting from confirmed rather than projected data

Post-execution: actual FX rates and bank fees sourced from AISP transaction data. The AI automatically posts:
- FX gain (BAS 3960) or FX loss (BAS 7960): difference between invoice rate and execution rate
- Bank charges (BAS 6570)

No human touch required for reconciliation.

**Graduated autonomy:**

The AI earns the right to act without approval. Autonomy is not granted — it is demonstrated.

| Level | Trigger | Owner interaction |
|-------|---------|------------------|
| 0 — Supervised | New tenant (default) | Approve every payment batch |
| 1 — Threshold | Override rate < 5% over 90 days + owner opt-in | Routine payments below configured threshold execute automatically |
| 2 — Scheduled | Override rate < 2% over 180 days + owner opt-in | Full payment runs execute on schedule; owner gets summary, not approval request |
| 3 — Autonomous | Override rate < 1% over 365 days + owner opt-in | Full autonomous treasury within defined rules; only anomalies surface |

The autonomy level is a visible number in the product. Earning it is a product experience. Owners can always step back to a lower level.

### Layer 3 — The owner's interface

**Primary surface: push notifications**

> *"Payment batch ready — 7 invoices, €34,200 + SEK 85,000, due this week. Approve?"*
> Owner authenticates with biometrics. Done. 30 seconds.

> *"IBAN flagged for Müller GmbH — they changed their bank. Confirm new details?"*
> Owner reviews and confirms in the app.

**Secondary surface: conversational AI**

Natural language as the control surface. Examples:

- *"Why did you code the Amazon invoice to 6540?"*
- *"What's my EUR exposure next 30 days?"*
- *"Hold all payments above 50,000 SEK until Monday."*
- *"Show me what's overdue from customers."*
- *"How much have we spent on software subscriptions this quarter?"*

The AI explains its decisions, accepts instructions, and surfaces relevant context on request.

**Tertiary surface: web dashboard** *(Phase B)*

A command centre for accounting firms managing multiple clients. Not the primary surface for the SME owner. Built after the core product is proven.

---

## Monetisation

**Hybrid model:**

| Stream | Description | Rationale |
|--------|-------------|-----------|
| Base subscription | Monthly SaaS, tiered by invoice volume or company size | Captures recurring value of AI bookkeeping layer |
| Transaction fee | Basis points on payment volume processed through PISP | Captures value of payment execution; aligns incentives |

The two streams compound: more payment volume → more learning → better AI → higher subscription retention → more payment volume.

---

## Phased roadmap

| Phase | What ships | AI maturity | Target |
|-------|-----------|-------------|--------|
| **MVP** | Foreign currency AP: Fortnox → validate → PAIN.001 → PISP → GL write-back. HTMX web approval UI. Multi-tenant. | None — rule-based validation only | First 3 design partners |
| **V1** | Full AP (domestic SEK + FX). AI GL coding. Mobile app + push notifications. Graduated trust level 0→1. | RAG on tenant history, shared base model | Paid launch, 20–50 tenants |
| **V2** | AR automation. FX exposure dashboard. Cash flow forecasting. Conversational interface. | Per-tenant SFT, self-correction loop | 100–200 tenants |
| **V3** | Autonomous treasury (level 2→3). Multi-ERP (Visma eEkonomi). FX alerts and hedging signals. | Cross-tenant learning, mature model | Series A story |
| **V4** | Accounting firm channel. White-label. Multi-client dashboard. | Firm-level insights | Phase B |

---

## North star metric

**% of total AP+AR volume processed without owner intervention.**

Target for a mature tenant (12 months on platform): **≥ 90% autonomous**.

This number going up is the product working. Every override is a model failure to learn from. Every autonomous payment is trust compounding.

---

## Competitive positioning

cobalt-dingo is not an accounting tool (Fortnox does that). It is not a bank (the PISP partner does that). It is the intelligent connective tissue between the two — the layer that understands *what needs to happen*, orchestrates it across both systems, and explains itself to the owner.

**Three moats:**

1. **Integration depth** — Fortnox WebSocket + GL write-back + PISP combination takes months to build correctly. Incumbents won't prioritise SMEs. Fintech competitors will build the happy path and break on edge cases. BDD/ATDD from day one means the test suite is the compliance artifact.

2. **Learning data** — Every invoice processed, every override corrected trains the model. The longer a tenant stays, the smarter their AI gets. The more tenants onboard, the better the shared base gets. Both flywheels spin in the same direction.

3. **Trust at the money layer** — The first time cobalt-dingo catches a duplicate invoice, flags an IBAN change from a spoofed supplier email, or auto-captures a 2% early payment discount the owner didn't notice — that's a customer for life.

---

## Constraints and principles

- **Multi-tenant from day one** — each tenant has isolated Fortnox OAuth2 credentials, isolated learning data, isolated payment flows
- **BDD/ATDD throughout** — acceptance criteria before implementation at every phase; test suite doubles as compliance record
- **Graduated autonomy, never surprise autonomy** — money never moves in a new way without explicit owner opt-in
- **Explain everything** — every AI decision must be explainable on demand; no black box coding or black box payments
- **Privacy by design** — per-tenant data isolation; shared model training uses anonymised patterns, not raw invoice data

---

## Open questions (pre-architecture)

1. Mobile: native iOS/Android vs. PWA? Biometric approval and push notification requirements may favour native.
2. LLM for conversational interface: local model via LiteLLM (koala) vs. cloud API? Cost and latency trade-off.
3. SFT pipeline: where does per-tenant fine-tuning run? koala (GPU) is the natural target.
4. Fortnox marketplace listing: timing relative to V1 launch?
5. PSD2 bank API coverage: which of the ~20 target banks have conformant PSD2 APIs vs. requiring bilateral agreements?
