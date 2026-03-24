package main

import (
	"context"
	"io"
	"log/slog"
)

// StorageTarget defines the destination for sovereign exfiltration.
type StorageTarget interface {
	Exists(ctx context.Context, repoID, headHash string) (bool, error)
	Put(ctx context.Context, repoID, headHash string, data io.Reader) error
}

// LocalArchive implements local disk storage for debugging (Pulse 1-2).
type LocalArchive struct {
	RootPath string
}

func (l *LocalArchive) Exists(ctx context.Context, repoID, headHash string) (bool, error) {
	// Simulation for MVP
	return false, nil
}

func (l *LocalArchive) Put(ctx context.Context, repoID, headHash string, data io.Reader) error {
	slog.Info("local-archive-put", "repo", repoID, "hash", headHash)
	return nil
}

// DriveMCP implements Google Drive exfiltration via the MCP server.
type DriveMCP struct{}

func NewDriveMCP() *DriveMCP {
	return &DriveMCP{}
}

func (d *DriveMCP) Exists(ctx context.Context, repoID, headHash string) (bool, error) {
	// Pulse 2: Google MCP server integration logic here
	return false, nil
}

func (d *DriveMCP) Put(ctx context.Context, repoID, headHash string, data io.Reader) error {
	slog.Info("drive-mcp-exfiltration", "repo", repoID, "hash", headHash)
	return nil
}

// GCSProxy implements Google Cloud Storage bucket exfiltration.
type GCSProxy struct {
	BucketName string
}

func (g *GCSProxy) Exists(ctx context.Context, repoID, headHash string) (bool, error) {
	return false, nil
}

func (g *GCSProxy) Put(ctx context.Context, repoID, headHash string, data io.Reader) error {
	slog.Info("gcs-bucket-exfiltration", "bucket", g.BucketName, "repo", repoID)
	return nil
}

// MockStorage is used for testing.
type MockStorage struct {
	LastPutRepo string
}

func (m *MockStorage) Exists(ctx context.Context, repoID, headHash string) (bool, error) {
	return false, nil
}

func (m *MockStorage) Put(ctx context.Context, repoID, headHash string, data io.Reader) error {
	m.LastPutRepo = repoID
	return nil
}
