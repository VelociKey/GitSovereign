# Pulse 1: Functional Sovereignty & UI Blueprint (Draft)

## 1. UI Interaction Surface (The "What")

### A. The Nodal Setup Flow
- **Authentication**: Firebase Email-Link (Passwordless).
- **SPIFFE Identity**: Display current Device SVID and Authority status.
- **Provider Mapping**: Connect to GitHub organizations and authorize the **Google MCP** drive/bucket selector.

### B. The Sovereign Tree Dashboard (Deduplication Gauge)
- **Visual Feedback**: Real-time progress bar of the Organization Org-Scan.
- **Value Metric**: Display total GB scanned vs. GB harvested (Deduplication Efficiency).
- **The "Sovereign Node" List**: A searchable table of all unique repository states currently anchored in storage.

### C. The Firehorse Recovery HUD (Active Pipeline)
- **Exfiltration Heartbeat**: A sine-wave or pulse animation synced with active UDP/QUIC packet flow.
- **Throughput Gauge**: Direct telemetry of the in-memory RAM-piping speed.
- **Sovereignty Timer**: A countdown-to-sub-second recovery ticker once rehydration begins.

### D. Compliance & Assurance Surface
- **Historical Harvests**: Timeline of past Zero-Disk Harvests.
- **Pinnacle-Assurance Reports**: One-click download of the `eBNF` audit file for SOC2 examiners.
- **Kill-Switch Interface**: Manual trigger for immediate remote token revocation (CAEP/RISC simulation).

## 2. Key High-Level Functional Steps (Process Blueprint)

1.  **Identity Anchor**: The local workstation service verifies its **SPIFFE SVID** identity.
2.  **Organization Scan**: The **Parallel Control Plane** shards a scan across the GitHub Organization, computing hashes for all default branch heads.
3.  **Deduplication Filter**: Compare current GitHub hashes with the **Sovereign Tree** in storage. Only novel states proceed to harvest.
4.  **Zero-Disk Harvest**:
    - **Buffer Allocation**: A private, non-persistent RAM buffer is allocated.
    - **QUIC Tunnel**: Establish a raw UDP tunnel to the target storage (Drive via MCP/GCS).
    - **Piping**: Execute `git bundle` directly into the QUIC stream, bypassing the local filesystem entirely.
5.  **Rehydrate & Verify**: On the destination side, the binary rehydrates the bundle in memory, verifies the hash, and generates the **Assurance Report**.
6.  **Assurance Finalization**: Publish the report and telemetry result to the Dashboard.

## 3. Mandatory UI Attributes (Aesthetics Mandate)
- **Fluidity**: All gauges must update at 60fps (Flutter WASM-GC).
- **Transparency**: Every packet must feel "visible" through micro-animations.
- **Sovereign Aesthetic**: Modern high-contrast dark mode with neon "Firehorse" telemetry accents.
