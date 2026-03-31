# ADR-017: Sovereign CAS Addressing via BLAKE3

## Status
**Proposed** — 2026-03-31

## Depends On
- CYC-00268 (Adaptive Harvesting Pipeline)
- ADR-016 (Per-Branch Backup Tree)

## Context

Sovereign CAS stores blobs and packfiles in a 2-character prefix tree on Google Drive (`hashes/{xx}/{caskey}`). 

Previously, `CasKey` generation (via the internal `gitsovkey` package) defaulted to SHA-256. While highly secure, SHA-256 is slow on sequential streams. As the Sovereign Fleet targets enterprise data throughput (gigabytes per minute), a 30-goroutine pipeline reading from network streams quickly encounters CPU bottlenecking when hashing every chunk sequentially. 

Furthermore, the `MethodPACK` density heuristic (ADR-014) bundles numerous small files into single archive blobs. If we only hash the resulting packfile, we lose cryptographic verification of individual packed files during rehydration.

The GitHub-provided `SHA-1` is inherently broken (SHAttered) for cryptographic collision resistance. While useful for logical `O(1)` branch comparisons (as seen in `tree_compare.go`), it is fundamentally unsafe to use for global object deduplication in shared Google Drive storage, especially since malicious actors can craft different files with the same SHA-1.

## Decision

Migrate the Sovereign Fleet's `gitsovkey` cryptographic identity generator to **BLAKE3**.

1. **Standalone Files**: The `CasKey` written to the `tree.jebnf` manifest will be a 256-bit BLAKE3 digest of the file contents.
2. **Packed Files**: For files bundled together (`MethodPACK`), the individual file's BLAKE3 `CasKey` will *still* be calculated on the fly as it streams into the pack. The `PackKey` itself will also be a BLAKE3 digest of the entire archive structure.
3. **Prefix Routing**: The first 2 characters of the BLAKE3 digest dictate the Drive subfolder (`00` through `ff`).

## Rationale

1. **Throughput**: BLAKE3 is 10-15x faster than SHA-256 and naturally utilizes multi-threading via SIMD instructions (AVX2/AVX-512) for concurrent chunk hashing. It easily outpaces disk and network I/O, meaning cryptography zero-ops the pipeline bottleneck.
2. **Inherent Merkle Trees**: Because BLAKE3 is physically constructed as a parallel Merkle tree, it natively maps to partitioned packfiles, allowing piece-wise verification of streams.
3. **Impeccable Security**: Outputting a 256-bit digest, it offers indistinguishable security bounds from SHA-256 (no known vulnerabilities, length-extension attacks impossible) while actively preventing malicious collision insertion.

## Design Changes

### `gitsovkey` Package Modifications

The existing `gitsovkey` package will drop the `crypto/sha256` standard library import in favor of the optimized Go module:

```go
// Run: go get lukechampine.com/blake3

import "lukechampine.com/blake3"

// CASKey generates a 256-bit hex-encoded physical storage key using BLAKE3.
func CASKey(data []byte) string {
    h := blake3.Sum256(data)
    return fmt.Sprintf("gitsov_%x", h)
}

// NewCASStream creates a hashing writer that generates a BLAKE3 digest.
func NewCASStream() *blake3.Hasher {
    return blake3.New(32, nil)
}
```

### Manifest Updates (`tree.jebnf`)

`CasKey` changes meaning from a generic hash to a strictly guaranteed BLAKE3 256-bit digest.

```
::Olympus::Firehorse::BranchTree::v1 {
  ...
  Entries [
    // Standalone blob: CasKey is the BLAKE3 digest of the file
    { Path = "src/main.go"; Mode = "100644"; SHA = "b_sha1..."; CasKey = "gitsov_blake3_a1b2..."; Size = 4096; },
    
    // Packed blob: CasKey is STILL the BLAKE3 digest of the file, PackKey is the BLAKE3 digest of the packfile
    { Path = "scripts/dep.sh"; Mode = "100755"; SHA = "c_sha1..."; PackKey = "gitsov_blake3_packX..."; PackOffset = 1024; CasKey = "gitsov_blake3_c9d8..."; Size = 2048; }
  ];
}
```

### Harvest Streaming (`github.go` / `storage.go`)

When using `io.Copy` from the GitHub REST API to the Google Drive QUIC stream, we inject an `io.TeeReader` that routes through `blake3.New()`. 

Since BLAKE3 is drastically faster than network streaming speeds, injecting it into a `TeeReader` imposes 0% overhead on the download/upload pipe.

## Consequence & Assurance

- **Zero-Disk Streaming**: Because BLAKE3 maintains streaming `Write([]byte)` states linearly, we can compute the CasKey in flight without buffering the file to disk in `/tmp` natively supporting our Zero-Disk directive.
- **Assurance**: The `Rehydration Assurance Report` (CYC-00269 phase 5) can instantly verify the integrity of recovered files by streaming them through BLAKE3 en route back to GitHub, immediately identifying data corruption within Google Drive partitions.
