---
adr:           0004
title:         "One authenticated web front end, in internal/ui; the receipts server serves only /health"
status:        accepted
date:          2026-09-08
deciders:      Mathias
supersedes:    null
verify:        "scripts/assert-single-front-end.sh"
---

# ADR-0004 — One authenticated web front end, in `internal/ui`; the receipts server serves only `/health`

## Status

**Accepted by Mathias, 2026-09-08** — the "move into `internal/ui`" form, not the
"delete the receipts server outright" variant that was also offered.

Decided alongside two related answers on the same day, which this ADR's
reasoning depends on:

- **#57 landed on option C** (shared `internal/receipts` core, exposed both as a
  standalone CLI and as a hyperguild skill). So the collector is heading toward
  deployment rather than staying shelved, which is what made this decision
  urgent rather than theoretical.
- **#31 landed on step-up authentication for sensitive actions.** That is the
  requirement this ADR cites to reject a forward-auth outpost, and it is now an
  agreed requirement rather than an anticipated one.

`internal/receipts/httpserver` keeps `/health` and nothing else. Note that #66
(merged as v0.22.0) already put the admin routes behind an authenticator as an
interim guard; that code is deliberately small enough to discard when the move
happens.

## Context

The coo-agent consumption (`infra` ADR-0021) brought a second web UI into this
repo. There are now two:

| | `internal/ui` | `internal/receipts/httpserver/ui` |
|---|---|---|
| Deploy unit | `cmd/server` → `books.d-ma.be` | `cmd/receipts` — not deployed |
| Auth | Authentik OIDC + session middleware | **none** |
| Pages | invoices, chat, Fortnox connect/status | status, auth, keys, audit |
| Data | live | `ui/stub` fakes |

The receipts admin UI exposes API key creation and revocation, OAuth token
refresh and revocation, and the audit log, with no authentication of any kind.
It is not currently reachable — `cmd/receipts` has no Deployment, Service or
Ingress on the cluster — but #57 is an open decision about deploying the
receipt-collector, and deploying this as it stands publishes an unauthenticated
credential console.

So a decision is needed before #57 is taken, not after.

## Decision

**The admin pages move into `internal/ui`, behind the existing OIDC session
middleware. `internal/receipts/httpserver` keeps `/health` and nothing else.**
One authenticated front end, one session implementation, one CSRF story, one
place where step-up policy is expressed.

### Why this does not contradict ADR-0021

ADR-0021 deliberately kept the two *runtimes* separate, so that "a receipts/IMAP
fault must not be able to take down production AP invoice processing." That
reasoning is sound and is not weakened here, because **it is about the collector
daemon, not about an admin console.**

The admin UI is not on the collector's failure path. It reports on state the
collector produces; it does not process mail. Serving those pages from
`cmd/server` leaves the collector exactly as isolated as ADR-0021 requires — the
`cmd/receipts-collector` daemon keeps its own deploy unit and its own blast
radius. What changes is only where a few read-mostly pages are rendered.

The distinction worth keeping: **isolate failure domains, not HTTP handlers.**

### Why not an Authentik forward-auth outpost in front of the receipts server

This is the cheapest option and would close the authentication hole in an
afternoon without touching Go code. Rejected for this surface specifically.

Forward-auth answers *"is this request authenticated?"*. It cannot express
*"re-authenticate for **this** action"*, because `max_age` and `prompt` belong in
the application's own authorize request. Authentik's authentication flow is
brand-global with a 30-day persistent session (#31), so a forward-auth-protected
credential console inherits that session wholesale and has no mechanism to
tighten it. It would solve P1 and make P3 unsolvable at that layer.

Step-up on credential operations is the requirement that decides this, and it
only exists inside the app.

### Why not add OIDC to the receipts server instead

Symmetrical and workable, and it keeps the pages where they already are — which
is a real advantage given they are already written and tested. Rejected because
it produces two independent session implementations guarding the same
credentials, and step-up policy would have to be specified, implemented and kept
in sync twice. Two implementations of an authorisation rule is one more than the
number that will stay correct.

## Consequences

**Positive.** One authentication path to reason about and to get right. Step-up
(#31) becomes expressible. `cmd/receipts` may not need an ingress at all, which
simplifies #57. The already-written templates and handler are reused rather than
rewritten.

**Negative / watch.**

- **The pages must be moved, and moved code is where the wiring bugs live.** The
  routes are currently mounted under `/ui` with stub data; the move must connect
  them to real stores and to the signed-in user at the same time. Two changes at
  once is precisely how a route ends up unauthenticated by accident, so the
  route-enumeration test in Phase C should land *before* the move, asserting
  redirect-to-Authentik for every registered route.
- **`internal/ui` grows**, and it already holds invoices and chat. If it becomes
  unwieldy, split it by concern behind the same middleware rather than by
  standing up a second server — the latter is what this ADR exists to prevent.
- **The receipts server becoming health-only makes it look vestigial**, and
  someone may reasonably ask why it exists at all. That is a fair question for
  #57 to answer, and this ADR does not pre-empt it: a collector that runs as a
  CronJob may not need an HTTP server, in which case the right end state is
  deleting it rather than shrinking it.

## Validation

`verify: scripts/assert-single-front-end.sh` — asserts the end state that
exactly one package registers authenticated HTML routes, and that
`internal/receipts/httpserver` registers no route other than `/health`.

**RED today**, because the receipts server currently mounts a full unauthenticated
admin UI. It goes green when Phase C completes and stays green as the check that
a second front end has not reappeared.
