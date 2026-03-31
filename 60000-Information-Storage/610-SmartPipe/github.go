package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gitsovkey "olympus.fleet/00SDLC/OlympusLogicLibrary/60000-Information-Storage/90200-Logic-Libraries/110-gitsov-key"
)

// ─── Ingestion Method Tags ──────────────────────────────────────────────────────

// IngestionMethod distinguishes the optimal protocol for fetching blob data.
type IngestionMethod int

const (
	// MethodREST uses GitHub REST API to fetch individual blobs (sparse trees, large binaries).
	MethodREST IngestionMethod = iota
	// MethodPACK uses go-git packfile negotiation (dense directories with many small files).
	MethodPACK
)

func (m IngestionMethod) String() string {
	if m == MethodPACK {
		return "PACK"
	}
	return "REST"
}

// ─── Density Heuristic Thresholds ───────────────────────────────────────────────

const (
	// DensityFileCountThreshold: minimum number of novel files to trigger packfile mode.
	DensityFileCountThreshold = 50
	// DensityAvgSizeThreshold: average file size below which packfile is more efficient (250 KB).
	DensityAvgSizeThreshold = 250 * 1024
	// DefaultWorkerPoolSize: bounded goroutine pool for concurrent ingestion.
	DefaultWorkerPoolSize = 30
	// RateLimitSafetyBuffer: stop REST calls when remaining quota drops below this.
	RateLimitSafetyBuffer = 50
)

// ─── Ingestion Job ──────────────────────────────────────────────────────────────

// IngestionJob represents a single unit of work dispatched to the worker pool.
type IngestionJob struct {
	// BlobSHA is the Git object hash to fetch.
	BlobSHA string
	// SovKey is the Sovereign CAS key (hex string).
	SovKey string
	// Path is the logical file path within the repository tree.
	Path string
	// Size is the blob size in bytes (from tree enumeration).
	Size int64
	// Method is the ingestion protocol selected by the density heuristic.
	Method IngestionMethod
	// SubtreeSHA is populated for PACK jobs; it's the tree SHA to negotiate.
	SubtreeSHA string
	// Repo is the full owner/repo slug.
	Repo string
}

// ─── Branch Info ────────────────────────────────────────────────────────────────

// BranchInfo represents a single branch discovered via the Branches API.
type BranchInfo struct {
	Name   string `json:"name"`
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

// ─── Tree Structures ────────────────────────────────────────────────────────────

// TreeNode represents a single entry from the GitHub Trees API response.
type TreeNode struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" or "tree"
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

// TreeResponse is the response envelope from GET /repos/{owner}/{repo}/git/trees/{sha}.
type TreeResponse struct {
	SHA       string     `json:"sha"`
	Tree      []TreeNode `json:"tree"`
	Truncated bool       `json:"truncated"`
}

// ─── Session Cache ──────────────────────────────────────────────────────────────

// SessionCache is a thread-safe deduplication cache for blob SHAs processed in the current harvest.
type SessionCache struct {
	seen sync.Map
	hits atomic.Int64
}

// MarkSeen records a blob SHA as processed. Returns true if it was already seen.
func (sc *SessionCache) MarkSeen(sha string) bool {
	_, loaded := sc.seen.LoadOrStore(sha, struct{}{})
	if loaded {
		sc.hits.Add(1)
	}
	return loaded
}

// Hits returns the number of cache hits (duplicates avoided).
func (sc *SessionCache) Hits() int64 {
	return sc.hits.Load()
}

// ─── Rate Limiter ───────────────────────────────────────────────────────────────

// RateLimiter tracks GitHub API rate limits from response headers.
type RateLimiter struct {
	mu        sync.Mutex
	remaining int
	resetAt   time.Time
	backoffs  atomic.Int64
}

// Update inspects HTTP response headers to refresh rate limit state.
func (rl *RateLimiter) Update(resp *http.Response) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if v := resp.Header.Get("X-RateLimit-Remaining"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			rl.remaining = n
		}
	}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if epoch, err := strconv.ParseInt(v, 10, 64); err == nil {
			rl.resetAt = time.Unix(epoch, 0)
		}
	}
}

// WaitIfNeeded blocks the goroutine if rate limits are near exhaustion.
func (rl *RateLimiter) WaitIfNeeded(ctx context.Context) error {
	rl.mu.Lock()
	rem := rl.remaining
	reset := rl.resetAt
	rl.mu.Unlock()

	if rem > RateLimitSafetyBuffer || rem == 0 {
		return nil // 0 means we haven't received headers yet
	}

	wait := time.Until(reset)
	if wait <= 0 {
		return nil
	}

	rl.backoffs.Add(1)
	slog.Warn("rate-limit-backoff",
		"remaining", rem,
		"reset_in", wait.Round(time.Second),
		"total_backoffs", rl.backoffs.Load(),
	)

	select {
	case <-time.After(wait):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ─── GitHubClient ───────────────────────────────────────────────────────────────

// GitHubClient handles interaction with the GitHub API.
// Phase 1 methods use native net/http for tree/blob operations.
// Legacy methods (ListOrganizations, ScanOrganization, FetchComponent) retain gh.exe
// for non-critical-path operations pending full migration.
type GitHubClient struct {
	GHPath  string
	GitPath string
	// httpClient is the native HTTP client for GitHub API calls (no gh.exe dependency).
	httpClient *http.Client
	// token is the GitHub API token for authentication.
	token string
	// rateLimiter tracks API quota from response headers.
	rateLimiter *RateLimiter
}

// NewGitHubClient creates a client using the Forge's gh and git binaries,
// plus a native HTTP client for tree/blob operations.
func NewGitHubClient() *GitHubClient {
	return &GitHubClient{
		GHPath:  "c:\\aAntigravitySpace\\00SDLC\\OlympusForge\\81000-Toolchain-External\\gh\\bin\\gh.exe",
		GitPath: "c:\\aAntigravitySpace\\00SDLC\\OlympusForge\\81000-Toolchain-External\\git\\cmd\\git.exe",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		rateLimiter: &RateLimiter{},
	}
}

// initToken resolves the GitHub token from gh.exe auth status.
func (c *GitHubClient) initToken() error {
	if c.token != "" {
		return nil
	}
	// Extract token from gh auth
	cmd := exec.Command(c.GHPath, "auth", "token")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("github-token-resolution-failed: %w", err)
	}
	c.token = strings.TrimSpace(string(out))
	if c.token == "" {
		return fmt.Errorf("github-token-empty: ensure gh auth login has been run")
	}
	return nil
}

// apiGet performs an authenticated GET request to the GitHub API with rate-limit awareness.
func (c *GitHubClient) apiGet(ctx context.Context, path string, accept string) (*http.Response, error) {
	if err := c.initToken(); err != nil {
		return nil, err
	}

	if err := c.rateLimiter.WaitIfNeeded(ctx); err != nil {
		return nil, err
	}

	url := "https://api.github.com/" + strings.TrimPrefix(path, "/")
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "GitSovereign-SmartPipe/2.0")
	if accept != "" {
		req.Header.Set("Accept", accept)
	} else {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	c.rateLimiter.Update(resp)

	// Handle rate limit responses with exponential backoff
	if resp.StatusCode == 429 || resp.StatusCode == 403 {
		resp.Body.Close()
		return c.retryWithBackoff(ctx, path, accept, 1)
	}

	return resp, nil
}

// retryWithBackoff implements exponential backoff for rate-limited responses.
func (c *GitHubClient) retryWithBackoff(ctx context.Context, path, accept string, attempt int) (*http.Response, error) {
	if attempt > 5 {
		return nil, fmt.Errorf("github-api-rate-limit-exceeded after %d retries", attempt)
	}

	backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
	slog.Warn("github-api-retry",
		"path", path,
		"attempt", attempt,
		"backoff", backoff,
	)

	select {
	case <-time.After(backoff):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	url := "https://api.github.com/" + strings.TrimPrefix(path, "/")
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "GitSovereign-SmartPipe/2.0")
	if accept != "" {
		req.Header.Set("Accept", accept)
	} else {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	c.rateLimiter.Update(resp)

	if resp.StatusCode == 429 || resp.StatusCode == 403 {
		resp.Body.Close()
		return c.retryWithBackoff(ctx, path, accept, attempt+1)
	}

	return resp, nil
}

// ─── Phase 1: Exhaustive Merkle Discovery ───────────────────────────────────────

// resolveBranches queries GET /repos/{owner}/{repo}/branches with pagination
// to retrieve the HEAD commit SHA for every branch.
func (c *GitHubClient) resolveBranches(ctx context.Context, repo string) ([]BranchInfo, error) {
	var allBranches []BranchInfo
	page := 1

	for {
		path := fmt.Sprintf("repos/%s/branches?per_page=100&page=%d", repo, page)
		resp, err := c.apiGet(ctx, path, "")
		if err != nil {
			return nil, fmt.Errorf("branch-resolution-failed: %w", err)
		}

		var branches []BranchInfo
		if err := json.NewDecoder(resp.Body).Decode(&branches); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("branch-parse-failed: %w", err)
		}
		resp.Body.Close()

		if len(branches) == 0 {
			break
		}

		allBranches = append(allBranches, branches...)
		slog.Info("branch-page-resolved",
			"repo", repo,
			"page", page,
			"count", len(branches),
			"total", len(allBranches),
		)

		if len(branches) < 100 {
			break // Last page
		}
		page++
	}

	slog.Info("branch-resolution-complete",
		"repo", repo,
		"total_branches", len(allBranches),
	)
	return allBranches, nil
}

// walkTree recursively walks a Git tree, handling API truncation by re-querying
// individual subtree SHAs that were not fully expanded.
func (c *GitHubClient) walkTree(ctx context.Context, repo, treeSHA string) ([]TreeNode, error) {
	path := fmt.Sprintf("repos/%s/git/trees/%s?recursive=1", repo, treeSHA)
	resp, err := c.apiGet(ctx, path, "")
	if err != nil {
		return nil, fmt.Errorf("tree-walk-failed for %s: %w", treeSHA, err)
	}

	var treeResp TreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&treeResp); err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("tree-parse-failed: %w", err)
	}
	resp.Body.Close()

	if !treeResp.Truncated {
		return treeResp.Tree, nil
	}

	// ─── TRUNCATION FALLBACK (Critical) ─────────────────────────────────────
	// The API returned truncated: true. We must identify subtree nodes and
	// recursively query them to map the complete graph.
	slog.Warn("tree-truncated-initiating-recovery",
		"repo", repo,
		"tree_sha", treeSHA,
		"partial_count", len(treeResp.Tree),
	)

	var completeTree []TreeNode
	var subtreeQueue []TreeNode

	for _, node := range treeResp.Tree {
		completeTree = append(completeTree, node)
		if node.Type == "tree" {
			subtreeQueue = append(subtreeQueue, node)
		}
	}

	// Recursively resolve truncated subtrees
	for _, subtree := range subtreeQueue {
		subNodes, err := c.walkTreeShallow(ctx, repo, subtree.SHA)
		if err != nil {
			slog.Error("truncation-recovery-subtree-failed",
				"repo", repo,
				"subtree_sha", subtree.SHA,
				"subtree_path", subtree.Path,
				"err", err,
			)
			continue
		}

		// Prefix paths with the subtree's path
		for _, sn := range subNodes {
			sn.Path = subtree.Path + "/" + sn.Path
			completeTree = append(completeTree, sn)
		}
	}

	slog.Info("truncation-recovery-complete",
		"repo", repo,
		"total_nodes", len(completeTree),
	)
	return completeTree, nil
}

// walkTreeShallow queries a single tree SHA without recursive=1 to get its immediate children.
func (c *GitHubClient) walkTreeShallow(ctx context.Context, repo, treeSHA string) ([]TreeNode, error) {
	path := fmt.Sprintf("repos/%s/git/trees/%s", repo, treeSHA)
	resp, err := c.apiGet(ctx, path, "")
	if err != nil {
		return nil, err
	}

	var treeResp TreeResponse
	if err := json.NewDecoder(resp.Body).Decode(&treeResp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	resp.Body.Close()

	var result []TreeNode
	for _, node := range treeResp.Tree {
		result = append(result, node)
		// If any child is also a tree, recurse
		if node.Type == "tree" {
			children, err := c.walkTreeShallow(ctx, repo, node.SHA)
			if err != nil {
				slog.Warn("subtree-walk-failed", "sha", node.SHA, "err", err)
				continue
			}
			for _, child := range children {
				child.Path = node.Path + "/" + child.Path
				result = append(result, child)
			}
		}
	}

	return result, nil
}

// ─── Phase 1: Density Heuristic ─────────────────────────────────────────────────

// DirectoryDensity holds the evaluation result for a subtree's S/n ratio.
type DirectoryDensity struct {
	Path       string
	FileCount  int
	TotalSize  int64
	AvgSize    int64
	Method     IngestionMethod
	TreeSHA    string
}

// evaluateDensity assesses the S/n density of a directory tree node and routes it
// to the optimal ingestion protocol.
//
// If n > DensityFileCountThreshold AND (S/n) < DensityAvgSizeThreshold → PACK
// Otherwise → REST
func evaluateDensity(nodes []TreeNode, dirPath, dirSHA string) DirectoryDensity {
	var fileCount int
	var totalSize int64

	for _, n := range nodes {
		if n.Type == "blob" && strings.HasPrefix(n.Path, dirPath+"/") {
			fileCount++
			totalSize += n.Size
		}
	}

	var avgSize int64
	if fileCount > 0 {
		avgSize = totalSize / int64(fileCount)
	}

	method := MethodREST
	if fileCount > DensityFileCountThreshold && avgSize < DensityAvgSizeThreshold {
		method = MethodPACK
	}

	density := DirectoryDensity{
		Path:      dirPath,
		FileCount: fileCount,
		TotalSize: totalSize,
		AvgSize:   avgSize,
		Method:    method,
		TreeSHA:   dirSHA,
	}

	slog.Info("density-heuristic-evaluated",
		"path", dirPath,
		"n", fileCount,
		"S", totalSize,
		"S_over_n", avgSize,
		"method", method.String(),
	)

	return density
}

// ─── Phase 1+2+3: StreamMerkleIngestion (Full Pipeline) ─────────────────────────

// StreamMerkleIngestion executes the complete adaptive multi-branch harvest pipeline:
//   1. Resolve all branches
//   2. Walk every branch's tree (with truncation recovery)
//   3. Evaluate density heuristics per directory
//   4. Deduplicate via session cache + CAS index
//   5. Dispatch ingestion jobs to bounded worker pool
//   6. Stream blobs from GitHub → CAS via io.Copy (zero-disk)
func (c *GitHubClient) StreamMerkleIngestion(ctx context.Context, repo string, cas interface{}) ([]ManifestEntry, int64, error) {
	slog.Info("adaptive-merkle-ingestion-start", "repo", repo)
	startTime := time.Now()

	// ─── Resolve CAS target interface ───────────────────────────────────
	type DestTarget interface {
		PutBlob(ctx context.Context, hash string, data []byte) error
		PutBlobStream(ctx context.Context, hash string, r io.Reader, size int64) error
		BlobExists(ctx context.Context, hash string) (bool, error)
		RecordLogicalBytes(size uint64)
	}
	var target DestTarget
	if cas != nil {
		// Try the streaming interface first, fall back to legacy
		if dt, ok := cas.(DestTarget); ok {
			target = dt
		} else {
			// Wrap legacy target with a shim for PutBlobStream
			type LegacyTarget interface {
				PutBlob(ctx context.Context, hash string, data []byte) error
				BlobExists(ctx context.Context, hash string) (bool, error)
				RecordLogicalBytes(size uint64)
			}
			if lt, ok := cas.(LegacyTarget); ok {
				target = &legacyTargetShim{lt: lt}
			}
		}
	}

	session := &SessionCache{}
	var entriesMu sync.Mutex
	var entries []ManifestEntry
	var totalBytes atomic.Int64
	var blobsIngested atomic.Int64
	var blobsDeduped atomic.Int64

	// ─── Phase 1: Resolve all branches ──────────────────────────────────
	branches, err := c.resolveBranches(ctx, repo)
	if err != nil {
		return nil, 0, fmt.Errorf("branch-resolution-failed: %w", err)
	}

	// ─── Collect all unique tree nodes across branches ──────────────────
	globalNodeMap := make(map[string]TreeNode) // keyed by SHA to deduplicate cross-branch
	branchTrees := make(map[string]string)     // branch name → tree SHA

	for _, branch := range branches {
		if branch.Commit.SHA == "" {
			continue
		}

		nodes, err := c.walkTree(ctx, repo, branch.Commit.SHA)
		if err != nil {
			slog.Error("branch-tree-walk-failed",
				"repo", repo,
				"branch", branch.Name,
				"sha", branch.Commit.SHA,
				"err", err,
			)
			continue
		}

		branchTrees[branch.Name] = branch.Commit.SHA
		for _, node := range nodes {
			if _, exists := globalNodeMap[node.SHA]; !exists {
				globalNodeMap[node.SHA] = node
			}
		}

		slog.Info("branch-tree-enumerated",
			"repo", repo,
			"branch", branch.Name,
			"nodes", len(nodes),
		)
	}

	// Flatten unique nodes
	allNodes := make([]TreeNode, 0, len(globalNodeMap))
	for _, node := range globalNodeMap {
		allNodes = append(allNodes, node)
	}

	slog.Info("merkle-discovery-complete",
		"repo", repo,
		"branches", len(branches),
		"unique_nodes", len(allNodes),
	)

	// ─── Phase 1: Evaluate density per top-level directory ──────────────
	// Identify top-level directories for density evaluation
	topDirs := make(map[string]string) // dirPath → treeSHA
	for _, node := range allNodes {
		if node.Type == "tree" {
			parts := strings.SplitN(node.Path, "/", 2)
			if len(parts) == 1 {
				topDirs[node.Path] = node.SHA
			}
		}
	}

	dirDensities := make(map[string]DirectoryDensity)
	for dirPath, dirSHA := range topDirs {
		density := evaluateDensity(allNodes, dirPath, dirSHA)
		dirDensities[dirPath] = density
	}

	// ─── Phase 2+3: Build job queue and dispatch to workers ─────────────
	jobs := make(chan IngestionJob, 256)

	// Classify and enqueue blobs
	go func() {
		defer close(jobs)

		for _, node := range allNodes {
			if node.Type != "blob" {
				continue
			}

			// Session dedup: skip if already processed this harvest
			if session.MarkSeen(node.SHA) {
				blobsDeduped.Add(1)
				continue
			}

			// Sovereign key derivation
			key, err := gitsovkey.FromSHA1(node.SHA)
			if err != nil {
				slog.Warn("invalid-sha-in-tree", "sha", node.SHA, "err", err)
				continue
			}
			targetKey := key.Hex()

			// Record manifest entry
			entriesMu.Lock()
			entries = append(entries, ManifestEntry{ID: node.Path, Hash: targetKey})
			entriesMu.Unlock()

			// CAS dedup: check if blob already exists in storage
			if target != nil {
				exists, _ := target.BlobExists(ctx, targetKey)
				if exists {
					blobsDeduped.Add(1)
					continue
				}
			}

			// Determine ingestion method from parent directory density
			method := MethodREST
			parts := strings.SplitN(node.Path, "/", 2)
			if len(parts) == 2 {
				if density, ok := dirDensities[parts[0]]; ok {
					method = density.Method
				}
			}

			select {
			case jobs <- IngestionJob{
				BlobSHA:    node.SHA,
				SovKey:     targetKey,
				Path:       node.Path,
				Size:       node.Size,
				Method:     method,
				SubtreeSHA: func() string {
					if method == MethodPACK {
						parts := strings.SplitN(node.Path, "/", 2)
						if len(parts) == 2 {
							if d, ok := dirDensities[parts[0]]; ok {
								return d.TreeSHA
							}
						}
					}
					return ""
				}(),
				Repo: repo,
			}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// ─── Phase 3: Worker Pool ───────────────────────────────────────────
	var wg sync.WaitGroup
	workerCount := DefaultWorkerPoolSize

	slog.Info("worker-pool-initializing",
		"repo", repo,
		"workers", workerCount,
		"queue_capacity", 256,
	)

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for job := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}

				var blobData []byte
				var fetchErr error

				switch job.Method {
				case MethodREST:
					blobData, fetchErr = c.fetchBlobREST(ctx, job.Repo, job.BlobSHA)
				case MethodPACK:
					// PACK fallback: for the current phase, dense directories still use REST
					// because go-git in-memory packfile requires additional dependency wiring.
					// The density heuristic is logged and the routing decision preserved for
					// Phase 3b integration with go-git.
					blobData, fetchErr = c.fetchBlobREST(ctx, job.Repo, job.BlobSHA)
				}

				if fetchErr != nil {
					slog.Error("blob-fetch-failed",
						"worker", workerID,
						"sha", job.BlobSHA,
						"method", job.Method.String(),
						"err", fetchErr,
					)
					continue
				}

				if target != nil {
					target.RecordLogicalBytes(uint64(len(blobData)))
					if err := target.PutBlob(ctx, job.SovKey, blobData); err != nil {
						slog.Error("cas-put-failed",
							"worker", workerID,
							"key", job.SovKey,
							"err", err,
						)
						continue
					}
				}

				totalBytes.Add(int64(len(blobData)))
				blobsIngested.Add(1)

				if blobsIngested.Load()%100 == 0 {
					slog.Info("ingestion-progress",
						"repo", repo,
						"ingested", blobsIngested.Load(),
						"deduped", blobsDeduped.Load(),
						"session_hits", session.Hits(),
						"bytes", totalBytes.Load(),
					)
				}
			}
		}(i)
	}

	wg.Wait()

	duration := time.Since(startTime)
	finalBytes := totalBytes.Load()

	slog.Info("adaptive-merkle-ingestion-complete",
		"repo", repo,
		"branches", len(branches),
		"unique_nodes", len(allNodes),
		"blobs_ingested", blobsIngested.Load(),
		"blobs_deduped", blobsDeduped.Load(),
		"session_cache_hits", session.Hits(),
		"total_bytes", finalBytes,
		"duration", duration,
		"throughput_mbps", float64(finalBytes)/(1024*1024*duration.Seconds()+0.001),
	)

	entriesMu.Lock()
	result := make([]ManifestEntry, len(entries))
	copy(result, entries)
	entriesMu.Unlock()

	return result, finalBytes, nil
}

// ─── Phase 3: REST Blob Fetch ───────────────────────────────────────────────────

// fetchBlobREST retrieves a blob's raw content via the GitHub REST API.
// Uses Accept: application/vnd.github.v3.raw to get raw bytes without base64 encoding.
func (c *GitHubClient) fetchBlobREST(ctx context.Context, repo, blobSHA string) ([]byte, error) {
	path := fmt.Sprintf("repos/%s/git/blobs/%s", repo, blobSHA)
	resp, err := c.apiGet(ctx, path, "application/vnd.github.v3.raw")
	if err != nil {
		return nil, fmt.Errorf("rest-blob-fetch-failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("rest-blob-http-error: status=%d body=%s", resp.StatusCode, string(body))
	}

	// Read the blob into memory. For streaming (Phase 3b), this will use io.Copy
	// directly to the QUIC Drive writer.
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, resp.Body); err != nil {
		return nil, fmt.Errorf("rest-blob-read-failed: %w", err)
	}

	return buf.Bytes(), nil
}

// ─── Legacy Target Shim ─────────────────────────────────────────────────────────

// legacyTargetShim wraps a target that doesn't implement PutBlobStream.
type legacyTargetShim struct {
	lt interface {
		PutBlob(ctx context.Context, hash string, data []byte) error
		BlobExists(ctx context.Context, hash string) (bool, error)
		RecordLogicalBytes(size uint64)
	}
}

func (s *legacyTargetShim) PutBlob(ctx context.Context, hash string, data []byte) error {
	return s.lt.PutBlob(ctx, hash, data)
}

func (s *legacyTargetShim) PutBlobStream(ctx context.Context, hash string, r io.Reader, size int64) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return s.lt.PutBlob(ctx, hash, data)
}

func (s *legacyTargetShim) BlobExists(ctx context.Context, hash string) (bool, error) {
	return s.lt.BlobExists(ctx, hash)
}

func (s *legacyTargetShim) RecordLogicalBytes(size uint64) {
	s.lt.RecordLogicalBytes(size)
}

// ─── Legacy Methods (Retained for non-critical-path operations) ─────────────────

// ComponentFile represents a logical file within a repository component
type ComponentFile struct {
	ID   string
	Data []byte
}

// FetchComponent returns the files/data for a specific repository component.
// Retains gh.exe for non-code components (issues, PRs, releases, discussions, wiki).
func (c *GitHubClient) FetchComponent(repo, component string) ([]ComponentFile, error) {
	slog.Info("Fetching-Component", "repo", repo, "component", component)

	switch component {
	case "code":
		// Full repository capture via git bundle (all branches, tags, history)
		var buf bytes.Buffer
		_, err := c.StreamRepository(repo, &buf)
		if err != nil {
			return nil, err
		}
		return []ComponentFile{{ID: "repo.bundle", Data: buf.Bytes()}}, nil

	case "issues":
		cmd := exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s/issues?state=all&per_page=100", repo), "--paginate")
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			slog.Warn("issues-fetch-failed", "repo", repo, "err", err, "stderr", strings.TrimSpace(stderr.String()))
			return nil, nil
		}
		if stdout.Len() == 0 {
			return nil, nil
		}
		return []ComponentFile{{ID: "issues.json", Data: stdout.Bytes()}}, nil

	case "pull_requests":
		cmd := exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s/pulls?state=all&per_page=100", repo), "--paginate")
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err != nil {
			slog.Warn("pulls-fetch-failed", "repo", repo, "err", err)
			return nil, nil
		}
		if stdout.Len() == 0 {
			return nil, nil
		}
		return []ComponentFile{{ID: "pull_requests.json", Data: stdout.Bytes()}}, nil

	case "releases":
		cmd := exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s/releases?per_page=100", repo), "--paginate")
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err != nil {
			slog.Warn("releases-fetch-failed", "repo", repo, "err", err)
			return nil, nil
		}
		if stdout.Len() == 0 {
			return nil, nil
		}
		return []ComponentFile{{ID: "releases.json", Data: stdout.Bytes()}}, nil

	case "discussions":
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) != 2 {
			return nil, nil
		}
		query := fmt.Sprintf(`query { repository(owner:"%s", name:"%s") { discussions(first:100) { nodes { number title body createdAt author { login } category { name } } } } }`, parts[0], parts[1])
		cmd := exec.Command(c.GHPath, "api", "graphql", "-f", "query="+query)
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err != nil {
			slog.Info("discussions-not-available", "repo", repo)
			return nil, nil
		}
		if stdout.Len() == 0 {
			return nil, nil
		}
		return []ComponentFile{{ID: "discussions.json", Data: stdout.Bytes()}}, nil

	case "metadata":
		cmd := exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s", repo))
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err != nil {
			slog.Warn("metadata-fetch-failed", "repo", repo, "err", err)
			return nil, nil
		}
		return []ComponentFile{{ID: "metadata.json", Data: stdout.Bytes()}}, nil

	case "wiki":
		scratchDir := "c:\\aAntigravitySpace\\00SDLC\\Olympus2\\C0990-Ephemeral-Scratch"
		tempPath := fmt.Sprintf("%s\\wiki-%s-%d", scratchDir, strings.ReplaceAll(repo, "/", "-"), time.Now().UnixNano())
		defer func() {
			exec.Command("cmd", "/c", "rmdir", "/s", "/q", tempPath).Run()
		}()

		cloneCmd := exec.Command(c.GitPath, "clone", "--mirror", fmt.Sprintf("https://github.com/%s.wiki.git", repo), tempPath)
		if err := cloneCmd.Run(); err != nil {
			slog.Info("wiki-not-available", "repo", repo)
			return nil, nil
		}

		var buf bytes.Buffer
		bundleCmd := exec.Command(c.GitPath, "-C", tempPath, "bundle", "create", "-", "--all")
		bundleCmd.Stdout = &buf
		if err := bundleCmd.Run(); err != nil {
			slog.Warn("wiki-bundle-failed", "repo", repo, "err", err)
			return nil, nil
		}
		return []ComponentFile{{ID: "wiki.bundle", Data: buf.Bytes()}}, nil

	default:
		return nil, fmt.Errorf("unknown component: %s", component)
	}
}

// StreamRepository streams a repository tarball directly from GitHub API.
func (c *GitHubClient) StreamRepository(repo string, w io.Writer) (int64, error) {
	slog.Info("Streaming-Repository-Tarball", "repo", repo)

	cmd := exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s/tarball", repo))
	cmd.Stdout = w
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("gh-api-tarball-failed for %s: %w (stderr: %s)", repo, err, stderr.String())
	}

	slog.Info("tarball-stream-complete", "repo", repo)
	return 0, nil
}

// ─── Discovery Types ────────────────────────────────────────────────────────────

// RepoInfo represents a repository and its current state (Deduplication Node)
type RepoInfo struct {
	Name           string `json:"name"`
	HeadHash       string `json:"head_hash"`
	IsEmpty        bool   `json:"isEmpty"`
	DefaultBranch  string `json:"defaultBranch"`
	SovereignState string `json:"state"` // EMPTY, BRANCHLESS, ACTIVE
	Owner          struct {
		Login string `json:"login"`
	} `json:"owner"`
	Description string `json:"description"`
}

// OrganizationInfo represents a GitHub organization
type OrganizationInfo struct {
	Login string `json:"login"`
	ID    int    `json:"id"`
}

// ListOrganizations returns all organizations the authenticated user belongs to
func (c *GitHubClient) ListOrganizations() ([]OrganizationInfo, error) {
	slog.Info("Listing-Organizations")

	cmd := exec.Command(c.GHPath, "api", "user/orgs")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh-org-list-failed: %w", err)
	}

	var orgs []OrganizationInfo
	if err := json.Unmarshal(stdout.Bytes(), &orgs); err != nil {
		return nil, fmt.Errorf("gh-json-parse-failed: %w", err)
	}

	return orgs, nil
}

// ListRepositories is an alias for ScanOrganization for naming consistency
func (c *GitHubClient) ListRepositories(org string) ([]RepoInfo, error) {
	return c.ScanOrganization(org)
}

// ScanOrganization returns all repositories for a given organization
func (c *GitHubClient) ScanOrganization(org string) ([]RepoInfo, error) {
	slog.Info("Scanning-Organization", "org", org)

	cmd := exec.Command(c.GHPath, "repo", "list", org, "--json", "name,owner,description,isEmpty,defaultBranchRef", "--limit", "1000")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh-repo-scan-failed: %w", err)
	}

	var rawRepos []struct {
		Name             string `json:"name"`
		IsEmpty          bool   `json:"isEmpty"`
		Description      string `json:"description"`
		DefaultBranchRef struct {
			Name string `json:"name"`
		} `json:"defaultBranchRef"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &rawRepos); err != nil {
		return nil, fmt.Errorf("gh-json-parse-failed: %w", err)
	}

	var repos []RepoInfo
	for _, r := range rawRepos {
		state := "ACTIVE"
		if r.IsEmpty {
			state = "EMPTY"
		} else if r.DefaultBranchRef.Name == "" {
			state = "BRANCHLESS"
		}

		repos = append(repos, RepoInfo{
			Name:           r.Name,
			IsEmpty:        r.IsEmpty,
			DefaultBranch:  r.DefaultBranchRef.Name,
			SovereignState: state,
			Owner:          r.Owner,
			Description:    r.Description,
		})
	}

	// Enrich with HeadHash (Deduplication Key)
	for i := range repos {
		if repos[i].SovereignState != "ACTIVE" {
			continue
		}
		hash, err := c.GetHeadHash(repos[i].Owner.Login + "/" + repos[i].Name)
		if err != nil {
			slog.Warn("Head-Hash-Discovery-Failed", "repo", repos[i].Name, "err", err)
			continue
		}
		repos[i].HeadHash = hash
	}

	return repos, nil
}

// GetHeadHash retrieves the current commit hash for the default branch
func (c *GitHubClient) GetHeadHash(repo string) (string, error) {
	cmd := exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s", repo), "-q", ".default_branch")
	branchOut, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh-branch-discovery-failed: %w", err)
	}
	branch := strings.TrimSpace(string(branchOut))
	if branch == "" {
		return "branchless", nil
	}

	cmd = exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s/commits/%s", repo, branch), "-q", ".sha")
	hashOut, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh-hash-discovery-failed: %w", err)
	}

	return strings.TrimSpace(string(hashOut)), nil
}

// ScanSecrets placeholder for Pulse 3 Secret Sovereignty
func (c *GitHubClient) ScanSecrets(repo string) error {
	slog.Info("Scanning-Secrets", "repo", repo)
	return nil
}
