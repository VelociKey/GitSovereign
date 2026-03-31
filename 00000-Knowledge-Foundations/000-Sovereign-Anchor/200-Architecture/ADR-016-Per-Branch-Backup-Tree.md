# ADR-016: Sovereign Backup Tree — Per-Branch Component Structure

## Status
**Proposed** — 2026-03-31

## Supersedes
Flat `manifest.jebnf` per repository (CYC-00268 original design)

## Context

The original harvest writes a single flat `manifest.jebnf` per repository with all components (code, issues, releases) mixed together. This makes it impossible to:

1. **Browse by branch** — there's no way to see what branches were captured
2. **Selective recovery** — a user cannot recover just one branch, or just the issues
3. **Branch-level diffing** — no way to compare two branches within the backup
4. **Partial restore** — recovering a single subdirectory from a specific branch is not possible

A Sovereign Backup must be a **self-describing tree** that mirrors the repository's branch structure and allows arbitrary subset selection for rehydration.

## Decision

Restructure the backup storage into a **hierarchical tree** organized as:

```
Repository → Branches → Components → Entries
```

Each branch carries its own complete component tree. Repo-level data (issues, PRs, discussions, releases) exists at the repository root since it is branch-independent.

## Storage Tree Structure

```
Orgs/
  {org}/
    manifest.jebnf                          ← Org-level: list of repos
    {repo}/
      ├── manifest.jebnf                    ← Repo root manifest (metadata, summary)
      ├── topology.jebnf                    ← Full commit DAG (ADR-015), all branches
      │
      ├── branches/
      │   ├── main/
      │   │   ├── manifest.jebnf            ← Branch manifest: HEAD SHA, file count, timestamp
      │   │   └── tree.jebnf                ← Complete file tree → CAS hash mapping
      │   │
      │   ├── develop/
      │   │   ├── manifest.jebnf
      │   │   └── tree.jebnf
      │   │
      │   └── feature-auth/
      │       ├── manifest.jebnf
      │       └── tree.jebnf
      │
      └── components/
          ├── issues.jebnf                  ← CAS hash → full issue export
          ├── pull_requests.jebnf           ← CAS hash → full PR export
          ├── releases.jebnf                ← CAS hash → full release export
          ├── discussions.jebnf             ← CAS hash → GraphQL export
          ├── metadata.jebnf                ← CAS hash → repo metadata
          └── wiki.bundle                   ← CAS hash → git bundle (full wiki history)
```

> **Note**: Actual file content (blobs) remains in the CAS prefix tree at `hashes/{xx}/{gitsovkey}`. The tree above is the **semantic manifest layer** — it contains references (CAS hashes), not data.

## Document Formats

### Repo Root Manifest (`{repo}/manifest.jebnf`)

```
::Olympus::Firehorse::RepoBackup::v2 {
    Repo = "SovereignApp";
    Org = "VelociKey";
    State = "ACTIVE";
    CapturedAt = "2026-03-31T16:00:00Z";
    DefaultBranch = "main";

    BranchCount = 3;
    Branches = ["main", "develop", "feature-auth"];

    Components = ["issues", "pull_requests", "releases",
                  "discussions", "metadata", "wiki"];

    Topology {
        Hash = "gitsov_f8a2bc...";
        CommitCount = 1247;
        DepthMode = "FULL";
    }

    Stats {
        TotalBlobs = 3842;
        TotalBytes = 148329012;
        UniqueBlobs = 2901;
        DeduplicationRatio = 0.245;
    }
}
```

### Branch Manifest (`branches/{branch}/manifest.jebnf`)

```
::Olympus::Firehorse::BranchBackup::v1 {
    Branch = "develop";
    HeadCommitSHA = "abc123def456...";
    HeadTreeSHA = "tree789abc...";
    CapturedAt = "2026-03-31T16:00:00Z";

    Stats {
        FileCount = 247;
        TotalBytes = 12480000;
        Directories = 34;
    }

    Tree {
        Hash = "gitsov_branch_tree_hash...";
        Format = "tree.jebnf";
    }
}
```

### Branch Tree (`branches/{branch}/tree.jebnf`)

The complete file→CAS mapping for this branch at HEAD:

```
::Olympus::Firehorse::BranchTree::v1 {
  Branch = "develop";
  HeadSHA = "abc123def456...";
  CapturedAt = "2026-03-31T16:00:00Z";
  
  Entries [
    { Path = "src/main.go"; Mode = "100644"; SHA = "blob_sha_aaa..."; CasKey = "gitsov_3db8ac..."; Size = 4096; },
    { Path = "scripts/deploy.sh"; Mode = "100755"; SHA = "blob_sha_bbb..."; CasKey = "gitsov_a1f0bc..."; Size = 2048; },
    { Path = "docs/README.md"; Mode = "100644"; SHA = "blob_sha_ccc..."; CasKey = "gitsov_c4e2d1..."; Size = 8192; }
  ];
  
  Directories [
    { Path = "src"; EntryCount = 12; TotalBytes = 48000; },
    { Path = "scripts"; EntryCount = 3; TotalBytes = 6144; },
    { Path = "docs"; EntryCount = 8; TotalBytes = 32000; }
  ];
}
```

### Repo-Level Components (`components/{name}`)

Each component file is a lightweight reference to a CAS-stored blob:

```
::Olympus::Firehorse::ComponentRef::v1 {
  Component = "issues";
  CasKey = "gitsov_issue_hash...";
  Format = "github_api_v3_json";
  RecordCount = 89;
  CapturedAt = "2026-03-31T16:00:00Z";
}
```

The actual issue/PR/release data lives in the CAS at `hashes/{xx}/{gitsov_key}`.

## Selective Recovery Use Cases

This tree structure enables the following recovery scenarios:

### 1. Full Repository Recovery
```
recover -org VelociKey -repo SovereignApp
```
Restores all branches, all components, full topology.

### 2. Single Branch Recovery
```
recover -org VelociKey -repo SovereignApp --branch develop
```
Reads `branches/develop/tree.jebnf`, uploads only those blobs, creates a single-branch repo.

### 3. Code Only (No Issues/PRs)
```
recover -org VelociKey -repo SovereignApp --components code
```
Restores all branches' code trees, skips issues/PRs/releases/discussions.

### 4. Issues + Releases Only (No Code)
```
recover -org VelociKey -repo SovereignApp --components issues,releases
```
Creates an empty repo and imports just the structured data.

### 5. Single Branch + Specific Components
```
recover -org VelociKey -repo SovereignApp --branch main --components code,releases
```
Restores code from `main` only, plus releases.

### 6. Subdirectory Recovery (Future)
```
recover -org VelociKey -repo SovereignApp --branch main --path src/auth/
```
Reads `branches/main/tree.jebnf`, filters entries to `src/auth/**`, uploads only matching blobs.

### 7. Point-in-Time Recovery (With Topology)
```
recover -org VelociKey -repo SovereignApp --branch main --at-commit abc123
```
Uses `topology.jebnf` to find commit `abc123`'s tree SHA, then walks that tree's entries from the CAS.

## Harvest Pipeline Changes

### Phase 1 (Discovery) — Per-Branch Tree Capture

After `resolveBranches()` and `walkTree()`, instead of merging all nodes into a flat global map, store each branch's tree separately:

```go
// For each branch, store its own tree.jebnf
for _, branch := range branches {
    nodes, _ := c.walkTree(ctx, repo, branch.Commit.SHA)
    
    branchTree := BranchTree{
        Branch:    branch.Name,
        HeadSHA:   branch.Commit.SHA,
        Entries:   toBranchEntries(nodes),  // path, mode, sha, cas_key, size
        Dirs:      computeDirStats(nodes),
    }
    
    // Serialize and store as CAS blob
    treeJEBNF := serializeToJEBNF(branchTree)
    treeKey := gitsovkey.FromBytes(treeJEBNF)
    cas.PutBlob(ctx, treeKey.Hex(), treeJEBNF)
    
    // Write branch manifest to semantic tree
    cas.WriteBranchManifest(ctx, org, repo, branch.Name, branchTree)
}
```

### Phase 2 (Deduplication) — Cross-Branch Blob Dedup

Blobs shared across branches are still stored once in the CAS. Each branch's `tree.jebnf` independently references the same `cas_key`. This is pure CAS — no duplication.

```
main/tree.jebnf:      "src/main.go" → cas_key "gitsov_3db8ac..."
develop/tree.jebnf:   "src/main.go" → cas_key "gitsov_3db8ac..."   ← same key, stored once
```

### Phase 3 (Worker Pool) — No Change

The blob upload pipeline is unchanged. The only addition is writing the per-branch tree.jebnf and manifest files to the semantic tree at the end.

### Phase 4 (Topology) — Stored at Repo Root

The `topology.jebnf` (ADR-015) spans all branches and is stored at the repo root, not per-branch. It references branch names that correspond to the `branches/` subdirectories.

### New Phase: Component Capture

Repo-level components (issues, PRs, etc.) are written to `components/` with their CAS references:

```go
for _, comp := range ["issues", "pull_requests", "releases", "discussions", "metadata", "wiki"] {
    files, _ := source.FetchComponent(repo, comp)
    for _, f := range files {
        key := gitsovkey.FromBytes(f.Data)
        cas.PutBlob(ctx, key.Hex(), f.Data)
        cas.WriteComponentRef(ctx, org, repo, comp, key.Hex(), len(records))
    }
}
```

## Storage Methods Required

New methods on `GoogleDriveCAS`:

| Method | Purpose |
|---|---|
| `CreateBranchFolder(ctx, org, repo, branch)` | Create `branches/{branch}` subfolder |
| `WriteBranchManifest(ctx, org, repo, branch, tree)` | Write branch `manifest.jebnf` + `tree.jebnf` |
| `WriteComponentRef(ctx, org, repo, comp, hash, count)` | Write component reference file |
| `ListBranches(ctx, org, repo)` | Enumerate available branches for recovery UI |
| `GetBranchTree(ctx, org, repo, branch)` | Read a branch's `tree.jebnf` for selective recovery |
| `ListComponents(ctx, org, repo)` | Enumerate available repo-level components |

## Migration from v1 Manifests

Existing flat `manifest.jebnf` files (v1) remain readable. The rehydration pipeline detects format version:

```go
if manifest.Version == "RepoTree::v1" {
    // Legacy: flat component list, single branch assumed
    return legacyRehydrate(manifest)
}
// v2: per-branch tree structure
return selectiveRehydrate(manifest, options)
```

## Interaction Surface (Future)

The per-branch tree structure maps directly to a browsable UI in the Flutter dashboard:

```
📂 VelociKey
  📂 SovereignApp
    🔀 Branches (3)
      ├── 🌿 main        [247 files, 12.4 MB]  ☑️
      ├── 🌿 develop     [251 files, 13.1 MB]  ☑️
      └── 🌿 feature-auth [253 files, 13.5 MB] ☐
    📋 Components
      ├── 📝 Issues (89)         ☑️
      ├── 🔃 Pull Requests (42)  ☐
      ├── 📦 Releases (12)       ☑️
      ├── 💬 Discussions (5)     ☐
      └── 📖 Wiki                ☑️
    
    [🔄 Recover Selected]  [📊 Compare Branches]
```

Users check/uncheck branches and components, then hit recover.

## Consequences

### Pros
- **User agency**: Users select exactly what to recover — no all-or-nothing
- **Branch-level browsing**: Each branch is independently inspectable
- **Minimal overhead**: Per-branch manifests are small jeBNF files (KB), not redundant blob copies
- **CAS efficiency preserved**: Cross-branch blob dedup is unchanged
- **UI-ready**: Tree structure maps directly to a navigation/selection interface
- **Future-proof**: Subdirectory and point-in-time recovery become possible

### Cons
- **More Drive API calls**: Creating per-branch subfolders + manifests (~3 calls per branch)
- **Slightly larger semantic tree**: More manifest files, but negligible vs. blob data
- **Version migration**: v1 manifests need compatibility shim
