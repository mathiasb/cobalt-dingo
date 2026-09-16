---
adr:           "0008"
title:         File export is the first supported submission channel; platform initiation is a roadmap item, not a prerequisite
status:        accepted
date:          2026-09-16
deciders:      Mathias
supersedes:    null
---

# ADR-0008 — File export is the first supported submission channel; platform initiation is a roadmap item, not a prerequisite

## Status

Accepted 2026-09-16 by Mathias, answering the channel-sequencing `DECISION NEEDED` in
`docs/payment-authority-and-liability.md` §6. Amends the framing of issue #20 without
superseding any ADR: `DECISIONS.md` 2026-04-15 (technology provider, platform holds the
licences) is unchanged and is in fact what makes this coherent.

## Context

Issue #20 has been open since 2026-06-05, labelled a BLOCKER, with the action "make an
exploratory call within 30 days" and the assumption stated as existential: *"If no
suitable partner exists at early-stage scale without enterprise contracts, the payment
pipeline cannot be built."* On 2026-09-16 — day 103 — it had zero comments and no
assignee, while the repository shipped to v0.64.0 across 248 Go files.

That is the symptom the decision addresses. The cause is the framing: the assumption is
binary and the reality is not. An approved ISO 20022 batch can reach a bank three ways:

- **(a) File export.** The payer uploads the file in their own corporate bank portal
  under their own bank agreement. No third-party access right is exercised; we never
  touch a payment account.
- **(b) Platform initiation.** A licensed platform initiates on the payer's behalf. The
  platform is the regulated entity; our exposure is contractual, through them.
- **(c) Corporate host-to-host.** The payer's own bilateral corporate channel, under
  their ordinary banking-services agreement.

On (c) the estate's own research carries an explicit confidence marker that this ADR
repeats rather than smooths: the boundary is *"a structural inference ... corroborated
by market practice"*, to be treated as *"very likely true, worth confirming per-bank"*
rather than settled law (`corporate-treasury-apis-sit-outside-psd2-psr-by-structure-not-explicit-carveout`,
2026-09-06).

Two further facts bear on it. Channel (a) **already exists in this repository** as the
signed XML download. And the nearest competitor — `erp-mafia/accounted`, 348 stars,
1189 merged PRs, a complete Swedish accounting ERP — ships exactly channel (a) for
supplier payments, with no platform integration anywhere in its tree. The incumbent's
shipped product is the file.

## Decision

**Harden file export into a supported, evidenced product path first; pursue platform
initiation in parallel as a commercial conversation rather than a prerequisite; treat
the submission channel as a port with per-channel regulatory posture.**

The strongest case against is real and should be stated: **the one click is the
product.** A tool that produces a file the user then uploads is a spreadsheet with
better manners, and the willingness-to-pay question in #22 may turn entirely on
removing that step. That case does not win *today* for one reason: every artifact
channel (a) forces us to build — the approval record, the evidence chain, the
reconciliation and FX exception lane, the duplicate-suppression key — is precisely what
a platform will require before it integrates us. Channel (a) is not a detour around the
platform conversation; it is the preparation for it, conducted at the lowest regulatory
exposure and against real money.

Consequently **#20 is a channel-quality item, not a blocker**, and should be relabelled.
A "no" from every platform costs the click, not the pipeline.

## Consequences

**Positive:**

- The payment path becomes buildable now, against the channel that already exists, with
  no dependency on a counterparty who has not been contacted.
- `PaymentSubmitter` (already declared in `internal/domain/ports.go`) gains adapters
  rather than branches. File export is an adapter, not the absence of one.
- Our regulatory posture becomes a property of the adapter. This is the useful
  discipline: **any sentence of the form "cobalt-dingo is or is not in scope of PSD2" is
  wrong unless it names the channel.**

**Negative / watch:**

- **The product is materially weaker without the click**, and pricing (#22) must be set
  against what channel (a) actually delivers, not against the roadmap. If early buyers
  say the file is worthless without initiation, that is the signal that this sequencing
  was wrong — and it is a cheap signal to collect, which is the point.
- Delaying the platform conversation delays discovering a disqualifying commercial term.
  Mitigation: parallel, not sequential. The conversation is not gated on (a) shipping.
- Three adapters is three regulatory positions to keep straight in client-facing
  material, which is a documentation burden that did not exist when there was assumed
  to be one path.

## Validation

- Channel (a) counts as *supported* when a real batch is produced, approved under
  ADR-0007, uploaded to a real bank portal, executed, and reconciled back — including
  the FX and fee difference — with every evidence artifact in §4 of
  `payment-authority-and-liability.md` present afterwards. Tracked against #88.
- #20's own definition of done still applies to channel (b): a written summary of one
  platform's commercial and technical requirements, with a per-channel go/no-go.

**Revisit trigger:** the first paying customer, or the first serious prospect, states
that file export without initiation does not solve their problem — or a platform
quotes terms that make (b) reachable sooner than (a) can be hardened.
