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
# 1. Loopback
# ------------------------------------------------------------
echo "[server-a] Configuring loopback 10.1.0.1/32..."
ip addr add 10.1.0.1/32 dev lo 2>/dev/null || echo "[server-a] Loopback already set"
ip link set lo up

# ------------------------------------------------------------
# 2. Uplinks
# ------------------------------------------------------------
wait_for_iface() {
  local dev=$1
  local max_s=${2:-30}
  if ip link show "$dev" >/dev/null 2>&1; then
    echo "[server-a] $dev already present."
    return 0
  fi
  echo "[server-a] Waiting for containerlab to attach $dev (max ${max_s}s)..."
  local i
  for i in $(seq 1 "$max_s"); do
    if ip link show "$dev" >/dev/null 2>&1; then
      echo "[server-a] $dev attached after ${i}s."
      return 0
    fi
    sleep 1
  done
  echo "[server-a] WARNING: $dev still missing — continuing."
}

wait_for_iface eth1 30
wait_for_iface eth2 30

echo "[server-a] Bringing up eth1 (Leaf1 uplink)..."
ip link set eth1 up 2>/dev/null || echo "[server-a] WARNING: eth1 up failed"

echo "[server-a] Bringing up eth2 (Leaf2 uplink)..."
ip link set eth2 up 2>/dev/null || echo "[server-a] WARNING: eth2 up failed"

# ------------------------------------------------------------
# 3. IP forwarding
# ------------------------------------------------------------
echo "[server-a] Enabling IP forwarding..."
sysctl -w net.ipv4.ip_forward=1 >/dev/null

# ------------------------------------------------------------
# 4. FRR
# ------------------------------------------------------------
echo "[server-a] Starting FRR..."
chown -R frr:frr /etc/frr/

/usr/lib/frr/frrinit.sh start

for i in $(seq 1 10); do
  if vtysh -c "show version" >/dev/null 2>&1; then
    echo "[server-a] FRR is up."
    break
  fi
  echo "[server-a] Waiting for FRR... ($i/10)"
  sleep 2
done

# ------------------------------------------------------------
# 5. Convergence pause (see PHASE1-VERIFY.md for detailed checks)
# ------------------------------------------------------------
echo "[server-a] Waiting for OSPF to converge (15s)..."
sleep 15

echo ""
echo "[server-a] ========================================="
echo "[server-a] Network status"
echo "[server-a] ========================================="
echo ""
echo "--- Interfaces ---"
ip addr show eth1 2>/dev/null || true
ip addr show eth2 2>/dev/null || true
ip addr show lo

echo ""
echo "--- OSPF Neighbors ---"
vtysh -c "show ip ospf neighbor" 2>/dev/null || echo "FRR not ready yet"

echo ""
echo "--- Route Table ---"
ip route show
ip route delete default

echo ""
echo "[server-a] Phase 1 ready."
echo "[server-a] Expected OSPF neighbors: Leaf1 (10.0.2.1), Leaf2 (10.0.2.5)"
echo "[server-a] Stable address for NFs: 10.1.0.1"
echo ""

# ------------------------------------------------------------
# Phase 2
# ------------------------------------------------------------

wait_http() {
  local url=$1
  echo "[server-a] Waiting for $url..."
  for i in $(seq 1 30); do
    if curl -sf "$url" >/dev/null 2>&1; then
      echo "[server-a] Ready: $url"
      return 0
    fi
    sleep 2
  done
  echo "[server-a] WARNING: timeout waiting for $url — continuing"
}

echo "[server-a] Starting NRF..."
/usr/local/bin/nrf -config /etc/5g-sim/nrf.yaml >/var/log/nrf.log 2>&1 &
wait_http http://10.1.0.1:8080/health

echo "[server-a] Starting AMF..."
/usr/local/bin/amf -config /etc/5g-sim/amf.yaml >/var/log/amf.log 2>&1 &
wait_http http://10.1.0.1:8090/health

echo "[server-a] Starting SMF..."
/usr/local/bin/smf -config /etc/5g-sim/smf.yaml >/var/log/smf.log 2>&1 &
wait_http http://10.1.0.1:8081/health

echo ""
echo "[server-a] ========================================="
echo "[server-a] Phase 2 complete — NRF, AMF, SMF running"
echo "[server-a] ========================================="
echo ""

tail -f /var/log/nrf.log /var/log/amf.log /var/log/smf.log 2>/dev/null || tail -f /dev/null
