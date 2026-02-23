# CLAUDE.md

This file gives Claude (and any AI assistant) the context needed to work effectively
in this repository without needing to re-explain the project each session.

---

## Project

**Name:** 5g-sim
**Module:** `github.com/afroash/5g-sim`
**Purpose:** A 5G core network simulator written in Go, built as a learning tool
to understand the 3GPP standard hands-on. Not production software — clarity and
spec traceability matter more than performance.

---

## Owner

- **GitHub:** afroash
- **Background:** Systems engineer, aerospace/satcom domain, experienced with Go,
  Robot Framework, Linux, networking (VLANs, OSPF)
- **Goal:** Understand 3GPP/5G architecture by building it layer by layer

---

## Architecture

This project uses **free5GC's open-source libraries** for the hard codec work
(APER encoding, NGAP message types) so we can focus on procedures and behaviour.

### Key External Dependencies

| Package | Purpose | 3GPP Ref |
|---|---|---|
| `github.com/free5gc/aper` | ASN.1 Packed Encoding Rules codec | X.691 |
| `github.com/free5gc/ngap` | NGAP encoder/decoder + all message types | TS 38.413 |
| `github.com/ishidawataru/sctp` | SCTP transport for Linux | TS 38.412 |

### Internal Package Map

```
internal/
├── sctp/         SCTP server - accepts gNB connections         TS 38.412
├── ngap/         NGAP message handler + builder (wraps free5gc) TS 38.413
├── nas/          NAS protocol codec                            TS 24.501
├── nrf/          Network Repository Function - service registry TS 29.510
├── amf/          Access & Mobility Management Function         TS 23.502
├── smf/          Session Management Function                   TS 29.502
├── gnb/          gNB simulator (the "radio side")             TS 38.401
├── procedures/   5G procedure state machines                   TS 23.502
└── gtp/          GTP-U user plane tunneling                   TS 29.281

pkg/
├── pcap/         PCAP writer - inspect traffic in Wireshark
└── logger/       Structured logger with spec section tagging

cmd/
├── amf/          AMF binary entry point
├── smf/          SMF binary entry point
├── gnb/          gNB simulator binary entry point
└── nrf/          NRF binary entry point
```

---

## Build Order

Work through these in sequence — each layer depends on the previous:

1. `internal/sctp`      — SCTP server (transport foundation)
2. `internal/ngap`      — NGAP handler + builder (wraps free5gc/ngap)
3. `internal/nas`       — NAS message decode
4. `internal/nrf`       — NRF service registry + `cmd/nrf`
5. `internal/amf`       — AMF context + NG Setup handling + `cmd/amf`
6. `internal/gnb`       — gNB simulator that connects to AMF + `cmd/gnb`
7. `internal/smf`       — SMF session handling + `cmd/smf`
8. `internal/procedures`— Registration + PDU Session state machines
9. `internal/gtp`       — GTP-U user plane
10. `pkg/pcap`          — PCAP output for Wireshark validation

---

## Key 3GPP Specs (bookmark these)

| Spec | Title | What it covers |
|---|---|---|
| TS 23.501 | System Architecture for 5G | Overall NF architecture |
| TS 23.502 | Procedures for 5G | Registration, PDU session flows |
| TS 38.300 | NR Overall Description | Radio side overview |
| TS 38.401 | NG-RAN Architecture | gNB architecture |
| TS 38.412 | NG Signalling Transport | SCTP for NGAP |
| TS 38.413 | NGAP | The N2 interface protocol |
| TS 24.501 | NAS for 5G | UE ↔ AMF messaging |
| TS 29.244 | PFCP | Control/user plane separation |
| TS 29.281 | GTP-U | User plane tunneling |
| TS 29.510 | NRF Services | Service discovery |

Download specs free from: https://www.3gpp.org/ftp/Specs/

---

## Coding Conventions

- **Spec traceability:** Every non-trivial function should have a comment
  referencing the spec and section it implements. Example:
  ```go
  // BuildNGSetupResponse builds the NG Setup Response message.
  // Ref: TS 38.413 §9.2.6.2
  func BuildNGSetupResponse(...) {}
  ```
- **Error handling:** Always wrap errors with context (`fmt.Errorf("ngap decode: %w", err)`)
- **Goroutines:** Each NF runs as a goroutine; procedures are message-passing sequences
- **No global state:** Pass context explicitly; use structs with methods
- **Tests:** Every builder/handler should have a unit test

---

## First Milestone Target

**NG Setup Handshake** — a gNB connects to the AMF over SCTP, sends an
`NGSetupRequest`, and receives a valid `NGSetupResponse`. This exercises:

- SCTP transport (`internal/sctp`)
- NGAP decode using `free5gc/ngap` (`internal/ngap`)
- AMF message handling (`internal/amf`)
- NGAP encode for the response (`internal/ngap`)

Once this works, output a PCAP and verify in Wireshark.
