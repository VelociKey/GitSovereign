package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"log/slog"
)

// GitHubClient handles interaction with the GitHub API via the 'gh' toolchain
type GitHubClient struct {
	GHPath string
}

// RepoInfo represents a repository and its current state (Deduplication Node)
type RepoInfo struct {
	Name        string `json:"name"`
	HeadHash    string `json:"head_hash"`
	Owner       string `json:"owner"`
	Description string `json:"description"`
}

// NewGitHubClient creates a client using the Forge's gh binary
func NewGitHubClient() *GitHubClient {
	return &GitHubClient{
		GHPath: "c:\\aAntigravitySpace\\00SDLC\\OlympusForge\\81000-Toolchain-External\\gh\\bin\\gh.exe",
	}
}

// ScanOrganization returns all repositories for a given organization (Parallel-First Discovery)
func (c *GitHubClient) ScanOrganization(org string) ([]RepoInfo, error) {
	slog.Info("Scanning-Organization", "org", org)
	
	// Execute gh repo list with JSON output
	// Command: gh repo list <org> --json name,owner,description --limit 1000
	cmd := exec.Command(c.GHPath, "repo", "list", org, "--json", "name,owner,description", "--limit", "1000")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh-repo-scan-failed: %w", err)
	}
	
	var repos []RepoInfo
	if err := json.Unmarshal(stdout.Bytes(), &repos); err != nil {
		return nil, fmt.Errorf("gh-json-parse-failed: %w", err)
	}
	
	// Enrich with HeadHash (Deduplication Key)
	// In production, this would be a parallelized scan across the repo list
	for i := range repos {
		hash, err := c.GetHeadHash(repos[i].Owner + "/" + repos[i].Name)
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
	// Command: gh api repos/<repo> -q ".default_branch"
	// Then: gh api repos/<repo>/commits/<branch> -q ".sha"
	cmd := exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s", repo), "-q", ".default_branch")
	branchOut, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh-branch-discovery-failed: %w", err)
	}
	branch := strings.TrimSpace(string(branchOut))
	
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
