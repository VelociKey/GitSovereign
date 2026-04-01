package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"
)

// MockRoundTripper for intercepting HTTP calls
type MockRoundTripper struct {
	responses map[string]*http.Response
}

func (m *MockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if resp, ok := m.responses[req.URL.RequestURI()]; ok {
		return resp, nil
	}
	return &http.Response{
		StatusCode: 404,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString("Not Found")),
	}, nil
}

// MockStorage for storage verification
type MockStorage struct {
	blobs    map[string][]byte
	LastSync time.Time
}

func (m *MockStorage) PutBlob(ctx context.Context, hash string, data []byte) error {
	if m.blobs == nil {
		m.blobs = make(map[string][]byte)
	}
	m.blobs[hash] = data
	return nil
}

func (m *MockStorage) PutBlobStream(ctx context.Context, hash string, r io.Reader, size int64) error {
	if m.blobs == nil {
		m.blobs = make(map[string][]byte)
	}
	data, _ := io.ReadAll(r)
	m.blobs[hash] = data
	return nil
}

func (m *MockStorage) BlobExists(ctx context.Context, hash string) (bool, error) {
	if m.blobs == nil {
		return false, nil
	}
	_, ok := m.blobs[hash]
	return ok, nil
}

func (m *MockStorage) RecordLogicalBytes(size uint64) {}
func (m *MockStorage) RecordNotary(ctx context.Context, record gitsovnotary.NotaryRecord) error {
	return nil
}
func (m *MockStorage) GetMetrics() map[string]interface{} { return nil }

func (m *MockStorage) UpdateRootManifest(ctx context.Context, orgs []string) error { return nil }
func (m *MockStorage) UpdateOrgManifest(ctx context.Context, org string, repos []string) error { return nil }
func (m *MockStorage) UpdateRepoManifest(ctx context.Context, org, repo, state string, categories map[string][]ManifestEntry) error { return nil }
func (m *MockStorage) GetManifestMetadata(ctx context.Context, org, repo string) (time.Time, error) {
	if !m.LastSync.IsZero() {
		return m.LastSync, nil
	}
	return time.Time{}, nil
}
me{}, nil
}
