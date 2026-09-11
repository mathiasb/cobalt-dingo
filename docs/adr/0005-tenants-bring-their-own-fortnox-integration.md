---
adr:           0005
title:         Each tenant registers their own Fortnox integration; cobalt-dingo stores the credentials encrypted
status:        proposed
date:          2026-09-11
deciders:      "pending: Mathias"
supersedes:    null
---

# ADR-0005 — Each tenant registers their own Fortnox integration; cobalt-dingo stores the credentials encrypted

## Status

**Proposed.** The direction was chosen by Mathias on 2026-09-11 in conversation —
*"users bring their own"*, with the other users being *"a real SaaS product with
paying tenants"* — and the first piece is built (`internal/crypto`, AES-256-GCM).
This ADR exists because that choice has consequences well outside the storage
question and none of them were written down. It needs accepting or amending
before the per-tenant table lands.

## Context

cobalt-dingo talks to Fortnox through a **connected app**, identified by a client
id and client secret. Until now there has been exactly one such app per mode,
its credentials injected as environment variables from 1Password via ESO.

Two facts about Fortnox shape everything here (`docs/fortnox-integration-model.md`,
read from the vendor's own documentation on 2026-09-08):

1. **A private integration works immediately** — no Fortnox review, no partner
   agreement, no marketplace listing. It is activated by a customer using the
   Client ID directly.
2. **A public integration requires publication**: requesting it, approving the
   Fortnox integration partner agreement, and passing a Fortnox review, against
   a commission.

The multi-tenant question (#46) therefore has two shapes, and they differ in
who owns the Fortnox relationship rather than in how the code is written.

What forced the decision now: production credentials had to be put *somewhere*,
and the choice of where encodes an assumption about the multi-tenant future.

## Decision

**Each tenant registers their own private Fortnox integration and supplies its
client id and secret to cobalt-dingo, which stores them encrypted at rest.**
The app-level credentials in the vault remain as the fallback for the operator's
own use.

Resolution is a chain: a tenant's own integration record if present, otherwise
the app-level credentials from the environment. That makes the per-tenant store
additive — the existing vault path stays correct and needs no migration.

### Why not a single public integration

It is the obvious alternative and the conventional SaaS shape: one integration,
every customer authorizes it, one secret to rotate, no credential ever handled
by a customer.

It loses on the thing that is hardest to get back: **it puts Fortnox in the
approval path.** Publication requires their review and a partner agreement with
a commission, on their timetable. Bring-your-own sidesteps that entirely — a
tenant's private integration works the moment they create it.

It is also worth being explicit that the conventional option is *safer* in one
respect, and that this decision accepts that cost: see the first negative
consequence below.

### Why not keep everything in the vault

Because a per-tenant credential in a shared vault is not per-tenant. It would
mean N items provisioned by the operator for credentials the operator does not
own, with no path for a tenant to supply or revoke their own.

## Consequences

**Positive:**

- **No vendor in the approval path.** No Fortnox review, no partner agreement,
  no commission, no waiting.
- Each tenant's blast radius is their own integration. Revoking one tenant does
  not touch another, and a leaked client secret is scoped to one Fortnox
  account rather than all of them.
- A tenant can revoke access from their side, in Fortnox, without asking us.
- Single-tenant deployments need no database for this: the env path still works.

**Negative / watch:**

- **A client secret now transits an HTTP form and our code, and lands in our
  database.** That is a materially larger attack surface than a value which only
  ever existed in 1Password and pod memory. It is the real cost of this decision
  and the reason `internal/crypto` exists. The specific things to hold: the
  field is write-only and never rendered back; it is never logged; and the
  master key lives in AgentSecrets, not in the database or the repo.
- **Onboarding friction is real and permanent.** Every customer must create a
  developer integration inside their own Fortnox account and paste two values.
  That is a support burden and a conversion cost, and it is the thing most
  likely to make this decision look wrong later. **Watch for it in onboarding
  drop-off**, not in code.
- **One master key encrypts every tenant's secret.** Compromise of that key
  compromises all of them. Envelope encryption with per-row data keys is the
  upgrade; deliberately not built yet, and recorded in the package doc.
- Key rotation currently means re-encrypting every row. Acceptable at a handful
  of tenants, not at a thousand.

## Validation

- **Proves out** if a tenant can be onboarded end to end without any Fortnox
  involvement on our side, and their first authorization succeeds against their
  own integration.
- **Kill condition, and it is observable by someone who is not the author:** if
  onboarding drop-off is dominated by the two-values step, the friction has
  outweighed the independence and a public integration becomes worth Fortnox's
  review process. Measure it at the first ten onboardings.
- **Revisit trigger for the crypto:** more than ~50 tenants, or any compliance
  requirement naming key rotation, moves the single master key to envelope
  encryption.

## Open questions this ADR does not settle

1. **Egress of tenant financial data to an LLM.** cobalt-dingo registers a chat
   handler and `LLM_BASE_URL` is set. Read-only access stops the books being
   *changed*; it does nothing about them being *sent*. With paying tenants this
   makes us a data processor: a DPA, sub-processor disclosure and a documented
   lawful basis are required, and the operator's own standing rule is that
   client data never reaches cloud APIs. **This is a blocker for the SaaS track
   and is tracked separately — it is not resolved by this ADR.**
2. **Identity.** Per-tenant records must be keyed by the internal user id from
   [ADR-0003](0003-credential-identity-is-not-the-oidc-subject.md), not the OIDC
   subject. #67 is that work and should land first, or these rows inherit the
   problem ADR-0003 exists to fix.
3. Whether a tenant is a *user* or an *organisation* with several users. The
   current tenant key is `<sub>:<mode>:<company>`, which is per-user by
   construction — two colleagues at one company would hold separate
   connections. Fine now, wrong for a team plan.
