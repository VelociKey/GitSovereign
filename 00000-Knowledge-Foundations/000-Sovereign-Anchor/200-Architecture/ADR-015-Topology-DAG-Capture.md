# ADR-015: Git Topology DAG Capture for Full-History Rehydration

## Status
**Proposed** — 2026-03-31

## Depends On
- CYC-00268 (Adaptive Harvesting Pipeline) — Implemented
- CYC-00269 (Rehydration Pipeline) — Planned

## Context

The CYC-00268 harvest captures **blob content** (file data) and maps it to logical paths via manifests. However, it does NOT capture the **topology** of the Git repository — the commit DAG, branch refs, tag refs, author metadata, or parent relationships. This means rehydration can only produce a single "snapshot" commit per branch, losing all history.

Git's power lies in its DAG structure. Without it, sovereignty over the repository's evolution history is incomplete.

## Decision

The harvest pipeline will capture a **Topology Tree** — a self-contained `jeBNF` document encoding the complete Git commit DAG, branch refs, and tag refs — stored as a separate CAS object in the semantic tree. This topology is **decoupled from blob content**: it references blob SHAs (which resolve to CAS keys) but does not contain file data.

## Topology Document Structure

```
::Olympus::Firehorse::Topology::v1 {
  Repo = "VelociKey/SovereignApp";
  CapturedAt = "2026-03-31T16:00:00Z";
  
  Branches {
    main = "abc123...";
    develop = "def456...";
    "feature/auth" = "789abc...";
  }
  
  Tags {
    "v1.0.0" { SHA = "aaa111..."; Type = "annotated"; Tagger = "dev@co.com"; Message = "Release v1.0.0"; Date = "2026-01-15T10:00:00Z"; }
    "v0.9.0" { SHA = "bbb222..."; Type = "lightweight"; }
  }
  
  Commits [
    Commit "abc123..." {
      Tree = "tree_sha_1...";
      Parents = ["prev_sha_1..."];
      Author { Name = "Alice"; Email = "alice@co.com"; Date = "2026-03-30T14:00:00Z"; }
      Committer { Name = "Alice"; Email = "alice@co.com"; Date = "2026-03-30T14:00:00Z"; }
      Message = "feat: add authentication module";
    },
    Commit "prev_sha_1..." {
      Tree = "tree_sha_0...";
      Parents = ["root_sha..."];
      Author { Name = "Bob"; Email = "bob@co.com"; Date = "2026-03-29T09:00:00Z"; }
      Committer { Name = "Bob"; Email = "bob@co.com"; Date = "2026-03-29T09:00:00Z"; }
      Message = "initial commit";
    },
    Commit "root_sha..." {
      Tree = "tree_sha_root...";
      Parents = [];
      Author { Name = "Bob"; Email = "bob@co.com"; Date = "2026-03-28T08:00:00Z"; }
      Committer { Name = "Bob"; Email = "bob@co.com"; Date = "2026-03-28T08:00:00Z"; }
      Message = "Initial commit";
    }
  ];
  
  Trees [
    Tree "tree_sha_1..." [
      { Path = "src/auth.go"; Mode = "100644"; Type = "blob"; SHA = "blob_sha_a..."; },
      { Path = "src/main.go"; Mode = "100644"; Type = "blob"; SHA = "blob_sha_b..."; },
      { Path = "README.md"; Mode = "100644"; Type = "blob"; SHA = "blob_sha_c..."; },
      { Path = "scripts/run.sh"; Mode = "100755"; Type = "blob"; SHA = "blob_sha_d..."; }
    ],
    Tree "tree_sha_0..." [
      { Path = "src/main.go"; Mode = "100644"; Type = "blob"; SHA = "blob_sha_b..."; },
      { Path = "README.md"; Mode = "100644"; Type = "blob"; SHA = "blob_sha_e..."; }
    ]
  ];
}
```

### Key Design Principles

1. **Separate from blobs**: The topology doc references blob SHAs but doesn't contain file data. Blob content lives in the CAS prefix tree (`hashes/{xx}/{key}`). This avoids duplication and allows independent versioning.

2. **Self-contained DAG**: Every commit in the `commits` map includes its parent SHAs, enabling full DAG reconstruction without any external queries.

3. **Tree snapshots per commit**: Each unique `tree` SHA maps to its full file listing (path, mode, type, blob SHA). This preserves file permissions (`100755` for executables) and directory structure at every point in history.

4. **Deduplicated trees**: Many commits share the same tree entries. The `trees` map is keyed by tree SHA, so shared trees are stored once.

5. **Tags with metadata**: Annotated tags include tagger identity, message, and date. Lightweight tags are just SHA pointers.

## Storage Location

The topology document is stored as a CAS blob in the semantic tree:

```
Orgs/
  VelociKey/
    SovereignApp/
      manifest.jebnf          ← existing (file paths → CAS keys)
      topology.jebnf           ← NEW (commit DAG, branches, tags, trees)
```

It can also be CAS-addressed itself (hashed and stored in the prefix tree) with a reference in the manifest:

```
topology = [
    { ID = "topology.jebnf"; Hash = "gitsov_f8a2bc..."; },
];
```

## Harvest Changes (github.go)

### New API Calls Required

| API Endpoint | Purpose | Rate Impact |
|---|---|---|
| `GET /repos/{owner}/{repo}/git/commits/{sha}` | Fetch commit metadata (author, message, parents, tree) | 1 call per commit |
| `GET /repos/{owner}/{repo}/git/refs` | List all refs (branches + tags) | 1 call (paginated) |
| `GET /repos/{owner}/{repo}/git/tags/{sha}` | Fetch annotated tag metadata | 1 call per annotated tag |

### DAG Walk Algorithm

```
function walkCommitDAG(branchHeads):
    visited = {}           // sha → CommitNode
    queue = branchHeads    // BFS from all branch tips
    
    while queue is not empty:
        sha = queue.dequeue()
        if sha in visited: continue
        
        commit = GET /repos/{owner}/{repo}/git/commits/{sha}
        visited[sha] = CommitNode{
            tree:      commit.tree.sha,
            parents:   commit.parents[].sha,
            author:    commit.author,
            committer: commit.committer,
            message:   commit.message,
        }
        
        // Capture the tree snapshot if not already seen
        if commit.tree.sha not in treesMap:
            treeNodes = walkTree(repo, commit.tree.sha)  // reuse existing walkTree()
            treesMap[commit.tree.sha] = treeNodes
        
        // Enqueue parents for traversal
        for parent in commit.parents:
            queue.enqueue(parent.sha)
    
    return visited, treesMap
```

### Depth Control

For repositories with very deep history (>10,000 commits), the walk must be bounded:

| Flag | Behavior |
|---|---|
| `--topology-depth 0` | Full history (default) |
| `--topology-depth 100` | Last 100 commits per branch |
| `--topology-depth 1` | HEAD-only (equivalent to current behavior) |

When depth-limited, the deepest captured commit will have its parents listed as `["__TRUNCATED__"]` in the topology, signaling to rehydration that history is incomplete beyond that point.

### Rate Limit Budget

For a repo with 5,000 commits across 10 branches:
- ~5,000 commit fetches
- ~3,000 unique tree fetches (shared trees deduplicated)
- At 5,000 req/hr (authenticated), this completes in ~1.6 hours

The existing `RateLimiter` from CYC-00268 handles backoff. The topology walk runs as a **separate phase** after blob harvesting, so the two workloads don't compete for quota.

## Rehydration Changes (CYC-00269)

With the topology document available, Phase 3 (Code Tree Reconstruction) transforms from snapshot restoration to **full-history replay**:

### Topological Commit Replay

```
function replayCommitDAG(topology, targetRepo):
    // Topological sort: parents before children
    sorted = topologicalSort(topology.commits)
    shaMapping = {}  // original SHA → new GitHub SHA
    
    for commit in sorted:
        // 1. Upload blobs for this commit's tree (if not already uploaded)
        tree = topology.trees[commit.tree]
        for entry in tree:
            if entry.sha not in blobMapping:
                data = CAS.GetBlob(gitsovkey.FromSHA1(entry.sha))
                newBlobSHA = GitHub.CreateBlob(targetRepo, data)
                blobMapping[entry.sha] = newBlobSHA
        
        // 2. Create tree with remapped blob SHAs
        newTreeSHA = GitHub.CreateTree(targetRepo, remapTree(tree, blobMapping))
        
        // 3. Create commit with remapped parent SHAs
        parents = [shaMapping[p] for p in commit.parents if p != "__TRUNCATED__"]
        newCommitSHA = GitHub.CreateCommit(targetRepo, {
            message:   commit.message,
            tree:      newTreeSHA,
            parents:   parents,
            author:    commit.author,      // preserves original author + timestamp
            committer: commit.committer,
        })
        shaMapping[commit.sha] = newCommitSHA
    
    // 4. Set branch refs
    for branch, sha in topology.branches:
        GitHub.CreateOrUpdateRef(targetRepo, "refs/heads/" + branch, shaMapping[sha])
    
    // 5. Set tag refs
    for tag, info in topology.tags:
        if info.type == "annotated":
            tagObj = GitHub.CreateTag(targetRepo, tag, shaMapping[info.sha], info.message, info.tagger)
            GitHub.CreateRef(targetRepo, "refs/tags/" + tag, tagObj.sha)
        else:
            GitHub.CreateRef(targetRepo, "refs/tags/" + tag, shaMapping[info.sha])
```

### What This Preserves

| Property | Without Topology | With Topology |
|---|---|---|
| File content | ✅ | ✅ |
| File permissions | ❌ (default 644) | ✅ (from tree mode) |
| Commit messages | ❌ (generic) | ✅ (original) |
| Author identity | ❌ | ✅ (name, email, date) |
| Committer identity | ❌ | ✅ |
| Commit timestamps | ❌ | ✅ |
| Parent relationships | ❌ | ✅ (full DAG) |
| Branch refs | ❌ (default only) | ✅ (all branches) |
| Tags | ❌ | ✅ (lightweight + annotated) |
| Merge commits | ❌ | ✅ (multiple parents) |
| Git blame history | ❌ | ✅ |

## Implementation Plan

### New Files

| File | Purpose |
|---|---|
| `610-SmartPipe/topology.go` | `TopologyDocument` struct, `walkCommitDAG()`, `captureTagRefs()`, serialization |
| `610-SmartPipe/topology_test.go` | DAG walk tests, topological sort verification |

### Modified Files

| File | Change |
|---|---|
| `github.go` | Add `getCommit()`, `listRefs()`, `getTag()` API methods |
| `storage.go` | Add `PutTopology()` / `GetTopology()` methods on `GoogleDriveCAS` |
| `control_plane.go` | Add topology capture phase after blob harvest in `executeExfiltration()` |
| `main.go` | Add `--topology-depth` flag |
| `rehydrate.go` (future) | Consume topology for full-history replay |

### Manifest Extension

The repo manifest gains a `topology` component:

```
::Olympus::Firehorse::RepoTree::v1 {
    Repo = "SovereignApp";
    State = "ACTIVE";
    Timestamp = "2026-03-31T16:00:00Z";
    Components {
        code = [ ... ];
        topology = [
            { ID = "topology.jebnf"; Hash = "gitsov_f8a2bc..."; },
        ];
        issues = [ ... ];
    }
}
```

## Consequences

### Pros
- **Full sovereignty**: Complete repository history reconstruction, not just snapshots
- **Git blame / bisect**: Works on rehydrated repos since each commit is faithfully replayed
- **Merge topology preserved**: Multi-parent commits reproduce correct branch/merge structure
- **File permissions**: Executable bit, symlinks captured in tree mode field
- **Independent from blobs**: Topology can be captured/updated without re-downloading all file content

### Cons
- **API budget**: Deep histories require thousands of additional API calls (mitigated by depth control)
- **Topology document size**: A 10,000-commit repo produces ~5-10MB topology jeBNF (acceptable for CAS)
- **Rehydration time**: Full replay is O(commits × tree_size) vs O(1) for snapshot (mitigated by blob dedup)
- **SHAs change**: GitHub assigns new SHAs on blob/tree/commit creation, so rehydrated SHAs will differ from originals (SHA mapping logged in assurance report)

## Verification

The topology can be independently validated:
1. **DAG integrity**: Every parent SHA referenced in a commit must exist in the `commits` map (or be `__TRUNCATED__`)
2. **Tree coverage**: Every blob SHA referenced in a tree must have a corresponding CAS entry
3. **Ref coherence**: Every branch/tag SHA must point to a valid commit in the map
4. **Cycle detection**: The DAG must be acyclic (topological sort must succeed)
