# Project context

<!-- Canonical project context. Edit this, run `task context:sync`.
     Root agent context from ~/dev/.context/AGENT.md is automatically
     prepended for harnesses that don't walk the directory tree. -->

## What this is

**Mathias's own finance infrastructure.** Single tenant, one operator, no customers.

It reads his live Fortnox company, collects and routes receipts and supplier invoices,
parses bank statements, exposes the books to an agent over MCP, and produces a scheduled
financial overview. The point is to make his own bookkeeping less work.

**It is read-only against the live books** — `ADR-0001`, still in force. Writes are off at
four independent layers, and `docs/irreversible-operations.md` explains why that boundary
is worth more than the convenience it costs.

**The venture framing is paused, not abandoned** — `ADR-0009`, 2026-09-16. cobalt-dingo
was scoped as a product (autonomous FCY treasury for Nordic SMEs, PISP payment execution,
multi-tenant SaaS). Payment execution is parked on risk grounds and the buyer, pricing and
co-founder questions went unanswered for three months. So: no buyer work, no pricing, no
customer-facing multi-tenancy, no payment platform engagement, no customer contract.
`docs/superpowers/specs/2026-04-15-product-vision.md` describes that paused product and
carries a banner saying so; `ADR-0007` and `ADR-0008` are the dormant payment design.

**Do not demolish the product shape.** `TenantID` stays first-class and the hexagonal
ports stay as they are. Keeping the multi-tenant shape costs nothing; removing it and
later re-adding it is a rewrite, and ADR-0009 carries live resumption triggers.

## Identity

- **Name**: cobalt-dingo
- **Owner**: Mathias
- **Client**: none — personal infrastructure (was: venture; see ADR-0009)
- **Repo**: `git.d-ma.be/mathias/cobalt-dingo`
- **Status**: active, maintained, single-tenant

## Stack

- **Primary language**: Go
- **UI layer**: HTMX + Templ (when applicable)
- **Fallback languages**: Python, TypeScript (justify in PR if used)
- **Build**: Task (taskfile.dev), not Make
- **Containers**: Docker (compose for dev, k3s for deploy)
- **Target infra**: koala (GPU workloads), iguana (services), flamingo (edge)

## Conventions

### Code style
- Go: follow `golines`, `gofumpt`, `golangci-lint` with project config
- Tests: table-driven, in `_test.go` next to source, `testify` for assertions
- Errors: wrap with `fmt.Errorf("operation: %w", err)`, no naked returns
- Naming: stdlib conventions, no stuttering (`http.Client` not `http.HTTPClient`)

### Architecture preferences
- Prefer standard library over frameworks (net/http over gin/echo)
- Dependency injection via constructor functions, not containers
- Configuration via environment variables, parsed at startup into a typed struct
- Structured logging via `slog`

### Git
- Conventional commits: `feat:`, `fix:`, `chore:`, `docs:`, `refactor:`
- Branch naming: `feat/short-description`, `fix/short-description`
- PRs: one concern per PR, description explains *why* not *what*

### Security
- No secrets in code, ever — use env vars or SOPS-encrypted files
- Client data never leaves local network unless explicitly cleared
- Dependencies: audit with `govulncheck` before adding

## Knowledge base access

The shared brain is reached through the plugin MCP servers
(`mcp__plugin_skills_brain__*`), not a local port. Query it before non-trivial work; this
repo's history is well covered, particularly under the `cobalt-dingo` and `fortnox` wings.

## Agent instructions

When acting as a coding agent on this project:

1. Read this file, then `DECISIONS.md` (frozen legacy) and `docs/adr/` (live) before
   starting work. Skills live in the `mathias/skills` plugin, not in a local `.skills/`
2. Run `task check` before committing (lint + test + vet)
3. If unsure about a convention, check `DECISIONS.md` or ask
4. Never modify files outside the project root without explicit permission
5. When adding a dependency, explain why in the commit message
6. For client projects: never send code or context to cloud APIs — use local models via LiteLLM
