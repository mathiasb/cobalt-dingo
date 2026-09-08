# How Fortnox apps and integrations actually work

Read from the Fortnox developer portal on **2026-09-08** and recorded here so the
next person planning #50, #46 or any token work does not have to re-derive it.
Every claim below is from the vendor's own documentation, not from memory —
this repo has already been bitten twice by a premise nobody checked.

Sources, all under <https://www.fortnox.se/developer/>:
`checklist`, `developer-portal`, `authorization`,
`guides-and-good-to-know/scopes`, `guides-and-good-to-know/rate-limits-for-fortnox-api`,
`guides-and-good-to-know/pricing-models`, `faq`.

## Private vs public integrations — the thing that unblocks #50

**A private integration works immediately. No Fortnox review, no partner
agreement, no marketplace listing.** It stays hidden and is activated by a
customer using the Client ID directly.

The checklist has seven steps. Steps 1–6 apply to everyone:

1. Register as a developer
2. Create the integration (name + scopes) in the Developer Portal, reached from
   the Menu inside a normal Fortnox account
3. Create a test database — up to **30 simultaneous** test environments,
   administered exactly like live companies
4. Connect the integration to an environment (configure redirect URI, get an
   authorization code)
5. Authorize via OAuth2 Authorization Code Flow
6. Make requests with the access token

**Step 7 — Publish — is public-integrations only.** Publishing requires
requesting publication, approving the Fortnox integration partner agreement, and
passing a Fortnox review. cobalt-dingo's production app for Mathias's own
company is a *private* integration and skips all of it.

This matters because #50 was written as if it might be gated on a Fortnox
approval process. It is not.

## What an integration record contains

Name, scopes, redirect URIs, category, launch/landing page URL, user-agreement
link, logo/icon/cover images, and a **pricing model** — either "Buy via Fortnox"
(monthly per-user billing that Fortnox administers, against a commission) or
"Buy via Website" (your own pricing text). Also whether a customer needs one
licence total or **one licence per user**.

The redirect URI does double duty: it is both the OAuth callback *and* the
landing page where a customer activates the integration.

**Open question, not answered by the docs:** whether a purely private
integration used on your own company incurs any cost or licence requirement.
The FAQ says "the customer must log in to Fortnox and order an integration
licence" via *Tilläggsbeställning* / *Manage users*, but does not distinguish
private from purchasable integrations. The pricing page defers specifics to a
Fortnox partner manager. **Assume a licence step may be required and be ready to
find it in the Fortnox UI**; do not treat #50 as blocked if a licence prompt
appears — it is expected, not a fault. This is the one thing here worth
confirming empirically rather than from documentation.

## Scopes

See ADR-0001's 2026-09-08 amendment for the decision consequences. The facts:

- **27 scopes.** There is no read-only variant of any of them: *"All scopes gives
  both read and write access to an endpoint and it is not possible to only have
  read access through the API."*
- Scopes are requested by the app, shown on the consent screen, and the issued
  token is limited to what was granted.
- **Changing an app's scopes does not upgrade existing tokens.** Per the FAQ, you
  must *"create new authorization-codes to get a new Access-Token with the new
  access"* — i.e. re-run the authorization flow. Plan the grant before
  connecting, not after.

Non-obvious mappings:

| Resource | Scope |
|---|---|
| SIE (`/3/sie/4`, used by assumption A1) | `bookkeeping` |
| Vouchers, Accounts, Financial Years | `bookkeeping` |
| Invoice Payments | `payment` |
| Supplier Invoice Payments | `payment` |
| Company Information | `companyinformation` |

The two `payment` rows are why `payment` is the scope to withhold: it is the one
that moves money.

## Token lifetimes — the operational hazard

| Token | Lifetime |
|---|---|
| Authorization code | **10 minutes** |
| Access token | **1 hour** |
| Refresh token | **45 days** |

**Refresh tokens rotate.** Using one issues a new refresh token and *"the old
Refresh Token then becomes invalid."*

Two consequences this repo must handle, and one it currently does not:

1. **Rotation makes concurrent refresh a real race.** Two processes holding the
   same refresh token — `cmd/server` and `cmd/mcp`, or either plus a scheduled
   job — will have one succeed and the other permanently invalidated. This is
   the branch issue #43 exists to make testable, and it is not hypothetical
   given #57 option C puts a scheduled collector beside the server.

2. **45 days of inactivity kills the connection.** Refresh happens on demand,
   when a request needs a token (`internal/fortnox/auth.go`). A read-only
   reconciliation app that nobody opens for a month and a half silently loses
   its production connection, and recovery is a full re-authentication — which,
   for a private integration, means a human at a browser. Tracked separately.

If the refresh token expires, per the docs: *"the user needs to re-authenticate
and we start again at authentication."*

## Rate limits

- **300 requests per minute, per client-id and tenant.**
- Enforced as a **sliding window of 5 seconds — effectively 25 requests per 5
  seconds.** A burst is not smoothed over the minute; it throttles until the
  average comes back down.
- Scoped **per access token and tenant, not per IP**, so each tenant on a
  multi-tenant deployment (#46) gets its own allowance.
- Exceeding returns **HTTP 429**. The documentation does not specify a
  `Retry-After` header, so do not depend on one.

The 25-per-5-seconds shape is the number that matters for any batch read — SIE
export plus per-supplier invoice fetches will hit it long before 300/min looks
close.

## What this changes

- **#50** is not gated on Fortnox approval. Create a private integration, pick
  the grant, connect. Be ready for a licence step in the Fortnox UI.
- **#43** (refresh-conflict testability) is upgraded by #57 landing on option C.
- Scope changes require re-authorization, so the grant decision is made once,
  before connecting — not iterated.
