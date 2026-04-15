#!/usr/bin/env bash
# fix-koala-sudoers.sh
# Run on koala as mathias. Fixes NOPASSWD rule so act_runner can call
# 'sudo k3s ctr' without a password prompt.
set -euo pipefail

sudo tee /etc/sudoers.d/act_runner > /dev/null << 'EOF'
# Allow act_runner to import images into k3s containerd
# Both the PATH symlink and the versioned binary are listed for safety
mathias ALL=(root) NOPASSWD: /usr/local/bin/k3s ctr *
mathias ALL=(root) NOPASSWD: /var/lib/rancher/k3s/data/current/bin/k3s ctr *
EOF

sudo chmod 440 /etc/sudoers.d/act_runner
sudo visudo -cf /etc/sudoers.d/act_runner && echo "✓ sudoers updated"
