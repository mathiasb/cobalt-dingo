# Project template

Harness-agnostic project scaffold using the Agent Skills open standard.

## Quick start

```bash
degit mathias/project-template my-new-project
cd my-new-project
task init
```

## Structure

```
.context/
├── PROJECT.md              ← Canonical project context (edit this)
├── mcp.json                ← MCP server config (generated on first sync)
└── system-prompt.txt       ← Generated: generic system prompt

.skills/
├── go-patterns/
│   └── SKILL.md            ← Agent Skills standard format
└── htmx-patterns/
    └── SKILL.md

scripts/
└── context-sync.sh         ← Adapter generator (finds root AGENT.md automatically)

Taskfile.yml                ← Task runner config
DECISIONS.md                ← Why things are the way they are
```

## Generated files (gitignored)

| File | Consumer | Notes |
|------|----------|-------|
| `AGENTS.md` | Crush, Pi, Antigravity | Root + project concatenated |
| `CLAUDE.md` | Claude Code | Project-only (inherits root via tree walk) |
| `.cursorrules` | Cursor | Root + project concatenated |
| `.aider.conventions.md` | Aider | Root + project concatenated |
| `.context/system-prompt.txt` | Open WebUI, Mods, generic | Root + project concatenated |

## How root context works

The script walks up from the project directory looking for `~/dev/.context/AGENT.md`.

- **Claude Code**: inherits natively (reads every `CLAUDE.md` up the tree) → project CLAUDE.md is project-only
- **Everything else**: can't walk the tree → script concatenates root + project into each generated file

## Skills

Skills use the [Agent Skills open standard](https://github.com/badlogic/pi-skills). Each skill is a folder with a `SKILL.md` containing frontmatter:

```yaml
---
name: my-skill
description: What this skill does. When to use it.
---
# Instructions here
```

Supported natively by Claude Code, Pi, Crush, and Antigravity. No adapter needed for skills.

### Adding a skill

```bash
mkdir .skills/my-new-skill
# Create .skills/my-new-skill/SKILL.md with frontmatter + instructions
```

### Using pi-skills (cross-compatible)

```bash
# User-level (all projects)
git clone https://github.com/badlogic/pi-skills ~/.pi/agent/skills/pi-skills

# Symlink for Claude Code
ln -s ~/.pi/agent/skills/pi-skills/brave-search ~/.claude/skills/brave-search
```

## Usage with specific tools

**Claude Code**: `task context:sync:claude` → reads `CLAUDE.md` + discovers `.skills/*/SKILL.md`

**Crush**: `task context:sync:agents` → reads `AGENTS.md` + discovers `.skills/*/SKILL.md`

**Pi**: `task context:sync:agents` → reads `AGENTS.md` + discovers `.skills/*/SKILL.md` (or symlink `.skills/` to `.pi/skills/`)

**Antigravity**: `task context:sync:agents` → reads `AGENTS.md` + discovers `.skills/*/SKILL.md`

**Cursor**: `task context:sync:cursor` → reads `.cursorrules`

**Mistral Vibe**: Run root-level `task context:sync:vibe` once → `vibe --agent mathias`

**Open WebUI / Mods**: Copy `.context/system-prompt.txt` into a preset or pipe it

**Any other tool**: Point at `.context/PROJECT.md` directly — it's human-readable markdown
