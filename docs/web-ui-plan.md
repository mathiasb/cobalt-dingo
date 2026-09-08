# Plan — a web UI where credentials are managed as a logged-in Authentik user

**Status:** plan, not started. Written 2026-09-08 against `main` at v0.17.1.
**Tracker:** #47 (the UI), #31 (step-up decision), #57 (deployment decision).
**Decisions this depends on:** [ADR-0003](adr/0003-credential-identity-is-not-the-oidc-subject.md),
[ADR-0004](adr/0004-one-authenticated-front-end.md) — both `proposed`.

Goal: connect Fortnox, see token health, rotate and revoke API keys, and read
the audit log — in a browser, signed in through Authentik, with no terminal.

---

## What already exists

Verified by reading the tree, not the issues. #47's body predates the coo-agent
merge and describes work that partly arrived with it.

There are **two web UIs in this repo**:

| | `internal/ui` | `internal/receipts/httpserver/ui` |
|---|---|---|
| Origin | cobalt-dingo | coo-agent, via `infra` ADR-0021 |
| Deploy unit | `cmd/server` → `books.d-ma.be` | `cmd/receipts` — **not deployed anywhere** |
| Auth | Authentik OIDC + session middleware | **none at all** |
| Pages | invoices, chat, Fortnox connect/status | status, auth, keys, audit |
| Data | live | `ui/stub` fakes |

So most of #47 is already built: `handler.go`, five `.templ` templates, tests,
and routes that match the issue's specification almost exactly. What it lacks is
authentication, real data, and any connection to the signed-in user.

**This plan is therefore mostly about wiring and identity, not about building a
UI from scratch.**

---

## The three problems

### P1 — the admin UI has no authentication

`internal/receipts/httpserver/server.go` mounts the UI with no middleware. Not
Basic Auth, not an API key, despite #47's security section requiring one. The
routes it exposes:

```
GET    /ui/keys          list API keys
POST   /ui/keys          create one
DELETE /ui/keys/{id}     revoke one
POST   /ui/token/refresh refresh the Fortnox OAuth token
DELETE /ui/token         revoke it
GET    /ui/audit         read the audit log
```

That is a credential console. **It is not currently exposed** — `cmd/receipts`
has no Deployment, Service or Ingress anywhere on the cluster (checked). Only
`cobalt-dingo` is deployed, at `books.d-ma.be`.

The risk is #57, which is an open decision about how to deploy the
receipt-collector. Deploying this as it stands publishes an unauthenticated
credential console. **Phase A closes that door before the decision is taken**,
rather than relying on whoever takes it remembering this.

### P2 — credentials are keyed by the OIDC subject

`internal/auth/session.go`:

```go
func (s Session) TenantID() string { return s.Sub + ":" + string(s.Mode) }
```

The Fortnox token store is keyed by that. **This has already caused one silent
outage**: the Dex → Authentik migration changed `sub`, login stayed completely
green, and the Fortnox integration broke with `no token for tenant <64-hex>:sandbox`
(brain: `idp-migration-orphans-subject-keyed-tokens`). The OIDC subject is
provider-scoped and not portable.

Building a *credential management* UI on this key makes the failure worse rather
than more visible. Today it surfaces as a broken invoice page. With this UI it
would render "Not connected" and offer a **Connect** button — inviting a
reconnect that writes a second token under the new key and orphans the first,
silently, with the UI reporting success. A credential console must never be able
to mistake "we lost your key" for "you never had one".

Addressed by ADR-0003.

### P3 — no step-up authentication

`internal/auth/oidc.go` builds the authorize URL as `AuthCodeURL(state)` — no
`prompt`, no `max_age`. Authentik's `default-authentication-login` session is
**30 days, persistent, and brand-global** (#31): there is no per-provider
authentication flow, so the long session applies to cobalt-dingo too.

Consequence: anyone holding a 30-day Authentik session on any device can rotate
or revoke money-path credentials with no re-authentication. Browsing invoices
under a long session is defensible. Revoking a payment credential is not.

Addressed by Phase D. The mechanism is entirely in this repo — `max_age` on our
own authorize request — which is what #31 already identified.

---

## Target shape

**One authenticated front end**, in `internal/ui`, behind the existing OIDC
middleware. The admin pages move there; the receipts HTTP server keeps only its
`/health` probe. See ADR-0004 for why, including why ADR-0021's fault-isolation
argument does not extend to a second admin console.

Rejected alternative: put an Authentik forward-auth outpost in front of the
receipts server. It answers "is this request authenticated" but cannot express
"re-authenticate for *this* action" — `max_age` has to be in the application's
own authorize request. Forward-auth would solve P1 and leave P3 unsolvable.

---

## Phases

Ordered so the security holes close before the features land.

### Phase A — fail closed on unauthenticated admin routes

Smallest possible change, done first and independently of everything else.

`httpserver.New` returns an error unless an authenticator is configured, so an
unauthenticated admin console cannot be started even by accident.

**Done when:** a test asserts `New(Config{})` with no authenticator returns an
error and binds no listener; `cmd/receipts` fails loudly at startup rather than
serving. This is deliberately a startup failure, not a config default — a
default can be overridden by someone who does not know what it protects.

### Phase B — stable credential identity (ADR-0003)

Introduce an internal user ID, resolved from the verified email at first login
and stored, with the OIDC subject recorded but not used as the key. Migrate the
existing token.

**Done when:** a test proves that changing `sub` for the same verified email
resolves to the same credential owner and does not orphan a stored token — i.e.
the exact Dex→Authentik failure, reproduced and prevented. Until that test
exists, this is unproven regardless of how the code reads.

### Phase C — fold the admin pages in behind OIDC

Move `status`, `auth`, `keys`, `audit` into `internal/ui`, wire them to the real
stores instead of `ui/stub`, and mount under the existing session middleware.

**Done when:** an unauthenticated request to any `/ui/*` route redirects to
Authentik, asserted by a table test over every route — including the ones added
after this plan, so use a route-enumeration test rather than a per-route list.

### Phase D — step-up on credential operations (resolves #31)

Split routes into two classes and apply `max_age` to the sensitive one:

| Class | Routes | Policy |
|---|---|---|
| Read | invoices, chat, status, audit | long session, as today |
| **Credential** | connect/disconnect Fortnox, token refresh/revoke, key create/revoke | re-auth if the IdP session is older than the step-up TTL |

**Recommended TTL: 15 minutes.** Long enough that a working session doesn't
re-prompt mid-task; short enough that a borrowed or forgotten browser is not a
credential console. `prompt=login` on every credential action was considered and
rejected as too aggressive — it trains the user to click through auth prompts,
which is worse than a 15-minute window.

**Done when:** a fresh session performs a credential operation without a prompt,
and a session older than the TTL is redirected to re-authenticate. Testable
without a real IdP by asserting the constructed authorize URL carries
`max_age`, plus a handler test with an aged `auth_time`.

### Phase E — real data behind the admin pages

API key store with keys hashed at rest and plaintext shown exactly once; audit
log reads; live health checks replacing the stubs.

**Done when:** a created key's plaintext appears in exactly one response and is
not retrievable afterwards, asserted by a test that re-fetches and checks.

---

## Security notes that are easy to get wrong

- **Never log or render a token or key value**, including in an audit entry.
  The audit log records that a key was created and which label — never the key.
  The estate's rule is that tool output is persisted forever; the same applies
  to an audit table.
- **A key's plaintext exists once, in one response.** If that response is lost,
  the key is regenerated, not recovered.
- **CSRF:** HTMX posts need a token; `SameSite=Strict` on the session cookie is
  necessary but not sufficient once there are `DELETE` routes.
- **The disconnect button is destructive and reversible only by re-running
  OAuth.** It needs a confirmation step, and per ADR-0003 it must distinguish
  "disconnect this credential" from "I cannot find your credential".

---

## What this plan does not cover

Multi-tenant (#46) — every route here assumes a single signed-in owner. The
credential identity in ADR-0003 is deliberately the seam a tenant model would
later attach to, but building it is out of scope.

Role-based access. One admin user, as #47 already scoped.

---

## Open questions

1. **Step-up TTL** — 15 minutes is a recommendation, not a measurement. It is
   cheap to change and worth revisiting after a week of real use.
2. **Where the internal user ID lives** — postgres alongside the token store is
   the obvious answer; it becomes load-bearing for ADR-0003's migration.
3. **Does `books.d-ma.be` stay the only hostname?** If the admin UI folds in,
   yes, and the receipts deploy unit (#57) never needs an ingress at all — which
   simplifies that decision considerably.
