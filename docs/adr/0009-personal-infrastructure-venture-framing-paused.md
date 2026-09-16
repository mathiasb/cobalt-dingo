---
adr:           "0009"
title:         cobalt-dingo runs as single-tenant personal finance infrastructure; the venture framing is paused, not abandoned
status:        accepted
date:          2026-09-16
deciders:      Mathias
supersedes:    null
---

# ADR-0009 — cobalt-dingo runs as single-tenant personal finance infrastructure; the venture framing is paused, not abandoned

## Status

Accepted 2026-09-16 by Mathias, answering the product question raised in #108 after
payment execution was parked on risk grounds the same day.

Does not supersede any ADR. `ADR-0007` and `ADR-0008` remain **accepted and dormant**:
they are the design of record for the payment path if the venture resumes, and nothing
about them is withdrawn.

## Context

The forces, in the order they matter.

**The vision is payment-centric and payments are parked.**
`docs/superpowers/specs/2026-04-15-product-vision.md` (v0.3.0, *Approved*) opens with
"Autonomous treasury for Nordic SMEs". Layer 2 is the payment layer. Parking payment
execution removes the MVP as the doc defines it, half the monetisation model (the
transaction-fee stream on payment volume), the mechanism behind the north-star metric,
and one of the three stated moats. What remains is real and shipped — read-only Fortnox
data, receipts, statements, the MCP surface, the scheduled overview — but it is not the
product in the one-liner.

**The venture's own existential questions have not moved in 103 days.** The 2026-06-05
grill raised three with 30-day actions: engage a PISP platform (#20), onboard Jonas as
co-founder (#21), define buyer and pricing (#22). On 2026-09-16 all three were open with
zero comments and no assignee. Its falsification criterion — one consultant processing a
real client's real foreign payment within 90 days — expired 2026-09-03 untested.

**The adjacent space is occupied.** `erp-mafia/accounted` ships a complete, agent-native,
AGPL Swedish accounting product free and self-hostable, at roughly a merged PR every two
hours. The one thing it has no answer to is foreign-currency payment execution, which is
precisely the capability now parked. Competing on the remainder is competing on their
ground.

**TELOS never carried this as a venture project.** `wiki/telos/decisions/projects.md`
(2026-06-12) lists `coo-agent` under Career/revenue and mentions cobalt-dingo only as a
thing Authentik was migrated across. The venture framing existed in this repo's documents
and in a Claude.ai conversation; it was never in the intention substrate that is supposed
to say what the work is for. This decision brings the repo back into line with TELOS
rather than departing from it.

## Decision

**cobalt-dingo is Mathias's own finance infrastructure: single tenant, one operator, no
customers. The venture framing — buyer, pricing, co-founder, SaaS, payment execution — is
paused with an explicit resumption trigger, rather than left to decay.**

Concretely, what is now **out of scope**: buyer and pricing work, co-founder onboarding,
PISP or platform engagement, customer-facing multi-tenancy, a customer contract and the
liability position it needs, Kivra partner access, and the non-technical admin UI whose
purpose was onboarding people who are not Mathias.

What is **in scope**: whatever makes his own books less work. The read-only Fortnox path,
receipts collection and routing, statements, the MCP surface, the scheduled overview, and
keeping the production connection alive.

**Deliberately not done: no demolition.** `TenantID` stays first-class, `ADR-0005`
(tenants bring their own Fortnox integration) stands, and the hexagonal ports stay as they
are. Keeping the multi-tenant *shape* costs approximately nothing; removing it and later
re-adding it costs a rewrite. A pause that triggers a teardown is how a pause becomes
irreversible by accident.

**Why this rather than narrowing to a smaller product.** Three narrower products were on
the table: FCY payment *preparation* without execution, an AI bookkeeper for Fortnox, and
a read-only financial-visibility tool. The first keeps the only real differentiator but
still needs the buyer and the contract that have not been answered in three months. The
second walks directly into `accounted`'s strength. The third is honest but thin. All three
share one defect: they keep the overhead of being a product — positioning, pricing,
support expectations, a compliance surface — while the questions that make a product a
product remain unowned. Paying that overhead for a product with no buyer is the cost this
decision removes.

## Consequences

**Positive:**

- The regulatory and compliance surface drops to near zero. `R-COMP-03` (defect liability
  resting on a contract that does not exist) is not applicable while there is no customer;
  #106 and #103 stop being gates on anything.
- A large slice of the backlog closes honestly rather than sitting open pretending to be
  work: #20, #21, #22 as venture questions, and the SaaS-track items (#46, #63, and the
  SaaS half of #84) as out of scope.
- The autonomy contradiction in #108 mostly dissolves. The vision's ladder described a
  product with tenants; `ADR-0007` governs anything that touches money and is unaffected.
- Effort concentrates on the one thing that has a live user: the production Fortnox
  connection and the read path on top of it (#95 is the immediate instance).

**Negative / watch:**

- **Personal tooling has no external forcing function.** Nothing outside will catch a
  regression or an unfinished path. The estate's own gates — `task check`, the ADR series,
  the risk register — become the only discipline, and they are self-imposed. Watch for the
  repo quietly dropping below the standard the rest of the estate holds.
- **The FCY wedge is perishable.** Fortnox could build foreign-currency AP natively, and
  `accounted` could extend from domestic to cross-border, at any time. If either happens
  the resumption option is worth much less. This is a real cost of waiting and it is not
  mitigable from here.
- **"Paused" decays into "abandoned" without a trigger**, which is why one is written
  below rather than left to judgement.
- `ADR-0007` and `ADR-0008` now describe a path nobody is walking. They must not be read
  as current roadmap. The pointer added to the vision document says so.

## Validation

The decision is working if, three months on, the repo is still maintained, the production
connection is alive, and no effort has been spent on buyer, pricing or tenancy work. It
has failed if the repo goes stale — a paused venture and an unmaintained tool are
different outcomes, and only the first was chosen.

**Resumption triggers** — plural, and each observable by someone who is not Mathias:

1. A person outside the household asks to use it for their own books, unprompted.
2. #21 and #22 acquire a named owner and a date — that is, the co-founder and buyer
   questions become work rather than aspirations.
3. A payment platform or bank approaches on terms that make channel (b) of `ADR-0008`
   cheap, rather than us approaching them.

Any one of these reopens this ADR. None of them is "it feels like the right time".
