# ADR-014: Firehorse Sovereign Transport (Raw QUIC + jeBNF)

## Status
Accepted (Supersedes ADR-013 for GitSovereign)

## Context
Project GitSovereign ("Firehorse") requires sub-second recovery and 100% data sovereignty for enterprise-scale repository harvests. ADR-013 mandated the use of ConnectRPC + Cronet (HTTP/3) for standard interaction surfaces. However, for large-scale `git bundle` transport, the ConnectRPC/HTTP overhead (header serialization, flow control layering) creates a performance bottleneck that prevents the "Sub-Second" promise for medium-to-large repositories.

## Decision
We will bypass the ConnectRPC/HTTP layer for the GitSovereign data plane and implement a **Raw QUIC Stream** transport.

### 1. Unified UDP/QUIC Plane
- Use `quic-go` (native Go implementation) to manage direct QUIC streams.
- Zero-Disk Harvest: Pipe the stdout of `git bundle` directly into a QUIC Stream writer in memory-mapped buffers.

### 2. jeBNF Metadata Framing
- Direct `jeBNF` serialization of metadata (repository headers, hashes, audit timestamps) as the framing logic for each QUIC stream segment.
- Avoid JSON/ProtocolBuffer serialization overhead for metadata during the high-speed harvest.

### 3. Parallel Control Plane
- Implement a worker-pool orchestrator in Go that manages multiple parallel QUIC streams.
- Control plane telemetry (backpressure, congestion) will be communicated via a separate, dedicated QUIC control stream.

## Consequences
- **Positive**: 
    - 10-20% reduction in transport overhead compared to HTTP/3.
    - Simplified "Zero-Disk" implementation without HTTP multi-part complexities.
    - Native support for the "Firehorse" HUD telemetry directly from QUIC stream metrics.
- **Neutral**: 
    - Requires custom stream management (Multiplexing) on top of raw QUIC.
- **Negative**:
    - Bypasses standard web proxies that only inspect HTTP/TLS. (Requirement: Firehorse requires UDP/QUIC egress parity).
