# Project GitSovereign: Architectural Vision (Firehorse)

## 0. Vision
Created as the "Firehorse" reincarnation of GitHub backup utilities, GitSovereign provides 100% data sovereignty and sub-second recovery for enterprise-scale repository sets. It is designed to run with equal efficiency on a local workstation or as a GCP Cloud Run instance.

## 1. Core Architecture
- **Backend**: Native Go service leveraging `quic-go` for low-latency UDP transport.
- **Frontend**: Flutter WASM-GC interaction surface, embedded into the Go binary.
- **Transport (ADR-014)**: QUIC/HTTP3 for all Google Drive egress; raw QUIC streams using `jeBNF` for metadata serialization on the rehydration path.
- **Identity**: Firebase Passwordless Auth with Google Workspace group validation (PBAC).
- **Ingestion**: Adaptive multi-branch pipeline with density heuristic routing (CYC-00268).
- **Backup Structure (ADR-016)**: Per-branch component tree enabling selective subset recovery.

## 2. The "Firehorse" Pipeline (CYC-00268)
1.  **Discovery**: Resolve all branches via paginated REST API. Walk every tree with truncation recovery. Evaluate S/n density heuristic per directory to route to REST or Packfile ingestion.
2.  **Deduplication**: Two-tier gate — `sync.Map` session cache + ADPH perfect hash CAS index. Cross-branch SHA dedup ensures blobs shared across branches are only processed once.
3.  **Harvest**: 30-goroutine bounded worker pool streams blobs from GitHub REST API with rate-limit awareness (exponential backoff on 429/403). Zero-disk `io.Copy` piping from GitHub to Drive.
4.  **Topology Capture (ADR-015)**: BFS walk of the commit DAG from all branch heads. Captures every commit (message, author, committer, timestamps, parents, tree SHA), all branch/tag refs, and per-commit tree snapshots with file permissions. Stored as a separate `topology.jebnf` CAS object. Depth-controllable via `--topology-depth`.
5.  **Storage (ADR-016)**: Per-branch backup tree in Google Drive. Each branch gets its own `manifest.jebnf` + `tree.jebnf` (file→CAS mapping). Repo-level components (issues, PRs, releases, discussions, wiki) stored at the repo root. Blobs remain in CAS prefix tree (256 subfolders). All egress over HTTP/3 (QUIC).
6.  **Assurance**: Generate a real-time `jeBNF` Assurance Report verifying the recovery speed and integrity.

> See [CYC-00268-Adaptive-Harvesting-Pipeline.md](200-Architecture/CYC-00268-Adaptive-Harvesting-Pipeline.md) for full implementation detail.

## 3. The Sovereign Backup Tree (ADR-016)
A backup is a **browsable tree** organized by branch, enabling selective subset recovery:

```
Orgs/{org}/{repo}/
  ├── manifest.jebnf              Repo summary: branches, stats, metadata
  ├── topology.jebnf              Full commit DAG across all branches (ADR-015)
  ├── branches/
  │   ├── main/
  │   │   ├── manifest.jebnf      Branch HEAD SHA, file count, timestamp
  │   │   └── tree.jebnf          Complete file path → CAS hash mapping
  │   ├── develop/
  │   └── feature-auth/
  └── components/
      ├── issues.jebnf            Repo-level: CAS reference → issue export
      ├── pull_requests.jebnf
      ├── releases.jebnf
      ├── discussions.jebnf
      └── wiki.bundle             CAS reference → git bundle
```

Blobs are stored once in the CAS prefix tree (`hashes/{xx}/{key}`). Branch trees and repo components reference them by key. Cross-branch dedup is inherent — identical files across branches point to the same CAS blob.

> See [ADR-016-Per-Branch-Backup-Tree.md](200-Architecture/ADR-016-Per-Branch-Backup-Tree.md) for full specification and UI mockup.

## 4. The Rehydration Pipeline (CYC-00269)
The inverse of harvest: reconstruct a GitHub repository from the backup tree. Supports **selective recovery** — users choose which branches and components to restore.

1.  **Manifest Resolution**: Browse `Orgs/{org}/{repo}/branches/` to enumerate available branches. List `components/` for repo-level data. Validate CAS integrity for selected subset.
2.  **Target Provisioning**: Create (or verify empty) target GitHub repository. Apply original metadata. Safety gate refuses to overwrite non-empty repos without `--force`.
3.  **Full-History Replay**: When `topology.jebnf` is available, replay the commit DAG in topological order for selected branches. Blobs uploaded once and shared via SHA mapping. Falls back to snapshot mode if topology not present.
4.  **Structured Data Re-Import**: Only selected components: issues, releases, discussions via API. Wiki via `git push --mirror`.
5.  **Verification**: Compare rehydrated tree SHAs against branch manifests. Generate `jeBNF` Rehydration Assurance Report.

> See [CYC-00269-Sovereign-Rehydration-Pipeline.md](200-Architecture/CYC-00269-Sovereign-Rehydration-Pipeline.md) for full implementation plan.
> See [ADR-015-Topology-DAG-Capture.md](200-Architecture/ADR-015-Topology-DAG-Capture.md) for the topology specification.

## 4. Sovereign Safeguards
- **Kill-Switch**: OIDC Shared Signals (CAEP/RISC) remote revocation.
- **Egress Monitor**: Native Go watcher that terminates the process on unauthorized IP attempts.
- **Identity Anchor**: Device-verified SPIFFE/SVID identity documents.
- **Rate Resilience**: Inspects `X-RateLimit-Remaining`/`X-RateLimit-Reset` with automatic backoff.
- **QUIC Fallback**: Automatic HTTP/2 fallback when corporate firewalls block UDP.
