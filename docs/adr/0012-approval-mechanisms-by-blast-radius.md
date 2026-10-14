---
adr:           "0012"
title:         "Approval has four mechanisms, chosen by blast radius, not by artifact type"
status:        accepted
date:          2026-10-10
deciders:      Mathias
supersedes:    null
verify:        "scripts/assert-approval-mechanism-matches-risk.sh"
---

# ADR-0012 — Approval has four mechanisms, chosen by blast radius, not by artifact type

## Status

**Accepted 2026-10-14 by Mathias (tier 4, live approval).** Evidence: in a recorded session on koala, after this ADR and its PR #125 were read and discussed, Mathias wrote "Ok, jag accepterar ADR-0012" (his condition was that it could be accepted and is not irreversible; it is a status line and is reverted by one commit). The `verify:` script named in the frontmatter does not exist yet; tracked as a follow-up, so the Validation section is not yet executable.

Proposed 2026-10-10 by Mathias, arising directly out of merging PR #124 (ADR-0011)
and noticing the merge convention used to get there was itself undecided. Not yet
accepted — first write-up, for review.

## Context

PR #124 (ADR-0011, Anton's operator tier) needed merging. The existing mechanism —
`tbd_ship` / `pr_merge`, gated on CI green plus a `risk/low` label on a linked
issue — correctly refused to auto-merge it, because no issue was linked and
nothing was labelled. The fallback was: a human opens the Gitea UI and clicks
merge.

Mathias's objection, raised live while this was happening, is the trigger for
this ADR and is worth recording close to verbatim because the reasoning is the
decision:

> "...it's just a hassle for me to open up an interface and do clicking... I've
> already made a decision right now... this should be my merge action, this
> discussion, this type of discussion."

The objection is correct and names a real conflation in the existing design.
**The label-and-click mechanism was serving two purposes that do not need the
same mechanism:**

1. Proof that a human considered the change and approved it.
2. A specific UI artifact (a click, a label) that happens to be how that proof
   is currently captured.

Nothing about (1) requires (2). A live, attributed, timestamped verbal
instruction from Mathias, in a recorded session, after the content of the
change was discussed, is *at least as strong* evidence of (1) as a label
applied by rote — arguably stronger, since a label can be added out of habit
with no engagement, while a verbal "yes, merge it" in context cannot.

At the same time, the gate exists for a real reason that must not be lost in
fixing the friction: `ADR-0010`'s whole argument is that a scope reachable by
convention is not a control — `agentsquad#47`: "instructions ... behavior-
shaping, never enforcement." If "a human approved this" can be satisfied by an
agent merely asserting that a human approved it, the gate is gone, not
improved. The fix has to make live approval *actually* harder to fake than a
label, not easier to skip.

A second idea surfaced in the same conversation and is folded in here rather
than filed separately, because it is the same question answered at a different
point on the same axis: between "one reviewer agent" (already built, ADR-0010)
and "a human in the loop" there is room for **multiple reviewer agents with
deliberately different framings** — a council — catching what a single
reviewer's one perspective structurally cannot.

## Decision

**Approval is chosen by blast radius, not by what kind of file changed or which
tool produced the diff. Four mechanisms exist; a change uses the cheapest one
whose independence genuinely matches its risk.**

| Tier | Mechanism | What it proves | Used for |
|---|---|---|---|
| 1 | **Label + CI.** `risk/low` on a linked issue, CI green. Existing `tbd_ship`/`pr_merge` gate, unchanged. | Nothing was judged live; the risk was pre-classified and the classification is cheap to challenge after the fact. | Genuinely low-blast-radius change: typo fixes, doc wording, a rule whose violation is immediately visible and reversible. |
| 2 | **Single independent reviewer agent.** `ADR-0010`'s rubric reviewer: different model, built from `main`, blocks on `high`+ findings. | A perspective that did not write the diff looked at it against a versioned, diffable rubric. | Ordinary `agent/*`-lane code changes — unchanged from ADR-0010, this tier already exists and is not being modified. |
| 3 | **Reviewer council.** Multiple agents, deliberately different framings (e.g. security-focused, regulatory/liability-focused, decisions-consistency-focused), run against the same diff; findings are pooled, not blended into one score — same discipline ADR-0010 already applies against blending severity into one number. | Several structurally different blind spots were checked, not just one more opinion. | Changes with real surface area but that do not themselves move money, touch legal exposure, or set a new precedent — e.g. a new domain type touching several packages, a new ADR that extends an existing accepted decision rather than introducing a new authority boundary. |
| 4 | **Live human approval.** An explicit, attributed, timestamped instruction from Mathias (or, once ADR-0011 lands, an `owner`-tier human) given in a session where the content was actually discussed — voice, chat, or UI click are equally valid forms of the same thing. | A human engaged with the specific artifact and said yes to it, specifically. | Anything irreversible; anything touching money, legal/regulatory exposure, or another user's authority (ADR-0007's red tier, ADR-0011's red tier); anything that sets a new precedent for how the estate operates — including, reflexively, this ADR itself. |

**The four tiers are not a ladder every change climbs — a change is assigned
the tier matching its own risk, same logic as ADR-0010's severity-from-rubric
principle.** A docs-only ADR proposing a non-authority-boundary change can be
tier 1 or 2; this ADR is tier 4, because it changes the approval mechanism
itself, which is exactly the kind of precedent-setting change the table names.

### What makes tier 4 (live approval) a real gate and not just "the agent says so"

This is the part that has to be got right or the fix becomes a regression,
per the warning in Context above. Three properties, modelled directly on
`ADR-0007`'s evidence requirements for payment approval — reused rather than
reinvented, because the shape is identical: a human act bound to a specific
artifact, with the binding recorded as evidence rather than asserted after
the fact:

1. **The approval must name the specific artifact** — a PR number, a commit
   sha, a file path and content hash — not a general topic. "Yes, merge it"
   immediately after the content of PR #124 was discussed in the same session
   counts; "yes, do the thing we talked about" from a different, unrelated
   session does not.
2. **The record is the transcript or chat log itself, not a paraphrase
   written by the agent afterwards.** The evidence is what was actually said,
   in order, including the back-and-forth that led to the decision — the same
   principle as ADR-0007's "approval binds to the content hash of the exact
   artifact displayed," applied to conversations instead of payment batches.
3. **An agent cannot promote itself from tier 1/2/3 to tier 4 by claiming a
   human would approve.** Tier 4 requires an actual turn in the conversation
   where the human gives the instruction, addressed to this specific change,
   in this specific session. A session summary or a memory note recalling
   that "Mathias generally likes this kind of thing" is not tier 4 evidence.

### Where the council (tier 3) differs from one more reviewer

A second single-reviewer pass is not a council; it is the same mechanism run
twice and inherits the same blind spots, which is the mistake multiplying
reviewers without changing their framing would make. A council requires each
agent to be given a genuinely different brief — not just "review this diff"
again, but "review this diff specifically for regulatory/liability exposure"
or "review this diff specifically for consistency with existing ADRs." The
findings are reported per-framing, not merged into one pass/fail, mirroring
ADR-0010's existing refusal to let severity be blended or self-assigned by the
model under review.

## Consequences

**Positive:**

- Removes a real piece of friction (Mathias having to leave a conversation to
  click a button) without removing the thing the friction was protecting —
  the two were conflated and are now separated.
- Gives the estate a named, reusable vocabulary for "how much independent
  judgment does this change need" that is not the same question as "is this
  code or is this docs" — several existing single-reviewer-only gates (the
  rubric reviewer, `required_approvals` discussions in #112/#117) can cite
  this instead of re-deriving the reasoning per repo.
- The council option (tier 3) is new capability, not just a relabeling of
  what exists — it fills a real gap between "one agent reviewed it" and "a
  human reviewed it" that the estate did not have a name for before.

**Negative / watch:**

- **Tier 4's evidentiary requirements are the entire safety of this ADR.** If
  "a live approval happened" is ever accepted on the strength of an agent's
  say-so rather than an inspectable transcript, the gate has been removed,
  not replaced. This is the same warning ADR-0007 gives about approval
  records generally and it applies with the same force here.
- **Tier assignment is itself a judgment call**, and a self-interested agent
  (or a tired human) could under-classify a tier-4 change as tier 2 to avoid
  the friction this ADR was written to reduce elsewhere. The mitigation is
  the same one ADR-0010 uses for severity: tier is named by a fixed list of
  change *categories* (irreversible, money, legal/regulatory, authority
  boundary, precedent-setting) rather than by whoever is proposing the
  change, and a reviewer at any tier may escalate to a higher tier if a
  change looks miscategorised.
- **The council mechanism (tier 3) is new and unproven.** No fixture or
  calibration exists yet for how well differently-framed agents actually
  catch different things, versus just producing three similar reports with
  three different headers. Worth the same caution ADR-0010 names for its own
  single reviewer in issue #113 — calibrate before trusting it for anything
  that matters.
- **This ADR itself was approved under a mechanism it had not yet defined**
  — Mathias's "yes, continue" through this conversation is the tier-4
  evidence for ADR-0012 existing at all, which is slightly recursive and
  worth naming rather than hiding.

## Validation

- `scripts/assert-approval-mechanism-matches-risk.sh` — behavioural: for a
  sample of past merges, confirm the mechanism used (label, single reviewer,
  council, live approval) is at or above the tier its change category
  required, not merely that some mechanism was used.
- A change categorised as touching money, legal exposure, authority, or
  precedent that merged via tier 1 or 2 alone is a finding against this ADR,
  not a false positive to explain away.
- Tier 4 approvals should be traceable to an actual transcript excerpt naming
  the artifact, on request — if one cannot be produced, the approval did not
  meet this ADR's own bar and the merge should be treated as unapproved
  retroactively, however uncomfortable that is.

**Revisit trigger:** the council mechanism (tier 3) is actually built and
used enough times to know whether differently-framed agents catch
meaningfully different things, or a tier-4 approval is challenged after the
fact because its evidence trail was too thin to confirm what was actually
approved.

## What this does not decide

- The specific implementation of the reviewer council (which models, how many
  framings, where it runs) — that is engineering work downstream of this
  decision, not part of it.
- Any change to ADR-0007's payment-approval model, ADR-0010's agent-lane
  rubric gate, or ADR-0011's operator-tier red-line. This ADR is about the
  *mechanism* for approving changes to the system, not about who may approve
  payments or who may reach red-tier actions inside the product.
- Whether tier 1 (label + CI) should be tightened or loosened as a default —
  unchanged from its current `tbd_ship`/`pr_merge` behaviour.
