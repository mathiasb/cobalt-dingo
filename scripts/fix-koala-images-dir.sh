#!/usr/bin/env bash
# fix-koala-images-dir.sh
# Run once on koala as mathias.
# Creates the k3s agent images directory owned by mathias so the act_runner
# can copy OCI archives there without needing sudo for mkdir/cp.
set -euo pipefail

DIR="/var/lib/rancher/k3s/agent/images"

sudo mkdir -p "${DIR}"
sudo chown "$(whoami):$(whoami)" "${DIR}"
sudo chmod 755 "${DIR}"
echo "✓ ${DIR} owned by $(whoami)"
