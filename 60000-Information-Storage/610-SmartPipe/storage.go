package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// ManifestEntry represents a mapping from a logical ID to a CAS hash
type ManifestEntry struct {
	ID   string
	Hash string
}

// StorageTarget defines the destination for sovereign exfiltration.
type StorageTarget interface {
	// Pure CAS Layer
	PutBlob(ctx context.Context, hash string, data []byte) error
	BlobExists(ctx context.Context, hash string) (bool, error)
	RecordLogicalBytes(size uint64)
	GetMetrics() map[string]interface{}

	// Semantic Tree Layer
	UpdateRootManifest(ctx context.Context, orgs []string) error
	UpdateOrgManifest(ctx context.Context, org string, repos []string) error
	UpdateRepoManifest(ctx context.Context, org, repo, state string, categories map[string][]ManifestEntry) error
	GetManifestMetadata(ctx context.Context, org, repo string) (time.Time, error)
}

// GoogleDriveCAS implements the three-tier semantic tree and pure CAS.
type GoogleDriveCAS struct {
	RootID        string
	HashesID      string
	OrgsID        string
	token         string
	LogicalTotal  uint64 // Total bytes processed logically
	PhysicalTotal uint64 // Total bytes actually uploaded to CAS
	mu            sync.Mutex
}

func NewGoogleDriveCAS(rootID string) *GoogleDriveCAS {
	return &GoogleDriveCAS{RootID: rootID}
}

// GetMetrics returns the CAS efficiency metrics
func (g *GoogleDriveCAS) GetMetrics() map[string]interface{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	
	ratio := 0.0
	if g.LogicalTotal > 0 {
		ratio = 1.0 - (float64(g.PhysicalTotal) / float64(g.LogicalTotal))
	}
	
	return map[string]interface{}{
		"logical_bytes":  g.LogicalTotal,
		"physical_bytes": g.PhysicalTotal,
		"cas_hit_ratio":  ratio,
	}
}

func (g *GoogleDriveCAS) init(ctx context.Context) error {
	if g.token != "" && g.HashesID != "" {
		return nil
	}

	out, err := exec.Command("gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return fmt.Errorf("failed to get gcloud token: %w", err)
	}
	g.token = strings.TrimSpace(string(out))

	g.HashesID, _ = g.getOrCreateFolder(ctx, "hashes", g.RootID)
	g.OrgsID, _ = g.getOrCreateFolder(ctx, "Orgs", g.RootID)

	return nil
}

func (g *GoogleDriveCAS) BlobExists(ctx context.Context, hash string) (bool, error) {
	if err := g.init(ctx); err != nil { return false, err }
	query := fmt.Sprintf("name = '%s' and '%s' in parents and trashed = false", hash, g.HashesID)
	url := "https://www.googleapis.com/drive/v3/files?q=" + url.QueryEscape(query)
	
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+g.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil { return false, err }
	defer resp.Body.Close()

	var res struct {
		Files []struct { ID string `json:"id"` } `json:"files"`
	}
	json.NewDecoder(resp.Body).Decode(&res)
	return len(res.Files) > 0, nil
}

func (g *GoogleDriveCAS) PutBlob(ctx context.Context, hash string, data []byte) error {
	if err := g.init(ctx); err != nil { return err }
	slog.Info("cas-put-blob", "hash", hash, "size", len(data))
	
	g.mu.Lock()
	g.PhysicalTotal += uint64(len(data))
	g.mu.Unlock()

	return g.uploadFile(ctx, hash, g.HashesID, "application/octet-stream", bytes.NewReader(data))
}

// RecordLogicalBytes tracks the total size processed
func (g *GoogleDriveCAS) RecordLogicalBytes(size uint64) {
	g.mu.Lock()
	g.LogicalTotal += size
	g.mu.Unlock()
}

func (g *GoogleDriveCAS) UpdateRootManifest(ctx context.Context, orgs []string) error {
	if err := g.init(ctx); err != nil { return err }
	manifest := "::Olympus::Firehorse::RootTree::v1 {\n    Organizations = [\n"
	for _, o := range orgs {
		manifest += fmt.Sprintf("        %q,\n", o)
	}
	manifest += "    ];\n}\n"
	slog.Info("tree-update-root")
	return g.uploadFile(ctx, "root.jebnf", g.OrgsID, "text/plain", strings.NewReader(manifest))
}

func (g *GoogleDriveCAS) UpdateOrgManifest(ctx context.Context, org string, repos []string) error {
	if err := g.init(ctx); err != nil { return err }
	orgID, _ := g.getOrCreateFolder(ctx, org, g.OrgsID)
	manifest := fmt.Sprintf("::Olympus::Firehorse::OrgTree::v1 {\n    Org = %q;\n    Repositories = [\n", org)
	for _, r := range repos {
		manifest += fmt.Sprintf("        %q,\n", r)
	}
	manifest += "    ];\n}\n"
	slog.Info("tree-update-org", "org", org)
	return g.uploadFile(ctx, "manifest.jebnf", orgID, "text/plain", strings.NewReader(manifest))
}

func (g *GoogleDriveCAS) UpdateRepoManifest(ctx context.Context, org, repo, state string, categories map[string][]ManifestEntry) error {
	if err := g.init(ctx); err != nil { return err }
	orgID, _ := g.getOrCreateFolder(ctx, org, g.OrgsID)
	repoID, _ := g.getOrCreateFolder(ctx, repo, orgID)

	manifest := fmt.Sprintf("::Olympus::Firehorse::RepoTree::v1 {\n    Repo = %q;\n    State = %q;\n    Timestamp = %q;\n    Components {\n", 
		repo, state, time.Now().Format(time.RFC3339))
	for cat, entries := range categories {
		manifest += fmt.Sprintf("        %s = [\n", cat)
		for _, e := range entries {
			manifest += fmt.Sprintf("            { ID = %q; Hash = %q; },\n", e.ID, e.Hash)
		}
		manifest += "        ];\n"
	}
	manifest += "    }\n}\n"
	
	slog.Info("tree-update-repo", "repo", repo)
	return g.uploadFile(ctx, "manifest.jebnf", repoID, "text/plain", strings.NewReader(manifest))
}

func (g *GoogleDriveCAS) GetManifestMetadata(ctx context.Context, org, repo string) (time.Time, error) {
	if err := g.init(ctx); err != nil {
		return time.Time{}, err
	}

	orgID, _ := g.getOrCreateFolder(ctx, org, g.OrgsID)
	repoID, _ := g.getOrCreateFolder(ctx, repo, orgID)

	query := fmt.Sprintf("name = 'manifest.jebnf' and '%s' in parents and trashed = false", repoID)
	url := "https://www.googleapis.com/drive/v3/files?q=" + url.QueryEscape(query) + "&fields=files(id,modifiedTime)"

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+g.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()

	var res struct {
		Files []struct {
			ID           string    `json:"id"`
			ModifiedTime time.Time `json:"modifiedTime"`
		} `json:"files"`
	}
	json.NewDecoder(resp.Body).Decode(&res)

	if len(res.Files) == 0 {
		return time.Time{}, fmt.Errorf("manifest-not-found")
	}
	return res.Files[0].ModifiedTime, nil
}

func (g *GoogleDriveCAS) getOrCreateFolder(ctx context.Context, name, parentID string) (string, error) {
	// Search for existing
	query := fmt.Sprintf("name = '%s' and '%s' in parents and mimeType = 'application/vnd.google-apps.folder' and trashed = false", name, parentID)
	url := "https://www.googleapis.com/drive/v3/files?q=" + url.QueryEscape(query)
	
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+g.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()

	var res struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	json.NewDecoder(resp.Body).Decode(&res)

	if len(res.Files) > 0 {
		return res.Files[0].ID, nil
	}

	// Create new
	createUrl := "https://www.googleapis.com/drive/v3/files"
	body := fmt.Sprintf(`{"name": %q, "parents": [%q], "mimeType": "application/vnd.google-apps.folder"}`, name, parentID)
	req, _ = http.NewRequestWithContext(ctx, "POST", createUrl, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil { return "", err }
	defer resp.Body.Close()

	var newFolder struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&newFolder)
	return newFolder.ID, nil
}

func (g *GoogleDriveCAS) uploadFile(ctx context.Context, name, parentID, mimeType string, data io.Reader) error {
	url := "https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart"
	boundary := "cas_upload_boundary"

	var b bytes.Buffer
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: application/json; charset=UTF-8\r\n\r\n")
	b.WriteString(fmt.Sprintf(`{"name": %q, "parents": [%q]}`, name, parentID))
	b.WriteString("\r\n--" + boundary + "\r\n")
	b.WriteString(fmt.Sprintf("Content-Type: %s\r\n\r\n", mimeType))

	bodyReader := io.MultiReader(&b, data, strings.NewReader("\r\n--" + boundary + "--\r\n"))

	req, _ := http.NewRequestWithContext(ctx, "POST", url, bodyReader)
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Content-Type", "multipart/related; boundary="+boundary)

	resp, err := http.DefaultClient.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload-failed: %s - %s", resp.Status, string(body))
	}
	return nil
}

func (g *GoogleDriveCAS) Exists(ctx context.Context, repoID, headHash string) (bool, error) { return false, nil }
func (g *GoogleDriveCAS) Put(ctx context.Context, repoID, headHash string, data io.Reader) error { return nil }
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

// GoogleDriveStorage implements real Google Drive exfiltration.
type GoogleDriveStorage struct {
	FolderID string
}

func NewGoogleDriveStorage(folderID string) *GoogleDriveStorage {
	return &GoogleDriveStorage{FolderID: folderID}
}

func (g *GoogleDriveStorage) getService(ctx context.Context) (*drive.Service, error) {
	// Dynamically fetch token from gcloud for high-fidelity fleet operations
	out, err := exec.Command("gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get gcloud token: %w", err)
	}
	token := strings.TrimSpace(string(out))

	return drive.NewService(ctx, option.WithTokenSource(&staticTokenSource{AccessToken: token}))
}

type staticTokenSource struct {
	AccessToken string
}

func (s *staticTokenSource) Token() (*oauth2.Token, error) {
	// Note: We need oauth2 package but we will use a simple HTTP client if we want to avoid extra deps
	return nil, fmt.Errorf("not implemented")
}

// Put uploads the repository data to the specified Google Drive folder.
func (g *GoogleDriveStorage) Put(ctx context.Context, repoID, headHash string, data io.Reader) error {
	slog.Info("gdrive-put-start", "repo", repoID, "hash", headHash)

	// Fetch token
	out, err := exec.Command("gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return fmt.Errorf("failed to get gcloud token: %w", err)
	}
	token := strings.TrimSpace(string(out))

	// Construct API request directly to avoid complex dependency management in this turn
	url := "https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart"
	
	filename := fmt.Sprintf("%s_%s.bundle", repoID, headHash)
	
	// Boundary for multipart
	boundary := "sovereign_fleet_boundary"
	
	var b bytes.Buffer
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: application/json; charset=UTF-8\r\n\r\n")
	b.WriteString(fmt.Sprintf(`{"name": %q, "parents": ["%s"]}`, filename, g.FolderID))
	b.WriteString("\r\n--" + boundary + "\r\n")
	b.WriteString("Content-Type: application/octet-stream\r\n\r\n")
	
	// Use io.MultiReader to avoid loading everything into memory twice
	bodyReader := io.MultiReader(&b, data, strings.NewReader("\r\n--" + boundary + "--\r\n"))
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, bodyReader)
	if err != nil { return err }
	
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "multipart/related; boundary="+boundary)
	
	resp, err := http.DefaultClient.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gdrive-upload-failed: %s - %s", resp.Status, string(body))
	}

	slog.Info("gdrive-put-success", "repo", repoID, "filename", filename)
	return nil
}

func (g *GoogleDriveStorage) Exists(ctx context.Context, repoID, headHash string) (bool, error) {
	// Simple implementation for Pulse 6
	return false, nil
}

// ... existing code ...
