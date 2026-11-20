# Bet (draft): prove a controlled, reversible write path

Tracker: #139. Status: **drafted by an agent, not placed.** Only Mathias places a bet;
after a slice starts the outcome, guardrail, threshold and out-of-scope list are frozen
(`skills:bet`, iron law 4).

Discovery inputs: `2026-11-20-write-path-discovery-movement-1.md`,
`2026-11-20-write-path-discovery-evidence.md`. Governing: ADR-0001, ADR-0012, #65.

## Interview, 2026-11-20 (Mathias)

| Question | Answer | Consequence |
|---|---|---|
| Why write to the live books? | Prove the pattern | Time saved is not the measure. The bet is about a proven, reusable, safe agent-write pattern. |
| Build request-level event logging first? | Yes | Slice S1 (#140). Evidence comes from the system, not a manual log. |
| Outside check | Wife's accounting firm | The only external reviewer. Single source; recorded as such. |
| Appetite | One quarter | Read date 2027-02-20. |
| Manual measurement | Not now | #137 closed as superseded. |

## Bet

**Problem.** Agent writes to the live books are wanted eventually (ADR-0001) but
"very proven" has no observable definition (#65). Without one the boundary either never
moves or moves on a feeling. Mathias wants the pattern proven, not just the feature.

**Appetite.** One quarter, read 2027-02-20. Scope is cut, time is not extended.

**Outcome (people, not output).** By the read date the accounting firm's reviewer has
judged system-proposed vouchers, produced against the TEST company only, acceptable to
post as proposed. Proposed threshold, for Mathias to set or change *before* S1 starts:
at least 90% of at least 30 proposals acceptable, zero judged unsafe. Read from the
firm's written review.

**Guardrail.** Zero writes to the live company. `scripts/assert-live-readonly.sh` green
throughout. No Tier 3 operation attempted (supplier bank details, inbox delete, cancel
supplier invoice).

**Out of scope.** Payment execution (ADR-0007/0008, parked by ADR-0009). Tier 3
operations. Multi-tenant. Any write to the live company: that is a separate tier-4
decision (ADR-0012) taken only after this bet reads out.

**Path.** Full. Irreversible domain; personas do not count as evidence.

## Slices (ordered by what de-risks the outcome first)

| # | Slice | Issue | Risk |
|---|---|---|---|
| S1 | Request-level event log, no amounts or counterparties | #140 | risk/medium |
| S2 | Assertion: no type may send a non-GET to Fortnox outside the gate | #109 | risk/high |
| S3 | Successor-ADR draft with observable "proven enough" criteria | #65 | risk/high, decision |
| S4 | Propose-then-confirm voucher on the TEST company, with a counter-voucher drill | #45 | risk/high |
| S5 | Firm review of S4's proposals | to file when S4 is ready | needs Mathias |

S2 comes before S4: the repo must not hold two write verbs, one of which nobody designed
(#109's own ordering note). S3 and S4 need tier-4 approval from Mathias, with a
transcript naming the artifact.

## Evidence gap (honest state)

Iron law 1 says no bet without evidence. What exists:
- Strong on risk: ADR-0001, `irreversible-operations.md`, #45, #88, #109.
- Thin on demand: no usage telemetry; zero unbooked invoices at last report; two
  non-zero clearing accounts (2894, 2895) that may point at reconciliation instead
  (#138, still open).
- One outside reviewer, and she is a household member.

Because the outcome is "prove the pattern", demand evidence matters less than for a
product bet. But if #138 shows reconciliation is the real cost, say so at the read date
and consider whether the first write worth proving is a reconciliation posting.

## Reading the outcome (2027-02-20)

Validated, not validated, or unreadable. An unreadable measure is a defect in the bet
and is recorded as one. Learning goes to the brain either way.
