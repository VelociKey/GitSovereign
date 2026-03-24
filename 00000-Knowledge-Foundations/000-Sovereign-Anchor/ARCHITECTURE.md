# Project GitSovereign: Architectural Vision (Firehorse)

## 0. Vision
Created as the "Firehorse" reincarnation of GitHub backup utilities, GitSovereign provides 100% data sovereignty and sub-second recovery for enterprise-scale repository sets. It is designed to run with equal efficiency on a local workstation or as a GCP Cloud Run instance.

## 1. Core Architecture
- **Backend**: Native Go service leveraging `quic-go` for low-latency UDP transport.
- **Frontend**: Flutter WASM-GC interaction surface, embedded into the Go binary.
- **Transport (ADR-014)**: Raw QUIC streams using `jeBNF` for metadata serialization, bypassing the ConnectRPC/HTTP overhead.
- **Identity**: Firebase Passwordless Auth with Google Workspace group validation (PBAC).

## 2. The "Firehorse" Pipeline
1.  **Discovery**: Scan GitHub Organizations for unique repository hashes (Deduplication).
2.  **Harvest**: Initiate a Zero-Disk Harvest by piping `git bundle` directly into a memory-mapped QUIC stream.
3.  **Storage**: Multimodal landing zones (Google Drive via MCP or GCS Buckets).
4.  **Assurance**: Generate a real-time `jeBNF` Assurance Report verifying the recovery speed and integrity.

## 3. Sovereign Safeguards
- **Kill-Switch**: OIDC Shared Signals (CAEP/RISC) remote revocation.
- **Egress Monitor**: Native Go watcher that terminates the process on unauthorized IP attempts.
- **Identity Anchor**: Device-verified SPIFFE/SVID identity documents.
