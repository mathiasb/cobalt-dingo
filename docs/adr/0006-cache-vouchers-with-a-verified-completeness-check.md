---
adr:           0006
title:         Cache Fortnox vouchers, and never serve a cached answer whose completeness was not checked
status:        accepted
date:          2026-09-14
deciders:      Mathias
supersedes:    null
---

# ADR-0006 — Cache Fortnox vouchers, and never serve a cached answer whose completeness was not checked

## Status

**Accepted by Mathias, 2026-09-14**, with the instruction that framed it:
*"add caching with real caution as you know cache invalidation is a massive
challenge."*

## Context

Fortnox's `GET /3/vouchers` returns vouchers without their rows, despite
declaring `VoucherRows` in the OpenAPI spec. Rows arrive only from the
per-voucher detail endpoint, so any row-level analysis costs **one request per
voucher** (#89, fixed in v0.43.0).

Measured on the production company: 405 vouchers in the 2026 financial year. At
the documented rate limit — 300 per minute, 25 per 5 seconds, and this client's
limiter is set to 18 for headroom — that is **roughly two minutes per year of
books**, paid on every call. Five chat tools sit on that path.

What makes this decision worth writing down is the day it was taken on. The
Fortnox integration produced **four** wrong answers about account 1930 in one
session, from four different causes: an unpaginated voucher list, an
unpaginated chart of accounts, voucher rows that were never populated, and a
carried-forward balance an open year never sets. Every one was well-formed,
plausible, and confidently wrong. Not one was an error.

A cache is a fifth opportunity to produce exactly that, and a worse one,
because a cache's answer looks identical whether it is complete or not.

## Decision

**Cache voucher rows, and make completeness a checked property of every read
rather than an assumption.**

Three things make this specific design acceptable:

### 1. The cached objects are immutable

A posted voucher cannot be edited or deleted. Swedish bookkeeping corrects by
adding a further voucher (`docs/irreversible-operations.md`). So the hazard is
**not stale content** — a cached voucher stays true. The hazard is an
**incomplete** cache reporting itself as whole.

That reframing is what makes the problem tractable. General cache invalidation
is hard; detecting an append-only set that has fallen behind is not.

### 2. Incompleteness is detectable in one request

Fortnox returns `@TotalResources` in the `MetaInformation` of any list
response. One request yields the authoritative count for a year, which is
compared against the cached count on every read.

**This is the load-bearing property of the whole design.** Without it, caching
financial data here would not be defensible and this ADR would not exist.

### 3. Three states, not two

`domain.VoucherCacheState.Assess` returns `fresh`, `stale`, or **`unverified`**,
plus a human-readable reason.

`unverified` exists because a binary usable/unusable has to fold "I could not
reach Fortnox" into one of them, and both foldings are wrong: serving silently
hides a vendor outage behind confident numbers, and refusing makes every report
depend on the vendor being up. The caller decides, and **must** surface the
as-of date. The same reasoning produced the three-state contract in
`infra/scripts/op-safe.sh` after a two-state version caused a duplicate secret.

### Additional guards, each with a reason

- **Fewer remote than cached is flagged as a contradiction**, not resynced
  away. It contradicts immutability, which means an assumption is wrong —
  resyncing silently would erase the only evidence.
- **An age backstop remains even when counts agree.** Immutability is an
  assumption about the vendor, not something we enforce; `lastmodified` is a
  documented parameter on `/3/vouchers`, so Fortnox itself contemplates a
  voucher changing.
- **Zero cannot mean unknown.** `RemoteTotalUnknown = -1`, because zero is a
  legitimate total for an empty year — and conflating the two is how an empty
  answer becomes a confident one.
- **The cache stores facts, not answers.** Voucher rows, never computed
  summaries. A cached summary cannot be re-derived when the question changes,
  and every wrong answer this week came from measuring something adjacent to
  the question.

## Consequences

**Positive:**

- Row-level analysis becomes usable interactively instead of costing two
  minutes.
- A refresh is incremental via `lastmodified`, so the steady-state cost is a
  few requests rather than 405.
- Every figure can state how it knows it is complete, and when it was checked.

**Negative / watch:**

- **One extra request per read**, to fetch `@TotalResources`. Deliberate: it is
  the price of the guarantee, and a design that skipped it to save a request
  would be the design this ADR rejects.
- **The immutability assumption is the foundation.** If Fortnox ever permits
  editing a posted voucher, counts would agree while content diverged, and only
  the age backstop would catch it. **Watch for `lastmodified` returning
  vouchers whose numbers are already cached** — that is the observable symptom.
- A cache of financial data is now a second place the books exist. It is
  derived and rebuildable, and must never be the source for anything a human
  acts on without the as-of date attached.
- The `unverified` state pushes a judgement onto callers. A caller that ignores
  it reintroduces the whole problem, silently.

## Validation

- **Proves out** when a row-level query returns in under a second with a stated
  as-of time, and a cache made deliberately incomplete is reported stale rather
  than served.
- **Kill condition, observable by someone who is not the author:** if any report
  presents a cached figure without its as-of date, the design has failed in
  practice whatever the code does — the point was never the cache, it was that
  an incomplete answer cannot pass as a complete one.
- **Revisit trigger:** `lastmodified` returning an already-cached voucher
  number, which would mean posted vouchers are mutable after all and this ADR's
  first premise is wrong.
