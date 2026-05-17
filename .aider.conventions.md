# Agent context — Mathias workspace

<!-- Canonical root context for all AI coding agents.
     Lives at: ~/dev/.context/AGENT.md
     Applies to every project under ~/dev/ unless overridden.
     
     Run `task context:sync` from ~/dev/ to regenerate harness-specific files.
     Project-level context in .context/PROJECT.md layers on top of this. -->

## Who I am

I'm Mathias, a digital product manager and technology consultant based in Sweden.
I build software, research emerging tech, and deliver consulting engagements
for clients under NDA. I work across AI/ML, financial automation, web applications,
and climate/sustainability tech.

## How I work with agents

- I think like a product manager — I care about *why* before *how*
- I want agents to be opinionated and push back, not just execute blindly
- I prefer concise responses; skip ceremony and get to the point
- When I say "build this", I mean production-quality with tests, not a demo
- Ask me before making irreversible changes or adding heavy dependencies
- I work with confidential client data — never send it to cloud APIs unless I explicitly say it's OK

## Behavior rules

These rules apply to every task across every project, regardless of harness.

1. **No assumptions.** Don't hide confusion — surface it. Surface tradeoffs explicitly.
   Think before coding; if the problem is unclear, ask or state assumptions before acting.
2. **Minimum viable code.** Solve with the smallest change that works. Nothing
   speculative, no "while we're here" cleanups, no premature abstractions. Simplicity first.
3. **Surgical changes.** Touch only what the task requires. Leave unrelated code,
   files, and formatting alone. Diffs should be small and reviewable.
4. **Goal-driven execution.** Define clear success criteria up front for every task.
   Loop — implement, verify, refine — until those criteria are met. Don't claim
   completion without evidence (tests pass, command output, observed behavior).
5. **Trunk-Based Development — commit directly to main.** Every commit is one
   logical change (one tool, one fix, one test) with passing tests. Main is always
   deployable. Never create long-lived feature branches.

   **Exception — parallel agents on same repo:** If another agent is known to be
   actively working on the same repo simultaneously, create a short-lived branch
   (`agent/<description>`), finish the task, and merge to main within the same
   session. Do not leave agent branches open between sessions.

   **Exception — external contributor or client four-eyes requirement:** Use
   PR flow only when a human reviewer outside the project is required. Document
   the reason in PROJECT.md.

## Default stack

| Layer | Default | Fallback | Last resort |
|-------|---------|----------|-------------|
| Language | Go | Python | TypeScript, Java, C |
| UI | HTMX + Templ | Server-rendered HTML | React (only if SPA is justified) |
| Build | Task (taskfile.dev) | Make | — |
| Containers | Docker Compose (dev), k3s (prod) | — | — |
| DB | PostgreSQL + sqlc | SQLite | — |
| Search | pgvector (vector), BM25 | Qdrant (when >1M vectors or hybrid retrieval) | — |
| Logging | slog (structured) | — | — |
| Testing | Table-driven, testify | — | — |
| Agents (Go) | google.golang.org/adk + pkg/litellm adapter | — | — |

Exploratory: Rust, Zig — I'll tell you when I want these.

## Code conventions

- **Go style**: golines, gofumpt, golangci-lint
- **Errors**: `fmt.Errorf("operation: %w", err)` — never naked, never log-and-return
- **Naming**: stdlib conventions, no stuttering
- **Architecture**: prefer stdlib over frameworks, constructor injection, env-var config parsed into typed structs
- **Git**: conventional commits (`feat:`, `fix:`, `chore:`), commit directly to main,
  one logical change per commit, CI is the quality gate
- **Never**: long-lived feature branches, PRs for solo work, direct push without
  passing `task check` locally first
- **Security**: no secrets in code, govulncheck before adding deps, SOPS for encrypted config
- **Dependencies**: prefer stdlib. testify, slog, templ, sqlc, google.golang.org/adk (agent projects only) are pre-approved; anything else needs justification in the commit message

## Infrastructure

Three machines on Tailscale:

| Machine | Role | Key specs |
|---------|------|-----------|
| koala | GPU inference, heavy compute | RTX 5070, runs k3s + llama-swap + shared postgres18/pgvector |
| iguana | Services, builds | M2 Ultra Mac |
| flamingo | Daily driver, edge | Mac mini, ~/dev is here |

- **Model routing**: LiteLLM in front of llama-swap (local) + cloud APIs (when permitted)
- **Orchestration**: k3s cluster across all three machines
- **Networking**: Tailscale mesh

## Project landscape

All development repos live at `~/dev/` (softlink from `~/Documents/local-dev/`).

Organized in thematic folders:

| Folder | Focus | Count |
|--------|-------|-------|
| `GO/` | Go web frameworks, API integrations, learning projects | ~10 |
| `AI/` | ML research, AI frameworks (FinRL, DSPy, crawl4ai) | ~6 |
| `AGENTS/` | Autonomous agents, coding agents, MCP servers, infra | ~15 |
| `QKX/` | Invoice processing, financial automation, payment systems | ~13 |
| `XT/` | Climate data, sustainability (Klimatkollen, Garbo) | ~2 |

See `~/dev/PROJECT_SUMMARY.md` for detailed descriptions of each project.

### Key active projects

- **super-koala** (`AGENTS/`) — multi-component agent stack with LangGraph, DSPy, MCP
- **azure-tiger** (`QKX/`) — invoice extraction → ISO 20022 payment instructions
- **gocrwl** (`AGENTS/`) — Go web crawler with containerized deployment
- **koala-ai-stack** (`AGENTS/`) — local AI server infrastructure management
- **klimatkollen** (`XT/`) — Swedish municipal climate data platform

## Knowledge base — actively use it

A persistent brain (BM25 search + LLM-synthesised Q&A) survives across sessions,
hosts, and harnesses. It holds 100+ hard-won entries: infra incident postmortems,
Go pitfalls, framework gotchas, design principles, ADRs. **It is not optional
reference material — query it actively, not just when explicitly told.**

### When to query (treat as a reflex)

- **Before** starting a non-trivial task — search for prior art with the symptom
  AND the system component ("how did we solve X in Y?"). 5 seconds beats 5 hours.
- **When debugging** — search for the error string, the stack frame, the affected
  service. Past you may have already paid this tax.
- **Before adopting** a pattern, library, framework, or model name — check if it
  was tried and rejected, or what the integration footguns are.
- **When making architectural decisions** — search for the domain + "ADR" or
  "decision" to find prior reasoning before re-deriving it.
- **When a recommendation feels novel** — challenge yourself: "has this been
  documented?" The brain often has it.

### When to write

After you discover something that **future-you would forget** and that **isn't
recoverable from the code, git log, or PR description alone**:

- Bugs whose root cause is non-obvious and generalisable beyond this project.
- Framework / library / model-name quirks that bit you and would bite anyone.
- Design principles validated under fire (e.g. "every `_get` needs a `_list`").
- Postmortems for incidents: what broke, why, how diagnosed, what to do next time.

DON'T write project status, sprint progress, PR summaries, or "what I did this
session" — those rot fast and the originals are in git/gitea anyway. Brain
entries that age well are about *why*, *how to avoid*, and *what to do when*.

### How to access (per harness)

| Harness | Query | Write |
|---------|-------|-------|
| **Claude Code, Claude Desktop** | `brain_query` (BM25), `brain_answer` (LLM-synth + sources) MCP tools | `brain_write` MCP tool |
| **Crush, Pi, Antigravity, other MCP-capable** | same MCP server: `ingestion-brain` (via the `mcp__*_brain__*` namespace once authenticated) | same |
| **Anything HTTP-only (curl, scripts)** | `POST https://brain-mcp.d-ma.be/query` with `{"query":"..."}` (auth via `BRAIN_MCP_TOKEN`) | `POST .../write` with `{"content":"...","filename":"..."}` |
| **Browser / human inspection** | `https://gitea.d-ma.be/mathias/hyperguild` → `knowledge/` and `wiki/` markdown files |

- **Scoping**: defaults to `public` collection; client projects filter to `{client}` + `public`.
- **Routing**: brain_answer's LLM uses berget.ai as primary, iguana ollama as
  fallback. Both are configurable in the `supervisor/ingestion-deployment.yaml`
  on the koala k3s cluster; don't hardcode local-only model names into the
  berget URL (see knowledge entry on namespace mismatches).

### Quick reflex checks

If you find yourself about to say any of these out loud, you owe yourself a brain query first:

- "I think the issue might be..."
- "Let me try X and see..."
- "I'll just write a script to..."
- "This is probably a new bug..."
- "Has anyone done this before?" — *yes, probably, go check.*

## Client work rules

When working on a project tagged with a client name:
1. Never send code, data, or context to cloud APIs — use local models only
2. Never reference other client projects or their data
3. Keep all artifacts within the client's git org / directory
4. Treat everything as confidential unless told otherwise

## Harness-agnostic principles

This context is designed to work with any AI coding tool:
- Claude Code, Cursor, Aider, Open WebUI, Charmbracelet Mods/Crush
- Pi Coding Agent, Mistral Vibe, Antigravity
- Any tool that accepts a system prompt or reads a markdown context file

The canonical source is always `.context/AGENT.md` (root) and `.context/PROJECT.md` (per-project).
Derived files are committed (see *How context propagates* below) so a `git pull` on any host yields full agent context with no setup.

## How context propagates

Canonical sources of truth:
- Universal: `~/dev/.context/AGENT.md` (this file)
- Project: `<repo>/.context/PROJECT.md` (per-repo)

Derived files (committed, regenerated by `task context:sync`):
- `CLAUDE.md`, `AGENTS.md`, `.cursorrules`, `.aider.conventions.md`,
  `.context/system-prompt.txt`

Workflow:
1. Edit a canonical file. Run `task context:sync`. Commit canonical and
   derived together. Push.
2. On any other host, `git pull` brings both. Claude Code (tree-walking)
   uses `CLAUDE.md`; Crush / Pi / Antigravity (cwd-only) use `AGENTS.md`;
   Cursor uses `.cursorrules`; Aider uses `.aider.conventions.md`.
3. `task check` runs `context:sync` then asserts `git status --porcelain`
   is empty over the derived files (catches both modified-tracked drift
   and missing-untracked adapters). A drift fails the check with a
   message telling you to stage the regenerated files.

Behavior rules in this file and per-project rules in `PROJECT.md` apply
unconditionally on every host, every harness.

## Engineering Skills

Shared engineering skills are available in `~/dev/.skills/`. Load on demand via the index.

See `~/dev/.skills/SKILLS_INDEX.md` for the full list with descriptions and "use when" triggers.

Key skills:
- **TDD**: always write tests first — load `tdd` skill
- **Code Review**: load `code-review` skill before any review
- **SOLID/Clean Code**: load `solid` or `clean-code` skill for design work
- **Problem first**: load `problem-analysis` skill before coding non-trivial features

---

# Project context

<!-- Canonical project context. Edit this, run `task context:sync`.
     Root agent context from ~/dev/.context/AGENT.md is automatically
     prepended for harnesses that don't walk the directory tree. -->

## Identity

- **Name**: cobalt-dingo
- **Owner**: Mathias
- **Client**: venture
- **Repo**: 
- **Status**: active

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

This project can query the shared knowledge base via MCP or HTTP:

- **MCP endpoint**: `mcp://localhost:3100/knowledge`
- **HTTP fallback**: `http://localhost:3100/api/v1/search`
- **Scoping**: queries are filtered to collection `venture` + `public`

## Agent instructions

When acting as a coding agent on this project:

1. Read this file and all `SKILL.md` files in `.skills/` before starting work
2. Run `task check` before committing (lint + test + vet)
3. If unsure about a convention, check `DECISIONS.md` or ask
4. Never modify files outside the project root without explicit permission
5. When adding a dependency, explain why in the commit message
6. For client projects: never send code or context to cloud APIs — use local models via LiteLLM
