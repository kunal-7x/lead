#!/usr/bin/env bash
# Enforce Vobiz-only access to the LiveKit SIP/RTP published ports at the Docker
# forwarding layer.
#
# WHY: Docker publishes container ports by inserting its own iptables NAT/filter
# rules, which BYPASS UFW. For container-published (forwarded) traffic, the
# DOCKER-USER chain is consulted first, so that is where a real allowlist must
# live. This script allowlists Vobiz source IP(s) to 5060/udp and the narrowed
# RTP range 10000-10200/udp, and DROPs every other source to those ports.
# UFW still protects host-level (non-Docker) services. NO port is opened wide.
#
# Idempotent: safe to re-run; removes its own prior rules (matched by comment)
# before re-adding. Re-applied on boot / docker restart via a systemd unit.
set -euo pipefail

VOBIZ_IPS=("13.203.7.132")
TAG="livekit-vobiz"

# Wait for the DOCKER-USER chain to exist (Docker creates it on start).
for _ in $(seq 1 30); do
  if iptables -L DOCKER-USER -n >/dev/null 2>&1; then break; fi
  sleep 1
done

# Start from a clean DOCKER-USER chain so re-runs don't accumulate duplicates.
# On this box DOCKER-USER holds ONLY our rules (Docker leaves it empty by
# default), so flushing it is safe and idempotent.
iptables -F DOCKER-USER 2>/dev/null || true

apply() {
  local proto="$1" dport="$2"
  local ip
  for ip in "${VOBIZ_IPS[@]}"; do
    iptables -I DOCKER-USER -p "$proto" --dport "$dport" -s "$ip" \
      -m comment --comment "${TAG} allow" -j RETURN
  done
  iptables -A DOCKER-USER -p "$proto" --dport "$dport" \
    -m comment --comment "${TAG} deny" -j DROP
}

apply udp 5060
apply udp 10000:10200

# IPv6: Vobiz has no in-scope IPv6 address and the box has no public IPv6, but
# Docker also publishes on [::]. Defense-in-depth: DROP all IPv6 to these ports
# so they can never be reachable even if a v6 address is later assigned.
if command -v ip6tables >/dev/null 2>&1; then
  ip6tables -F DOCKER-USER 2>/dev/null || true
  for dport in 5060 10000:10200; do
    ip6tables -A DOCKER-USER -p udp --dport "$dport" \
      -m comment --comment "${TAG} deny6" -j DROP 2>/dev/null || true
  done
fi

echo "DOCKER-USER allowlist applied for: ${VOBIZ_IPS[*]}"
