package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func TestWalkTree_TruncationRecovery(t *testing.T) {
	ctx := context.Background()
	rt := &MockRoundTripper{
		responses: make(map[string]*http.Response),
	}
	client := &GitHubClient{
		httpClient:  &http.Client{Transport: rt},
		token:       "mock-token",
		rateLimiter: &RateLimiter{},
	}

	repo := "org/repo"
	rootSHA := "root-sha"
	subSHA := "sub-sha"

	// 1. Root tree is truncated
	rt.responses[fmt.Sprintf("/repos/%s/git/trees/%s?recursive=1", repo, rootSHA)] = &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body: io.NopCloser(bytes.NewBufferString(fmt.Sprintf(`{
			"sha": "%s",
			"truncated": true,
			"tree": [
				{"path": "file1.txt", "type": "blob", "sha": "f1-sha", "size": 10},
				{"path": "subdir", "type": "tree", "sha": "%s", "size": 0}
			]
		}`, rootSHA, subSHA))),
	}

	// 2. Recovery walk for subdir
	rt.responses[fmt.Sprintf("/repos/%s/git/trees/%s", repo, subSHA)] = &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body: io.NopCloser(bytes.NewBufferString(`{
			"sha": "sub-sha",
			"tree": [
				{"path": "file2.txt", "type": "blob", "sha": "f2-sha", "size": 20}
			]
		}`)),
	}

	nodes, err := client.walkTree(ctx, repo, rootSHA)
	if err != nil {
		t.Fatalf("walkTree failed: %v", err)
	}

	// Should have 3 nodes: file1.txt, subdir, and subdir/file2.txt
	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(nodes))
	}

	found := false
	for _, n := range nodes {
		if n.Path == "subdir/file2.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Recovered node subdir/file2.txt not found in tree")
	}
}

func TestResolveBranches(t *testing.T) {
	ctx := context.Background()
	rt := &MockRoundTripper{
		responses: make(map[string]*http.Response),
	}
	client := &GitHubClient{
		httpClient:  &http.Client{Transport: rt},
		token:       "mock-token",
		rateLimiter: &RateLimiter{},
	}

	repo := "org/repo"

	// Mock pagination: page 1
	rt.responses[fmt.Sprintf("/repos/%s/branches?per_page=100&page=1", repo)] = &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(`[{"name":"b1","commit":{"sha":"s1"}}]`)),
	}
	// Mock pagination: page 2 (empty)
	rt.responses[fmt.Sprintf("/repos/%s/branches?per_page=100&page=2", repo)] = &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(`[]`)),
	}

	branches, err := client.resolveBranches(ctx, repo)
	if err != nil {
		t.Fatalf("resolveBranches failed: %v", err)
	}

	if len(branches) != 1 {
		t.Errorf("Expected 1 branch, got %d", len(branches))
	}
}
