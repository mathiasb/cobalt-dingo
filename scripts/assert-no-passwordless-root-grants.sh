#!/usr/bin/env bash
# Executable assertion (infra#558): nothing in this repo's scripts, Taskfile or workflows installs a
# passwordless root grant on a host.
#
# Why: scripts/setup-koala-runner.sh and fix-koala-sudoers.sh wrote /etc/sudoers.d/act_runner with
# `NOPASSWD: k3s ctr *` for the HOST act_runner (2026-04-15). `k3s ctr *` is root-equivalent (a privileged
# bind-mounted container), CI moved to an in-cluster pod runner on 2026-08-25 (infra#233/#241), the host
# runner has been inactive since, and nothing used the grant. A grant like that must be a deliberate,
# reviewed host change (infra `host/`, ADR-0005), not a line in a setup script.
#
# Asserts on BEHAVIOUR (an installed sudoers fragment), searching code paths only; docs/ is excluded on
# purpose because ADRs and notes discuss the grant (the same trap assert-no-vendor-clients.sh records).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

self="scripts/assert-no-passwordless-root-grants.sh"
hits="$(grep -rIn -E 'NOPASSWD|/etc/sudoers\.d' scripts Taskfile.yml .gitea 2>/dev/null | grep -v "^${self}:" || true)"

if [ -n "$hits" ]; then
  echo "  RED   a script, task or workflow installs or mentions a passwordless sudo grant:"
  printf '%s\n' "$hits" | sed 's/^/        /'
  exit 1
fi
echo "  GREEN no passwordless root grant is installed by this repo's code paths"
