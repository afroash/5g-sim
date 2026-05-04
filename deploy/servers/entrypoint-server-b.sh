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
# 3. IP forwarding — required for GTP-U decap + N6 routing
# ------------------------------------------------------------
echo "[server-b] Enabling IP forwarding..."
sysctl -w net.ipv4.ip_forward=1 > /dev/null

# ------------------------------------------------------------
# 4. FRR — owns OSPF adjacency with both leaves
#    Also advertises 10.45.0.0/24 (UE pool) so return
#    traffic from internet-sim routes back here.
# ------------------------------------------------------------
echo "[server-b] Starting FRR..."
chown -R frr:frr /etc/frr/
/usr/lib/frr/frrinit.sh start

for i in $(seq 1 10); do
    if vtysh -c "show version" > /dev/null 2>&1; then
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

echo ""
echo "[server-b] Phase 1 ready."
echo "[server-b] Expected OSPF neighbors: Leaf1 (10.0.2.9), Leaf2 (10.0.2.13)"
echo "[server-b] Stable address for UPF/gNB: 10.1.1.1"
echo "[server-b] UE pool (10.45.0.0/24) advertised via OSPF."
echo ""

# ------------------------------------------------------------
# Phase 2 placeholder: start NFs
# Uncomment when binaries are baked in (Phase 2)
# ------------------------------------------------------------
# echo "[server-b] Starting UPF..."
# /usr/local/bin/upf &
#
# echo "[server-b] Starting gNB..."
# /usr/local/bin/gnb &

tail -f /var/log/frr/frr.log 2>/dev/null || tail -f /dev/null
