package main

import (
        "bytes"
        "context"
        "encoding/json"
        "fmt"
        "io"
        "log/slog"
        "net/http"
        "os/exec"
        "strings"
)
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
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err := json.Unmarshal(bodyBytes, &branches); err != nil {
		        return nil, fmt.Errorf("branch-parse-failed: %w body=%s", err, string(bodyBytes))
		}


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

	if resp.StatusCode != http.StatusOK {
	        body, _ := io.ReadAll(resp.Body)
	        resp.Body.Close()
	        return nil, fmt.Errorf("tree-walk-http-error: status=%d body=%s", resp.StatusCode, string(body))
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

// ─── Organization & Repository Discovery ────────────────────────────────────────

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

