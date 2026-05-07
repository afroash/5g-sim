#!/bin/bash
# ============================================================
# Server B — Entrypoint
# User Plane server: UPF, gNB (Phase 2)
# Phase 1: Network setup + OSPF only.
# ============================================================

set -uo pipefail

echo "[server-b] ========================================="
echo "[server-b] Starting Server B (User Plane)"
echo "[server-b] ========================================="

# ------------------------------------------------------------
# 1. Loopback — stable address UPF and gNB bind to
# ------------------------------------------------------------
echo "[server-b] Configuring loopback 10.1.1.1/32..."
ip addr add 10.1.1.1/32 dev lo 2>/dev/null || echo "[server-b] Loopback already set"
ip link set lo up

# ------------------------------------------------------------
# 2. Uplinks
# ------------------------------------------------------------
echo "[server-b] Bringing up eth1 (Leaf1 uplink)..."
ip link set eth1 up

echo "[server-b] Bringing up eth2 (Leaf2 uplink)..."
ip link set eth2 up

# ------------------------------------------------------------
# 3. IP forwarding + reverse path filter
#    ip_forward: required for GTP-U decap + N6 routing
#    rp_filter=0: allows GTP-U relay between UE-facing (port 2153)
#    and UPF-facing tunnels which arrive from different source subnets
# ------------------------------------------------------------
echo "[server-b] Enabling IP forwarding..."
sysctl -w net.ipv4.ip_forward=1 >/dev/null
sysctl -w net.ipv4.conf.all.rp_filter=0 >/dev/null
sysctl -w net.ipv4.conf.default.rp_filter=0 >/dev/null

# ------------------------------------------------------------
# 4. FRR — owns OSPF adjacency with both leaves
#    Also advertises 10.45.0.0/24 (UE pool) so return
#    traffic from internet-sim routes back here.
# ------------------------------------------------------------
echo "[server-b] Starting FRR..."
chown -R frr:frr /etc/frr/
/usr/lib/frr/frrinit.sh start

for i in $(seq 1 10); do
  if vtysh -c "show version" >/dev/null 2>&1; then
    echo "[server-b] FRR is up."
    break
  fi
  echo "[server-b] Waiting for FRR... ($i/10)"
  sleep 2
done

# ------------------------------------------------------------
# 5. Status
# ------------------------------------------------------------
echo "[server-b] Waiting for OSPF to converge (15s)..."
sleep 15

echo ""
echo "[server-b] ========================================="
echo "[server-b] Network status"
echo "[server-b] ========================================="
echo ""
echo "--- Interfaces ---"
ip addr show eth1
ip addr show eth2
ip addr show lo

echo ""
echo "--- OSPF Neighbors ---"
vtysh -c "show ip ospf neighbor" 2>/dev/null || echo "FRR not ready yet"

echo ""
echo "--- Route Table ---"
ip route show
ip route delete default

echo ""
echo "[server-b] Phase 1 ready."
echo "[server-b] Expected OSPF neighbors: Leaf1 (10.0.2.9), Leaf2 (10.0.2.13)"
echo "[server-b] Stable address for UPF/gNB: 10.1.1.1"
echo "[server-b] UE pool (10.45.0.0/24) advertised via OSPF."
echo ""

# ------------------------------------------------------------
# Phase 2: start NFs in dependency order with health checks
# UPF before gNB — gNB's PDU sessions reach the UPF on N3.
# gNB connects to AMF on Server A; retry loop handles timing.
# ------------------------------------------------------------

wait_http() {
  local url=$1
  echo "[server-b] Waiting for $url..."
  for i in $(seq 1 30); do
    if curl -sf "$url" >/dev/null 2>&1; then
      echo "[server-b] Ready: $url"
      return 0
    fi
    sleep 2
  done
  echo "[server-b] WARNING: timeout waiting for $url — continuing"
}

echo "[server-b] Starting UPF..."
/usr/local/bin/upf -config /etc/5g-sim/upf.yaml >/var/log/upf.log 2>&1 &
wait_http http://10.1.1.1:8002/health

echo "[server-b] Starting gNB..."
/usr/local/bin/gnb -config /etc/5g-sim/gnb.yaml >/var/log/gnb.log 2>&1 &
# gNB readiness is the SCTP association with AMF — its retry loop handles
# the case where Server A is not yet ready.

echo ""
echo "[server-b] ========================================="
echo "[server-b] Phase 2 complete — UPF and gNB running"
echo "[server-b] ========================================="
echo ""

tail -f /var/log/upf.log /var/log/gnb.log 2>/dev/null || tail -f /dev/null
