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

## Service accounts — the thing this document originally missed

Re-read 2026-09-12 at Mathias's insistence that the guidance here was wrong on
important points. It was. Source:
<https://www.fortnox.se/developer/blog/service-accounts>.

**Fortnox supports service accounts, and an unattended integration should use
one.** Quoting the vendor: activation with the service-account option connects
the access token to *"a custom-created user with a set of permissions suitable
for service-to-service integrations, thereby removing the dependency to a
specific person within the company."*

That is precisely cobalt-dingo's situation — a scheduled collector plus a
server, holding a refresh token that rotates and expires after 45 days. An
ordinary authorization binds the live books to whichever human approved it.

| | |
|---|---|
| Enable | Developer Portal, select **"Only administrator"** for the app |
| Request | `account_type=service` on the authorize redirect (the only valid value) |
| Who may authorize | **only a system administrator** of the customer |
| Limit | **one service account per client id per customer** |

Implemented as per-mode config (`FORTNOX_<MODE>_ACCOUNT_TYPE`), absent by
default. Absent rather than empty: an empty `account_type=` is undocumented,
and being ignored rather than rejected would yield a user-bound token that
looks like a service one — a difference invisible afterwards and costing a
re-authorization to correct. The sandbox stays a user authorization, because it
is already connected and switching it would buy nothing.

## One authorization is one company

The Fortnox consent flow selects **one** company. `apps.fortnox.se` shows a
company picker (*Välj företag*) before the consent dialog, and the token that
comes back is scoped to whichever company was picked. Nothing in the callback
says which one — `/3/companyinformation` is read with the fresh access token to
find out.

So **connecting several entities means running the flow once per entity**,
accumulating one token per company. cobalt-dingo stores them under
`<owner>:<mode>:<company>` and the page offers a "Connect another company" link
for each mode whether or not one is already connected. Before 2026-09-13 that
link was hidden as soon as a mode had any company, which left no way through
the UI to add a second.

### The organisation number is not a company identifier

One Fortnox account can hold several companies sharing one organisation number
— observed live with three under `556836-0688`, two of them identically named
"Definitely Mabe AB" (Fortnox IDs 1 and 3) plus "TEST Cobalt Dingo". The Fortnox
UI distinguishes them by an internal ID that `/3/companyinformation` does not
return; that endpoint gives `CompanyName` and `OrganizationNumber` only.

Since the tenant key is the organisation number, two such companies collide.
The callback therefore **refuses with 409** when the key is already held by a
differently-named company, rather than replacing its token. Renaming a company
in Fortnox trips the same guard, and the message says to disconnect and
reconnect. A real discriminator is #87.

## Which books am I actually touching?

**There is no Fortnox sandbox.** `FORTNOX_MODE` selects which OAuth credentials
this process uses; it does not select an environment. `config.Fortnox.BaseURL`
returns `https://api.fortnox.se` for both modes, and says so in a comment:
*"Sandbox and live use the same host — the environment is distinguished by the
OAuth credentials, not the URL."*

So the only thing separating test books from real books is **which company was
picked on the Fortnox consent screen**. That makes the picker a safety control,
and it is worth knowing what it looks like: a Fortnox Developer licence creates
test companies *under the licence holder's own organisation number*, so the
list shows the production company and its test companies adjacently, sharing a
number, sometimes sharing a name.

### Why the mode label is not a safeguard

- `FORTNOX_MODE=sandbox` set `AllowsWrites = true` **unconditionally** until
  2026-09-13, so sandbox was the *writable* mode — including in the deployed,
  internet-facing, multi-tenant server. Writes are now opt-in per mode via
  `FORTNOX_SANDBOX_ALLOW_WRITES` / `FORTNOX_PRODUCTION_ALLOW_WRITES`, both
  default-off, both requiring exactly `"true"`.
- `cmd/e2e-seed` and `cmd/e2e-teardown` build clients with `readOnly=false` and
  create, cancel and delete supplier invoices, customers, projects and assets.
- Both guarded only on `cfg.Mode != config.ModeSandbox` — a string in the
  environment. (They build their clients directly with `readOnly=false` rather
  than from `AllowsWrites`, so they still work with writes opt-in off; what
  gates them now is the company check.)
- The tenant key, the token filename and the log line all carry the mode and
  the organisation number. With test companies under the licence holder's
  number, **none of them can tell you which company you are about to write to.**

One mis-click on the consent screen therefore put a production token where a
test token was meant to go, and every downstream check agreed it was sandbox.

### The control

`fortnox.AssertCompany(baseURL, token, expected)` asks Fortnox which company a
token opens and refuses unless it matches, using a read-only client so the
check cannot itself write. `cmd/e2e-seed` and `cmd/e2e-teardown` call it before
constructing a writable client, against `FORTNOX_E2E_COMPANY`.

It fails closed three ways: unset expectation, unreadable company, any name
mismatch. An unset variable is an error rather than "allow anything", because a
forgotten variable would silently restore the original hazard.

**Set `FORTNOX_E2E_COMPANY` in your local `.env`** to the exact name Fortnox
reports for the test company — currently `TEST Cobalt Dingo`. CI writes it
alongside the other sandbox values.

## Three corrections to what this document said before

1. **The licence is a step, not a possible prompt.** This file called it an
   open question and said to treat a licence prompt as "expected rather than a
   fault". The FAQ is direct: *"The customer must log in to Fortnox and order
   an integration licence"*, via **Tilläggsbeställning** / Manage users. And
   for a hidden integration, *"customers can use your Client ID to find it in
   Fortnox"*. So the real order is: find by Client ID → order the licence →
   authorize.

2. **One redirect URI, not several.** The docs say `redirect_uri` *"Must match
   the Redirect URI for the app set in the Developer Portal. If omitted, it
   will default to the registered Redirect URI"* — singular throughout. Advice
   given earlier to register both a localhost callback and the production one
   was wrong.

3. **`state` is a required parameter**, not optional decoration. Worth noting
   because this repo shipped `state` carrying only the mode, with no CSRF value
   in it at all (#83, fixed).

## Verified unchanged

- 27 scopes, no read-only variant, and all six in the recommended grant exist.
- Token lifetimes: authorization code 10 minutes, access token 1 hour, refresh
  token 45 days, rotating on use.
- Token endpoint: `POST https://apps.fortnox.se/oauth-v1/token`, **HTTP Basic**
  with base64 `client_id:client_secret`, `application/x-www-form-urlencoded`
  body of `grant_type`, `code`, `redirect_uri`. The implementation matches.
- Authorize endpoint and parameters as implemented.
- Scopes are URL-encoded, space-delimited, case-sensitive.

## What this changes

- **#50** is not gated on Fortnox approval. Create a private integration, pick
  the grant, connect. Be ready for a licence step in the Fortnox UI.
- **#43** (refresh-conflict testability) is upgraded by #57 landing on option C.
- Scope changes require re-authorization, so the grant decision is made once,
  before connecting — not iterated.
