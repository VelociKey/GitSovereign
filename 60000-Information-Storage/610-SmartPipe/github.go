package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
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
		// Streaming zipball logic
		var buf bytes.Buffer
		_, err := c.StreamRepository(repo, &buf)
		if err != nil {
			return nil, err
		}
		return []ComponentFile{{ID: "archive.zip", Data: buf.Bytes()}}, nil

	case "issues":
		// Simulate several issue exports
		return []ComponentFile{
			{ID: "issue_1", Data: []byte(`{"title": "Bug: SmartPipe timeout", "body": "Details about the timeout..."}`)},
			{ID: "issue_2", Data: []byte(`{"title": "Feature: Merkle tree", "body": "Implementation plan for Merkle..."}`)},
		}, nil

	case "wiki":
		// Simulate several wiki pages
		return []ComponentFile{
			{ID: "Home.md", Data: []byte("# Welcome to GitSovereign Wiki\nThis is the root page.")},
			{ID: "Architecture.md", Data: []byte("# Architecture\nThree-tier semantic tree details.")},
		}, nil

	default:
		return nil, fmt.Errorf("unknown component: %s", component)
	}
}

// StreamRepository pipes a real zipball of the repository directly to the provided writer
func (c *GitHubClient) StreamRepository(repo string, w io.Writer) (int64, error) {
	slog.Info("Streaming-Repository-Real", "repo", repo)

	// 1. Attempt to get default branch
	cmd := exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s", repo), "-q", ".default_branch")
	branchOut, err := cmd.Output()
	branch := strings.TrimSpace(string(branchOut))

	// 2. Normal path with discovered branch
	if err == nil && branch != "" {
		cmd = exec.Command(c.GHPath, "api", fmt.Sprintf("repos/%s/zipball/%s", repo, branch))
		var stderr bytes.Buffer
		cmd.Stdout = w
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			return 0, nil
		}
		slog.Warn("gh-zipball-failed-trying-fallback", "repo", repo, "stderr", strings.TrimSpace(stderr.String()))
	}

	// 3. Fallback strategy for organizations with SAML or discovery issues (using git clone)
	slog.Info("executing-git-bridge-fallback", "repo", repo)
	
	// Create temp dir in @SCRATCH
	scratchDir := "c:\\aAntigravitySpace\\00SDLC\\Olympus2\\C0990-Ephemeral-Scratch"
	tempPath := fmt.Sprintf("%s\\git-bridge-%s-%d", scratchDir, strings.ReplaceAll(repo, "/", "-"), time.Now().UnixNano())
	
	// Ensure cleanup
	defer os.RemoveAll(tempPath)

	// Command: git clone --mirror https://github.com/<repo>.git <tempPath>
	cloneCmd := exec.Command(c.GitPath, "clone", "--mirror", fmt.Sprintf("https://github.com/%s.git", repo), tempPath)
	if err := cloneCmd.Run(); err != nil {
		return 0, fmt.Errorf("git-bridge-clone-failed: %w", err)
	}

	// Stream as archive
	archiveCmd := exec.Command(c.GitPath, "-C", tempPath, "archive", "--format=zip", "HEAD")
	archiveCmd.Stdout = w
	if err := archiveCmd.Run(); err != nil {
		return 0, fmt.Errorf("git-bridge-archive-failed: %w", err)
	}

	slog.Info("git-bridge-fallback-success", "repo", repo)
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
