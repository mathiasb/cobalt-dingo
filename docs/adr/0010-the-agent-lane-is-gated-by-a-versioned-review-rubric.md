---
adr:           "0010"
title:         The agent lane is gated by an LLM review against a versioned rubric; the human lane is advised by the same reviewer
status:        accepted
date:          2026-09-17
deciders:      Mathias
supersedes:    null
---

# ADR-0010 — The agent lane is gated by an LLM review against a versioned rubric; the human lane is advised by the same reviewer

## Status

Accepted 2026-09-17 by Mathias, answering two questions put together because the
first is unimplementable without the second: what authority an LLM reviewer gets,
and whether `main` is protected at all.

It resolves, for this repo, the fork recorded in the brain as
`homelab/hypotheses/semantic-gate-authority-fork` (filed 2026-07-23, undecided for
56 days), which named two opposite live precedents: `jonte888/FlowBrain`'s
`governance-review`, which can `REQUEST_CHANGES` and block, and
`mathias/brain-gardener`'s Judge, which is confirmation-only with zero blast
radius. The answer here is neither globally: authority is a property of the lane.

## Context

**Nothing reviews what the agent writes.** `agent-run.yml` opens a PR and its body
says "a human reviews, merges and closes — this workflow does none of those".
Under `ADR-0009` that human is one person, and ADR-0009's own first negative
consequence is that "personal tooling has no external forcing function. Nothing
outside will catch a regression or an unfinished path." This is the missing
function, and the gap is widest exactly where the author is a model.

**A stated control did not exist.** Measured 2026-09-17 via the Gitea API:

```
mathias/cobalt-dingo  main → {"protected": false, "required_approvals": 0}
mathias/dispatch      main → {"protected": false, ...}
mathias/hyperguild    main → {"protected": false, ...}
```

So `agent-run.yml:22` — "Agent edits to workflows or tests now arrive as a
reviewable diff **behind branch protection**" — was false. What held the line up
was the `Assert origin/main did not move` step, which is a **detector, not a
preventer**: it caught both real escapes (runs 5228 and 5915) after the fact and
failed the job. Valuable, and not a wall.

This is the same finding this estate levelled at `erp-mafia/accounted` a day
earlier — a control correctly identified, honestly documented, not switched on —
except that theirs at least enforces linear history, no force-push and signed
commits, and ours enforced nothing.

**The reference implementation is legible and partly inapplicable.** accounted runs
a two-stage LLM compliance reviewer (`pull_request` → `workflow_run`) against a
bundled, pinned skills package, posting a sticky comment; it has produced real
requirement changes (their PR #1340 removed a rule, #1598 escalated findings to
fix-with-rollout) and one finding triaged *satisfied-by-design with the reasoning
recorded*. The two-stage split exists solely to keep model credentials away from a
**fork's** head. We have no forks.

## Decision

**One reviewer, three authorities, matched to who is actually watching.**

| Lane | Trigger | Authority |
|---|---|---|
| Agent | PR from `agent/*` (all of `agent-run.yml`'s output) | **Blocks.** Findings at or above `high` fail the check |
| Human | Mathias's own `risk/medium`+ PRs | **Advises.** Sticky comment, job stays green |
| Any lane, broken reviewer | model returned nothing usable | **Always fails.** Exit 2 is not a lane question |

And: **`main` gains branch protection with a push whitelist of Mathias only.**
Trunk-based development is unchanged for him — behaviour rule 5's `risk/low` lane
still commits straight to main. What changes is that the agent's deploy key
cannot, which makes "THE AGENT NEVER PUSHES" structurally true rather than
policy-true.

Four properties carry the design, each answering a specific way this fails:

1. **Severity comes from `.review/rubric.md`, never from the model.** The diff is
   attacker-controlled text and the model reading it is the thing being asked to
   judge it. If the model graded its own findings, a diff could argue its way below
   the threshold. The model reports *which rule, where*; the rubric decides how
   much it matters. This is stricter than accounted, whose threshold is applied to
   a model-assigned severity.
2. **Fail closed, and distinguish "found nothing" from "produced nothing".** Only
   an explicit `{"findings":[]}` is a clean review. Empty output, prose, a missing
   key, or a rule the rubric does not define is an *unreviewed* PR and fails on
   every lane. accounted's own `workflow_run` reviewer went silent for ten
   consecutive PRs because "no output" exited 0.
3. **Single stage, but the trust split is real.** The job holds the model
   credential and a comment token, so it checks out the **base sha**, computes the
   diff with git plumbing only, and runs a binary compiled from the **base tree**.
   No code from the PR is built or executed. A PR editing the rubric or the
   reviewer is judged by the version on `main`.
4. **The gate can be answered, not only obeyed.** `review-accept: R-XXX-NN --
   reason` on the PR stops a rule blocking, and the acceptance plus its reason stay
   in the sticky comment. A reason is mandatory. Without this path a gate becomes
   either a rubber stamp or something you route around; accounted's
   satisfied-by-design triage is the behaviour being copied.

**Why not the alternatives.** *Advisory everywhere* is brain-gardener's shape and
has zero blast radius, but it enforces nothing, and accounted's own compliance gate
sitting at `severity_threshold_to_block: critical` — never tightened across four
edits to the file — is what advisory-forever looks like after six months.
*Blocking everywhere* is FlowBrain's shape; it would put false-positive friction on
Mathias's own `risk/low` work, which is most of the repo's throughput, and he is
the only person who could unblock himself, so the gate would teach him to bypass it.
*Copying accounted's two-stage split* imports a failure mode — invisible in the
checks list — in exchange for a fork-safety property we do not need.

**Why not their bot-only review with author self-merge.** It works at 1189 merges
in 7 months with zero red merges in the last 60, but it works because of their
loops and their two authors, not because of the gate: `protect-main` has **no
`required_status_checks` rule at all**, and `review:approved` is **0 across all
1189 merged PRs**. We are one person at a fraction of that throughput, so we get
the opposite trade: fewer, slower changes and therefore no excuse for the gate to
be behavioural.

## Consequences

**Positive:**

- The agent lane acquires a reviewer that is not the agent. `agent-run.yml`'s
  closing claim — CI is the ground-truth verdict — becomes true about the diff's
  content, not only its compilation.
- The rubric is a diff. A rule change is reviewable, attributable and revertable,
  and a finding cites a rule ID rather than a mood.
- `agent-run.yml:22` stops being false. The detector stays; it now backs a wall
  instead of substituting for one.
- The reviewer is a pure package with tests, so the part where correctness lives is
  verifiable without calling a model.

**Negative / watch:**

- **The model's judgment is uncalibrated.** `koala/qwen36-35b-a3b` is pinned and
  local (a diff can carry fixtures with real figures, and `R-DATA-01` applies to
  us as much as to what we review), but nothing yet establishes its
  false-positive or false-negative rate on this rubric. accounted answered this
  with `accounted-bench` — 103 tasks, suites scored separately, no blended score.
  We have no equivalent, and until we do the blocking authority rests on an
  unmeasured judge (**#113**). Watch for the first acceptance written to unblock a
  *correct* finding: that is the signal the rubric is wrong, not the model.
- **`R-TEST-01` will be the noisiest rule** and it is deliberately `high`. If it
  fires on diffs that genuinely need no test, fix the rule, not the threshold.
- **Truncation.** Diffs over 240 kB are cut and the comment says so. A large PR is
  therefore partially reviewed while looking reviewed; the note is the only thing
  preventing that misreading.
- **Required status checks are not wired yet** — see Validation. Until they are,
  a red review check is a visible signal on the PR, not a merge block (#112).
- Every PR now calls a model. Local, so the cost is latency and GPU contention on
  koala rather than money.

## Validation

- `internal/review` tests are the acceptance criteria for the decision logic:
  `TestParseFindings_FailsClosedOnNoOutput`,
  `TestParseFindings_SeverityComesFromTheRubricNotTheModel`,
  `TestBlocking_AcceptedRiskDowngradesButStaysVisible`,
  `TestParseRubric_TheShippedRubricParses`.
- **Not live yet, and the switch-on is Mathias's:** `enable_status_check` with the
  real check-context strings. Gitea's contexts cannot be guessed safely — a wrong
  string makes the gate either unenforced or everything unmergeable — so they are
  read off a real run first. Tracked as **#112**, whose acceptance criterion is a
  deliberate violation proving the block fires, not the setting being present. Per
  the estate's own definition of done, the expensive failure mode is
  built-but-never-initiated.
- The decision is working if, three months on, at least one agent PR has been
  blocked on a finding that was real, and at least one has been unblocked by a
  recorded acceptance. Zero of the first means the gate is scenery; zero of the
  second means the rubric is being treated as infallible, which it is not.
- It has failed if an acceptance appears with a reason like "not relevant" and no
  rubric change follows. The answer path is for answering findings, not for
  turning the gate off one rule at a time.
