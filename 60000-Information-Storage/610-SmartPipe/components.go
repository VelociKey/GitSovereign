package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

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
