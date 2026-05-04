#!/bin/bash
# ============================================================
# Server A — Entrypoint
# Control Plane server: NRF, AMF, SMF (Phase 2)
# Phase 1: Network setup + OSPF only.
# ============================================================

# Don't exit on error — let FRR retry gracefully
set -uo pipefail

echo "[server-a] ========================================="
echo "[server-a] Starting Server A (Control Plane)"
echo "[server-a] ========================================="

# ------------------------------------------------------------
# 1. Loopback — stable SBA address all NFs bind to
# ------------------------------------------------------------
echo "[server-a] Configuring loopback 10.1.0.1/32..."
ip addr add 10.1.0.1/32 dev lo 2>/dev/null || echo "[server-a] Loopback already set"
ip link set lo up

# ------------------------------------------------------------
# 2. Uplinks — ContainerLab assigns IPs via the ipv4 field,
#    but we need to ensure the interfaces are up.
# ------------------------------------------------------------
echo "[server-a] Bringing up eth1 (Leaf1 uplink)..."
ip link set eth1 up

echo "[server-a] Bringing up eth2 (Leaf2 uplink)..."
ip link set eth2 up

# ------------------------------------------------------------
# 3. IP forwarding
# ------------------------------------------------------------
echo "[server-a] Enabling IP forwarding..."
sysctl -w net.ipv4.ip_forward=1 > /dev/null

# ------------------------------------------------------------
# 4. FRR — owns OSPF adjacency with both leaves
# ------------------------------------------------------------
echo "[server-a] Starting FRR..."
# Ensure correct ownership
chown -R frr:frr /etc/frr/

# Start FRR services
/usr/lib/frr/frrinit.sh start

# Wait for FRR to come up
for i in $(seq 1 10); do
    if vtysh -c "show version" > /dev/null 2>&1; then
        echo "[server-a] FRR is up."
        break
    fi
    echo "[server-a] Waiting for FRR... ($i/10)"
    sleep 2
done

# ------------------------------------------------------------
# 5. Wait for OSPF to converge, then print status
# ------------------------------------------------------------
echo "[server-a] Waiting for OSPF to converge (15s)..."
sleep 15

echo ""
echo "[server-a] ========================================="
echo "[server-a] Network status"
echo "[server-a] ========================================="
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
echo "[server-a] Phase 1 ready."
echo "[server-a] Expected OSPF neighbors: Leaf1 (10.0.2.1), Leaf2 (10.0.2.5)"
echo "[server-a] Stable address for NFs: 10.1.0.1"
echo ""

# ------------------------------------------------------------
# Phase 2 placeholder: start NFs
# Uncomment when binaries are baked in (Phase 2)
# ------------------------------------------------------------
# echo "[server-a] Starting NRF..."
# /usr/local/bin/nrf &
#
# echo "[server-a] Starting AMF..."
# /usr/local/bin/amf &
#
# echo "[server-a] Starting SMF..."
# /usr/local/bin/smf &

# Keep container alive, tail FRR log for visibility
tail -f /var/log/frr/frr.log 2>/dev/null || tail -f /dev/null
