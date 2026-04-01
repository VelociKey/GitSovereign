package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RateLimitSafetyBuffer: stop REST calls when remaining quota drops below this.
const RateLimitSafetyBuffer = 50

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
