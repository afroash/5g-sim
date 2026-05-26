# 5g-sim

A 5G Standalone (SA) core network simulator in Go, built to trace 3GPP procedures end to end.

> **Learning project.** Spec traceability and understanding come first — not production hardening. Code and logs reference 3GPP sections where practical.

---

## What you get

| Component | Role |
|-----------|------|
| **NRF** | NF registration and discovery (SBA) |
| **UDM** | Subscriber data / Nudm_UECM |
| **AMF** | Registration, mobility, NAS via NGAP |
| **SMF** | PDU sessions, IP pool (`10.45.0.0/24` locally) |
| **UPF** | User plane (GTP-U + N6) |
| **gNB** | RAN simulator (SCTP/NGAP to AMF, GTP-U to UPF) |
| **UE** | NAS attach, TUN or userspace data plane, connectivity test |
| **Observatory** | Web UI: NF health, procedures, packets, multi-UE spawn |

NGAP/NAS codecs use **free5GC** libraries (`free5gc/aper`, `free5gc/ngap`) so the project can focus on procedures rather than ASN.1 from scratch.

---

## Quick start — local (recommended)

**Prerequisites:** Go 1.22+, `curl`, Node.js (once, for the observatory UI).

### 1. Build the observatory UI (once)

```bash
cd web/observatory && npm install && npm run build && cd ../..
```

### 2. Start the core stack (one terminal)

Starts NRF → UDM → SMF → AMF → UPF → gNB → UE supervisor in the background, with `OBSERVATORY_URL` set for procedure/packet events.

```bash
./scripts/run-local-stack.sh
```

Logs: `.local-stack/logs/`. Stop: `./scripts/stop-local-stack.sh`.

### 3. Open the observatory (same or second terminal)

```bash
go run ./cmd/observatory
```

Open **http://127.0.0.1:9090** — topology health, live message log (procedures / packets), AMF UE list, **+ Add UE** for extra subscribers.

The stack script starts the **UE supervisor** on `:9080`; the observatory auto-spawns **UE-001** when configured (`configs/observatory.yaml`). Default UE uses **standalone** data plane (userspace + synthetic ping to `10.100.0.1` when no TUN).

### Subscribers

Provision SUPIs in [`configs/subscribers.yaml`](configs/subscribers.yaml). AMF and UE reject unknown subscribers.

### Manual / per-process mode

If you prefer separate terminals, start in order with:

```bash
export OBSERVATORY_URL=http://127.0.0.1:9090
go run ./cmd/nrf && go run ./cmd/udm && go run ./cmd/smf && go run ./cmd/amf \
  && go run ./cmd/upf && go run ./cmd/gnb && go run ./cmd/ue
```

Single-instance UE (no supervisor): `go run ./cmd/ue -instance`.

---

## ContainerLab — full fabric lab

End-to-end topology: Cisco IOL OSPF fabric, control/user-plane servers, UE container, **internet-sim** at **`10.100.0.1`**.

```bash
./deploy/build.sh
cd deploy
containerlab deploy -t 5g-sim.clab.yml
```

After image or entrypoint changes:

```bash
containerlab deploy -t 5g-sim.clab.yml --reconfigure
```

Allow ~90s for OSPF and NF startup, then:

```bash
docker exec ue tail -30 /var/log/ue.log
docker exec ue curl -sS --max-time 5 http://10.100.0.1/
```

Details: **[deploy/README.md](deploy/README.md)**.

Observatory against CLAB NFs (from host): `go run ./cmd/observatory -config configs/observatory-clab.yaml` (UE supervisor still on host `:9080` unless you use the in-container UE only).

---

## UE modes (summary)

| Mode | When | Data plane |
|------|------|------------|
| **Local stack** | `./scripts/run-local-stack.sh` | Standalone (default): no TUN required |
| **CLAB fabric** | `deploy` UE container | Kernel TUN + GTP through UPF N6 + OSPF to internet-sim |
| **Multi-UE** | UE supervisor `:9080` or observatory **+ Add UE** | Per-instance TUN (`ue0`, `ue1`, …) when `NET_ADMIN` available |

Profiles: `go run ./cmd/ue -profile local|clab` (overridable with `-config`). CLAB uses `-instance` inside the UE container.

---

## Project layout

```
5g-sim/
├── cmd/           # nrf, udm, amf, smf, upf, gnb, ue, observatory
├── internal/      # NF logic, sctp, ngap, nas, gtp, observatory
├── pkg/           # pcap, obspub, logger
├── configs/       # observatory, subscribers, local NF snippets
├── deploy/        # ContainerLab topology, Docker images, fabric configs
├── web/observatory/   # React UI (embedded into observatory binary)
├── scripts/       # run-local-stack.sh, stop-local-stack.sh
└── docs/          # Developer notes (not required for day-to-day use)
```

---

## Observability

- **Observatory** — HTTP/WebSocket GUI when `OBSERVATORY_URL` is set on NFs.
- **PCAP** — NGAP/GTP captures under `./captures*` when enabled by NF config.
- **Structured logs** — JSONL alongside pcaps where configured.

---

## 3GPP references

| Spec | Topic |
|------|--------|
| TS 23.501 / 23.502 | Architecture and procedures |
| TS 38.413 | NGAP (N2) |
| TS 24.501 | NAS |
| TS 29.281 | GTP-U |
| TS 29.503 / 29.510 | UDM / NRF |

---

## Developer notes

The [`docs/`](docs/) directory holds session handoffs, UE mode plans, and CLAB debugging notes. **Usage that matters for running the simulator lives in this README and [deploy/README.md](deploy/README.md).**

---

## References

- [free5GC](https://github.com/free5gc/free5gc)
- [3GPP specifications](https://www.3gpp.org/ftp/Specs/)
- [Wireshark 5G dissectors](https://wiki.wireshark.org/5G)
