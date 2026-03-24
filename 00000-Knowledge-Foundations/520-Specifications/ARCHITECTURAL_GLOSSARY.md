# K110: Project GitSovereign Architectural Glossary (Brain Transfer)

## 1. Firehorse Heartbeat
The raw UDP/QUIC stream metric derived from the `quic-go.ConnectionStats`. It represents the real-time health and throughput of the memory-mapped bundle pipe. The "Heartbeat" is visualized in the Interaction Surface as a high-frequency sine wave synced to the `BytesTransferred` delta.

## 2. Zero-Disk Harvest
The tactical mandate to completely bypass the local filesystem during the repository exfiltration phase. This is achieved by piping the `git bundle create - --all` STDOUT directly into a `quic.Stream` writer. This avoids "Disk-Spike" latency and ensures compliance with high-security (SOC2/HIPAA) RAM-only data handling mandates.

## 3. Sovereign Tree (Hash Node)
A deduplication data structure consisting of a hash-map where the Key is the `SHA-256(RepoName + BranchName + HeadHash)` and the Value is the storage location (GCS/Drive path). This allows for O(1) deduplication checks before initiating a QUIC harvest.

## 4. Digital Exit
The strategic capability to perform an instantaneous, verified "platform-departure" from a centralized SaaS provider (GitHub/GitLab) to a sovereign, verified archive without any interruption to the organization's CI/CD "Availability" metrics.

## 5. Value Unit (VU)
The atomic unit of GitSovereign monetization. Unlike disk-based pricing, a VU is triggered by:
- **Harvest Verification**: A successful, authenticated ZDH with an Assurance Report.
- **Rehydration Simulation**: A sub-second verified restore into a virtualized environment (Chaos Simulation).
- **Assurance Proof**: The generation of a cryptographically signed jeBNF audit record.
