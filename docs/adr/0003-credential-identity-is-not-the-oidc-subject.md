---
adr:           0003
title:         "Downstream credentials are keyed by an internal user ID, never by the OIDC subject"
status:        accepted
date:          2026-09-08
deciders:      Mathias
supersedes:    null
verify:        "scripts/assert-credential-key-stability.sh"
---

# ADR-0003 — Downstream credentials are keyed by an internal user ID, never by the OIDC subject

## Status

**Accepted by Mathias, 2026-09-08**, with the migration to be run **now** rather
than deferred.

That timing is part of the decision, not an implementation detail. The re-key
migration must execute while a session under the *current* OIDC subject still
exists; after the next Authentik change of any kind — `sub_mode`, a recreated
provider, a new IdP — the old-key-to-user mapping is gone and the existing
Fortnox token is unrecoverable, leaving re-authorization as the only path. The
alternative offered was "accept the decision, migrate later", and it was
explicitly not taken.

## Context

`internal/auth/session.go` keys the Fortnox token store by the OIDC subject:

```go
func (s Session) TenantID() string { return s.Sub + ":" + string(s.Mode) }
```

**This has already failed once, in production, silently.** Migrating the OIDC
client from Dex to Authentik changed the subject for the same human. Login,
discovery, callback and middleware were all green; `/invoices` rendered and then
reported `failed to load invoices from Fortnox`, with `load token: no token for
tenant <64-hex>:sandbox` in the logs. The credential was not deleted — it was
orphaned under a key nobody would ever compute again. Recorded in the brain as
`idp-migration-orphans-subject-keyed-tokens`.

The OIDC subject is **provider-scoped and deliberately opaque**. Authentik's
`sub_mode` alone (`hashed_user_id`, `user_uuid`, `user_email`) changes its shape,
and recreating a provider can change it without any migration being involved. It
is a correct identifier *within one issuer's lifetime* and nothing more.

### Why this becomes more urgent now

Today the orphaning surfaces as a broken invoice page — bad, but visibly bad.

The planned credential UI (#47) changes the failure mode. It would look up the
credential, find none under the new key, render **"Not connected"**, and offer a
Connect button. A reconnect then writes a second token under the new key while
the first stays orphaned — and the UI reports success throughout. The user is
told everything is fine, twice, while credential state quietly forks.

**A credential console must never be able to mistake "we lost your key" for "you
never had one."** That distinction is impossible to draw when the key itself is
the thing that changed.

## Decision

**Credentials are keyed by an internal user ID that this application owns.** The
ID is minted at first login and resolved thereafter from the verified email
claim. The OIDC subject is recorded against the user for audit and debugging,
and is never used as a lookup key for stored credentials.

`Session.TenantID()` stops deriving from `Sub`.

Three properties make this worth the migration:

1. **Issuer changes stop being credential-affecting events.** Swapping IdP,
   changing `sub_mode`, or recreating a provider becomes a login concern only.
2. **A lost credential is distinguishable from an absent one.** The user record
   persists across the change, so the UI can say "your Fortnox connection needs
   re-authorising" rather than "not connected" — which is both true and
   actionable, and does not invite a silent fork.
3. **It is the seam a tenant model attaches to later** (#46), rather than
   `sub` becoming load-bearing in a second place.

### Why not verified email directly

Email is the obvious candidate and is what the brain entry recommends as "best
long-term". Rejected as the *stored* key, though used as the resolution input:
email addresses change, and when one does, every credential row keyed by it
needs rewriting — the same migration this ADR exists to avoid, deferred rather
than removed. An internal ID resolved *from* email keeps the stable key stable
and makes an email change a one-row update.

### Why not accept the orphan and re-run OAuth each time

Genuinely viable for a single-user homelab app, and it is what happened last
time: reconnect once, move on. Rejected because the cost is not the reconnect,
it is that **the failure is silent and arrives at an unpredictable time** — the
next IdP change, months later, with no warning and a UI that says everything is
fine. Paying a bounded migration cost once to remove a recurring silent failure
is the trade this ADR takes.

## Consequences

**Positive.** The already-observed failure cannot recur. The credential UI can
state credential status truthfully. Identity provider work stops being coupled
to the money path.

**Negative / watch.**

- **A migration is required for the existing token**, and it is the risky part
  of this decision, not the code. The current token sits under a `sub`-derived
  key; it must be re-keyed to the new user ID. Getting this wrong orphans the
  credential in exactly the way this ADR is about. It should be a one-shot
  migration with the old key logged, and it must be run while a session under
  the *current* `sub` still exists — after another IdP change, the mapping is
  gone and reconnect is the only path left.
- **First login now writes**, so an unauthenticated-but-authenticated-looking
  request path creates a user row. Mint only after full token verification.
- **The email claim must be verified.** Keying on an unverified email is worse
  than keying on `sub`, because it is attacker-influenceable rather than merely
  unstable. If Authentik's claim cannot be trusted as verified, this ADR's
  premise fails and it should be superseded rather than worked around.
- **Mode stays in the key** (`:sandbox` / `:production`). That part of the
  current scheme is correct and deliberately unchanged — sandbox and production
  credentials must never resolve to each other.

## Validation

`verify: scripts/assert-credential-key-stability.sh`, which asserts the end
state: no credential lookup path derives its key from the OIDC subject.

Behavioural, not vocabulary-based — it asserts on the call, not on the word
`sub` appearing (ADR-0020 records a false positive where a "no X integration"
assertion matched the comments explaining the absence of X).

**RED until Phase B lands**, because `Session.TenantID()` currently does exactly
what the decision forbids. It also fails closed if the regression test is absent.

The test that matters, and the one the assertion depends on: **feed the same
verified email with two different subjects and assert both resolve to the same
credential owner with the stored token intact.** That is the Dex→Authentik
incident reproduced. An implementation that has not been run against that case
is unproven no matter how it reads.

**Revisit trigger.** If cobalt-dingo becomes multi-tenant (#46), the "one user
owns the credentials" assumption here is replaced by a tenant model, and this
ADR should be superseded rather than stretched to cover organisations.
