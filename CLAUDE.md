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
