package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func TestEvaluateDensity(t *testing.T) {
	nodes := []TreeNode{
		{Path: "dir1/file1", Type: "blob", Size: 100},
		{Path: "dir1/file2", Type: "blob", Size: 200},
		{Path: "dir2/file3", Type: "blob", Size: 500},
	}

	// Test case 1: Sparse directory (REST)
	density := evaluateDensity(nodes, "dir1", "sha1")
	if density.Method != MethodREST {
		t.Errorf("Expected MethodREST for sparse directory, got %s", density.Method)
	}

	// Test case 2: Dense directory with many small files (PACK)
	// We need 51+ files for PACK
	denseNodes := []TreeNode{}
	for i := 0; i < 60; i++ {
		denseNodes = append(denseNodes, TreeNode{Path: fmt.Sprintf("dense/file%d", i), Type: "blob", Size: 1000})
	}
	density = evaluateDensity(denseNodes, "dense", "sha2")
	if density.Method != MethodPACK {
		t.Errorf("Expected MethodPACK for dense directory, got %s", density.Method)
	}
}

func TestStreamMerkleIngestion(t *testing.T) {
	ctx := context.Background()
	cas := &MockStorage{blobs: make(map[string][]byte)}

	// Setup GitHubClient with a mock http.Client
	rt := &MockRoundTripper{
		responses: make(map[string]*http.Response),
	}
	client := &GitHubClient{
		httpClient:  &http.Client{Transport: rt},
		token:       "mock-token",
		rateLimiter: &RateLimiter{},
	}

	repo := "org/repo"
	headSHA := "0123456789abcde0123456789abcde0123456789"
	blobSHA := "abcdef0123456789abcdef0123456789abcdef01"

	// Mock branch resolution
	rt.responses[fmt.Sprintf("/repos/%s/branches?per_page=100&page=1", repo)] = &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(fmt.Sprintf(`[{"name":"main","commit":{"sha":"%s"}}]`, headSHA))),
	}
	// Mock branch resolution termination
	rt.responses[fmt.Sprintf("/repos/%s/branches?per_page=100&page=2", repo)] = &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(`[]`)),
	}

	// Mock tree walk (with recursive flag)
	rt.responses[fmt.Sprintf("/repos/%s/git/trees/%s?recursive=1", repo, headSHA)] = &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(fmt.Sprintf(`{"sha":"%s","tree":[{"path":"file1.txt","type":"blob","sha":"%s","size":12}],"truncated":false}`, headSHA, blobSHA))),
	}

	// Mock blob fetch
	rt.responses[fmt.Sprintf("/repos/org/repo/git/blobs/%s", blobSHA)] = &http.Response{
		StatusCode: 200,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString("hello world")),
	}

	entries, bytesProcessed, err := client.StreamMerkleIngestion(ctx, repo, cas)
	if err != nil {
		t.Fatalf("StreamMerkleIngestion failed: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(entries))
	}
	if entries[0].Hash[0:5] != "GS-3:" {
		t.Errorf("Expected BLAKE3 key (GS-3:), got %s", entries[0].Hash)
	}

	if bytesProcessed != 11 { // "hello world" is 11 bytes
		t.Errorf("Expected 11 bytes processed, got %d", bytesProcessed)
	}

	// The hex key produced by gitsovkey.FromSHA1(blobSHA)
	// Since gitsovkey is an external library, we verify the blob exists in CAS
	if len(cas.blobs) != 1 {
		t.Errorf("Expected 1 blob in CAS, got %d", len(cas.blobs))
	}
}
