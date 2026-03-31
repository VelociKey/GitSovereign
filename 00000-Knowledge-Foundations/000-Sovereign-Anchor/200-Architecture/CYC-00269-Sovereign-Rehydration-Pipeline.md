# CYC-00269: Sovereign Rehydration Pipeline — Branch Recovery Plan

## Status
**Planned** — 2026-03-31

## Depends On
- CYC-00268 (Adaptive Multi-Branch Harvesting Pipeline) — Implemented

## Context

The harvesting pipeline (CYC-00268) captures repository state as:

| Component | Storage Format | CAS Location |
|-----------|---------------|--------------|
| **code** | Raw blob bytes per file | `hashes/{prefix}/{gitsovkey}` |
| **issues** | JSON export (all states, paginated) | `hashes/{prefix}/{gitsovkey}` |
| **pull_requests** | JSON export | `hashes/{prefix}/{gitsovkey}` |
| **releases** | JSON export | `hashes/{prefix}/{gitsovkey}` |
| **discussions** | GraphQL JSON export | `hashes/{prefix}/{gitsovkey}` |
| **metadata** | Repository metadata JSON | `hashes/{prefix}/{gitsovkey}` |
| **wiki** | Git bundle (full history) | `hashes/{prefix}/{gitsovkey}` |

The **semantic tree** uses a branch-level hierarchy mapping logical SHAs to physical CAS keys:
```
branches/main/tree.jebnf = [
    { Path = "src/main.go"; Mode = "100644"; SHA = "abc1234..."; CasKey = "gitsov_blake3_3db8ac..."; },
    { Path = "scripts/init.sh"; Mode = "100755"; SHA = "def4567..."; PackKey = "gitsov_blake3_packabc..."; PackOffset = 10485; },
];
```

### What Is NOT Stored (Critical Constraints)
- **Git submodules**: Excluded (`type: "commit"` nodes skipped)
- **Release Assets**: Binary attachments on releases are not currently downloaded

*Note: As of ADR-015, full commit DAGs, parent histories, timestamps, authors, tags, and file permissions (100755) ARE natively captured in the `topology.jebnf` manifest.*

## Decision

Implement a 5-phase rehydration pipeline that reconstructs a repository from CAS blobs into a fresh GitHub repository, restoring the latest file tree and re-importing structured data (issues, PRs, discussions) via the GitHub API.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│              Phase 1: Manifest Resolution                        │
│  Google Drive CAS ──► Parse branches/{branch}/tree.jebnf        │
│  Resolve org/repo topology, enumerate components for selection  │
└──────────────────────────┬──────────────────────────────────────┘
                           │ ManifestEntry[]
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│              Phase 2: Target Repository Provisioning             │
│  GitHub API ──► Create repo (or verify empty target)            │
│  Set default branch, description, topics from metadata.json     │
└──────────────────────────┬──────────────────────────────────────┘
                           │ owner/repo ready
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│              Phase 3: Code Tree Reconstruction                   │
│                                                                  │
│  3a. Stream blobs from CAS ──► GitHub Git Database API          │
│      POST /repos/{owner}/{repo}/git/blobs (Create Blob)         │
│      [Bounded worker pool, QUIC ingress from Drive]             │
│                                                                  │
│  3b. Build Git Tree object from blob SHAs                       │
│      POST /repos/{owner}/{repo}/git/trees (Create Tree)         │
│      [Batched in chunks of 100 entries]                          │
│                                                                  │
│  3c. Create Commit pointing to root tree                        │
│      POST /repos/{owner}/{repo}/git/commits                     │
│                                                                  │
│  3d. Update branch ref to commit                                │
│      PATCH /repos/{owner}/{repo}/git/refs/heads/{branch}        │
└──────────────────────────┬──────────────────────────────────────┘
                           │ Branch restored
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│              Phase 4: Structured Data Re-Import                  │
│  4a. Issues:       POST /repos/{owner}/{repo}/import/issues     │
│  4b. Releases:     POST /repos/{owner}/{repo}/releases          │
│  4c. Discussions:  GraphQL createDiscussion mutation             │
│  4d. Wiki:         git push wiki.bundle to {repo}.wiki.git      │
└──────────────────────────┬──────────────────────────────────────┘
                           │ All components restored
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│              Phase 5: Verification & Assurance Report             │
│  Compare: rehydrated tree SHA vs. original tree SHA             │
│  Generate: Rehydration Assurance Report (jeBNF)                 │
└─────────────────────────────────────────────────────────────────┘
```

## Phase 1: Manifest Resolution

### Inputs
- **org**: GitHub organization name
- **repo**: Repository name to rehydrate
- **source_folder_id**: Google Drive root folder ID (same as harvest target)

### Steps
1. Authenticate to Google Drive via `gcloud auth print-access-token`
2. Navigate semantic tree: `Orgs/{org}/{repo}/branches/` completely or selectively (`Orgs/{org}/{repo}/branches/main/tree.jebnf`)
3. Parse the jeBNF specific to the requested recovery profile.
4. For each `code` entry, resolve the CAS hash to a BLAKE3 prefix folder path:
   ```
   hash "gitsov_blake3_3db8ac..." → hashes/3d/gitsov_blake3_3db8ac...
   ```
5. **Integrity pre-check**: Verify all referenced BLAKE3 hashes exist via `BlobExists()` (ADPH index or Drive API). Abort if any are missing with a detailed gap report.

### Output
```go
type RehydrationManifest struct {
    Org        string
    Repo       string
    State      string
    Timestamp  time.Time
    Components map[string][]ManifestEntry  // code, issues, etc.
    Missing    []string                     // CAS hashes not found
}
```

## Phase 2: Target Repository Provisioning

### Steps
1. **Check if target exists**: `GET /repos/{owner}/{repo}`
   - If exists and non-empty → **abort** (refuse to overwrite unless `--force` flag)
   - If exists and empty → proceed
   - If 404 → create via `POST /user/repos` or `POST /orgs/{org}/repos`

2. **Apply metadata** from `metadata.json` CAS blob:
   - Description, homepage, topics, visibility (public/private)
   - Default branch name
   - `PATCH /repos/{owner}/{repo}`

3. **Initialize** the repository with an empty commit if needed:
   - This creates the default branch ref that Phase 3 will update

### Safety Gate
```
If target repo has existing commits AND --force is not set:
    ABORT with "REHYDRATION REFUSED: Target repository is not empty"
```

## Phase 3: Code Tree Reconstruction

This is the critical path. We reconstruct a Git commit from flat CAS blobs using the GitHub Git Database API (low-level Git object creation).

### 3a. Blob Upload (Bounded Worker Pool)

For each `code` manifest entry:

1. Stream blob from Google Drive CAS via QUIC/HTTP3. If the blob is stored inside a `PackKey`, issue an HTTP/3 `Range` Request:
   ```
   GET https://www.googleapis.com/drive/v3/files/{PackKeyID}?alt=media
   Range: bytes={PackOffset}-{PackOffset+Size-1}
   ```
   *(This drastically limits memory and network usage by pulling only precise vectors from multi-GB pack archives)*
2. Verify the 256-bit BLAKE3 digest on the fly via `io.TeeReader` against the expected `SHA` or `CasKey`.
3. Upload to GitHub as a Git blob:
   ```
   POST /repos/{owner}/{repo}/git/blobs
   {
     ...
     "encoding": "base64"
   }
   ```
3. Record the returned GitHub blob SHA for tree construction

**Concurrency**: 20 workers (lower than harvest due to GitHub create rate limits)
**Rate Limiting**: Same `RateLimiter` from CYC-00268 with 429/403 backoff
**Memory**: Stream from Drive → base64 encode → POST. For large files (>10MB), use chunked encoding.

### 3b. Tree Construction

Once all blobs are uploaded, construct the Git tree object:

```
POST /repos/{owner}/{repo}/git/trees
{
  "tree": [
    {"path": "src/main.go",  "mode": "100644", "type": "blob", "sha": "<github-blob-sha>"},
    {"path": "README.md",    "mode": "100644", "type": "blob", "sha": "<github-blob-sha>"},
    ...
  ]
}
```

**GitHub limit**: 1000 entries per tree create call. For repos with >1000 files:
- Build subtrees bottom-up (directories first)
- Reference subtree SHAs in parent tree
- Final root tree references all top-level entries

### 3c. Commit Creation

```
POST /repos/{owner}/{repo}/git/commits
{
  "message": "Sovereign Rehydration: Restored from GitSovereign CAS backup\n\nSource: Google Drive CAS\nOriginal Timestamp: {manifest.Timestamp}\nRehydration Agent: GitSovereign v2.0",
  "tree": "<root-tree-sha>",
  "parents": []   // No parent for initial commit (or existing HEAD for --force)
}
```

### 3d. Branch Ref Update

```
POST /repos/{owner}/{repo}/git/refs
{
  "ref": "refs/heads/{default_branch}",
  "sha": "<commit-sha>"
}
```

Or for existing refs:
```
PATCH /repos/{owner}/{repo}/git/refs/heads/{default_branch}
{
  "sha": "<commit-sha>",
  "force": true
}
```

### Multi-Branch & History Restoration (v2 topology)
Since ADR-015, single-commit snapshots are bypassed if a `topology.jebnf` block is detected.
- The rehydration engine parses the full Commit DAG map from `topology`
- Sorts it topologically (parents first)
- Uploads blobs, then chunks tree creations `POST /git/trees`
- Reconstructs every commit with identical parent/author/message references
- Reassembles `main`, `feature/x`, `bugfix/y` refs via `POST /git/refs` in one cohesive block.

## Phase 4: Structured Data Re-Import

### 4a. Issues

Parse `issues.json` from CAS. Use the GitHub Issue Import API for bulk import with original metadata:

```
POST /repos/{owner}/{repo}/import/issues
{
  "issue": {
    "title": "...",
    "body": "...",
    "created_at": "...",
    "labels": [...],
    "assignee": "...",
    "state": "closed"
  },
  "comments": [...]
}
```

**Ordering**: Import in ascending `number` order to preserve issue numbering where possible.

### 4b. Releases

Parse `releases.json` from CAS:

```
POST /repos/{owner}/{repo}/releases
{
  "tag_name": "v1.0.0",
  "name": "...",
  "body": "...",
  "draft": false,
  "prerelease": false
}
```

**Note**: Release assets (binary attachments) are NOT captured in current harvest. Flag for future enhancement.

### 4c. Discussions

Parse `discussions.json` from CAS. Use GraphQL mutation:

```graphql
mutation {
  createDiscussion(input: {
    repositoryId: "<repo-node-id>",
    categoryId: "<category-id>",
    title: "...",
    body: "..."
  }) { discussion { id } }
}
```

**Constraint**: Discussion categories must be pre-created on the target repo.

### 4d. Wiki

The wiki is stored as a Git bundle (full history preserved):

```bash
git clone --mirror {wiki.bundle} /tmp/wiki-restore
git -C /tmp/wiki-restore remote set-url origin https://github.com/{owner}/{repo}.wiki.git
git -C /tmp/wiki-restore push --mirror origin
```

This is the **only component that preserves full history**, since it was harvested via `git clone --mirror` + `git bundle`.

## Phase 5: Verification & Assurance

### Tree SHA Comparison
After rehydration, query the reconstructed tree:
```
GET /repos/{owner}/{repo}/git/trees/{branch_sha}?recursive=1
```
Compare the file count and total size against the original manifest entries.

### Integrity Report (jeBNF)
```
::Olympus::Firehorse::RehydrationAssurance::v1 {
    Org = "VelociKey";
    Repo = "SovereignApp";
    Timestamp = "2026-03-31T16:00:00Z";
    Source = "Google Drive CAS";
    RestoredComponents {
        Code {
            Files = 1247;
            TotalBytes = 48329012;
            TreeSHA_Match = true;
        }
        Issues { Count = 89; Imported = 89; }
        Releases { Count = 12; Imported = 12; }
        Discussions { Count = 5; Imported = 5; }
        Wiki { HistoryPreserved = true; }
    }
    Status = "VERIFIED";
    Duration = "4m12s";
}
```

## CLI Interface

```
harvester -mode rehydrate \
          -org VelociKey \
          -repo SovereignApp \
          -target-org VelociKey-Recovery \
          -target-repo SovereignApp-restored \
          --components code,issues,releases \
          --dry-run
```

| Flag | Description |
|------|-------------|
| `-org` | Source org (as stored in manifest) |
| `-repo` | Source repo name |
| `-target-org` | Destination GitHub org (defaults to `-org`) |
| `-target-repo` | Destination repo name (defaults to `-repo`) |
| `--components` | Comma-separated list: `code,issues,pull_requests,releases,discussions,wiki,metadata` (default: all) |
| `--force` | Allow overwriting non-empty target repo |
| `--dry-run` | Parse manifest and verify CAS integrity without writing |
| `--branch` | Specific branch to restore (default: all captured branches) |

## Implementation Files

| File | Purpose |
|------|---------|
| `610-SmartPipe/rehydrate.go` | Core rehydration pipeline (Phases 1, 3, 5) |
| `610-SmartPipe/rehydrate_import.go` | Structured data re-import (Phase 4) |
| `610-SmartPipe/storage.go` | Add `GetBlob()` / `GetBlobStream()` methods to `GoogleDriveCAS` |
| `610-SmartPipe/github.go` | Add `CreateBlob()`, `CreateTree()`, `CreateCommit()`, `UpdateRef()` methods |
| `610-SmartPipe/main.go` | Add `rehydrate` mode to CLI switch |

## Risk Matrix

| Risk | Severity | Mitigation |
|------|----------|------------|
| Target repo not empty | High | Safety gate: refuse unless `--force` |
| CAS blobs missing (partial backup) | High | Phase 1 integrity pre-check with gap report |
| GitHub rate limits on blob creation | Medium | 20-worker pool with `RateLimiter` backoff |
| Issue number gaps (imports don't preserve exact numbers) | Low | Document in assurance report |
| Discussion categories don't exist on target | Low | Pre-create categories or skip with warning |
| Large files >100MB (GitHub limit) | Medium | Detect and flag; suggest Git LFS migration |
| Git history not preserved | Low | Fixed! ADR-015 `topology.jebnf` now carries full DAG metadata |
| File permissions (executable bit) | Low | Fixed! `100755` preserved in `topology.jebnf` tree modes |

## Future Enhancements

1. **History-Preserving Harvest**: Store commit graph metadata (parents, messages, authors, timestamps) alongside blobs to enable full history reconstruction
2. **Branch Topology Map**: Persist branch→commit→tree mapping in manifest for multi-branch restoration
3. **Release Asset Capture**: Download and CAS-store binary release assets
4. **Git LFS Support**: Detect LFS pointer files and restore LFS objects
5. **Incremental Rehydration**: Restore only files changed since a given timestamp
6. **go-git Packfile Push**: Use go-git to construct and push a packfile directly (bypassing per-blob API calls) for massive repos
