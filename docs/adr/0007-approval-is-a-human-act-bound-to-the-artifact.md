---
adr:           "0007"
title:         The customer is the initiator, and an approval is a human act bound to the exact artifact it authorises
status:        accepted
date:          2026-09-16
deciders:      Mathias
supersedes:    null
---

# ADR-0007 — The customer is the initiator, and an approval is a human act bound to the exact artifact it authorises

## Status

Accepted 2026-09-16 by Mathias, answering the `DECISION NEEDED` blocks in
`docs/payment-authority-and-liability.md` §5 and the initiator question in §9.
The liability allocation raised alongside it is **not** decided here — see
*What this does not decide*.

## Context

`ADR-0001` keeps cobalt-dingo read-only against live books, so no payment path exists
yet. Before one is built, the question "who may cause money to move, and what proves
they did" has to be answered, because the answer shapes the domain model rather than
following from it.

Three forces:

1. **Irreversibility is worse here than in the ledger.** `docs/irreversible-operations.md`
   covers corrections in the books, where the path is a *rättelseverifikation*. A wrong
   payment has no such path: the correction is a recovery request to a counterparty who
   may not cooperate.
2. **Evidence decays.** The obvious implementation of approval is a status transition.
   Months later it cannot answer *what* was approved, because the supplier's IBAN has
   changed since and regenerating the artifact from current state does not reproduce
   what the approver saw.
3. **The regulatory mechanics we would otherwise design against are not knowable.** The
   PSR is described as clarifying SCA for business-to-business flows, but the actual
   mechanics are not public in any source the estate has found (research 2026-09-06),
   and current corporate SCA exemptions should not be assumed to carry over.

An alternative existed and was considered: approval per individual payment rather than
per batch. It is rejected below.

## Decision

**The customer is the initiator of every payment; approval is a human act, recorded as
evidence bound to the content hash of the exact artifact displayed; one approver is
required now, and the record holds a set of approvers so that dual authorisation above
a threshold is later policy rather than a migration of evidence.**

Concretely:

- **Initiator.** cobalt-dingo never initiates in its own name, on any channel. This
  fixes the legal posture set in `DECISIONS.md` 2026-04-15 (technology provider; the
  platform, where one is used, is the regulated entity) and is what makes the
  per-channel positions in `ADR-0008` coherent.
- **Approval binds to an artifact, not to an intent.** "Approve the September batch" is
  not an authorisation; "approve these bytes, whose hash is X" is. The bytes are stored
  at approval, not regenerated at use.
- **The approval record names the authority the approver acted under** — either their
  own signatory right, or a specific recorded power of attorney with its scope and
  validity window. A consultant's approval is valid exactly when a PoA covers it, and
  unprovable without the reference.
- **Drift is a refusal.** If beneficiary details or settlement state changed since the
  approval record was written, submission is refused and re-approval required.
- **Approval is absent from the agent surface** — not scope-gated. A scope can be
  granted by a later change; a tool that is not in the catalogue cannot be reached by
  one.

**Why single-per-batch rather than per-payment.** The strongest case for per-payment is
blast radius: one mistaken approval moves one payment rather than all of them. It does
not win, because realistic use is bulk-approving a list, which reconstitutes batch
approval while feeling safer — and the batch is the unit the payer actually reviews and
the unit the bank deduplicates. Per-payment buys friction and a false sense of
granularity. The genuine control on blast radius is the threshold-based second
approver, which this decision keeps reachable rather than building now.

## Consequences

**Positive:**

- "The payer authorised this" becomes provable from stored data by someone who was not
  there, which is the foundation every liability position in #106 rests on.
- Deterministic batch generation stops being a stylistic preference: it is what makes
  the hash meaningful. The bank's own duplicate detection independently requires the
  same property, so the two constraints agree rather than compete.
- Dual authorisation, and any future SCA step-up the PSR turns out to require, are
  policy changes against an evidence shape that already accommodates them.

**Negative / watch:**

- Storing artifacts and their hashes makes approvals **large and permanently retained**:
  the batch is *underlag* under BFL 7 kap, so seven years applies to the approval
  record and the execution confirmation with it. Storage growth is a known cost, and
  deletion paths must be designed around the retention rather than against it.
- The refusal-on-drift rule will produce re-approvals that feel gratuitous to a user
  whose supplier merely corrected a typo. If re-approval friction becomes the top
  support complaint, the fix is a better diff in the re-approval screen, **not** a
  tolerance window on what counts as drift.
- `SignaturePolicy` exists as a seam with one implementation. A seam with one
  implementation is speculative generality unless the PSR's B2B SCA mechanics land
  differently from the current single policy — that is the bet, and it is explicit.

## Validation

- `TestApproval_BindsToDisplayedArtifactHash`
- `TestApproval_RecordsActorIdentityAndAuthorityBasis`
- `TestSubmit_RefusesWhenBeneficiaryChangedSinceApproval`
- `TestPain001_RegeneratesByteIdentically`
- `TestMCP_ApprovalToolAbsentFromCatalog`
- Risk register `R-DATA-01`, `R-AGENT-01` move to `mitigated` only when all five pass.

**Revisit trigger:** the adopted PSR text publishes B2B SCA mechanics that a single
`SignaturePolicy` cannot express, or a customer requires dual authorisation before the
threshold work is scheduled — whichever comes first.

## What this does not decide

Who bears the loss when a payment goes wrong. The initiator question is settled here;
the allocation is not, and it is not settled by the instruments that authorise the
payment either — see #106 and `docs/payment-authority-and-liability.md` §3.1.
