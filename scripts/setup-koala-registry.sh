#!/usr/bin/env bash
# setup-koala-registry.sh
# Run once on koala as mathias.
# Deploys a local OCI registry on localhost:5000 and configures k3s to
# treat it as an insecure (plain HTTP) registry.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ── 1. Configure k3s to allow insecure access to localhost:5000 ──────────────

info()  { echo "▶ $*"; }
ok()    { echo "✓ $*"; }

info "Configuring k3s insecure registry..."
sudo mkdir -p /etc/rancher/k3s
sudo tee /etc/rancher/k3s/registries.yaml > /dev/null << 'EOF'
mirrors:
  "localhost:5000":
    endpoint:
      - "http://localhost:5000"
configs:
  "localhost:5000":
    tls:
      insecure_skip_verify: true
EOF
ok "registries.yaml written"

# ── 2. Restart k3s to pick up registry config ─────────────────────────────────

info "Restarting k3s..."
sudo systemctl restart k3s
sleep 10
kubectl get nodes && ok "k3s restarted"

# ── 3. Deploy the registry ────────────────────────────────────────────────────

info "Deploying local registry..."
kubectl apply -f "${REPO_ROOT}/k8s/registry/registry.yaml"

# Wait for registry pod to be ready
kubectl rollout status deployment/registry -n registry --timeout=120s
ok "registry deployed"

# ── 4. Verify ─────────────────────────────────────────────────────────────────

sleep 3
curl -s http://localhost:5000/v2/ | grep -q '{}' \
  && ok "registry is reachable at localhost:5000" \
  || echo "⚠ registry not yet responding — check: kubectl logs -n registry deploy/registry"
