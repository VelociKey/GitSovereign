package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"olympus.fleet/00SDLC/OlympusLogicLibrary/60000-Information-Storage/90200-Logic-Libraries/110-gitsov-key"
)

// GitHubClient handles interaction with the GitHub API via the 'gh' toolchain
type GitHubClient struct {
	GHPath  string
	GitPath string
}

// ComponentFile represents a logical file within a repository component
type ComponentFile struct {
	ID   string
	Data []byte
}

// FetchComponent returns the files/data for a specific repository component
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
		// Real issue export via gh API (paginated, all states)
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
		// GitHub Discussions require GraphQL
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
		// Full wiki capture via git bundle (preserves edit history)
		scratchDir := "c:\\aAntigravitySpace\\00SDLC\\Olympus2\\C0990-Ephemeral-Scratch"
		tempPath := fmt.Sprintf("%s\\wiki-%s-%d", scratchDir, strings.ReplaceAll(repo, "/", "-"), time.Now().UnixNano())
		defer os.RemoveAll(tempPath)

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

// StreamMerkleIngestion executes a true zero-disk harvest using GitHub's Trees API to stream only missing blobs directly into CAS.
func (c *GitHubClient) StreamMerkleIngestion(ctx context.Context, repo string, cas interface{}) ([]ManifestEntry, int64, error) {
	slog.Info("Starting-Merkle-Directed-Ingestion", "repo", repo)

	// Since cas is passed via reflection/interface from main scope to avoid circular deps
	type DestTarget interface {
		PutBlob(ctx context.Context, hash string, data []byte) error
		BlobExists(ctx context.Context, hash string) (bool, error)
		RecordLogicalBytes(size uint64)
	}
	var target DestTarget
	if cas != nil {
		target = cas.(DestTarget)
	}

	// 1. Get default branch (or HEAD)
	cmd := exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s", repo), "-q", ".default_branch")
	branchOut, err := cmd.Output()
	branch := strings.TrimSpace(string(branchOut))
	if err != nil || branch == "" {
		branch = "HEAD"
	}

	// 2. Query GitHub Trees API recursively
	cmd = exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s/git/trees/%s?recursive=1", repo, branch))
	treeOut, err := cmd.Output()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch merkle tree: %w", err)
	}

	var treeResp struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"` // "blob" or "tree"
			SHA  string `json:"sha"`
			Size int64  `json:"size"`
		} `json:"tree"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(treeOut, &treeResp); err != nil {
		return nil, 0, fmt.Errorf("failed to parse merkle tree: %w", err)
	}

	if treeResp.Truncated {
		slog.Warn("merkle-tree-truncated", "repo", repo)
	}

	var totalBytes int64
	var entries []ManifestEntry

	// 3. Process the flat array of Git objects
	for _, node := range treeResp.Tree {
		if node.Type != "blob" && node.Type != "tree" {
			continue // ignore commits/submodules for now
		}

		// Use the native Sovereign representation
		key, err := gitsovkey.FromSHA1(node.SHA)
		if err != nil {
			slog.Warn("invalid-sha-in-tree", "sha", node.SHA)
			continue
		}

		// The CAS primary key uses the Sovereign Key's exact Hex() representation
		targetKey := key.Hex()
		entries = append(entries, ManifestEntry{ID: node.Path, Hash: targetKey})

		// ZERO-DISK: Check CAS. If we have it, skip!
		if target != nil {
			exists, _ := target.BlobExists(ctx, targetKey)
			if exists {
				continue // deduplication win
			}
		}
		
		if node.Type != "blob" {
			continue // only fetch blobs
		}

		// Fetch the blob only if missing.
		blobCmd := exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s/git/blobs/%s", repo, node.SHA), "-H", "Accept: application/vnd.github.v3.raw")
		blobData, err := blobCmd.Output()
		if err != nil {
			slog.Error("failed-to-fetch-blob", "sha", node.SHA)
			continue
		}

		if target != nil {
			target.RecordLogicalBytes(uint64(len(blobData)))
			err = target.PutBlob(ctx, targetKey, blobData)
			if err != nil {
				slog.Error("failed-to-put-blob", "sha", node.SHA, "err", err)
			} else {
				totalBytes += int64(len(blobData))
			}
		}
	}

	slog.Info("Merkle-Directed-Ingestion-Complete", "repo", repo, "newBytes", totalBytes)
	return entries, totalBytes, nil
}

// StreamRepository streams a repository tarball directly from GitHub API.
// ZERO-DISK MANDATE: No local storage pass-through.
func (c *GitHubClient) StreamRepository(repo string, w io.Writer) (int64, error) {
	slog.Info("Streaming-Repository-Tarball", "repo", repo)

	// Execute gh api repos/:repo/tarball to stream the default branch
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

// NewGitHubClient creates a client using the Forge's gh and git binaries
func NewGitHubClient() *GitHubClient {
	return &GitHubClient{
		GHPath:  "c:\\aAntigravitySpace\\00SDLC\\OlympusForge\\81000-Toolchain-External\\gh\\bin\\gh.exe",
		GitPath: "c:\\aAntigravitySpace\\00SDLC\\OlympusForge\\81000-Toolchain-External\\git\\cmd\\git.exe",
	}
}


// ListOrganizations returns all organizations the authenticated user belongs to
func (c *GitHubClient) ListOrganizations() ([]OrganizationInfo, error) {
	slog.Info("Listing-Organizations")

	// Execute gh api user/orgs
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

// ListRepositories is an alias for ScanOrganization for naming consistency in Pulse 6
func (c *GitHubClient) ListRepositories(org string) ([]RepoInfo, error) {
	return c.ScanOrganization(org)
}

// ScanOrganization returns all repositories for a given organization (Parallel-First Discovery)
func (c *GitHubClient) ScanOrganization(org string) ([]RepoInfo, error) {
	slog.Info("Scanning-Organization", "org", org)
	
	// Execute gh repo list with JSON output
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

// GetHeadHash retrieves the current commit hash for the default branch (Deduplication Anchor)
func (c *GitHubClient) GetHeadHash(repo string) (string, error) {
	// 1. Get default branch
	cmd := exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s", repo), "-q", ".default_branch")
	branchOut, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh-branch-discovery-failed: %w", err)
	}
	branch := strings.TrimSpace(string(branchOut))
	if branch == "" {
		return "branchless", nil
	}

	// 2. Get latest commit hash
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
	// Placeholder: gh secret list --repo <repo>
	return nil
}
