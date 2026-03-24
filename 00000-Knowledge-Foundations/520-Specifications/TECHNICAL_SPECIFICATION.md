# K111: GitSovereign Technical Specification (Firehorse Protocol v1)

## 1. Stream Framing Protocol (Raw QUIC)
The Firehorse data plane consists of multiple parallel QUIC streams:
- **Control Stream (0)**: Permanent, keeps the `jeBNF` session state.
- **Harvest Streams (1-N)**: Ephemeral, dedicated to a single `git bundle` pipe.

### 1.1 Control Stream Metadata
The Control Stream (ID 0) uses `jeBNF` serialization for every session event:
```jebnf
Event {
    ID = "FIREHORSE_INIT_HARVEST"
    TargetRepo = "VelociKey/GitSovereign"
    HeadHash = "3db8ac..."
    WorkerID = 4
    StorageTarget = "GCS_Sovereign_01"
}
```

### 1.2 Harvest Stream Data (Framing)
Every Harvest Stream (ID 1-N) is prefixed with a `jeBNF` **SegmentHeader**:
- **Magic**: `FHORSE\x01`
- **HeaderSize**: 4 bytes (BigEndian)
- **HeaderContent**: `jeBNF` metadata (StreamID, Offset, IsFinal)
- **DataBlob**: Raw `git bundle` binary data.

## 2. Zero-Disk Buffer Management
- **Buffer Mode**: `mmap.RDONLY` for reading the source repository.
- **Buffer Pipe**: `io.Copy(quicStream, gitBundleStdout)` for the exfiltration.
- **Memory Ceiling**: 512MB default buffer per worker to prevent OOM/Platform-Kill.

## 3. High-Frequency Deduplication (HFD)
Before every harvest, the Control Plane executes a **Bloom Filter** check against the `SovereignTree` for the Org. 
- **Hash Algorithm**: `SHA-256` of the full Git Bundle header.
- **Deduplication Threshold**: 80% (Historical average for typical Org branching).

## 4. Assurance Proof (jeBNF Schema)
The final `AssuranceReport` is generated as a signed `EBNF` document:
```jebnf
::Olympus::Firehorse::AssuranceProof::v1
Proof {
    Signature = "<ED25519_Digital_Sig>"
    Integrity = 1.0
    RedundancyNodes = 3
    StorageProof = "gs://sovereign-exit/CYC-00267/3db8ac..."
    MetadataDeltas = [ "hash", "timestamp", "owner" ]
}
```
