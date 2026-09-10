# Changelog

All notable changes to cobalt-dingo are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
This project uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Pre-1.0 minor bumps (`0.x`) signal meaningful milestones. Breaking changes to
the domain model or port interfaces will be called out explicitly.

---

## [Unreleased]

---

## [0.25.0] — 2026-09-10

### Fixed

- **The receipt collector could only see mail nobody had touched** (#81). It
  searched `UNSEEN` in `INBOX`, so anything read or archived was invisible.
  Three real Hetzner invoices were reported as non-existent because of it — one
  was on screen while the agent asserted it was not there. Now searches All Mail
  with no `\Seen` filter.

  The substantive change is separating two fused meanings: `\Seen` is the
  *user's read state*, and using it as the collector's work queue meant marking
  a receipt handled also marked it read, and refusing to look at read mail meant
  never revisiting anything. Processed-ness now lives in its own label
  (`cobalt-dingo/collected`), keyed by Message-ID because UIDs change when Gmail
  re-labels.

- **The collector could not run against a real backlog** (#79). Unbounded
  `UNSEEN` plus full body fetches meant 41,000 messages and gigabytes against
  Gmail's 2,500 MB/day IMAP ceiling; the first dry run was killed after ten
  minutes. Now bounded by `SINCE` with a per-run cap that **refuses rather than
  truncates**, and envelopes are fetched before bodies — 2,854 body fetches
  became fourteen.

- **Six non-receipts were being routed into bookkeeping.** Widening rules by
  sender in 0.24.0 fixed a 46% miss rate and introduced the opposite error:
  Anthropic, 1Password and Berget send security alerts from the same addresses
  as their invoices. Drop rules now shield them, plus subject constraints on the
  three sender-only rules. Failed payments and upcoming charges are also dropped
  — neither is an expense that exists.

- **A stale Fortnox inbox address had received ten supplier invoices** (#80).
  Mail to a dead `arkivplats.se` address does not bounce; the invoices simply
  never appeared in Fortnox.

- **`validateDate` rejected same-day vouchers for two hours every night.** It
  compared a voucher *date* against `time.Now()` as an *instant*, so a date-only
  value read as the future while UTC was still on the previous day. Latent for
  an unknown period; found only because the gate ran after local midnight.

  The first fix normalised both sides to a UTC calendar day, which passed
  locally (UTC+2) and failed in CI (UTC) — the regression test caught it. A
  voucher date carries no timezone, so one day of tolerance is unavoidable:
  "today" in Auckland is tomorrow in UTC. **This deliberately loosens the check**
  — a voucher mis-dated one day ahead is no longer rejected here, and is instead
  caught by reconciliation against the bank. Two days ahead is still refused.

### Added

- `internal/receipts.Scope` — a validated collection bound that refuses an
  absent date, an over-long window, or a missing cap rather than defaulting to
  everything. Defaulting is what produced #79.
- Dry runs now report *what* would be routed, not just how many. The six false
  positives were only visible because the list was reviewed rather than the
  count.

---

## [0.24.0] — 2026-09-09

### Fixed

- **Routing rules ANDed their criteria, not ORed** (#56, #48). `ruleMatches`
  returned true if *either* the sender or the subject matched, so a rule naming
  both — which is how every rule in the routing config is written — fired on the
  subject alone. Any mail from anyone with "Invoice" in the subject would have
  routed to Fortnox as a supplier invoice, and the sender constraint was
  decorative. Nothing caught it because every rule in the existing tests named
  only one criterion. Criteria are now ANDed, alternatives within a criterion
  ORed, and an omitted criterion is not a constraint, so single-criterion rules
  are unchanged.

- **The example routing config did not load into the binary that reads it.** It
  used `accounts:` with nested `imap:` blocks, rules with nested `match:` maps
  and a destinations *map*; `cmd/receipts-collector` expects `sources:`,
  flat `match_from` / `match_subject` lists and a destinations *list*. It failed
  to parse outright — and had it parsed, `yaml.v3` ignores unknown fields, so
  following the documented steps would have produced zero sources, zero rules
  and a collector that ran, forwarded nothing and reported success. Rewritten to
  the real schema, with every vendor from #56 preserved.

- **The example lived at a path nothing reads.** `config/receipts/receipt-sources.example.yml`,
  while the loader opens `config/receipt-sources.yml`. Both paths are now
  gitignored for the real file and the example names the right destination.

- **`.gitignore` did not cover `.op-*.env`.** The existing `.env.*` rule matches
  `.env.<something>`, not `.op-receipts.env` — a distinction easy to assert and
  easy to get wrong. Caught by checking `git check-ignore` rather than by reading
  the pattern.

- **`.gitignore` did not exclude the real routing config**, which carries IMAP
  usernames and the Mynt and Fortnox inbox addresses. #56 asserted it was
  ignored; it was not. Added, with a negative rule keeping the example tracked.

- The drop rule for Apple price-change notices sat *after* the Apple receipt
  rule, so first-match-wins meant it could never fire. Moved to the top, and it
  now matches the Swedish subject as well as the English one.

- **Routing rules matched 54% of what is actually forwarded; now 93%.**
  Measured against ground truth — 209 receipts Mathias forwarded to the Mynt
  inbox by hand — rather than against the merchant list someone wrote from
  memory. Before: 112 covered from 5 senders, 95 missed across 39. After: 192
  covered from 28 senders, 15 missed across 14.

  Four misses were pattern bugs that could never have matched anything:
  `anthropic` matched `*@anthropic.com` while invoices arrive from the
  `mail.anthropic.com` **subdomain**; `parkster` matched `.com` for a `.se`
  domain; and the `google` and `apple` subject allowlists were narrower than
  the subjects those senders actually use.

  These land **with** the AND-semantics fix rather than after it. Those subject
  conditions are only too *tight* once criteria are correctly ANDed — under the
  OR behaviour being replaced, `google-play-receipt` matched every message from
  any `@google.com` address regardless of subject. Fixing one without the other
  swaps a wrong behaviour for a different wrong behaviour.

  The remaining additions show what the rule set was missing structurally: it
  covered subscription merchants, while the real spend is travel, parking, road
  tolls, payment intermediaries and SaaS. 15 one-off merchants still miss;
  a merchant allowlist cannot cover a long tail (#78).

- `TestRulesStillRejectNonReceipts` guards the opposite failure. Widening rules
  to catch more receipts is how newsletters end up in the books — the flat
  domain list this replaces counted 57 Expressen adverts as receipts.

### Added

- **`cmd/receipts-check` and `task receipts:check`** — proves every configured
  mail account authenticates before the collector is pointed at real mail. It
  opens each folder with **EXAMINE, not SELECT**, so it is read-only at the
  protocol level and cannot set `\Seen` even if something below it is wrong.
  Passwords resolve inside an `op run` subprocess and never reach argv or stdout.
  Verified against three live Gmail accounts.

- `.op-receipts.example.env` — the `op run` env-file template. Holds `op://`
  references, not values. Records why these items live in the **HomeLab** vault
  rather than AgentSecrets: a Gmail App Password is minted by a human inside a
  Google account, so its creation is operator-governed, and AgentSecrets is for
  keys the agent generates. Also records that all references must resolve from
  one vault, because a mixed-vault env-file fails wholesale.

- Config loading moved to `internal/receipts/receipts/config.go` with a
  `Validate` that rejects a file which parses but does nothing — no sources, no
  rules, an inline password instead of `password_env`, a rule matching nothing,
  or a rule pointing at a destination that does not exist. Previously the schema
  lived privately inside `cmd/receipts-collector`, which is how it drifted out of
  agreement with the example in the first place.

- `TestExampleConfigLoads` — asserts the committed
  example parses into `cmd/receipts-collector`'s own structs and yields non-empty
  sources, destinations and rules, that no source inlines a password, and that
  every rule points at a destination that exists. This is the assertion whose
  absence let the example drift out of usability unnoticed.

- `TestRuleWithBothCriteriaRequiresBoth` and `TestSingleCriterionRulesStillMatch`.
  Both fixes mutation-verified: restoring the OR, and drifting the example's
  `sources:` key back to `accounts:`, each turn the suite red.

---

## [0.23.0] — 2026-09-08

### Decided

- **ADR-0003 accepted** — credentials are keyed by an internal user ID, never by
  the OIDC subject. Accepted with the migration to run **now**: the re-key must
  happen while a session under the current `sub` still exists, and the option to
  defer it was offered and declined.
- **ADR-0004 accepted** — the receipts admin pages move into `internal/ui` behind
  the existing Authentik session; `internal/receipts/httpserver` keeps `/health`
  and nothing else.
- **#57 → option C** — `internal/receipts` stays a provider-agnostic core exposed
  both as a standalone CLI and as a hyperguild skill.
- **#31 → step-up authentication** on sensitive actions only. Browsing rides the
  30-day Authentik session; approving or submitting a payment forces fresh
  authentication. This is also the requirement ADR-0004 cites against forward-auth.
- **#45 → rebuild `create_voucher`, sandbox-only**, with a two-step server-side
  propose/confirm handle rather than a described convention (infra ADR-0012
  rule 2). ADR-0001 keeps it away from live books meanwhile.
- **#8 → per-currency totals with count ordering**, no value-ranking for now.
  ECB reference rates chosen as the eventual FX source, deferred to its own ADR.

### Fixed

- **The recorded Fortnox scope list was missing `companyinformation`** — 27
  scopes, not 26. The earlier count came from transcribing the enumeration
  column by eye rather than reading every row, and it produced a live
  recommendation to "check whether `companyinformation` exists in the portal".
  It does; the page maps *Company Information* to it. Corrected in ADR-0001 and
  `.env.example`, both of which now also record that SIE maps to `bookkeeping`
  and that both payment resources map to `payment`.

### Added

- `docs/fortnox-integration-model.md` — how Fortnox apps and integrations
  actually work, read from the vendor portal rather than assumed. The headline
  for #50: **a private integration works immediately**, with no Fortnox review
  or partner agreement; only marketplace publication needs those. Also records
  token lifetimes (authorization code 10 min, access 1 h, **refresh 45 days,
  rotating**), the 300/min limit that is really **25 requests per 5 seconds**,
  and that changing an app's scopes requires re-authorization rather than
  upgrading existing tokens.

---

## [0.22.0] — 2026-09-08

### Security

- **`internal/receipts/httpserver` mounts the admin UI behind an authenticator**
  (#66). It previously mounted `/ui/token/refresh`, `/ui/token`, `/ui/keys`,
  `/ui/keys/{id}` and `/ui/audit` with no middleware at all — Fortnox token
  refresh and revocation, API-key issuance and revocation, and the audit log.

  Latent rather than an incident: verified 2026-09-08 that `cmd/receipts` has no
  Deployment, Service or Ingress on the cluster and is wired to `ui/stub` fakes.
  Fixed now because #57 is an open decision about deploying it, and the gap
  between "someone decides to deploy this" and "someone notices it has no auth"
  is one `kubectl apply`.

- `New` returns `(*Server, error)` and yields `ErrNoAuthenticator` — and no
  `Server` — when `cfg.Auth` is nil. Returning nothing constructible is the
  point: a `Server` value that exists can be `Start`ed.

- `cmd/receipts` requires `RECEIPTS_ADMIN_TOKEN` and exits non-zero naming it.

### Added

- `httpserver.Authenticator` and `httpserver.BearerToken`, which compares
  SHA-256 digests with `hmac.Equal` so the comparison is constant-time over
  fixed-length input. Tokens shorter than 24 characters are rejected at
  construction: a config value that silently degrades the control is the same
  hole with a plausible-looking value in it.

- The health probe is registered on an outer mux and the admin mux is mounted
  wholesale behind the middleware, so a route added later is protected by
  default rather than by the author remembering. Pinned by a table-driven test
  over all ten routes; all three guards are mutation-verified.

---

## [0.21.0] — 2026-09-08

### Security

- **`cmd/server` no longer fails open when OIDC setup fails** (#71). When
  `auth.NewOIDCHandler` returned an error the process logged
  *"OIDC setup failed — running without auth"* and served anyway, with no
  middleware at all — exposing `/invoices`, `/chat`, the payment endpoints and
  the Fortnox connect routes on `books.d-ma.be` to anyone who could reach the
  ingress, with `tenantID()` resolving to `"default"`. `/healthz` is a static
  200 handler, so the liveness probe reported the pod healthy throughout.

  The trigger is known to occur on this estate: `gooidc.NewProvider` performs
  one-shot discovery at boot, and a koala host reboot on 2026-07-17 restarted
  every pod at once, putting gitea-mcp's discovery ~12s ahead of Authentik
  serving (brain: `homelab/failures/gitea-mcp-oidc-coboot-race-silent-jwt-degradation`).
  gitea-mcp degraded to rejecting valid tokens — noisy. cobalt-dingo degraded
  to accepting everyone — silent.

  Now a startup failure. A co-boot race produces `CrashLoopBackOff` until
  Authentik serves, then recovers unattended. Visible and self-healing, rather
  than invisible and public.

- **Serving with no authenticator at all now also requires an explicit opt-in**
  (`COBALT_ALLOW_UNAUTHENTICATED=true`, development only). A dropped
  `OIDC_ISSUER_URL` secret previously produced the same public deployment by
  omission. The opt-in is honoured *only* when no issuer is configured — it
  cannot launder an issuer that was asked for and failed.

### Added

- `secureHandler` (`cmd/server/wiring.go`) — the auth wiring extracted from
  `main()`, with tests. The fail-open survived because nothing inside `main()`
  is reachable from a test; the same structural cause, and the same repair, as
  `BuildMCPDeps` in ADR-0001. Both guards are mutation-verified: restoring the
  fail-open, or dropping the middleware wrapping, each turn the suite red.

---

## [0.20.0] — 2026-09-08

### Fixed

- **`govulncheck` is a real gate again** (#70). `Taskfile.yml` ran
  `govulncheck ./... || true`, so `task check` reported green regardless of what
  the scan found, locally and in CI. `.context/AGENT.md` asks for "govulncheck
  before adding deps"; that was a convention with nothing behind it — the same
  vacuous-gate shape `infra` ADR-0020 was written about, and the third one found
  in this repo this week.

  Chose option 1 from #70: drop `|| true` outright. Verified empirically first,
  because the whole objection to doing this was noise — govulncheck exits 0 on
  an advisory in a module you merely *require* and non-zero only when your code
  calls a vulnerable symbol, so the common case stays quiet without any extra
  flags. Reversible: option 3 (advisory + a comment saying so) is the fallback
  if it proves noisy in practice.

  Proved the gate discriminates rather than assuming it: a throwaway module
  calling `norm.NFC.String` (one of GO-2026-5970's listed symbols) exits **3**,
  while merely requiring the module exits **0**. A gate that can only pass is
  the thing being removed here, so it is not enough for the new one to be green.

- Bumped `golang.org/x/text` v0.31.0 → v0.39.0 (indirect), clearing
  `GO-2026-5970`. Not called by this code, which is why the advisory was
  invisible behind `|| true`. The scan is now clean, so the gate starts from
  green rather than from a known exception.

- CI now builds `govulncheck` from source (pinned v1.7.0) with the job's own Go,
  instead of using the runner's pre-installed copy. That copy is built against
  go1.25's source-processing packages while `go list` is go1.26, so it exits **1
  — a tool error, not a finding** — on every invocation. Removing `|| true`
  turned that into a red build immediately, which is the correct behaviour: a
  scanner that cannot run is not a pass. The same toolchain skew already forces
  golangci-lint to be installed per-job. The `|| true` had been hiding a tool
  that stopped working, not the noise it was added to suppress.

- **The gate then found real vulnerabilities on its first working run.** `go.mod`
  pinned `go 1.26.1` exactly, so CI's `setup-go` resolved to precisely that
  toolchain, and go1.26.1's standard library carries six advisories reachable
  from this code via `validator.ValidationError.Error`: `GO-2026-6218`
  (`net/url`), `GO-2026-6090` and `GO-2026-5856` (`crypto/tls`), `GO-2026-6089`
  (`net/http`), `GO-2026-6088` (`encoding/xml`) and `GO-2026-5972`
  (`encoding/asn1`). All fixed in go1.26.6; `go.mod` now floors there.

  These were invisible for as long as the scan was suffixed with `|| true` —
  `net/http` and `crypto/tls` findings in an internet-facing app.

  **They are also invisible from a local `task check`.** govulncheck reports
  standard-library vulnerabilities against the toolchain it is run with, and the
  development host is on go1.27.0, which is unaffected. A green local scan says
  nothing about CI or about the shipped image. The only honest verification for
  a stdlib finding is the environment that builds the artifact.

---

## [0.19.1] — 2026-09-08

### Changed

- The "no read-only Fortnox scope" finding in 0.19.0 was recorded on report.
  Now **cited** from Fortnox's own documentation, which states it without
  qualification: *"All scopes gives both read and write access to an endpoint
  and it is not possible to only have read access through the API."*
  ([source](https://www.fortnox.se/developer/guides-and-good-to-know/scopes),
  retrieved 2026-09-08). ADR-0001's premise had already been wrong once by
  being taken from an unchecked document; replacing one unverified claim with
  another would have repeated that.
- Full Fortnox scope set recorded in ADR-0001 and `.env.example`, so the
  narrowest grant can be chosen deliberately rather than guessed.

---

## [0.19.0] — 2026-09-08

### Fixed

- **Corrected a safety control the repo documented but that does not exist.**
  Fortnox connected-app scopes are *resource*-scoped, not *verb*-scoped —
  granting `supplierinvoice` grants read **and** write. There is no read-only
  Fortnox scope. `.env.example` previously claimed "Fortnox itself will refuse
  writes at the API gateway. Belt and braces." and listed a "read-only" scope
  set identical to the sandbox's minus `payment`.
- `.env.example` also still used the pre-rename `real_readonly` /
  `FORTNOX_REAL_RO_*` names and a Taskfile target that no longer exists.

### Changed

- **ADR-0001 amended** (decision unchanged). Production has **two** enforcement
  layers, not three, and both live in this repository. The client-side gate is
  the only thing between this application and the live company's books.
- New guidance: the only lever Fortnox offers is *which resources* to grant.
  Grant the narrowest set the read paths need; never grant `payment`.

### Added

- `TestProductionWiring_ERPWriterCannotWrite` pins the money path —
  `ERPWriter.RecordAndBookkeep`, the only code here that POSTs and PUTs to
  Fortnox. `BuildMCPDeps` never constructs it, so the existing wiring test did
  not reach it. Verified by mutation.

---

## [0.18.1] — 2026-09-08

### Fixed

- **OIDC login now sends and verifies a `nonce`** (#68, PR #69). `state` proves
  this browser started a login; the nonce binds the returned ID token to the
  authorize request that asked for it. Neither was previously present.
  An empty nonce in the token is rejected as well as a mismatched one, so a
  provider that silently stopped returning the parameter cannot disable the
  check. Both login cookies are cleared on callback.

### Changed

- ID-token verification sits behind a small `idTokenVerifier` interface
  returning a `verifiedIdentity` this package owns. Signature verification
  stays inside `go-oidc`; only the result is reshaped. This existed to make the
  nonce comparison reachable from a test — `gooidc.IDToken` keeps its claims
  unexported, so a hand-built one always fails `Claims()`.

---

## [0.18.0] — 2026-09-08

Plan and decisions for the Authentik-backed credential UI: `docs/web-ui-plan.md`,
ADR-0003 (credential identity is not the OIDC subject) and ADR-0004 (one
authenticated front end), both proposed.

---

## [0.17.1] — 2026-09-08

### Fixed

- `Transaction.IdempotencyKey` built its hash with an unchecked `fmt.Fprintf`.
  Now built with `Sprintf` and hashed in one call, removing the error path
  rather than discarding it. Caught by CI — `golangci-lint` cannot run locally
  on koala at present (`infra#375`).

---

## [0.17.0] — 2026-09-08

Statement ingestion. Implements the structural requirement from
[ADR-0002](docs/adr/0002-ingest-files-not-vendor-integrations.md).

### Added

- **`internal/statements`** — a `Parse` port plus a **camt.053** parser, the
  first independent source of transactions. Fortnox's API exposes no bank
  endpoint, so a reconciliation reading Fortnox alone checks Fortnox against
  itself; this is what it gets checked against.
- **`Statement.CheckBalance()`** — opening balance plus the net of every booked
  transaction must equal closing. The only check that catches a parser silently
  dropping a row.
- **`Transaction.IdempotencyKey()`** — excludes provenance deliberately, so
  re-ingesting an overlapping export cannot double-count and switching from a
  manual export to an aggregator does not re-key existing rows.

### Notes

Only `BOOK` entries are ingested; pending entries break the balance identity
while looking correct. Amounts are parsed from the decimal string exactly,
never via `float64`.

---

## [0.16.0] — 2026-09-08

ADR-0001 and ADR-0002 accepted by Mathias. Phase 0 (live production Fortnox
OAuth, issue #50) unblocked.

---

## [0.15.0] — 2026-09-08

### Added

- **`docs/adr/`** — a repo-local ADR series, each record carrying an executable
  `verify:` assertion per `infra` ADR-0020. `DECISIONS.md` frozen with a pointer.
- **`docs/live-financial-data.md`** — problem analysis for reading the live
  company's data: source access matrix, phases, assumptions, risks.
- **`BuildMCPDeps`** — one wiring point for the Fortnox adapters, deriving
  read-only capability from config instead of accepting it from callers, and
  refusing to build a write-capable client in production mode.

### Fixed

- The eight-adapter construction block was duplicated verbatim in `cmd/server`
  and `cmd/mcp`, each threading `readOnly` as an anonymous positional bool. The
  read-only guarantee rested on ten call sites being individually correct.

---

## [0.7.0] – [0.14.0]

**Not documented here.** These releases shipped between 2026-05-01 and
2026-09-07 while this file went unmaintained — including the coo-agent
consumption (`infra` ADR-0021), the MCP server consolidation, the CI agent-run
pause, and the Fortnox mode rename from `real_readonly` to `production`.

Backfilling nine reconstructed summaries nobody verified would be a second
unreliable source of truth, so the gap is marked rather than filled. For this
range, read `git log` between tags and `docs/adr/`. Discipline restarts at
0.15.0 above. See issue #64.

---

## [0.6.0] — 2026-05-01

Mode separation — explicit `FORTNOX_MODE` selects between the sandbox
(write-capable) and real_readonly (live company, read-only) Fortnox
connected apps. Lays the safety groundwork for using cobalt-dingo as a
read-only AI command center over real Fortnox data while keeping the
sandbox available for write-action testing.

### Added

- **`FORTNOX_MODE` config**, required by every binary that touches Fortnox.
  Two modes: `sandbox` (write-capable) and `real_readonly` (read-only).
  A third `real_readwrite` is reserved for a future milestone.
- **Mode-prefixed credential keys** (`FORTNOX_SANDBOX_*`,
  `FORTNOX_REAL_RO_*`) and **mode-prefixed token files**
  (`.fortnox-tokens-sandbox.json`, `.fortnox-tokens-real-ro.json`). Two
  Fortnox connected apps are configured side-by-side with no shared state
  between them.
- **Client-side write gate** in `fortnox.Client.do`: when constructed with
  `readOnly=true`, any non-GET/HEAD request is refused locally with
  `ErrReadOnlyClient` before reaching Fortnox. Defense in depth on top of
  the connected app's OAuth scope.
- **Mode-explicit Taskfile targets**: `mcp:sandbox`, `mcp:real-ro`,
  `fortnox:auth:sandbox`, `fortnox:auth:real-ro`, `fortnox:check:sandbox`,
  `fortnox:check:real-ro`. No bare `task mcp` exists — every invocation
  must commit to a mode so an MCP client cannot accidentally connect to
  the wrong company.
- **Chat UI mode banner** rendered on `/chat`, color-coded by mode
  (sandbox=green, real_readonly=amber). Shows mode label and whether
  writes are enabled.
- **Startup banners** in every Fortnox-touching binary print the active
  mode, token file, and (where relevant) writes-allowed flag.
- **Unit tests** covering the write gate (refuses POST/PUT/DELETE/PATCH
  in readOnly, allows GET/HEAD; passes through unchanged when not
  readOnly) and the config loader (rejects unset/invalid mode, rejects
  missing credentials per mode, no cred leakage between modes).
- **`.env.example`** documenting the new layout including read-only
  scope guidance for the live connected app.

### Changed

- **`config.Fortnox.Env` removed** — replaced by `config.Fortnox.Mode`
  (`config.Mode` typed enum). Existing `cfg.IsSandbox()` preserved as a
  shortcut around `cfg.Mode.IsSandbox()`.
- **`fortnox.NewClient` signature** now requires a `readOnly bool` third
  parameter. All call sites updated.
- **All 7 ledger/connector adapters** propagate `readOnly` from
  construction through to the raw Fortnox client.
- **`e2e-seed`, `e2e-teardown`, `probe-sandbox`** explicitly refuse any
  mode other than `sandbox` rather than relying on a string check.
- **CI workflow** writes `FORTNOX_SANDBOX_*` keys (mapped from existing
  Gitea secrets) and sets `FORTNOX_MODE=sandbox` per step.
- **Token storage** functions `LoadToken`/`SaveToken` now take a path
  argument; `file.NewTokenStore` takes a path; both wired to
  `cfg.Mode.TokenFile()`.

### Migration

For local development, rename keys in `.env` from `FORTNOX_CLIENT_ID` →
`FORTNOX_SANDBOX_CLIENT_ID` (and equivalents for `_CLIENT_SECRET`,
`_REDIRECT_URI`, `_SCOPES`, `_INVOICE_INBOX`); drop `FORTNOX_ENV` (the
Taskfile sets `FORTNOX_MODE` per-target). Rename
`.fortnox-tokens.json` → `.fortnox-tokens-sandbox.json` (or re-run
`task fortnox:auth:sandbox`). To use real_readonly mode, register a
second Fortnox connected app with read-only scopes, populate the
`FORTNOX_REAL_RO_*` block, and run `task fortnox:auth:real-ro`.

---

## [0.5.1] — 2026-04-28

GitOps deploy + sandbox seed buildout. No user-facing behavior change;
release covers infra and test-data plumbing for the v0.5.0 surfaces.

### Changed

- **CI deploy** runs through Flux instead of direct `kubectl apply` /
  `set image`. Job clones `gitea.d-ma.be/mathias/infra`, sed-patches
  `k3s/apps/cobalt-dingo/deployment.yaml`, commits and pushes; Flux
  reconciles within ~10s. Cluster state is now declared in the infra
  repo and auto-corrects manual drift. See `DECISIONS.md` 2026-04-27.
- **Fortnox OAuth scopes** extended to cover the v0.5.0 surfaces:
  `companyinformation`, `bookkeeping`, `customer`, `invoice`, `supplier`,
  `supplierinvoice`, `payment`, `currency`, `project`, `costcenter`,
  `assets`, `settings`. Drops `inbox` (unused).
- **Fortnox client rate limiter** moved from per-method opt-in to a
  `Client.do(req)` wrapper used by every request. Cap dialled to
  18 req/5 s — Fortnox's documented 25/5 s appears to use a sliding
  window that 429s under bursts.
- **Deploy verification** dropped `sudo k3s kubectl` in favour of plain
  `kubectl` (the runner has a working `~/.kube/config`).

### Added

- `cmd/probe-sandbox` — checks all 18 Fortnox endpoints we care about
  and reports record counts.
- `cmd/e2e-seed` extended with five customers (NO/DE/FI/SE), ten
  customer invoices spanning every aging bucket (paid ×2, current ×3,
  overdue 1–30/31–60/90+), three projects (ongoing ×2, completed),
  two cost centers (ENG, SALES), one fixed asset (computer, mid-
  depreciation).
- New `internal/fortnox` CRUD: `CreateCustomer`, `SetCustomerActive`,
  `ListCustomers`, `CreateCustomerInvoice`, `BookkeepCustomerInvoice`,
  `CancelCustomerInvoice`, `FullyPayCustomerInvoice`, `CreateProject`,
  `SetProjectStatus`, `ListProjectsByPrefix`, `CreateCostCenter`,
  `SetCostCenterActive`, `ListCostCentersByPrefix`, `CreateAsset`,
  `ListAssetsByPrefix`.

### Notes for next session

- `cmd/e2e-teardown` still only deactivates suppliers — extending
  it to cover the new entity types is the natural next step.
- Asset POST quirks: response wrapper is `Assets` (plural) even
  though the request wrapper is `Asset` (singular); `AcquisitionStart`
  must be the 1st of a month; the field name is mistranslated as
  "Avskrivningsstart" in error messages.

---

## [0.5.0] — 2026-04-25

Financial command center milestone. Conversational analytics layer across six
Fortnox ledgers, exposed via MCP (38 tools) and an HTMX chat fallback UI.

### Added
- **Domain layer** — 15 new types (`CustomerInvoice`, `Account`, `Voucher`, `Project`,
  `CostCenter`, `Asset`, `Company`, etc.) and 7 port interfaces (`SupplierLedger`,
  `CustomerLedger`, `GeneralLedger`, `ProjectLedger`, `CostCenterLedger`,
  `AssetRegister`, `CompanyInfo`)
- **Aggregation library** (`internal/analyst/`) — `AgingBuckets`, `GroupBy`,
  `SumMinorUnits`, `DaysOverdue`, `OrderedKeys` with 13 table-driven tests
- **Fortnox client extensions** — generic `Get()` with rate limiting (25 req/5s),
  `GetAllPages()` for automatic pagination
- **7 Fortnox adapters** — supplier ledger, customer ledger, general ledger,
  project ledger, cost center ledger, asset register, company info; all tested
  with `httptest`
- **MCP server** (`cmd/mcp/`) — stdio transport, 38 tools:
  - 7 AP tools (summary, overdue, by-supplier, by-currency, aging, history, detail)
  - 7 AR tools (mirrors AP for customer invoices)
  - 7 GL tools (chart of accounts, balances, activity, vouchers, detail,
    predefined accounts, financial years)
  - 3 project tools (list, transactions, profitability)
  - 3 cost center tools (list, transactions, analysis)
  - 2 asset tools (list, detail)
  - 9 analytics tools (cash flow forecast, expense analysis, period comparison,
    yearly comparison, gross margin trend, top customers/suppliers,
    sales vs purchases, company info)
- **HTMX chat UI** (`GET /chat`) — Claude API with tool use, SSE streaming,
  Chart.js visualisations, conversation history
- **Taskfile** — `task mcp` and `task mcp:build` commands

### Changed
- `cmd/server/main.go` — wires chat handler when `ANTHROPIC_API_KEY` is set
- `internal/config/config.go` — added `Claude` config struct
- `go.mod` — added `mark3labs/mcp-go`, `stretchr/testify`

---

## [0.4.0] — 2026-04-17

Full pipeline milestone. The payment lifecycle is now end-to-end: detect → enrich
(cached) → generate → persist → submit → confirm → write-back.

### Added
- **A — PostgreSQL adapter** (`internal/adapter/postgres/`)
  - `Store` — shared pool, sqlc `Queries` wrapper
  - `TokenStore` — `AtomicRefresh` via `UPDATE WHERE refresh_token = $old`; zero rows → `ErrTokenConflict`
  - `BatchRepo` — transactional batch + items insert; `Get` loads items, `List` does not
  - `TenantRepo` — `Get` + `DefaultDebtorAccount`
  - `internal/adapter/postgres/pgstore/` — sqlc-generated query code; regenerate with `task db:sqlc`
  - `sqlc.yaml` config; `task db:sqlc` added to Taskfile
- **D — Supplier IBAN cache** (`adapter/fortnox/supplier_cache.go`)
  - `CachingEnricher` wraps any `SupplierEnricher` with per-(tenant, supplier) TTL map
  - `Invalidate()` for WebSocket `supplier-updated-v1` event-driven invalidation
  - Wired at 5-min TTL in `main.go`; per-request dedup in handler still present
- **B — PISP submission**
  - `domain.BatchService` — `SaveDraft`, `Submit` orchestrate draft → submitted lifecycle
  - `adapter/pisp.Stub` — no-op PaymentSubmitter that logs and returns a fake ref
  - `POST /invoices/batch/submit` handler + `SubmitConfirmation` templ component
  - Submit button live when `DATABASE_URL` is set; gracefully disabled otherwise
- **C — FX delta and ERP write-back**
  - `domain.CalculateFXDelta` — computes `(execRate − invRate) × minorUnits` in öre
  - `domain.BatchService.ConfirmExecution` — per-item ERPWriter calls; partial failure collection
  - `domain.ERPWriter` interface + `adapter/fortnox.ERPWriter` — `POST /3/supplierinvoicepayments` + `PUT .../bookkeep`
  - `fortnox.RecordPayment` + `BookkeepPayment` raw client methods

### Changed
- `cmd/server/main.go` — wires postgres adapters when `DATABASE_URL` is set; supplier cache in all live configurations
- `lib/pq` promoted from indirect to direct dependency

---

## [0.3.0] — 2026-04-16

Hexagonal architecture milestone. The domain layer is now fully decoupled from
all external systems. All I/O flows through explicit port interfaces.

### Added
- `internal/domain/ports.go` — `InvoiceSource`, `SupplierEnricher`, `BatchRepository`,
  `TenantRepository`, `TokenStore`, `PaymentSubmitter` port interfaces
- `internal/domain/batch.go` — `Batch`, `BatchItem`, `BatchStatus` domain types with
  lifecycle states: `draft → submitted → confirmed → reconciled`
- `internal/domain/tenant.go` — `TenantID`, `Tenant`, `DebtorAccount` (supports future
  PISP handle)
- `internal/domain/money.go` — `Money` fixed-point type (`int64` minor units + ISO 4217
  currency); replaces `float64` throughout the domain
- `internal/adapter/fortnox/connector.go` — `Connector` implements `InvoiceSource` +
  `SupplierEnricher`; handles token refresh with `AtomicRefresh` to prevent rolling-token
  race conditions
- `internal/adapter/file/tokenstore.go` — file-backed `TokenStore` with mutex-guarded CAS
  for single-process token management; production path for postgres adapter
- `internal/adapter/fake/` — stub `InvoiceSource` + `SupplierEnricher` for development
  without Fortnox credentials
- `migrations/001_initial.up.sql` + `down.sql` — initial schema: `tenants`,
  `debtor_accounts`, `fortnox_tokens`, `payment_batches`, `batch_items`; amounts stored as
  `BIGINT` minor units
- `cmd/migrate/main.go` — standalone migration runner using golang-migrate
- `OAuthToken.Valid()` on domain token type (30-second buffer)
- `task db:migrate` and `task db:migrate:down` in Taskfile

### Changed
- `internal/ui/handler.go` — `Server` now accepts `domain.InvoiceSource` +
  `domain.SupplierEnricher` ports instead of raw `config.Fortnox`; no fortnox import;
  all handlers propagate `context.Context`
- `cmd/server/main.go` — wires real or fake adapters at startup based on whether
  `FORTNOX_CLIENT_ID` is set
- Deleted `internal/invoice/` package — merged into `internal/domain/`

---

## [0.2.0] — 2026-04-15

Live Fortnox integration milestone. Real invoices replace fake data; e2e CI pipeline
runs against the Fortnox sandbox on every push.

### Added
- Fortnox OAuth2 flow (`cmd/fortnox-auth/`) — authorization code exchange, token
  persistence to `.fortnox-tokens.json`
- Supplier IBAN/BIC enrichment — `GET /3/suppliers/{n}` lookup before PAIN.001 assembly;
  invoices without IBAN are silently excluded from the batch
- Per-request supplier cache in `handler.go` — deduplicates API calls within a single
  batch operation
- `cmd/e2e-seed/` + `cmd/e2e-teardown/` — sandbox data management for CI
- Full e2e CI job: seed → run server → hit `/invoices` → verify batch XML → teardown
- Automatic token refresh on expiry (30-second buffer) with file persistence
- `fakeInvoices` fallback when `FORTNOX_CLIENT_ID` is unset (dev mode)
- Batch XML download endpoint (`GET /invoices/batch/download`)

### Changed
- UI batch panel redesigned with currency-grouped sections, collapsible XML preview,
  and download link
- `PendingInvoice` extended with `IBAN` and `BIC` fields
- `BatchSummary` and `BatchGroup` types extracted to support grouped rendering

---

## [0.1.0] — 2026-04-14

First working pipeline milestone. FCY invoice detection, PAIN.001 generation, and
a styled browser UI.

### Added
- PAIN.001 generation (`internal/payment/pain001.go`) — `pain.001.001.03` XML with
  per-currency `PmtInf` blocks; EUR blocks carry SEPA service level
- Invoice queue (`internal/domain/`) — FCY filter (`IsForeignCurrency`), `Queue`,
  `Sync`, `Enrich`
- HTMX + Templ UI — invoice list, `POST /invoices/batch` partial, download link
- SEB Green Design System styling (chlorophyll v4 components)
- Acceptance test suite using godog/Gherkin — 7 scenarios covering PAIN.001
  generation, FCY filtering, control sum accuracy, multi-currency grouping
- Fortnox connector (`internal/fortnox/client.go`) — `GET /3/supplierinvoices?filter=unpaid`
- golangci-lint v2 with revive, errcheck, gofumpt linters
- k3s deployment via CI on koala (local runner)
- Smoke test in CI: container health check on `/healthz`

---

## [0.0.1] — 2026-04-08

Initial project scaffold. Go module, Taskfile, Gitea CI skeleton, k3s deploy
pipeline, project context files.
