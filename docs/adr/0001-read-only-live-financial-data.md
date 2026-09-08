---
adr:           0001
title:         "Live company data is read-only; the write path stays sandbox-only until a later ADR opens it"
status:        proposed
date:          2026-09-08
deciders:      "pending: Mathias"
supersedes:    null
verify:        "scripts/assert-live-readonly.sh"
---

# ADR-0001 — Live company data is read-only; the write path stays sandbox-only until a later ADR opens it

## Status

**Proposed.** Drafted by an agent. Pointing this system at the live books of a
real company is a decision with financial and tax consequences, and is not an
agent's to take. Awaiting Mathias.

## Context

cobalt-dingo has been developed entirely against the Fortnox **sandbox**. The
goal now is to read the live company's real financial data — Fortnox, mail,
bank, cards, portfolio — without any possibility of writing to it.

### What already exists

Mode separation shipped in **v0.6.0** (2026-05-01) as `FORTNOX_MODE`, then with
the values `sandbox` and `real_readonly`. The value was later renamed to
`production`, with write capability moved behind an explicit
`FORTNOX_PRODUCTION_ALLOW_WRITES=true`. `internal/config/config.go` still
carries the note that `real_readwrite` is "reserved for a future milestone".
That rename is the lineage of this ADR: the read-only-live idea was decided
before, in a weaker form, and this record supersedes the reasoning rather than
the mechanism.

Three enforcement layers are already built:

1. **Connected-app OAuth scope**, configured in Fortnox's web console.
2. **Config** — `config.Fortnox.AllowsWrites`, which in `production` is false
   unless `FORTNOX_PRODUCTION_ALLOW_WRITES` is exactly `"true"`.
3. **Client-side gate** — `fortnox.Client.do` refuses any non-GET/HEAD request
   with `ErrReadOnlyClient` *before* any HTTP traffic leaves the process, and
   `internal/fortnox/client_test.go` proves both the refusal and that the test
   server sees zero bytes.

`cmd/e2e-seed` and `cmd/e2e-teardown` — the two binaries that create and delete
Fortnox data — each hard-refuse to run unless `cfg.Mode == ModeSandbox`. That
guard was verified present during this analysis, not assumed.

### The actual gap, which is not the gate

The gate is correct and tested. **The wiring is neither.**

`readOnly` is a bare positional `bool`, threaded by hand through eight adapter
constructors in a fat `main()` (`cmd/server/main.go:152` onward; the extraction
is tracked as issue #10):

```go
readOnly := !cfg.AllowsWrites
gl := adapterfortnox.NewGeneralLedgerAdapter(baseURL, tokenStore, readOnly)
// ...seven more, each taking the same anonymous trailing bool
```

Nothing in the type system distinguishes `NewGeneralLedgerAdapter(u, t, true)`
from `(u, t, false)`. A new adapter added by a future contributor — human or
agent — that forgets the parameter, or is handed a literal, silently acquires
write capability against the live company. There is no test at the wiring
level; every existing test constructs a client directly. The guarantee
currently rests on **ten call sites being individually correct, checked by
nobody**.

This is the estate's recurring failure mode, and it has a name in
`.context/AGENT.md`: a remembered convention is an unreliable control.

### What is not in scope of this decision

Writing is only one way to damage a live-data system. A read-only agent that
can be prompted into summarising the company's ledger to an external model is
an **egress** problem, governed by `infra` ADR-0014, not by this ADR. Recorded
here so the next reader does not mistake "read-only" for "safe".

## Decision

**cobalt-dingo may read live company data in `production` mode. No process in
this repository may hold a write-capable Fortnox client while
`Mode == production`, and that invariant is asserted by a test at the wiring
level, not maintained by convention.**

Concretely:

- `production` with `AllowsWrites=false` is the only supported live
  configuration. `FORTNOX_PRODUCTION_ALLOW_WRITES` is set nowhere — not in the
  Deployment, not in a Secret, not in a dotfile.
- The three existing layers stay. Defence in depth is deliberate: the OAuth
  scope is configured in a vendor web UI that nobody diffs and no CI reads, so
  it cannot be the only control.
- A new test builds the **full adapter set exactly as `cmd/server` builds it**,
  in production configuration, against an `httptest` server that records every
  request, and asserts the server receives zero non-GET requests. This is the
  test that fails when someone adds a ninth adapter and forgets the bool.
- Opening the live write path requires a **new ADR that supersedes this one**,
  not a configuration change.

### Why not rely on the Fortnox connected-app scope alone

It is the strongest control — Fortnox refuses the write regardless of what our
code does — and it would make layers 2 and 3 redundant. Rejected because it is
invisible to this repository: it lives in a vendor console, it is not in git,
CI cannot read it, and a scope silently widened during unrelated troubleshooting
leaves no trace here. A control that cannot be asserted from the repo cannot be
the only control.

## Consequences

**Positive.** The live-data guarantee becomes falsifiable rather than
asserted. Adding an adapter that forgets the flag turns CI red at the wiring
test rather than reaching the live company. The three layers degrade
independently.

**Negative / watch.**

- **Boolean blindness stays.** This ADR adds a test around the defect rather
  than removing it. The type-level fix — a `ReadOnly`/`Writable` capability type
  that cannot be constructed from `production` config — is the real repair, and
  is deliberately not taken now because it touches all eight adapters and rule 2
  says smallest change that works. **Watch for:** the second time someone wires
  an adapter wrong. Once is a slip; twice means the test caught what the type
  system should have.
- **Three layers means three places to get it wrong**, and two of them
  (the OAuth scope, the deployed configuration) are outside this repo's test
  reach. The assertion covers the deployed configuration; the OAuth scope
  remains genuinely unverifiable from here. Stated rather than papered over.
- **`production` is now a routine mode, not an exceptional one.** The banner in
  `cmd/fortnox-check` printing `PRODUCTION` stops being alarming, which is
  exactly when a mode confusion becomes possible. The token files are already
  mode-suffixed (`.fortnox-tokens-production.json`), which is the mitigation.

## Validation

`verify: scripts/assert-live-readonly.sh`, which exits non-zero unless **all**
of the following hold. Any missing precondition is RED, never "nothing to
check" (ADR-0020 constraint 4):

1. A production token exists in the configured token store.
2. `FORTNOX_PRODUCTION_ALLOW_WRITES` is absent from every deployed manifest and
   secret for this app.
3. The wiring test passes.

**This assertion is RED on the day this ADR is written**, because condition 1
is false — live production OAuth has never been performed (issue #50). That is
deliberate, and is the ADR-0020 constraint-1 check: an assertion that passes
before the decision is in force is the wrong assertion. It goes GREEN when
Phase 0 completes, and RED again the moment anyone enables writes.

**Revisit trigger.** This ADR is superseded, not amended, when a bookkeeping
workflow genuinely requires writing to the live company — the most likely
candidate is automated voucher creation, currently open as issue #45. At that
point the question is not "flip the flag" but "which specific endpoints, gated
how, confirmed by whom".
