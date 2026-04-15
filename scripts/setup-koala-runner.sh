#!/usr/bin/env bash
# setup-koala-runner.sh
# Run as mathias on koala. Installs and registers the Gitea act_runner.
#
# Usage:
#   GITEA_RUNNER_TOKEN=<token> bash setup-koala-runner.sh
#
# Token: Gitea → Site Administration → Actions → Runners → Create Runner

set -euo pipefail

GITEA_INSTANCE="https://gitea.d-ma.be"
RUNNER_NAME="koala"
RUNNER_LABELS="self-hosted:host,linux,amd64"
RUNNER_WORKDIR="/var/lib/act_runner"

# ── Helpers ──────────────────────────────────────────────────────────────────

info()  { echo "▶ $*"; }
ok()    { echo "✓ $*"; }
die()   { echo "✗ $*" >&2; exit 1; }

require_token() {
  if [[ -z "${GITEA_RUNNER_TOKEN:-}" ]]; then
    die "GITEA_RUNNER_TOKEN is not set. Export it before running this script."
  fi
}

# ── 1. buildah ───────────────────────────────────────────────────────────────

info "Installing buildah..."
sudo pacman -S --noconfirm --needed buildah
buildah --version | head -1 && ok "buildah ready"

# ── 2. Task ──────────────────────────────────────────────────────────────────

info "Installing Task..."
if ! command -v task &>/dev/null; then
  if pacman -Qi go-task &>/dev/null; then
    # Arch installs as 'go-task' binary on some versions — symlink to 'task'
    GOTASK_BIN=$(pacman -Ql go-task | awk '{print $2}' | grep -E '/bin/[^/]+$' | head -1)
    if [[ -n "$GOTASK_BIN" && -x "$GOTASK_BIN" ]]; then
      sudo ln -sf "$GOTASK_BIN" /usr/local/bin/task
      ok "symlinked ${GOTASK_BIN} → /usr/local/bin/task"
    fi
  fi
  # Still not found? Install from upstream script
  if ! command -v task &>/dev/null; then
    sudo sh -c "$(curl -fsSL https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin
  fi
fi
task --version | head -1 && ok "task ready"

# ── 3. golangci-lint ─────────────────────────────────────────────────────────

info "Installing golangci-lint..."
if command -v golangci-lint &>/dev/null; then
  ok "golangci-lint already in PATH"
else
  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
    | sudo sh -s -- -b /usr/local/bin latest
fi
golangci-lint --version | head -1 && ok "golangci-lint ready"

# ── 4. govulncheck ───────────────────────────────────────────────────────────

info "Installing govulncheck..."
go install golang.org/x/vuln/cmd/govulncheck@latest
sudo ln -sf "${HOME}/go/bin/govulncheck" /usr/local/bin/govulncheck
command -v govulncheck && ok "govulncheck ready"

# ── 5. act_runner ────────────────────────────────────────────────────────────

info "Installing act_runner..."
if command -v act_runner &>/dev/null; then
  ok "act_runner already installed: $(act_runner --version)"
else
  VER=$(curl -fsSL "https://gitea.com/gitea/act_runner/releases" \
    | grep -oP 'act_runner/releases/tag/v\K[0-9]+\.[0-9]+\.[0-9]+' | head -1)
  [[ -z "$VER" ]] && die "Could not determine latest act_runner version"
  info "Downloading act_runner v${VER}..."
  curl -fsSL \
    "https://gitea.com/gitea/act_runner/releases/download/v${VER}/act_runner-${VER}-linux-amd64" \
    -o /tmp/act_runner
  chmod +x /tmp/act_runner
  sudo mv /tmp/act_runner /usr/local/bin/act_runner
  ok "act_runner v${VER} installed"
fi

# ── 6. sudoers for k3s ctr ───────────────────────────────────────────────────

info "Configuring sudoers for k3s ctr..."
sudo tee /etc/sudoers.d/act_runner > /dev/null << 'EOF'
# Allow act_runner to import images into k3s containerd
mathias ALL=(root) NOPASSWD: /var/lib/rancher/k3s/data/current/bin/k3s ctr *
EOF
sudo chmod 440 /etc/sudoers.d/act_runner
sudo visudo -cf /etc/sudoers.d/act_runner && ok "sudoers valid"

# ── 7. Runner working directory ──────────────────────────────────────────────

info "Creating runner working directory ${RUNNER_WORKDIR}..."
sudo mkdir -p "${RUNNER_WORKDIR}"
sudo chown "$(whoami):$(whoami)" "${RUNNER_WORKDIR}"
ok "directory ready"

# ── 8. Register runner ───────────────────────────────────────────────────────

require_token

if [[ -f "${RUNNER_WORKDIR}/.runner" ]]; then
  ok "Runner already registered (.runner file exists) — skipping registration"
else
  info "Registering runner with ${GITEA_INSTANCE}..."
  # act_runner writes .runner into the current directory — cd first
  (
    cd "${RUNNER_WORKDIR}"
    act_runner register \
      --no-interactive \
      --instance "${GITEA_INSTANCE}" \
      --token    "${GITEA_RUNNER_TOKEN}" \
      --name     "${RUNNER_NAME}" \
      --labels   "${RUNNER_LABELS}"
  )
  ok "Runner registered"
fi

# ── 9. systemd service ───────────────────────────────────────────────────────

info "Installing systemd service..."
sudo tee /etc/systemd/system/act_runner.service > /dev/null << EOF
[Unit]
Description=Gitea Actions Runner (koala)
After=network.target

[Service]
Type=simple
User=$(whoami)
WorkingDirectory=${RUNNER_WORKDIR}
Environment=PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin
Environment=HOME=${HOME}
ExecStart=/usr/local/bin/act_runner daemon
Restart=always
RestartSec=10s

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now act_runner
ok "act_runner service enabled and started"

# ── 10. Verify ───────────────────────────────────────────────────────────────

echo ""
echo "── Verification ─────────────────────────────────────────────────────────"
sudo systemctl status act_runner --no-pager -l | head -20
echo ""
echo "Next: confirm runner appears online at ${GITEA_INSTANCE}/-/admin/runners"
echo "Then push a commit to trigger the first CI run:"
echo "  task push   (from your dev machine)"
