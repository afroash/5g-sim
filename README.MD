# 5g-sim

A 5G core network simulator written in Go, built layer by layer to understand the
3GPP standard from the inside out.

> **Learning project.** The goal is spec traceability and understanding — not
> production readiness. Every piece of code maps back to a 3GPP spec section.

---

## What This Is

5g-sim simulates the key components of a 5G Standalone (SA) core network:

- A **gNB** (base station) that initiates connections and sends UE traffic
- An **AMF** (Access & Mobility Management Function) handling registration
- An **SMF** (Session Management Function) managing data sessions
- An **NRF** (Network Repository Function) for service discovery
- A **UPF** (User Plane Function) concept via GTP-U tunneling

Instead of implementing ASN.1 encoding from scratch, this project builds on
**free5GC's open-source Go libraries** (`free5gc/aper`, `free5gc/ngap`) for the
codec layer — letting us focus on the procedures and behaviour defined in the specs.

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────┐
│                        5G Core (SBA)                     │
│                                                          │
│   ┌─────┐    N8/N12   ┌─────┐    N11    ┌─────┐        │
│   │ NRF │◄───────────►│ AMF │◄─────────►│ SMF │        │
│   └─────┘             └──┬──┘           └──┬──┘        │
│                          │ N2              │ N4         │
└──────────────────────────┼─────────────────┼────────────┘
                           │ SCTP/NGAP       │ PFCP
                      ┌────▼────┐       ┌────▼────┐
                      │   gNB   │       │   UPF   │
                      └─────────┘       └─────────┘
```

---

## Project Structure

```
5g-sim/
├── cmd/                  # Binary entry points
│   ├── amf/              # AMF process
│   ├── smf/              # SMF process
│   ├── gnb/              # gNB simulator process
│   └── nrf/              # NRF process
│
├── internal/             # Core logic
│   ├── sctp/             # SCTP server (NGAP transport)
│   ├── ngap/             # NGAP message handler + builder
│   ├── nas/              # NAS protocol codec
│   ├── nrf/              # NRF service registry
│   ├── amf/              # AMF state and logic
│   ├── smf/              # SMF state and logic
│   ├── gnb/              # gNB simulator
│   ├── procedures/       # 5G procedure state machines
│   └── gtp/              # GTP-U user plane tunneling
│
├── pkg/
│   ├── pcap/             # PCAP writer (Wireshark validation)
│   └── logger/           # Structured logger with spec refs
│
└── specs/
    └── notes/            # Spec cross-reference notes
```

---

## Key Dependencies

```bash
go get github.com/free5gc/aper        # ASN.1 PER codec
go get github.com/free5gc/ngap        # NGAP message types + codec
go get github.com/ishidawataru/sctp   # SCTP transport
```

---

## 3GPP Spec Reference

| Spec | Title |
|---|---|
| TS 23.501 | System Architecture for 5G |
| TS 23.502 | Procedures for 5G |
| TS 38.401 | NG-RAN Architecture |
| TS 38.412 | NG Signalling Transport (SCTP) |
| TS 38.413 | NGAP — N2 Interface |
| TS 24.501 | NAS Protocol for 5G |
| TS 29.281 | GTP-U User Plane |
| TS 29.510 | NRF Services |

---

## Build Checklist

### 🔲 Phase 0 — Setup
- [x] Repo created: `github.com/afroash/5g-sim`
- [x] `go mod init github.com/afroash/5g-sim`
- [x] Directory structure scaffolded
- [ ] `go get` free5GC dependencies
- [ ] `go build ./...` compiles cleanly

---

### 🔲 Phase 1 — Transport Layer (SCTP + NGAP wire)
> Goal: Accept a raw NGAP message over SCTP and decode it

- [ ] `internal/sctp` — SCTP server that listens for gNB connections
- [ ] `internal/sctp` — SCTP client (for gNB simulator to use)
- [ ] `internal/ngap` — `Handler` type: receives raw bytes, calls `free5gc/ngap.Decoder`
- [ ] `internal/ngap` — `Builder` type: constructs outgoing NGAP PDUs
- [ ] Unit test: encode an `NGSetupRequest`, decode it back, verify fields

---

### 🔲 Phase 2 — NG Setup Handshake (First Real Procedure)
> Goal: gNB connects to AMF, NG Setup completes successfully
> Ref: TS 38.413 §9.2.6.1 / TS 38.401 §8.7

- [ ] `internal/amf` — `RAN` context struct (represents a connected gNB)
- [ ] `internal/amf` — Handle `NGSetupRequest` → validate → send `NGSetupResponse`
- [ ] `internal/gnb` — Connect to AMF over SCTP, send `NGSetupRequest`
- [ ] `internal/gnb` — Handle `NGSetupResponse`
- [ ] `cmd/amf` — Start AMF, listen on SCTP port 38412
- [ ] `cmd/gnb` — Connect to AMF, complete NG Setup, print result
- [ ] `pkg/pcap` — Write PCAP of the exchange, verify in Wireshark

---

### 🔲 Phase 3 — NRF & Service Discovery
> Goal: NFs register themselves and discover each other
> Ref: TS 29.510

- [ ] `internal/nrf` — In-memory NF registry (register, deregister, discover)
- [ ] `internal/nrf` — HTTP/2 REST API (OpenAPI-based, per TS 29.510)
- [ ] `cmd/nrf` — Start NRF on configurable port
- [ ] AMF registers with NRF on startup
- [ ] SMF discovers AMF via NRF

---

### 🔲 Phase 4 — NAS & UE Registration
> Goal: Simulated UE completes registration through gNB → AMF
> Ref: TS 23.502 §4.2.2, TS 24.501

- [ ] `internal/nas` — NAS message decoder (Registration Request)
- [ ] `internal/nas` — NAS message builder (Registration Accept)
- [ ] `internal/gnb` — Forward NAS messages between UE and AMF (transparent)
- [ ] `internal/amf` — Handle `InitialUEMessage` carrying NAS Registration Request
- [ ] `internal/amf` — UE context creation
- [ ] `internal/amf` — Send NAS Registration Accept
- [ ] `internal/procedures` — `RegistrationProcedure` state machine
- [ ] End-to-end test: UE registers, gets a 5G-GUTI back

---

### 🔲 Phase 5 — PDU Session Establishment
> Goal: UE gets a data session (IP address allocated)
> Ref: TS 23.502 §4.3.2

- [ ] `internal/smf` — SMF session context
- [ ] `internal/smf` — Handle PDU Session Establishment request from AMF (N11)
- [ ] `internal/amf` — N11 interface to SMF (HTTP/2)
- [ ] `internal/procedures` — `PDUSessionEstablishmentProcedure` state machine
- [ ] IP address pool allocation in SMF
- [ ] End-to-end test: UE gets an IP address

---

### 🔲 Phase 6 — User Plane (GTP-U)
> Goal: Actual IP packets flow through the simulated network
> Ref: TS 29.281

- [ ] `internal/gtp` — GTP-U packet encoder/decoder
- [ ] `internal/gtp` — UDP socket listener on port 2152
- [ ] `internal/gtp` — TEID (Tunnel Endpoint ID) management
- [ ] Route UE IP packets through GTP tunnel gNB ↔ UPF
- [ ] End-to-end test: ping through the simulated user plane

---

### 🔲 Phase 7 — Observability
> Goal: See everything happening, validate against real 5G captures

- [ ] `pkg/pcap` — Write PCAP files for all NGAP/NAS/GTP traffic
- [ ] `pkg/logger` — Spec-annotated structured logging
- [ ] Sequence diagram output (Mermaid) from procedure traces
- [ ] Wireshark validation: NGAP dissector confirms our message format

---

## Running

```bash
# Terminal 1 - Start NRF
go run ./cmd/nrf

# Terminal 2 - Start AMF
go run ./cmd/amf

# Terminal 3 - Start gNB (connects to AMF, triggers NG Setup)
go run ./cmd/gnb
```

---

## References

- [free5GC](https://github.com/free5gc/free5gc) — Open source 5G core (R15)
- [3GPP Specs](https://www.3gpp.org/ftp/Specs/) — Free download
- [Wireshark 5G dissectors](https://wiki.wireshark.org/5G) — For validating captures
