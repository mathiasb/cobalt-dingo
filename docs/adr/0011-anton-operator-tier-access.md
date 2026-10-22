---
adr:           "0011"
title:         "Anton is an operator, not a developer: a second human user, scoped by trust tier, keyed the same way as the first"
status:        proposed
date:          2026-10-10
deciders:      Mathias
supersedes:    null
verify:        "scripts/assert-operator-scope-enforced.sh"
---

# ADR-0011 — Anton is an operator, not a developer: a second human user, scoped by trust tier, keyed the same way as the first

## Status

Proposed 2026-10-10 by Mathias. Not yet accepted — this is the first write-up,
offered for review before any of it is built. Nothing here should be
implemented until the Decision section is accepted or amended.

## Context

Every decision taken so far in this repo — ADR-0003, ADR-0007, the whole of
`docs/payment-authority-and-liability.md` — was written for **one human user**.
`internal/auth` resolves a verified email to an internal owner ID and mints one
on first login (ADR-0003), but nothing downstream of that ever asks "which
owner, and what is *this* owner allowed to do" — there is one owner, it is
Mathias, and every capability in the app is implicitly his.

Anton (son, just finished Ekonomiprogrammet at Tibble gymnasium) is going to
operate parts of the finance/admin workflow: reviewing what the receipt
collector routed, approving or flagging items in the overview, running the
known-good `.claude/commands` playbooks, reading the audit trail. He is not
going to write code, open PRs, or touch the agent lane. The existing
`agent/*` vs human-authored-PR split in ADR-0010 has no answer for this,
because it assumes every human actor is Mathias.

Two things make this a real design question rather than a config tweak:

1. **There is no multi-user concept to extend.** `Session`, `TenantID()`,
   `OwnerDirectory` all resolve to "a credential owner" singular. Adding Anton
   is not "add a role flag to the existing user" — it is the first time this
   codebase has had to distinguish *who* is logged in from *what they may do*.
2. **The trust-tier vocabulary already exists, elsewhere.** The green/yellow/red
   model referenced in `docs/payment-authority-and-liability.md` Section 2's actor
   table (payer / consultant / system / agent / operator) and the Definitely
   Mabe ways-of-working notes is the right shape for Anton — but it has never
   been attached to a second real person with a login, only used informally.

This ADR is scoped to **access and authority**, not to the receipts/overview
features Anton will actually use day to day. Those are unaffected by this
decision — Anton uses the same product Mathias does. What changes is what the
product lets him do once he is in it.

## Decision

**Anton gets his own OIDC login, resolved to his own owner ID through the
existing ADR-0003 machinery — no new identity mechanism. What is new is a
`Role` on the owner record, checked at the point of action, not at the point
of login.**

Concretely:

1. **One more Authentik user, nothing else new at the login layer.**
   `UserDirectory.ResolveOwner` already mints an owner ID per verified email
   with no assumption that there is only ever one. Anton's first login mints
   `u_<hex>` exactly the way Mathias's did. ADR-0003's email-verification
   requirement is unchanged and applies to him identically.

2. **A `role` column on the user row: `owner` or `operator`.** Mathias is
   `owner` (implicitly, by being the only row today — migrated explicitly
   rather than left to default). Anton is `operator`. This is the one piece
   of new schema this ADR requires.

3. **Trust tiers are a property of the action, not the user, and `operator`
   is a ceiling, not a grant.** Reusing the vocabulary already in use
   informally:

   - **Green** — read-only actions: viewing the overview, the invoice list,
     the audit log, running `read-only-reporter`-class commands. Available to
     `operator` with no extra friction, same as `owner`.
   - **Yellow** — actions with a real but recoverable effect: marking a
     receipt reviewed, running `book-expense` against a draft, flagging an
     overview anomaly as handled. Available to `operator`, but every yellow
     action stamps the acting owner on the row (the actor-attribution shape
     already specified in issue #105 for the payment path generalises directly
     here) and is listed in a place Mathias can audit without being in the
     loop for each one.
   - **Red** — anything ADR-0007 already reserves for a human approval bound
     to an artifact (batch approval), anything touching Skatteverket, and
     changing another user's role or credentials. **`operator` cannot reach
     red, full stop, at the authorization-check level — not hidden in the UI,
     refused at the handler.** This is the same shape ADR-0007 Section 2 already
     uses for agents ("approval is absent from the agent surface — not
     scope-gated"): a capability an `operator` role cannot reach cannot be
     granted to one later by a config change, it has to be a new role.

4. **The check lives at the API/handler boundary, as one `RequireTier`
   middleware/helper, not scattered per-handler.** Same lesson issue #105 already
   drew for actor attribution: set once, checked at the boundary, never
   threaded through by convention. A convention-based check is the thing
   ADR-0010's own context section calls out as proven not to hold in this
   estate (`agentsquad#47`: "behavior-shaping, never enforcement").

5. **Anton's role is visible on every page he can reach and on nothing he
   can't** — the UI should not render a red action as a disabled button for
   him; per ADR-0007's "absent from the surface" principle, if he cannot do
   it, it should not appear to be a maybe.

### What this does not change

- **Fortnox credentials stay keyed exactly as ADR-0003 specifies** —
  `owner:mode:company`. Anton's owner ID gets its own row in that scheme if
  and when he is given his own Fortnox connection; more likely, for v1, he
  operates against the **same** connected company Mathias already has, which
  means the credential itself is never issued to Anton at all — see
  Credential handling below.
- **ADR-0007's approval model is untouched.** Anton approving a payment batch
  is not on the table; he is not a signatory, there is no PoA, and nothing in
  this ADR creates one. If that ever changes it is a new decision, not an
  extension of this one.
- **The agent lane (ADR-0010) is unaffected.** Anton is a human operator, not
  an agent; he does not get agent-lane scopes, and the rubric gate's authority
  split is untouched.

## Credential handling: 1Password, not a second Fortnox grant

Anton does not need, and should not be given, his own Fortnox OAuth
connection for v1. He needs to be able to *act inside the product* — which
authenticates him via Authentik/OIDC as above — not to independently
authenticate against Fortnox.

Where a human-held secret genuinely has to reach him (an API key for a tool
that has no OIDC front door, a one-off token), the channel is a **dedicated
1Password shared vault**, scoped to exactly the items his operator tasks
need:

- Anton is already a family member in the shared 1Password account; this is
  a new **vault**, not a new account.
- The vault holds only green/yellow-tier credentials by construction —
  ADR-0001's "production is read-only" boundary and this ADR's "operator
  cannot reach red" boundary should agree with each other. Nothing
  Skatteverket-related, no production Fortnox client secret, ever lands in
  it.
- Each item in the vault is there because a named operator task requires it,
  not because it was convenient to share everything at once — the same
  discipline ADR-0001/0002 apply to scope generally.

This is explicitly **not** a replacement for the OIDC login above. The login
is how Anton is identified and tier-checked inside the product; the vault is
how the handful of credentials his tasks need (if any, once tasks are
enumerated) reach him outside it.

## Consequences

**Positive:**

- The multi-user seam ADR-0003 already predicted ("the seam a tenant model
  attaches to later") gets its first real attachment, and it is a narrow one
  — a role column and a tier check, not a rewrite of the credential or
  session model.
- Anton's capability ceiling is enforced at the same layer ADR-0007 already
  trusts for the much higher-stakes approval boundary, so this is reusing a
  proven pattern rather than inventing a second one.
- Nothing here blocks or is blocked by the payment-authority work; it is
  orthogonal, and deliberately so.

**Negative / watch:**

- **This is the first code in the repo that has to reason about two users at
  once.** Every place that currently assumes "the session owner" is "the only
  owner" needs auditing, not just the auth package — the audit log, the
  overview, anything that renders "you" rather than "this owner". That audit
  is real work and is not scoped in detail here.
- **Role is binary today (`owner`/`operator`).** If a third tier turns out to
  be needed (e.g. a future bookkeeper who is neither family nor Mathias), this
  will want revisiting rather than stretching `operator` to cover two
  different trust levels.
- **The vault's scope will drift unless reviewed.** A 1Password vault scoped
  correctly on day one accumulates secrets over time the same way any shared
  resource does. Worth a standing note (not a new mechanism) to re-check its
  contents against "could this reach `operator` tier" periodically.
- **Because `operator` cannot reach red at the handler level, a mistake in
  that enforcement is a security bug, not a UX bug.** It deserves the same
  test discipline as ADR-0007's approval-absence requirement
  (`TestMCP_ApprovalToolAbsentFromCatalog` is the pattern to copy).

## Validation

- `TestOperatorRole_CannotReachRedTierHandler` — construct a session with
  `role=operator`, call a red-tier handler directly (not through the UI),
  assert refusal. Mirrors `TestMCP_ApprovalToolAbsentFromCatalog`'s shape:
  prove the surface is absent, not merely discouraged.
- `TestOperatorRole_YellowActionStampsActor` — an operator-performed yellow
  action persists the acting owner ID on the row, reusing whatever
  `domain.Actor` shape issue #105 lands on.
- `TestCredentialOwner_SecondRealUserDoesNotCollideWithFirst` — two distinct
  verified emails resolve to two distinct, stable owner IDs with independent
  role rows. Directly extends the existing
  `TestCredentialOwner_SurvivesSubjectChange` fixture rather than replacing it.
- `scripts/assert-operator-scope-enforced.sh` — behavioural, in the same style
  as the other `assert-*` scripts: attempts a red-tier action as an operator
  role and asserts it is refused end to end, not just that a function returns
  an error in a unit test.

**Revisit trigger:** a third distinct trust level is needed (observable: a
real candidate person who is neither `owner` nor a fit for today's
`operator` ceiling), or Anton's role needs to include a Fortnox connection of
his own rather than operating inside Mathias's — at which point ADR-0003's
tenant-key scheme needs a real second `owner:mode:company` row, not just a
second login.

## What this does not decide

- The specific list of yellow-tier actions Anton will actually use day to
  day — that is a product/UX scoping exercise against the existing
  `.claude/commands` and overview surfaces, not an access-control decision.
- Whether Anton ever becomes a payment approver. Not proposed here, and
  ADR-0007's reasoning about signatory authority and PoA would have to be
  revisited on its own terms if it ever is.
- Anything about Anton's employment, compensation, or role at Definitely
  Mabe AB outside of this product. Out of scope by construction.
