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
# 2. Uplinks — ContainerLab may attach veths slightly after netns start.
# ------------------------------------------------------------
wait_for_iface() {
  local dev=$1
  local max_s=${2:-30}
  if ip link show "$dev" >/dev/null 2>&1; then
    echo "[server-b] $dev already present."
    return 0
  fi
  echo "[server-b] Waiting for containerlab to attach $dev (max ${max_s}s)..."
  local i
  for i in $(seq 1 "$max_s"); do
    if ip link show "$dev" >/dev/null 2>&1; then
      echo "[server-b] $dev attached after ${i}s."
      return 0
    fi
    sleep 1
  done
  echo "[server-b] WARNING: $dev still missing — continuing; ip link may fail."
}

wait_for_iface eth1 30
wait_for_iface eth2 30

echo "[server-b] Bringing up eth1 (Leaf1 uplink)..."
ip link set eth1 up 2>/dev/null || echo "[server-b] WARNING: eth1 up failed"

echo "[server-b] Bringing up eth2 (Leaf2 uplink)..."
ip link set eth2 up 2>/dev/null || echo "[server-b] WARNING: eth2 up failed"

# ------------------------------------------------------------
# 3. IP forwarding + reverse path filter
# ------------------------------------------------------------
echo "[server-b] Enabling IP forwarding..."
sysctl -w net.ipv4.ip_forward=1 >/dev/null
sysctl -w net.ipv4.conf.all.rp_filter=0 >/dev/null
sysctl -w net.ipv4.conf.default.rp_filter=0 >/dev/null

# ------------------------------------------------------------
# 4. FRR
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
# 5. Let fabric converge (IOS + zebra); validate OSPF manually via PHASE1-VERIFY.md
# ------------------------------------------------------------
echo "[server-b] Waiting for OSPF to converge (30s)..."
sleep 30

echo ""
echo "[server-b] ========================================="
echo "[server-b] Network status"
echo "[server-b] ========================================="
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
echo "[server-b] Phase 1 ready."
echo "[server-b] Expected OSPF neighbors: Leaf1 (10.0.2.9), Leaf2 (10.0.2.13)"
echo "[server-b] Stable address for UPF/gNB: 10.1.1.1"
echo "[server-b] UE pool (10.45.0.0/24) advertised via OSPF after upf-n6 exists."
echo ""

# ------------------------------------------------------------
# Phase 2
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

# upf-n6 appears only after UPF starts; bind it to OSPF so 10.45.0.0/24 is originated.
if ip link show upf-n6 >/dev/null 2>&1; then
  echo "[server-b] Attaching upf-n6 to OSPF (passive; stub for 10.45.0.0/24)..."
  vtysh <<'VTYSH_EOF'
configure terminal
interface upf-n6
 ip ospf area 0
exit
VTYSH_EOF
  sleep 2
  if vtysh -c "show ip ospf interface upf-n6" 2>/dev/null | grep -q 'Area 0.0.0.0'; then
    echo "[server-b] OSPF on upf-n6 active."
  else
    echo "[server-b] WARNING: check 'vtysh -c \"show ip ospf interface upf-n6\"' — UE return path may be broken."
  fi
else
  echo "[server-b] WARNING: upf-n6 missing after UPF health — skipping OSPF attach."
fi

echo "[server-b] Starting gNB..."
/usr/local/bin/gnb -config /etc/5g-sim/gnb.yaml >/var/log/gnb.log 2>&1 &

echo ""
echo "[server-b] ========================================="
echo "[server-b] Phase 2 complete — UPF and gNB running"
echo "[server-b] ========================================="
echo ""

tail -f /var/log/upf.log /var/log/gnb.log 2>/dev/null || tail -f /dev/null
