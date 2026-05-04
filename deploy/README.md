# 5g-sim — Deploy

This directory contains everything needed to deploy the 5g-sim
network topology using ContainerLab.

## Structure

```
deploy/
├── build.sh                    # Build all Docker images
├── PHASE1-VERIFY.md            # Phase 1 test checklist
│
├── topology/
│   └── 5g-sim.clab.yml         # ContainerLab topology definition
│
├── configs/
│   ├── spine1/
│   │   └── ospf.partial        # Spine1 IOS-XE OSPF config
│   ├── leaf1/
│   │   └── ospf.partial        # Leaf1 IOS-XE OSPF config
│   ├── leaf2/
│   │   └── ospf.partial        # Leaf2 IOS-XE OSPF config
│   ├── internet-sim/
│   │   ├── frr.conf            # FRR OSPF + default-route injection
│   │   ├── daemons             # FRR daemons (zebra + ospfd)
│   │   └── entrypoint.sh      # Container startup
│   ├── server-a/
│   │   └── frr.conf            # Server A FRR OSPF (dual-homed)
│   └── server-b/
│       └── frr.conf            # Server B FRR OSPF (dual-homed)
│
└── servers/
    ├── Dockerfile.internet-sim # Alpine + FRR + nginx
    ├── Dockerfile.server-a     # Ubuntu + FRR (Phase 2: + NRF/AMF/SMF)
    ├── Dockerfile.server-b     # Ubuntu + FRR (Phase 2: + UPF/gNB)
    ├── server-a-netsetup.sh    # Interface + FRR startup for Server A
    ├── server-b-netsetup.sh    # Interface + FRR startup for Server B
    ├── entrypoint-server-a.sh  # Server A container entrypoint
    └── entrypoint-server-b.sh  # Server B container entrypoint
```

## Quick Start

```bash
# 1. Build images (from this directory)
./build.sh

# 2. Deploy topology
cd topology
sudo containerlab deploy -t 5g-sim.clab.yml

# 3. Verify (follow PHASE1-VERIFY.md)
```

## Addressing Summary

| Node           | Loopback/Address | Role                    |
|----------------|-----------------|-------------------------|
| internet-sim   | 10.100.0.1/32   | nginx HTTP target        |
| spine1         | 10.255.0.1/32   | WAN aggregation          |
| leaf1          | 10.255.0.2/32   | ToR left                 |
| leaf2          | 10.255.0.3/32   | ToR right                |
| server-a       | 10.1.0.1/32     | Control plane (NRF/AMF/SMF) |
| server-b       | 10.1.1.1/32     | User plane (UPF/gNB)     |
| UE pool        | 10.45.0.0/24    | Allocated by SMF         |
