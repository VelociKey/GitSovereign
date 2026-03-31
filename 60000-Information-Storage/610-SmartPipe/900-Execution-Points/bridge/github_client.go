package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

type GitHubClient struct {
	GHPath     string
	Secretless bool
}

func NewGitHubClient() *GitHubClient {
	root := "c:\\aAntigravitySpace"
	if envRoot := os.Getenv("ANTIGRAVITY_ROOT"); envRoot != "" {
		root = envRoot
	}
	return &GitHubClient{
		GHPath:     filepath.Join(root, "00SDLC", "OlympusForge", "81000-Toolchain-External", "gh", "bin", "gh.exe"),
		Secretless: true,
	}
}

func (c *GitHubClient) DiscoverOrgRepositories(ctx context.Context, orgName string) ([]string, error) {
	slog.Info("github-discovery-real", "org", orgName)

	cmd := exec.Command(c.GHPath, "repo", "list", orgName, "--json", "name,owner", "--limit", "1000")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh-repo-list-failed for %s: %w", orgName, err)
	}

	var repos []struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(out, &repos); err != nil {
		return nil, fmt.Errorf("json-parse-failed: %w", err)
	}

	var result []string
	for _, r := range repos {
		result = append(result, r.Owner.Login+"/"+r.Name)
	}
	slog.Info("github-discovery-complete", "org", orgName, "count", len(result))
	return result, nil
}

