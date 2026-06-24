#!/usr/bin/env bash
# vodstack host firewall — allow SSH + the three public services only.
#
# Docker publishes ports straight into iptables and can bypass ufw's normal
# rules, so we enforce on BOTH the host INPUT chain (ufw) and Docker's
# DOCKER-USER chain. Public services are already bound to the fixed IP and ops
# services to 127.0.0.1 in the compose file; this is defense-in-depth.
#
# Run as root:  sudo bash deploy/firewall.sh
set -euo pipefail

PUBLIC_PORTS=(38080 38082 38090)   # api, edge, web (keep in sync with .env.prod)
SSH_PORT=22

if ! command -v ufw >/dev/null 2>&1; then
  echo "ufw not found. Install it (apt-get install -y ufw) or adapt for your firewall." >&2
  exit 1
fi

echo "[*] Configuring ufw: default deny incoming, allow SSH + public ports..."
ufw --force reset
ufw default deny incoming
ufw default allow outgoing
ufw allow "${SSH_PORT}/tcp" comment 'SSH'
for p in "${PUBLIC_PORTS[@]}"; do
  ufw allow "${p}/tcp" comment 'vodstack public'
done
ufw --force enable
ufw status verbose

# --- Docker bypass guard -----------------------------------------------------
# Restrict externally-published Docker ports to the public set. Anything Docker
# binds to 127.0.0.1 is already local-only; this blocks unexpected 0.0.0.0 binds
# from the internet while leaving the intended public ports open.
if iptables -L DOCKER-USER >/dev/null 2>&1; then
  echo "[*] Hardening Docker-USER chain..."
  iptables -F DOCKER-USER
  # Allow established/related + loopback + internal docker traffic.
  iptables -A DOCKER-USER -i lo -j RETURN
  iptables -A DOCKER-USER -m state --state RELATED,ESTABLISHED -j RETURN
  for p in "${PUBLIC_PORTS[@]}"; do
    iptables -A DOCKER-USER -p tcp --dport "${p}" -j RETURN
  done
  # Drop other new inbound to published container ports from outside.
  iptables -A DOCKER-USER -m state --state NEW -j DROP
  iptables -A DOCKER-USER -j RETURN
  echo "[*] Docker-USER rules applied (note: not persisted across reboot — use"
  echo "    iptables-persistent / netfilter-persistent to save if desired)."
fi

echo "[+] Done. Public: ${PUBLIC_PORTS[*]} + SSH. Ops ports (MinIO/Grafana/Prometheus)"
echo "    are bound to 127.0.0.1 — reach them via: ssh -L <port>:127.0.0.1:<port> user@host"
