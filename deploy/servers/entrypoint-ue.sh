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
# 1. Wait for containerlab to attach eth1.
#    The leaf1 side is Ethernet1/0 (non-contiguous IOL slot),
#    so attachment can lag a few seconds after container start.
# ------------------------------------------------------------
echo "[ue] Waiting for eth1 to be attached by containerlab..."
for i in $(seq 1 60); do
  if ip link show eth1 >/dev/null 2>&1; then
    echo "[ue] eth1 is present (after ${i}s)"
    break
  fi
  if [ "$i" -eq 60 ]; then
    echo "[ue] ERROR: eth1 never appeared — check 'docker exec leaf1 ip -br link' for eth4"
  fi
  sleep 1
done

ip link set eth1 up

# ------------------------------------------------------------
# 2. Assign the UE's IP. ContainerLab does NOT auto-assign IPs
#    on links for `linux`-kind nodes — only for kinds with a
#    config template (cisco_iol, srl, etc.). So we add it here.
#    Idempotent: ignore "File exists" if we re-run.
# ------------------------------------------------------------
echo "[ue] Assigning 10.0.2.18/30 to eth1..."
ip addr add 10.0.2.18/30 dev eth1 2>/dev/null \
  || ip addr show eth1 | grep -q "10.0.2.18" \
  || { echo "[ue] ERROR: failed to assign IP to eth1"; }

# ------------------------------------------------------------
# 3. Static route — UE has no OSPF; reach gNB at 10.1.1.1
#    via leaf1 (10.0.2.17), now that 10.0.2.16/30 is on eth1.
# ------------------------------------------------------------
echo "[ue] Adding static route via leaf1 (10.0.2.17)..."
ip route delete default 2>/dev/null || true
ip route replace 10.1.1.1/32 via 10.0.2.17 dev eth1


echo ""
echo "--- Interfaces ---"
ip addr show eth1

echo ""
echo "--- Routes ---"
ip route show

# ------------------------------------------------------------
# 4. Wait for gNB SCTP port to be reachable before starting.
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
# 5. Start UE binary — logs go to /var/log/ue.log
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
