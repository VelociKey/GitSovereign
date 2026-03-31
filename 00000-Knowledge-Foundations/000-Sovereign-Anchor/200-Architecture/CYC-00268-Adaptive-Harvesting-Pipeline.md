# CYC-00268: Adaptive Multi-Branch Harvesting Pipeline

## Status
**Implemented** — 2026-03-31

## Supersedes
Pulse 6 "Merkle Directed Ingestion" (single-branch, `gh.exe`-dependent, flat CAS)

## Context
The original `StreamMerkleIngestion` engine contained severe architectural flaws:

| Flaw | Impact |
|------|--------|
| Blocking `exec.Command("gh.exe")` for every API call | OS subprocess overhead; UI/API lockup under load |
| Single-branch capture (default branch only) | Non-default branches silently dropped from backup |
| `truncated: true` logged but never resolved | Large repos missing thousands of files |
| Flat `hashes/` folder in Google Drive | Drive UI/API lockup at ~5000 files per folder |
| Sequential blob fetching | No concurrency; O(n) wall-clock time for n blobs |
| No rate-limit awareness | 403/429 responses cause unrecoverable failures |
| `io.ReadAll()` / `cmd.Output()` patterns | Full blob buffering in memory; no streaming |

## Decision
Refactor the SmartPipe ingestion and storage layers into a 4-phase adaptive pipeline that intelligently routes data through the optimal protocol based on tree density heuristics.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Phase 1: Merkle Discovery                     │
│  resolveBranches() ─► walkTree() ─► evaluateDensity()           │
│  [Paginated]          [Truncation     [S/n Heuristic]           │
│                        Recovery]                                 │
└──────────────────────────┬──────────────────────────────────────┘
                           │ TreeNode[]
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│                   Phase 2: Deduplication Gate                    │
│  SessionCache ──► CAS BlobExists() ──► IngestionJob{REST|PACK} │
│  [sync.Map]       [ADPH Index]         [Tagged dispatch]        │
└──────────────────────────┬──────────────────────────────────────┘
                           │ chan IngestionJob (cap: 256)
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│               Phase 3: Bounded Worker Pool (30)                  │
│  ┌──────────┐  ┌──────────┐        ┌──────────┐                │
│  │ Worker 0 │  │ Worker 1 │  ...   │ Worker 29│                │
│  └────┬─────┘  └────┬─────┘        └────┬─────┘                │
│       │ fetchBlobREST()                  │                      │
│       │ [Rate-Limited, Backoff]          │                      │
│       ▼                                  ▼                      │
│  io.TeeReader(github, BLAKE3_Hasher) → io.Copy(driveWriter)     │
│  [Calculates CasKey on the fly, zero-disk overhead]             │
└──────────────────────────┬──────────────────────────────────────┘
                           │ QUIC/HTTP3
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│             Phase 4: CAS Prefix Partitioning                     │
│  hashes/3d/gitsov_blake3... [2-char prefix of BLAKE3 digest]    │
│  http3.RoundTripper{TLS1.3} → Google Drive API                  │
└─────────────────────────────────────────────────────────────────┘
```

## Phase 1: Exhaustive Merkle Discovery

### Branch Resolution
- `resolveBranches()` paginates `GET /repos/{owner}/{repo}/branches?per_page=100`
- Returns HEAD commit SHA for **every** branch (not just default)
- Native `net/http` with Bearer token replaces `exec.Command("gh.exe")`

### Tree Walk with Truncation Recovery
- `walkTree()` queries `GET /repos/{owner}/{repo}/git/trees/{sha}?recursive=1`
- When `truncated: true`, identifies subtree nodes (`type: "tree"`) and recursively queries each via `walkTreeShallow()`
- Complete graph reconstruction guaranteed

### Density Heuristic (S/n Evaluation)
For each top-level directory:
- Let **n** = number of novel blob files in the directory
- Let **S** = total size of those files in bytes
- If `n > 50` AND `S/n < 250KB` → **PACK** (go-git packfile negotiation)
- Otherwise → **REST** (individual blob fetch)

All routing decisions logged via `slog.Info("density-heuristic-evaluated")`.

## Phase 2: In-Memory Deduplication

### Two-Tier Dedup
1. **Session Cache** (`sync.Map`): O(1) check — was this SHA processed *this harvest*?
2. **CAS Index** (`adph.Table`): O(1) perfect hash — does this blob exist in Google Drive?

Only blobs that pass both gates are dispatched to the worker pool.

### Cross-Branch Deduplication
Global `map[string]TreeNode` keyed by SHA ensures blobs shared across branches produce only one `IngestionJob`.

## Phase 3: Adaptive Worker Pool

### Concurrency
- 30 bounded goroutines (`DefaultWorkerPoolSize`)
- Buffered job channel (capacity 256)
- `sync.WaitGroup` for graceful shutdown

### REST Streams (Method A)
- `fetchBlobREST()` sends `Accept: application/vnd.github.v3.raw`
- Response body read via `io.Copy(&buf, resp.Body)` — no `cmd.Output()`

### Native Packfiles (Method B)
- Jobs tagged `MethodPACK` by density heuristic
- Downloads bulk GitHub packfiles natively
- **BLAKE3**: Uses `io.TeeReader` to compute `PackKey` for the archive, but also computes individual `CasKey`, `PackOffset`, and `Size` for inner files to support Selective Sector Recovery via Range requests.

### Rate Limit Resilience
- `RateLimiter.Update(resp)` inspects `X-RateLimit-Remaining` and `X-RateLimit-Reset`
- `RateLimiter.WaitIfNeeded(ctx)` blocks when remaining < 50
- Exponential backoff (2^n seconds) on 429/403, max 5 retries

## Phase 4: Sovereign CAS Partitioning

### HTTP/3 QUIC Transport (ADR-014)
```go
h3Transport := &http3.RoundTripper{
    TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13},
}
```
All Google Drive egress routes through QUIC. Automatic fallback to `http.DefaultClient` (HTTP/2) if UDP blocked.

### 2-Character Prefix Tree
Objects grouped physically by the first 2 hex characters of their BLAKE3 `CasKey` or `PackKey`:
- `hashes/3d/gitsov_blake3_3db8ac...`
- `hashes/a1/gitsov_blake3_a1f0bc...`
- 256 possible prefix folders (00–ff)

### Folder Cache
`sync.Map` caches prefix → Drive folder ID. Cache miss triggers one API call + store. Pre-warmed during index loading via `enumeratePrefixFolders()`.

### Zero-Disk Streaming
```go
bodyReader := io.MultiReader(&preamble, data, epilogue)  // compose multipart
resp, err := g.httpClient.Do(req)                         // QUIC stream
```
No `io.ReadAll()` anywhere in the upload path. Standard 32KB `io.Copy` buffers.

## Files Modified

| File | Change Summary |
|------|---------------|
| `610-SmartPipe/github.go` | Full rewrite: native HTTP, multi-branch, truncation recovery, density heuristic, session cache, worker pool, rate limiting |
| `610-SmartPipe/storage.go` | HTTP/3 transport, prefix tree CAS, folder cache, `PutBlobStream()`, QUIC fallback |
| `610-SmartPipe/BUILD.bazel` | Added `lukechampine.com/blake3`, `gitsov-key`, `adph`, `@com_github_quic_go_quic_go//http3` deps |
| `610-SmartPipe/jebnf_serializer.go` | (New) Pure `bytes.Buffer` strict jeBNF struct encoding for `BranchTree`, `Topology`, and `ComponentRef` |
| `610-SmartPipe/tree_compare.go` | (New) `O(N)` purely in-memory sub-millisecond comparative diff engine using logical `SHA` keys |

## Telemetry

| Log Event | Data |
|-----------|------|
| `branch-resolution-complete` | repo, total_branches |
| `tree-truncated-initiating-recovery` | repo, tree_sha, partial_count |
| `density-heuristic-evaluated` | path, n, S, S_over_n, method |
| `ingestion-progress` (every 100 blobs) | ingested, deduped, session_hits, bytes |
| `rate-limit-backoff` | remaining, reset_in, total_backoffs |
| `adaptive-merkle-ingestion-complete` | branches, unique_nodes, blobs_ingested, blobs_deduped, throughput_mbps |
| `cas-put-blob` | hash, size, prefix, transport |
| `prefix-folder-cached` | prefix, folder_id |

## Verification
- Build: `builder.exe -ws 60PROX/GitSovereign -pkg harvester`
- Dry-run: `harvester -mode harvest -org <org> --dry-run`
- Assurance: Pinnacle-Assurance report auto-generated post-harvest
