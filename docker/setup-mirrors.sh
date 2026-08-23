#!/usr/bin/env bash
set -euo pipefail
echo "Configuring docker daemon registry mirrors..."
sudo mkdir -p /etc/docker
cat <<'EOF' | sudo tee /etc/docker/daemon.json
{
  "registry-mirrors": [
    "https://mirrors.ustc.edu.cn",
    "https://docker.mirrors.ustc.edu.cn",
    "https://docker.mirrors.ustc.edu.cn"
  ]
}
EOF
echo "Restarting docker..."
sudo systemctl restart docker 2>&1 | tail -3
sleep 3
docker pull debian:bookworm-slim 2>&1 | tail -5