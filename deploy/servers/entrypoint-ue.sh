#!/bin/bash
# ============================================================
# UE Simulator — Entrypoint
# Standalone UE: connects to gNB, registers, establishes PDU
# session, creates TUN interface, and tests data connectivity.
# ============================================================

set -uo pipefail

echo "[ue] ========================================="
echo "[ue] Starting UE Simulator"
echo "[ue] ========================================="

# ------------------------------------------------------------
# 1. Interface — clab exec already ran "ip link set eth1 up"
#    but make it idempotent here too.
# ------------------------------------------------------------
echo "[ue] Bringing up eth1..."
ip link set eth1 up 2>/dev/null || true

# # ContainerLab assigns the IP to eth1 via the topology ipv4 field,
# # but we may need a moment for it to appear.
# for i in $(seq 1 5); do
#   if ip addr show eth1 | grep -q "10.0.2.18"; then
#     echo "[ue] eth1 has IP 10.0.2.18/30"
#     break
#   fi
#   echo "[ue] Waiting for eth1 IP... ($i/5)"
#   sleep 1
# done
#We setup Eth1 ip here.
#
echo "[ue] Adding IP 10.0.2.18/30 to eth1..."
ip addr add 10.0.2.18/30 dev eth1
echo "[ue] eth1 has IP 10.0.2.18/30"
# ------------------------------------------------------------
# 2. Routing — UE has no OSPF, so add a static default route
#    via leaf1 (10.0.2.17) which has full OSPF visibility.
#    This lets the UE reach gNB at 10.1.1.1 on server-b.
# ------------------------------------------------------------
echo "[ue] Adding static route via leaf1 (10.0.2.17)..."
ip route delete default
ip route add 10.1.1.1/32 via 10.0.2.17 dev eth1

echo ""
echo "--- Interfaces ---"
ip addr show eth1

echo ""
echo "--- Routes ---"
ip route show

# ------------------------------------------------------------
# 3. Wait for gNB SCTP port to be reachable before starting.
#    gNB binds on 10.1.1.1:38412 (SCTP/N2 interface).
#    Use TCP reachability as a proxy — SCTP isn't available
#    via nc in most images, but the HTTP health endpoint is.
# ------------------------------------------------------------
echo ""
echo "[ue] Waiting for gNB health endpoint (http://10.1.1.1:8003/health)..."
for i in $(seq 1 30); do
  if curl -sf http://10.1.1.1:8003/health >/dev/null 2>&1; then
    echo "[ue] gNB is ready."
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "[ue] WARNING: gNB health check timed out — starting anyway"
  else
    echo "[ue] Waiting for gNB... ($i/30)"
    sleep 2
  fi
done

# ------------------------------------------------------------
# 4. Start UE binary — logs go to /var/log/ue.log
#    After registration and PDU session setup, the binary
#    creates a TUN interface and runs a connectivity test.
# ------------------------------------------------------------
echo ""
echo "[ue] ========================================="
echo "[ue] Starting UE binary — logging to /var/log/ue.log"
echo "[ue] ========================================="
echo ""

/usr/local/bin/ue -config /etc/5g-sim/ue.yaml >/var/log/ue.log 2>&1 &
UE_PID=$!

# Give the UE time to register then print the log so far.
# The tail below keeps streaming it for the container lifetime.
sleep 5
echo "[ue] --- Initial log output ---"
cat /var/log/ue.log 2>/dev/null || true
echo "[ue] --- Tailing /var/log/ue.log ---"

# Keep the container alive; stream log output to stdout so
# "docker logs" / "clab exec" show live UE state.
tail -f /var/log/ue.log 2>/dev/null || wait "$UE_PID"
