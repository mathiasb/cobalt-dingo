#!/usr/bin/env bash
# Generates harness-specific context files from .context/PROJECT.md
# Project-level script — run from a project directory.
#
# For Claude Code: generates project-only CLAUDE.md (it inherits root via tree walk)
# For everything else: concatenates root AGENT.md + project PROJECT.md
#
# Cursor and Aider adapters removed 2026-09-01 — neither is a supported harness
# any more. Supported: Claude Code, Antigravity, OpenCode, Mistral Vibe, Crush,
# Pi. All read CLAUDE.md or AGENTS.md; system-prompt.txt remains for Open WebUI,
# Mods and raw API use.
#
# Usage: ./scripts/context-sync.sh [--force] [adapter...]
# Task:  task context:sync
#
# Override root context: ROOT_CONTEXT=~/dev/.context/AGENT.md ./scripts/context-sync.sh

set -euo pipefail

# Parse --force flag and collect adapter names separately
FORCE=false
ADAPTERS=()
for _arg in "$@"; do
  case "$_arg" in
    --force) FORCE=true ;;
    *) ADAPTERS+=("$_arg") ;;
  esac
done

PROJECT_FILE=".context/PROJECT.md"

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

# Pre-flight: reject unfilled {{...}} placeholders unless --force
if [ "$FORCE" = false ]; then
  _placeholders=$(grep -n '{{[^}]*}}' "$PROJECT_FILE" 2>/dev/null || true)
  if [ -n "$_placeholders" ]; then
    echo "Error: unfilled placeholders in $PROJECT_FILE:" >&2
    while IFS= read -r _match; do
      _lineno="${_match%%:*}"
      _content="${_match#*:}"
      _token=$(printf '%s' "$_content" | grep -o '{{[^}]*}}' | head -1)
      echo "  $PROJECT_FILE:$_lineno: unfilled placeholder $_token" >&2
    done <<< "$_placeholders"
    echo "" >&2
    echo "Fill these placeholders, then re-run: task context:sync" >&2
    echo "To bypass validation: bash scripts/context-sync.sh --force" >&2
    exit 1
  fi
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
  # Ensure baseline file exists with project-specific knowledge server
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
  fi

  # Merge root mcp-servers.json if found alongside root AGENT.md
  local root_mcp=""
  if [ -n "$ROOT_CONTEXT" ] && [ -f "$ROOT_CONTEXT" ]; then
    local candidate
    candidate="$(dirname "$ROOT_CONTEXT")/mcp-servers.json"
    [ -f "$candidate" ] && root_mcp="$candidate"
  fi

  if [ -z "$root_mcp" ]; then
    echo "  → .context/mcp.json (exists, no root mcp-servers.json found)"
    return
  fi

  # Root servers take precedence over project entries on key conflict
  local root_servers count updated
  root_servers=$(jq '.servers' "$root_mcp")
  count=$(printf '%s' "$root_servers" | jq 'keys | length')
  updated=$(jq --argjson root "$root_servers" \
    '.mcpServers = (.mcpServers + $root)' \
    .context/mcp.json)
  printf '%s\n' "$updated" > .context/mcp.json
  echo "  → .context/mcp.json (merged $count root servers)"
}

echo "Syncing project context from $PROJECT_FILE..."

if [ ${#ADAPTERS[@]} -eq 0 ]; then
  generate_claude
  generate_agents
  generate_system_prompt
  generate_mcp
else
  for adapter in "${ADAPTERS[@]}"; do
    case "$adapter" in
      claude)  generate_claude ;;
      agents)  generate_agents ;;
      prompt|system|openwebui|owui|generic) generate_system_prompt ;;
      mcp)     generate_mcp ;;
      *) echo "Unknown adapter: $adapter" >&2; exit 1 ;;
    esac
  done
fi

echo "Done."
