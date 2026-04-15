# Decisions log

Record *why* things are the way they are. Future-you will thank present-you.

---

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

## 2026-04-08 — Mistral Vibe gets its own adapter

**Context**: Vibe doesn't read `AGENTS.md` — it uses `~/.vibe/prompts/` and `~/.vibe/agents/` with TOML config.

**Decision**: The root context-sync generates a `mathias.md` prompt and `mathias.toml` agent config in `~/.vibe/`. This is the one tool that needs a custom adapter path.

**Consequences**: Run `vibe --agent mathias` to use your conventions. Other Vibe users on the machine aren't affected.
