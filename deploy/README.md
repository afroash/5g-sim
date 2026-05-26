# 5g-sim — Deploy

This directory contains everything needed to deploy the 5g-sim
network topology using ContainerLab (Cisco IOL leaves/spine, Linux
nodes for NFs, UE, and the internet simulator).

## Structure

```
deploy/
├── build.sh                    # Build all Docker images
├── PHASE1-VERIFY.md            # Phase 1 fabric / OSPF checklist
├── 5g-sim.clab.yml             # ContainerLab topology definition
│
├── configs/
│   ├── spine1/ospf.partial     # Spine IOS-XE OSPF
│   ├── leaf1/ospf.partial      # Leaf1 IOS-XE OSPF
│   ├── leaf2/ospf.partial      # Leaf2 IOS-XE OSPF
│   ├── internet-sim/
│   │   ├── frr.conf            # FRR OSPF + default-route injection
│   │   ├── daemons
│   │   └── entrypoint.sh       # Alpine + nginx on 10.100.0.1
│   ├── server-a/frr.conf       # Control-plane Linux OSPF (dual-homed)
│   └── server-b/frr.conf       # User-plane Linux OSPF (dual-homed)
│
└── servers/
    ├── Dockerfile.internet-sim
    ├── Dockerfile.server-a
    ├── Dockerfile.server-b
    ├── Dockerfile.ue
    ├── entrypoint-server-a.sh  # NRF / UDM / AMF / SMF startup
    ├── entrypoint-server-b.sh  # UPF / gNB startup + N6 OSPF hook
    └── entrypoint-ue.sh        # UE simulator startup
```

## Quick Start

```bash
# 1. Build images (from this directory)
./build.sh

# 2. Deploy topology (topology file lives in this directory)
containerlab deploy -t 5g-sim.clab.yml

# After changing images or entrypoints, refresh the lab:
containerlab deploy -t 5g-sim.clab.yml --reconfigure

# 3. Verify fabric + HTTP target (PHASE1-VERIFY.md), then exercise UE
```

## Addressing Summary

| Node           | Loopback / key prefix | Role                                      |
|----------------|------------------------|-------------------------------------------|
| internet-sim   | 10.100.0.1/32          | Simulated “internet” — nginx HTTP target |
| spine1         | 10.255.0.1/32          | WAN aggregation                           |
| leaf1          | 10.255.0.2/32          | ToR (server-a eth1, server-b eth1, UE)   |
| leaf2          | 10.255.0.3/32          | ToR (server-a eth2, server-b eth2)       |
| server-a       | 10.1.0.1/32            | Control plane (NRF, AMF, SMF)             |
| server-b       | 10.1.1.1/32            | User plane (UPF, gNB bind here)         |
| UE pool (N6)   | 10.45.0.0/24           | PDU addresses from SMF; UPF N6 / TUN      |

## User plane path to internet-sim

Traffic to the lab’s **internet simulator** uses **`10.100.0.1`** (nginx). That address is only reachable end-to-end once **routing on server-b** and **OSPF in the fabric** agree on where **`10.45.0.0/24`** (the UE pool) lives.

### Uplink (UE → internet-sim)

1. **UE** sends IP packets from its TUN address (e.g. `10.45.0.1`) toward **`10.100.0.1`**.
2. **gNB** encapsulates in **GTP-U** toward **server-b** (`10.1.1.1`), **UDP** port **2152** (UPF).
3. **UPF** decapsulates and writes the inner IPv4 packet into the **N6 TUN** (`upf-n6`). The kernel treats this like traffic arriving on that interface.
4. On **server-b**, the routing table follows **OSPF-learned routes**: default (and specifics) point **via leaf1/leaf2** toward **internet-sim**, not the Docker `eth0` management bridge (entrypoint removes the Docker default after a short convergence delay).

So from the host’s perspective: **UE IP → GTP-U → UPF → kernel forward → fabric → spine → internet-sim.**

### Downlink (internet-sim → UE)

Replies are destined to the UE address inside **`10.45.0.0/24`**. Every router toward **internet-sim** must know that prefix points back to **server-b**.

- **FRR** on server-b is configured to advertise **`10.45.0.0/24`** from **`network … area 0`** in [`configs/server-b/frr.conf`](configs/server-b/frr.conf).
- The **N6 TUN** interface **`upf-n6`** is created only when the **UPF process** starts, **after** FRR has already loaded its config. Until **`upf-n6`** exists, ospfd cannot attach that stub network.
- Therefore **[`servers/entrypoint-server-b.sh`](servers/entrypoint-server-b.sh)** waits for UPF health, then runs **`vtysh`** to apply **`interface upf-n6`** / **`ip ospf area 0`** so **`10.45.0.0/24`** is actually originated into **OSPF area 0**.
- **internet-sim** then installs **`10.45.0.0/24`** via OSPF and forwards reply packets **back through the fabric** to server-b; the **UPF** reads from **`upf-n6`**, matches the UE session, and **GTP-U encapsulates** toward the **gNB**.

If **`10.45.0.0/24`** is missing from **internet-sim**’s OSPF table, return traffic may fall back to the Docker bridge and the UE will see timeouts even when registration succeeds.

### Quick verification commands

```bash
# server-b: N6 should show Area 0 after UPF + entrypoint hook
docker exec clab-5g-sim-server-b vtysh -c 'show ip ospf interface upf-n6'

# internet-sim: must have 10.45.0.0/24 via fabric (not eth0 Docker default only)
docker exec clab-5g-sim-internet-sim vtysh -c 'show ip route ospf'

# UE / server-b → HTTP target
docker exec clab-5g-sim-ue curl -sS --max-time 5 http://10.100.0.1/
```

Replace `clab-5g-sim-*` with `docker ps --format '{{.Names}}'` names if your ContainerLab prefix differs.

For deeper fabric checks (IOS neighbours, dual-home, HTTP from servers), use **[PHASE1-VERIFY.md](PHASE1-VERIFY.md)**.
