# Decisions log

Record *why* things are the way they are. Future-you will thank present-you.

---

## 2026-04-27 — Deploy via infra repo + Flux instead of kubectl direct

**Context**: Pre-v0.5.x the deploy job ran `kubectl apply -f k8s/...` and `kubectl set image` directly. This bypasses Flux, so the cluster state diverged from the infra repo's declared state. With cobalt-dingo growing in significance, divergence becomes a real audit/drift problem.

**Decision**: The deploy job now (a) clones `gitea.d-ma.be/mathias/infra`, (b) updates `k3s/apps/cobalt-dingo/deployment.yaml` with the new image tag, (c) commits and pushes to infra, (d) annotates the Flux GitRepository and apps Kustomization to trigger immediate reconciliation. Total deploy latency: ~5–10s (vs ~5s for direct kubectl).

**Consequences**: Cluster state is now reflected in the infra repo. Flux auto-corrects manual drift. The local `localhost:5000` registry remains the build target — no change to the build flow. The `k8s/registry/` directory and `scripts/setup-koala-registry.sh` are deleted; the registry is now declared in `infra/k3s/apps/registry/`. The `k8s/namespace.yaml` and `k8s/deployment.yaml` files are also deleted to avoid being mistaken for the source of truth.

## 2026-04-08 — AGENTS.md as cross-tool standard, not CLAUDE.md

**Context**: Multiple tools (Crush, Pi, Antigravity) read `AGENTS.md` natively. Claude Code reads `CLAUDE.md`. Building on `CLAUDE.md` as the primary format locks into one vendor.

**Decision**: Canonical source is `.context/AGENT.md` (root) and `.context/PROJECT.md` (per-project). The adapter script generates both `AGENTS.md` and `CLAUDE.md` — identical content, two filenames. Crush, Pi, and Antigravity read `AGENTS.md`; Claude Code reads `CLAUDE.md`.

**Consequences**: One canonical file serves five+ tools. Adding a new tool that reads `AGENTS.md` requires zero adapter work.

## 2026-04-08 — Agent Skills standard (SKILL.md in folders) over flat markdown

**Context**: Claude Code, Pi, Crush, and Antigravity all support the Agent Skills open standard: a folder containing `SKILL.md` with frontmatter (`name`, `description`). Skills are discovered on-demand — only the description enters context, full instructions load when triggered.

**Decision**: Skills live in `.skills/{name}/SKILL.md` at project level. This replaces the earlier `.context/skills/{name}.md` flat-file approach.

**Consequences**: Skills are cross-compatible without adaptation. Pi auto-discovers them from `.pi/skills/` (symlink). Crush reads them natively. Progressive disclosure keeps context window lean.

## 2026-04-08 — Go + HTMX as default stack

**Context**: Need a default that's fast to prototype, easy to deploy as a single binary, and doesn't require a Node/npm toolchain for the UI layer.

**Decision**: Go with HTMX + Templ for server-rendered UI. Python as fallback for ML/data tasks. TypeScript only when a project genuinely needs a rich client-side SPA.

**Consequences**: Simpler deployment and dependency management. Agents need Go-specific skills.

## 2026-04-08 — Task over Make

**Context**: Makefiles have arcane syntax and poor cross-platform support.

**Decision**: Use Taskfile (taskfile.dev) — YAML-based, cross-platform, supports task dependencies.

**Consequences**: One extra binary to install. All project automation in `Taskfile.yml`.

## 2026-04-08 — Qdrant over ChromaDB for vector store

**Context**: Need collection-level isolation for client separation, payload filtering, runs well in k3s.

**Decision**: Qdrant. Native collection isolation, rich filtering, mature gRPC API.

**Consequences**: More operational complexity than Chroma, but isolation is non-negotiable for client work.

## 2026-04-15 — cobalt-dingo is the tech provider; PISP partner bank holds the licenses

**Context**: Two clarifications on the payment layer. (1) cobalt-dingo builds and supplies the payment technology stack, but the PISP partner bank operates it under their own PISP and AISP licenses and regulatory umbrella — cobalt-dingo is not the regulated entity. (2) The AISP capability comes via the same partner bank infrastructure.

**Decision**: cobalt-dingo supplies the engine; the partner bank is the licensed operator. Standard ISO 20022 PAIN.001 (no bank-specific profile variants for now). Payment status handling is dual-mode: webhook callbacks where banks support them, polling fallback otherwise. AISP enables reading end-customer bank account data (balances, transactions) via the partner bank's infrastructure — closes the Camt.053/054 reconciliation gap.

**Consequences**: The payment layer is a service cobalt-dingo builds and the partner bank deploys/operates. Real-time cash position, AR auto-matching, and bank fee reconciliation all flow from the AISP data feed. PSD2 bank API conformance across ~20 target banks needs mapping before architecture is finalised.

## 2026-04-15 — ERP-agnostic core; Fortnox is an adapter

**Context**: Fortnox is the MVP ERP integration. Future expansion to Visma Spcs/Spiris and other Nordic ERP systems is a stated requirement. If the internal domain model is Fortnox-shaped, that expansion becomes a rewrite. If Fortnox is an adapter, it becomes a new connector.

**Decision**: The cobalt-dingo core defines a stable, ERP-agnostic domain model for all entities: invoices, suppliers, GL accounts, payment instructions, voucher write-backs. Each ERP is a connector implementing a shared interface. Fortnox is the first connector. All payment, AI, and reconciliation logic operates against the internal model, never against ERP-specific types.

**Consequences**: Slightly more upfront design work for the domain model. Adding Visma Spcs/Spiris (or any other ERP) later requires only a new connector — no changes to the payment engine, AI bookkeeper, or reconciliation logic. This constraint must be enforced from the first line of code.

## 2026-04-15 — PAIN.001 generation is our responsibility, not Fortnox's

**Context**: Fortnox has an internal bank payment module that generates PAIN.001 files (supporting SEB, Handelsbanken, Swedbank, Nordea, Danske Bank). Discovery confirmed this module is accessible only via the Fortnox UI — no API endpoint exposes it. The `supplierinvoicepayments` API records completed payments; it does not initiate them.

**Decision**: cobalt-dingo owns the full payment pipeline: extract invoice + supplier data from Fortnox API → validate IBAN/BIC → assemble PAIN.001 XML → submit to bank via PSD2 PISP → write actual execution data back to Fortnox.

**Consequences**: We integrate once with a bank partner that operates as a PISP platform. Through that platform, our customers can pay from any bank the PISP supports — starting with the top ~20 Nordic and Northern European banks. cobalt-dingo implements the technical components: PAIN.001 generation, the PISP platform API integration, and the SCA/signature flow. The bank partner is not the customer's bank — they are the payment initiation layer that reaches all supported banks.

## 2026-04-15 — Three invoice sources collapse to one API surface

**Context**: The MVP scans three sources (inbox, e-invoice channel, manually imported). Discovery showed all three result in SupplierInvoice records in `/3/supplierinvoices`. The Inbox API only returns raw files (PDFs); Peppol e-invoices are auto-processed by Fortnox and surface as standard SupplierInvoice records with no distinguishing field.

**Decision**: Treat `/3/supplierinvoices` as the single scan surface. Use the WebSocket (`wss://ws.fortnox.se/topics-v1`) for real-time `supplier-invoice-created-v1` events, with `lastmodified` polling as fallback. Do not build separate inbox-scraping or e-invoice-channel logic.

**Consequences**: Simpler detection logic. Inbox scanning (for unprocessed PDFs) is deferred unless a future need emerges.

## 2026-04-15 — IBAN/BIC lives on Supplier entity, not on invoice

**Context**: Discovery confirmed IBAN and BIC are fields on the Supplier entity (`GET /3/suppliers/{SupplierNumber}`), not on SupplierInvoice. Every invoice validation step requires a separate supplier lookup.

**Decision**: Build a per-tenant in-memory Supplier cache (short TTL, ~5 min) populated lazily on first invoice batch per tenant. Rate limit is 25 req/5 sec — cache is non-optional at any meaningful batch size.

**Consequences**: Cache invalidation needed when supplier banking details change. WebSocket `supplier-updated-v1` event can drive invalidation.

## 2026-04-15 — FX delta voucher is our responsibility

**Context**: When `PUT /3/supplierinvoicepayments/{id}/bookkeep` is called, Fortnox creates a voucher at the execution CurrencyRate we supply. It does not automatically calculate or book the difference vs. the original invoice rate. Sandbox confirmation still needed, but guidance and architecture should assume we own this.

**Decision**: After bank confirmation, calculate FX gain/loss as (execution rate − invoice rate) × invoice amount in foreign currency. Post a separate voucher to BAS 7960 (loss) or 3960 (gain) + the AP clearing account. Post bank fees to 6570.

**Consequences**: We need to store the original invoice CurrencyRate at scan time so we can compute the delta after execution. Do not rely on re-fetching from Fortnox (rate may have changed if a user edited the invoice).

## 2026-04-16 — File-based TokenStore for dev; postgres TokenStore for production

**Context**: Token refresh needs CAS semantics to prevent two concurrent requests from both detecting an expired token, both calling Fortnox, and the second refresh invalidating the first. A mutex gives in-process safety. For production (multiple server replicas), only a database-level CAS (`UPDATE ... WHERE refresh_token = $old RETURNING *`) is safe.

**Decision**: `adapter/file.TokenStore` uses a mutex — safe for local dev and single-process deploys. `adapter/postgres.TokenStore` (next step) uses `UPDATE ... WHERE` CAS. `domain.ErrTokenConflict` is the sentinel for the race case; callers reload and retry with the winning token.

**Consequences**: Single-process deployments use the file adapter with no DB dependency. Multi-replica production deployments must use the postgres adapter. The domain port is the same in both cases — swapping is a one-line change in `main.go`.

## 2026-04-16 — Adapter/fortnox wraps internal/fortnox; not the reverse

**Context**: The domain defines `InvoiceSource` and `SupplierEnricher` ports. The raw Fortnox HTTP client lives in `internal/fortnox/`. Two ways to bridge: (a) make the raw client implement the ports directly, or (b) introduce a thin adapter package.

**Decision**: `internal/adapter/fortnox.Connector` wraps `*internal/fortnox.Client`. The raw client remains ignorant of domain types and interfaces — it maps JSON to domain types at the boundary, but carries no port implementation responsibility. The adapter owns token lifecycle (load, refresh, CAS conflict handling) and calls the raw client per-invocation.

**Consequences**: The raw client stays testable without domain machinery. The adapter is the seam — mocking `InvoiceSource` in tests doesn't require any HTTP setup. Adding a second ERP (e.g. Visma) means a new adapter package, not changes to `internal/fortnox/`.

## 2026-04-08 — Mistral Vibe gets its own adapter

**Context**: Vibe doesn't read `AGENTS.md` — it uses `~/.vibe/prompts/` and `~/.vibe/agents/` with TOML config.

**Decision**: The root context-sync generates a `mathias.md` prompt and `mathias.toml` agent config in `~/.vibe/`. This is the one tool that needs a custom adapter path.

**Consequences**: Run `vibe --agent mathias` to use your conventions. Other Vibe users on the machine aren't affected.
