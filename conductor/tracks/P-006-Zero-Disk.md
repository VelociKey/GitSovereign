# Implementation Plan: Zero-Disk Mandate & Scheduling (Pulse 6)

This plan fulfills the **Zero-Disk Mandate** by pivoting repository streaming to non-storage pass-through and implements **Sync Scheduling** by parsing the jeBNF registry.

## Objective
1.  **Zero-Disk Harvest**: Replace `git clone --mirror` with `gh api` tarball streaming in `github.go` to ensure no local disk is used during the retrieval of repository code.
2.  **jeBNF Scheduling**: Update `main.go` to parse `sync_registry.jebnf` and respect frequency settings (DAILY/WEEKLY) for all targets.

## Key Files & Context
-   **GitHub Client**: `60PROX/GitSovereign/60000-Information-Storage/610-SmartPipe/github.go`
-   **Main Orchestrator**: `60PROX/GitSovereign/60000-Information-Storage/610-SmartPipe/main.go`
-   **Sync Registry**: `60PROX/GitSovereign/C0100-Configuration-Registry/sync_registry.jebnf`

## Implementation Steps

### Pulse 6.1: Zero-Disk Streaming (github.go)
-   Update `StreamRepository(repo string, w io.Writer)`:
    -   Remove `git clone --mirror` logic.
    -   Remove local scratch directory usage.
    -   Execute `gh api repos/:repo/tarball` and pipe `Stdout` directly to the provided `io.Writer`.
    -   Capture and log `Stderr` for diagnostics.

### Pulse 6.2: Dynamic Scheduling (main.go)
-   Add `bufio` and `strings` to imports.
-   Update `LoadSyncRegistry()`:
    -   Resolve path to `60PROX/GitSovereign/C0100-Configuration-Registry/sync_registry.jebnf`.
    -   Implement a line-by-line scanner to parse the jeBNF structure.
    -   Populate `SyncRegistry` struct with `DefaultFrequency` and `Schedules`.
-   Implement `extractValue(line, key string)` helper for robust parsing of quoted jeBNF values.

## Verification & Testing
-   **Zero-Disk Test**: Run a harvest and verify that no `git-mirror-*` directories are created in `@SCRATCH`.
-   **Scheduling Test**:
    -   Modify `sync_registry.jebnf` to set a target to `WEEKLY`.
    -   Verify that `IsSyncDue` returns `false` if the last sync was within 7 days.
    -   Verify that `IsSyncDue` returns `true` for new repositories.

## Migration & Rollback
-   The previous "mirror" logic is archived in git history.
-   Rollback involves reverting `github.go` to the `exec.Command("git", "clone", "--mirror"...)` implementation.
