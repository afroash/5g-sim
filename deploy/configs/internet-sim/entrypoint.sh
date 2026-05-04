#!/bin/sh
# ============================================================
# Internet Sim — Entrypoint
# Alpine + FRR + nginx
# ============================================================

set -uo pipefail

echo "[internet-sim] ========================================="
echo "[internet-sim] Starting Internet Simulator"
echo "[internet-sim] ========================================="

# ------------------------------------------------------------
# 1. Loopback — nginx binds here, this is the UE curl target
# ------------------------------------------------------------
echo "[internet-sim] Configuring loopback 10.100.0.1/32..."
ip addr add 10.100.0.1/32 dev lo 2>/dev/null || echo "[internet-sim] Loopback already set"
ip link set lo up

# ------------------------------------------------------------
# 2. Uplink to spine1
# ------------------------------------------------------------
echo "[internet-sim] Bringing up eth1 (spine1 uplink)..."
ip link set eth1 up

# ------------------------------------------------------------
# 3. IP forwarding
# ------------------------------------------------------------
sysctl -w net.ipv4.ip_forward=1 > /dev/null

# ------------------------------------------------------------
# 4. FRR — Alpine path is /usr/lib/frr/frrinit.sh
#    Injects default route into OSPF for the whole fabric
# ------------------------------------------------------------
echo "[internet-sim] Starting FRR..."
chown -R frr:frr /etc/frr/
/usr/lib/frr/frrinit.sh start

for i in $(seq 1 10); do
    if vtysh -c "show version" > /dev/null 2>&1; then
        echo "[internet-sim] FRR is up."
        break
    fi
    echo "[internet-sim] Waiting for FRR... ($i/10)"
    sleep 2
done

# ------------------------------------------------------------
# 5. nginx — serves HTTP on 10.100.0.1:80 (and all interfaces)
# ------------------------------------------------------------
echo "[internet-sim] Starting nginx..."
nginx

echo ""
echo "[internet-sim] ========================================="
echo "[internet-sim] Ready"
echo "[internet-sim] HTTP target : http://10.100.0.1/"
echo "[internet-sim] OSPF        : advertising default route"
echo "[internet-sim] Uplink      : eth1 -> spine1 (10.0.0.0/30)"
echo "[internet-sim] ========================================="
echo ""

# Keep container alive, tail FRR log for visibility
tail -f /var/log/frr/frr.log 2>/dev/null || tail -f /dev/null
