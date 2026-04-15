#!/usr/bin/env bash
# Generates harness-specific context files from .context/PROJECT.md
# Project-level script — run from a project directory.
#
# For Claude Code: generates project-only CLAUDE.md (it inherits root via tree walk)
# For everything else: concatenates root AGENT.md + project PROJECT.md
#
# Usage: ./scripts/context-sync.sh [adapter...]
# Task:  task context:sync
#
# Override root context: ROOT_CONTEXT=~/dev/.context/AGENT.md ./scripts/context-sync.sh

set -euo pipefail

PROJECT_FILE=".context/PROJECT.md"
SKILLS_DIR=".skills"

# Walk up to find root .context/AGENT.md
find_root_context() {
  local dir
  dir="$(pwd)"
  while [ "$dir" != "/" ]; do
    dir="$(dirname "$dir")"
    if [ -f "$dir/.context/AGENT.md" ]; then
      echo "$dir/.context/AGENT.md"
      return
    fi
  done
  echo ""
}

ROOT_CONTEXT="${ROOT_CONTEXT:-$(find_root_context)}"

if [ ! -f "$PROJECT_FILE" ]; then
  echo "Error: $PROJECT_FILE not found. Are you in a project root?"
  exit 1
fi

if [ -n "$ROOT_CONTEXT" ] && [ -f "$ROOT_CONTEXT" ]; then
  echo "  Root context: $ROOT_CONTEXT"
else
  echo "  No root AGENT.md found (project context only)"
fi

# Emit root context + separator
root_block() {
  if [ -n "$ROOT_CONTEXT" ] && [ -f "$ROOT_CONTEXT" ]; then
    cat "$ROOT_CONTEXT"
    echo ""
    echo "---"
    echo ""
  fi
}

# ── Claude Code ──────────────────────────────────────────────
# Claude Code walks up the tree — it finds ~/dev/CLAUDE.md automatically.
# Project-level CLAUDE.md only needs project-specific context.
generate_claude() {
  cat "$PROJECT_FILE" > CLAUDE.md
  echo "  → CLAUDE.md (project-only; Claude Code inherits root)"
}

# ── AGENTS.md (Crush, Pi, Antigravity) ──────────────────────
# These tools read AGENTS.md from cwd but don't walk up.
# Concatenate root + project.
generate_agents() {
  { root_block; cat "$PROJECT_FILE"; } > AGENTS.md
  echo "  → AGENTS.md (root + project; Crush, Pi, Antigravity)"
}

# ── Cursor ───────────────────────────────────────────────────
generate_cursor() {
  {
    echo "# Cursor rules — auto-generated"
    echo "# Do not edit. Run: task context:sync"
    echo ""
    root_block
    cat "$PROJECT_FILE"
  } > .cursorrules
  echo "  → .cursorrules (root + project)"
}

# ── Aider ────────────────────────────────────────────────────
generate_aider() {
  { root_block; cat "$PROJECT_FILE"; } > .aider.conventions.md
  if [ ! -f .aider.conf.yml ]; then
    cat > .aider.conf.yml << 'YAML'
read: .aider.conventions.md
auto-commits: false
YAML
  fi
  echo "  → .aider.conventions.md (root + project)"
}

# ── Generic system prompt (Open WebUI, Mods, etc.) ──────────
generate_system_prompt() {
  {
    echo "You are a coding assistant working on a specific project."
    echo "Follow all conventions from both the root agent context and project context."
    echo ""
    echo "---"
    echo ""
    root_block
    cat "$PROJECT_FILE"
    echo ""
    echo "---"
  } > .context/system-prompt.txt
  echo "  → .context/system-prompt.txt (root + project)"
}

# ── MCP config ───────────────────────────────────────────────
generate_mcp() {
  if [ ! -f .context/mcp.json ]; then
    cat > .context/mcp.json << 'JSON'
{
  "mcpServers": {
    "knowledge": {
      "url": "http://localhost:3100/mcp",
      "description": "Project knowledge base — vector + graph retrieval"
    }
  }
}
JSON
    echo "  → .context/mcp.json (new)"
  else
    echo "  → .context/mcp.json (exists, skipped)"
  fi
}

echo "Syncing project context from $PROJECT_FILE..."

if [ $# -eq 0 ]; then
  generate_claude
  generate_agents
  generate_cursor
  generate_aider
  generate_system_prompt
  generate_mcp
else
  for adapter in "$@"; do
    case "$adapter" in
      claude)  generate_claude ;;
      agents)  generate_agents ;;
      cursor)  generate_cursor ;;
      aider)   generate_aider ;;
      prompt|system|openwebui|owui|generic) generate_system_prompt ;;
      mcp)     generate_mcp ;;
      *) echo "Unknown adapter: $adapter" ;;
    esac
  done
fi

echo "Done."
