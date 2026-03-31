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

	"olympus.fleet/00SDLC/OlympusLogicLibrary/60000-Information-Storage/90200-Logic-Libraries/110-gitsov-key"
	"olympus.fleet/00SDLC/OlympusLogicLibrary/60000-Information-Storage/90200-Logic-Libraries/120-adph"
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
	LogicalTotal  uint64
	PhysicalTotal uint64
	mu            sync.Mutex
	index         *adph.Table[gitsovkey.GitSovKey, bool]
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

// VerifyAccess performs a full authorization gate before harvesting begins.
// Steps: 1) Validate gcloud token  2) Verify target folder  3) Verify subfolders  4) Write probe
func (g *GoogleDriveCAS) VerifyAccess(ctx context.Context) error {
	slog.Info("auth-verify-start", "root_folder", g.RootID)

	// Step 1: Validate gcloud token
	out, err := exec.Command("gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return fmt.Errorf("AUTH FAILED: gcloud not authenticated. Run 'gcloud auth login' first: %w", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return fmt.Errorf("AUTH FAILED: gcloud returned empty token")
	}
	g.token = token
	slog.Info("auth-verify-token", "status", "valid", "length", len(token))

	// Step 2: Verify target root folder exists and is accessible
	folderURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?fields=id,name,mimeType,capabilities", g.RootID)
	req, _ := http.NewRequestWithContext(ctx, "GET", folderURL, nil)
	req.Header.Set("Authorization", "Bearer "+g.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("AUTH FAILED: cannot reach Google Drive API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return fmt.Errorf("AUTH FAILED: target folder %s not found. Check the folder ID", g.RootID)
	}
	if resp.StatusCode == 403 {
		return fmt.Errorf("AUTH FAILED: no access to folder %s. Share the folder with your gcloud account", g.RootID)
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("AUTH FAILED: unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var folderInfo struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		MimeType     string `json:"mimeType"`
		Capabilities struct {
			CanAddChildren bool `json:"canAddChildren"`
			CanEdit        bool `json:"canEdit"`
		} `json:"capabilities"`
	}
	json.NewDecoder(resp.Body).Decode(&folderInfo)

	if !folderInfo.Capabilities.CanAddChildren {
		return fmt.Errorf("AUTH FAILED: folder %q (%s) is read-only. Cannot create files", folderInfo.Name, g.RootID)
	}
	slog.Info("auth-verify-folder", "name", folderInfo.Name, "id", folderInfo.ID, "writable", folderInfo.Capabilities.CanAddChildren)

	// Step 3: Verify subfolders can be created/accessed
	hashesID, err := g.getOrCreateFolder(ctx, "hashes", g.RootID)
	if err != nil {
		return fmt.Errorf("AUTH FAILED: cannot create 'hashes' subfolder: %w", err)
	}
	g.HashesID = hashesID

	orgsID, err := g.getOrCreateFolder(ctx, "Orgs", g.RootID)
	if err != nil {
		return fmt.Errorf("AUTH FAILED: cannot create 'Orgs' subfolder: %w", err)
	}
	g.OrgsID = orgsID
	slog.Info("auth-verify-subfolders", "hashes_id", hashesID, "orgs_id", orgsID)

	// Step 4: Write and delete a probe file to confirm end-to-end write permission
	probeData := fmt.Sprintf("GitSovereign Authorization Probe: %s", time.Now().Format(time.RFC3339))
	probeErr := g.uploadFile(ctx, ".auth_probe", g.HashesID, "text/plain", strings.NewReader(probeData))
	if probeErr != nil {
		return fmt.Errorf("AUTH FAILED: write probe failed — token may lack drive.file scope: %w", probeErr)
	}

	// Clean up probe
	probeQuery := fmt.Sprintf("name = '.auth_probe' and '%s' in parents and trashed = false", g.HashesID)
	searchURL := "https://www.googleapis.com/drive/v3/files?q=" + url.QueryEscape(probeQuery)
	req2, _ := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	req2.Header.Set("Authorization", "Bearer "+g.token)
	resp2, err := http.DefaultClient.Do(req2)
	if err == nil {
		defer resp2.Body.Close()
		var res struct {
			Files []struct{ ID string `json:"id"` } `json:"files"`
		}
		json.NewDecoder(resp2.Body).Decode(&res)
		for _, f := range res.Files {
			delURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s", f.ID)
			delReq, _ := http.NewRequestWithContext(ctx, "DELETE", delURL, nil)
			delReq.Header.Set("Authorization", "Bearer "+g.token)
			http.DefaultClient.Do(delReq)
		}
	}

	slog.Info("auth-verify-complete", "status", "AUTHORIZED", "root_folder", folderInfo.Name)
	return nil
}

func (g *GoogleDriveCAS) loadIndex(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.index != nil { return nil }
	
	slog.Info("Loading minimum perfect hash CAS index from Google Drive")
	
	var rows []adph.SymbolRow[gitsovkey.GitSovKey, bool]
	pageToken := ""

	for {
		query := fmt.Sprintf("'%s' in parents and trashed = false", g.HashesID)
		urlStr := "https://www.googleapis.com/drive/v3/files?pageSize=1000&fields=nextPageToken,files(name)&q=" + url.QueryEscape(query)
		if pageToken != "" {
			urlStr += "&pageToken=" + pageToken
		}
		
		req, _ := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
		req.Header.Set("Authorization", "Bearer "+g.token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil { return err }

		var res struct {
			NextPageToken string `json:"nextPageToken"`
			Files []struct { Name string `json:"name"` } `json:"files"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			resp.Body.Close()
			return err
		}
		resp.Body.Close()

		for _, f := range res.Files {
			key, err := gitsovkey.FromHex(f.Name)
			if err == nil {
				rows = append(rows, adph.SymbolRow[gitsovkey.GitSovKey, bool]{Key: key, Value: true})
			}
		}

		if res.NextPageToken == "" {
			break
		}
		pageToken = res.NextPageToken
	}

	t, _ := adph.NewTable(rows)
	g.index = t
	slog.Info("CAS index loaded successfully", "keys_indexed", len(rows))
	return nil
}

func (g *GoogleDriveCAS) BlobExists(ctx context.Context, hash string) (bool, error) {
	if err := g.init(ctx); err != nil { return false, err }
	if err := g.loadIndex(ctx); err != nil { return false, err }

	key, err := gitsovkey.FromHex(hash)
	if err != nil { return false, err }

	_, found := g.index.Lookup(key)
	return found, nil
}

func (g *GoogleDriveCAS) PutBlob(ctx context.Context, hash string, data []byte) error {
	if err := g.init(ctx); err != nil { return err }
	slog.Info("cas-put-blob", "hash", hash, "size", len(data))
	
	g.mu.Lock()
	g.PhysicalTotal += uint64(len(data))
	
	if g.index != nil {
		if key, err := gitsovkey.FromHex(hash); err == nil {
			g.index.Add(key, true)
		}
	}
	
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

// DryRunCAS implements StorageTarget with full logging but zero writes.
// The entire pipeline runs (GitHub discovery, streaming, chunking, hashing, dedup)
// but nothing is uploaded to Google Drive.
type DryRunCAS struct {
	mu            sync.Mutex
	LogicalTotal  uint64
	PhysicalTotal uint64
	BlobCount     int
	ManifestCount int
}

func NewDryRunCAS() *DryRunCAS {
	slog.Info("dry-run-mode-active", "writes", "DISABLED")
	return &DryRunCAS{}
}

func (d *DryRunCAS) PutBlob(ctx context.Context, hash string, data []byte) error {
	d.mu.Lock()
	d.PhysicalTotal += uint64(len(data))
	d.BlobCount++
	d.mu.Unlock()
	slog.Info("dry-run-put-blob", "hash", hash, "size", len(data), "action", "SKIPPED")
	return nil
}

func (d *DryRunCAS) BlobExists(ctx context.Context, hash string) (bool, error) {
	return false, nil // Everything looks novel in dry-run
}

func (d *DryRunCAS) RecordLogicalBytes(size uint64) {
	d.mu.Lock()
	d.LogicalTotal += size
	d.mu.Unlock()
}

func (d *DryRunCAS) GetMetrics() map[string]interface{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	return map[string]interface{}{
		"logical_bytes":  d.LogicalTotal,
		"physical_bytes": d.PhysicalTotal,
		"cas_hit_ratio":  0.0,
		"dry_run":        true,
		"blobs_skipped":  d.BlobCount,
	}
}

func (d *DryRunCAS) UpdateRootManifest(ctx context.Context, orgs []string) error {
	d.mu.Lock()
	d.ManifestCount++
	d.mu.Unlock()
	slog.Info("dry-run-root-manifest", "orgs", len(orgs), "action", "SKIPPED")
	return nil
}

func (d *DryRunCAS) UpdateOrgManifest(ctx context.Context, org string, repos []string) error {
	d.mu.Lock()
	d.ManifestCount++
	d.mu.Unlock()
	slog.Info("dry-run-org-manifest", "org", org, "repos", len(repos), "action", "SKIPPED")
	return nil
}

func (d *DryRunCAS) UpdateRepoManifest(ctx context.Context, org, repo, state string, categories map[string][]ManifestEntry) error {
	d.mu.Lock()
	d.ManifestCount++
	d.mu.Unlock()
	totalEntries := 0
	for _, entries := range categories {
		totalEntries += len(entries)
	}
	slog.Info("dry-run-repo-manifest", "org", org, "repo", repo, "state", state, "entries", totalEntries, "action", "SKIPPED")
	return nil
}

func (d *DryRunCAS) GetManifestMetadata(ctx context.Context, org, repo string) (time.Time, error) {
	return time.Time{}, fmt.Errorf("dry-run-no-manifest") // Force all repos to appear due for sync
}
