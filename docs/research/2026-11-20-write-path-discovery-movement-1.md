# Discovery: should cobalt-dingo write to the live books? — movement 1 (problem space)

Date: 2026-11-20. Status: **draft, discovery phase, nothing decided.**
Method: `skills:discovery-framing`. Scope: controlled, reversible writes to the live
Fortnox company (Mathias, 2026-11-20: option a). Payment execution is out of scope
(ADR-0009, ADR-0007/0008); it is a separate decision.

Governing records: ADR-0001 (read-only, direction of travel toward writes), #65
("proven enough" is not observable), `docs/irreversible-operations.md`.

## The problem is not "enable writes"

Writes are a solution. The problem to size is the bookkeeping work that still lands on
Mathias after cobalt-dingo has read, collected and routed everything. That work has not
been measured. This note records what is known and what is not.

## What is known (from the repo, verified 2026-11-20)

| Fact | Source |
|---|---|
| Live books are read-only at four layers; Fortnox itself cannot give a read-only token, so three of the four are ours. | `docs/irreversible-operations.md`, ADR-0001 amendment |
| `payment` scope is withheld, the only vendor-side guard. | same |
| The MCP surface is 38 tools, all reads. | #45 |
| Voucher creation was dropped in consolidation because its confirmation was caller-side. A proper rebuild needs a propose-then-confirm handle, `AllowsWrites` gating, sandbox tests. | #45 |
| An ungated `CreateVoucher` exists in `internal/receipts/api/client.go`. Not imported today. No assertion forbids another. | #109 |
| There is no Fortnox sandbox. Test and production companies share an organisation number; only the consent-screen pick separates them. | #88 |
| Tier 1 corrections (vouchers) cannot be removed, only corrected by a *rättelseverifikation*. A consumed voucher number is permanent. | `irreversible-operations.md` |
| Tier 3 (supplier bank details, inbox DELETE, cancelling a supplier invoice) is quiet and has no audit trail to correct from. | same |
| The "proven enough" criteria in #65 are candidates only; none agreed. | #65 |

## What is not known (movement 2 must answer)

1. How many hours a month the manual bookkeeping steps cost, and which steps.
2. Which of those steps a write would remove, as opposed to a better read or a better draft.
3. Whether a *draft* the operator posts by hand in Fortnox captures most of the value
   with none of the irreversibility. (Cheapest alternative to writes; test it first.)
4. Where the accountant's review sits in the loop, and whether they would accept
   system-posted vouchers.
5. Whether Swedish bookkeeping rules (closed periods, declared VAT) constrain which
   vouchers may be system-posted. Mathias's judgement governs; the repo author states
   low confidence here.

## Assumptions, sorted and ranked (risk x certainty)

D = desirability, F = feasibility, V = viability.

| # | Assumption | Type | Risk | Certainty | Order |
|---|---|---|---|---|---|
| A1 | A material share of monthly bookkeeping time is posting vouchers that cobalt-dingo could propose. | D | high | low | 1 |
| A2 | A drafted voucher the operator posts by hand removes most of that time. | D | high | low | 2 |
| A3 | The read path is trustworthy enough to write from (no false discrepancy). | F | high | low | 3 |
| A4 | A propose-then-confirm handle plus a counter-voucher path makes a bad write recoverable at acceptable cost. | F | high | medium | 4 |
| A5 | Only low-risk voucher types (for example a fixed set of recurring entries) are needed. | V | medium | low | 5 |

Each becomes a falsifiable hypothesis in movement 2: "if X, then Y by date Z, else kill".

## Next

Movement 2 needs evidence only Mathias or the live data can give. Required inputs are
listed in the session hand-off. Verdicts (pursue / table / kill) come in movement 4.
