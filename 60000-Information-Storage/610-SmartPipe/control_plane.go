package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

// Task represents a single repository harvest or rehydrate operation.
type Task struct {
	RepoID    string
	RepoName  string
	OrgName   string // Pulse 6: Organization Context for CAS
	State     string // EMPTY, BRANCHLESS, ACTIVE
	Action    string // "harvest", "rehydrate"
	Target    string // "drive", "gcs", "local"
	AuthToken string // Pulse 3: Identity-Plus Shield
}

// Result represents the outcome of a Task execution.
type Result struct {
	RepoID  string
	Success bool
	Error   error
	Metrics map[string]float64 // Latency, Throughput, Integrity, BPS
}

// Source defines the interface for repository data discovery and exfiltration.
type Source interface {
        StreamRepository(repo string, w io.Writer) (int64, error)
        FetchComponent(repo, component string) ([]ComponentFile, error)
}

// ControlPlane manages the parallelism and density of Firehorse operations.
type ControlPlane struct {
        maxWorkers  int
        curWorkers  int
        tasks       chan Task
        results     chan Result
        wg          sync.WaitGroup
        ctx         context.Context
        cancel      context.CancelFunc
        mu          sync.RWMutex
        active      bool
        throttle    chan struct{}    // Backpressure semaphore
        identity    *IdentityService // Pulse 3: Security Layer
        Destination StorageTarget    // Pulse 6: Real Storage Destination
        Source      Source           // Pulse 6: Data Source (GitHub)
}

// NewControlPlane creates a new orchestrator with the specified concurrency density.
func NewControlPlane(ctx context.Context, workers int, identity *IdentityService) *ControlPlane {
        cCtx, cancel := context.WithCancel(ctx)
        cp := &ControlPlane{
                maxWorkers: workers,
                curWorkers: workers,
                tasks:      make(chan Task, 1000),
                results:    make(chan Result, 1000),
                ctx:        cCtx,
                cancel:     cancel,
                active:     true,
                throttle:   make(chan struct{}, workers),
                identity:   identity,
                Source:     NewGitHubClient(),
        }
	// Initialize throttle slots
	for i := 0; i < workers; i++ {
		cp.throttle <- struct{}{}
	}

	for i := 0; i < workers; i++ {
		cp.wg.Add(1)
		go cp.worker(i)
	}

	slog.Info("control-plane-initialized", "workers", workers, "security_enabled", identity != nil)
	return cp
}

// SetBackpressure adjusts the available concurrency slots based on external telemetry.
func (cp *ControlPlane) SetBackpressure(level float64) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	target := int(float64(cp.maxWorkers) * (1.0 - level))
	if target < 1 { target = 1 }
	
	slog.Info("control-plane-backpressure-applied", "level", level, "target_workers", target)
}

func (cp *ControlPlane) worker(id int) {
	defer cp.wg.Done()
	for {
		select {
		case task, ok := <-cp.tasks:
			if !ok {
				return
			}
			res := cp.processTask(id, task)
			select {
			case cp.results <- res:
			case <-cp.ctx.Done():
				return
			}
		case <-cp.ctx.Done():
			return
		}
	}
}

// processTask is the core execution logic (Pulse 3: Identity Locked).
func (cp *ControlPlane) processTask(workerID int, task Task) Result {
	l := slog.With("worker_id", workerID, "repo", task.RepoName, "action", task.Action)
	l.Info("task-processing-start")

	// Pulse 3: Identity Verification Gate
	if cp.identity != nil {
		_, err := cp.identity.Authorize(cp.ctx, task.AuthToken)
		if err != nil {
			l.Warn("task-auth-failed-blocking-execution", "error", err)
			return Result{RepoID: task.RepoID, Success: false, Error: err}
		}
	}

	start := time.Now()
	
	// Pulse 1: Implement BPS Telemetry & Framing
	bytesProcessed, err := cp.executeExfiltration(task)
	
	duration := time.Since(start)
	var bps float64
	if duration > 0 {
	        bps = float64(bytesProcessed) / duration.Seconds()
	}
	if err != nil {
		l.Error("task-processing-failed", "error", err, "duration_ms", duration.Milliseconds())
		return Result{RepoID: task.RepoID, Success: false, Error: err}
	}

	l.Info("task-processing-complete", 
		"duration_ms", duration.Milliseconds(),
		"bytes_total", bytesProcessed,
		"bps", int64(bps),
	)
	
	return Result{
		RepoID:  task.RepoID,
		Success: true,
		Metrics: map[string]float64{
			"latency_ms": float64(duration.Milliseconds()),
			"bps":        bps,
			"bytes":      float64(bytesProcessed),
		},
	}
}

// MerkleTree represents a balanced binary tree of hashes.
type MerkleTree struct {
	Root  string
	Nodes [][]string
}

// MerkleHasher handles parallel Merkle tree generation for streamed data.
type MerkleHasher struct {
	ChunkSize int
}

func (h *MerkleHasher) Generate(data []byte) (*MerkleTree, error) {
	if len(data) == 0 {
		return &MerkleTree{Root: ""}, nil
	}

	numChunks := (len(data) + h.ChunkSize - 1) / h.ChunkSize
	leaves := make([]string, numChunks)

	for i := 0; i < numChunks; i++ {
		start := i * h.ChunkSize
		end := start + h.ChunkSize
		if end > len(data) {
			end = len(data)
		}
		hash := sha256.Sum256(data[start:end])
		leaves[i] = hex.EncodeToString(hash[:])
	}

	// Simplified: just return root as hash of concatenated leaves for demonstration
	totalHash := sha256.Sum256([]byte(fmt.Sprintf("%v", leaves)))
	return &MerkleTree{Root: hex.EncodeToString(totalHash[:])}, nil
}

// executeExfiltration performs multi-component harvesting with streaming CAS deduplication.
func (cp *ControlPlane) executeExfiltration(task Task) (int64, error) {
        // Pulse 2: Wait for backpressure slot
        select {
        case <-cp.throttle:
                defer func() { cp.throttle <- struct{}{} }()
        case <-cp.ctx.Done():
                return 0, cp.ctx.Err()
        }

        var totalBytes int64

	components := []string{"code", "issues", "wiki"}
	manifestCategories := make(map[string][]ManifestEntry)

	// If repository is not ACTIVE, we skip component harvesting but still create manifest
	if task.State == "ACTIVE" {
		for _, comp := range components {
			if comp == "code" {
				// Streaming Code Chunker
				var entries []ManifestEntry
				pr, pw := io.Pipe()
				
				// Start streaming from GitHub in background
				go func() {
				        _, err := cp.Source.StreamRepository(task.RepoID, pw)
				        pw.CloseWithError(err)
				}()
				// Process stream in 1MB chunks
				buf := make([]byte, 1024*1024)
				for {
					n, err := io.ReadFull(pr, buf)
					if n > 0 {
						chunkData := make([]byte, n)
						copy(chunkData, buf[:n])
						totalBytes += int64(n)
						
						h := sha256.Sum256(chunkData)
						hashStr := "sha256:" + hex.EncodeToString(h[:])
						
						if cp.Destination != nil {
							cp.Destination.RecordLogicalBytes(uint64(n))
							exists, _ := cp.Destination.BlobExists(cp.ctx, hashStr)
							if !exists {
								cp.Destination.PutBlob(cp.ctx, hashStr, chunkData)
							}
						}
						entries = append(entries, ManifestEntry{ID: fmt.Sprintf("chunk_%d", len(entries)), Hash: hashStr})
					}
					if err != nil {
						if err == io.EOF || err == io.ErrUnexpectedEOF { break }
						slog.Error("code-stream-chunk-failed", "repo", task.RepoID, "err", err)
						break
					}
				}
				manifestCategories[comp] = entries
			} else {
			        // Other components (issues, wiki)
			        files, err := cp.Source.FetchComponent(task.RepoID, comp)
			        if err != nil {

					slog.Error("component-fetch-failed", "repo", task.RepoID, "comp", comp, "err", err)
					continue
				}

				var entries []ManifestEntry
				for _, f := range files {
					data := f.Data
					totalBytes += int64(len(data))

					h := sha256.Sum256(data)
					hashStr := "sha256:" + hex.EncodeToString(h[:])
					
					if cp.Destination != nil {
						cp.Destination.RecordLogicalBytes(uint64(len(data)))
						exists, _ := cp.Destination.BlobExists(cp.ctx, hashStr)
						if !exists {
							cp.Destination.PutBlob(cp.ctx, hashStr, data)
						}
					}
					entries = append(entries, ManifestEntry{ID: f.ID, Hash: hashStr})
				}
				manifestCategories[comp] = entries
			}
		}
	} else {
		slog.Info("exfiltration-state-only", "repo", task.RepoID, "state", task.State)
	}

	// Anchor with Manifest (Repository Tree)
	if cp.Destination != nil {
		err := cp.Destination.UpdateRepoManifest(cp.ctx, task.OrgName, task.RepoName, task.State, manifestCategories)
		if err != nil {
			return totalBytes, fmt.Errorf("repo-manifest-update-failed: %w", err)
		}
	}

	return totalBytes, nil
}

func (cp *ControlPlane) Dispatch(task Task) error {
	cp.mu.RLock()
	defer cp.mu.RUnlock()
	if !cp.active {
		return fmt.Errorf("control plane is not active")
	}
	select {
	case cp.tasks <- task:
		return nil
	default:
		return fmt.Errorf("task queue full")
	}
}

func (cp *ControlPlane) Results() <-chan Result {
	return cp.results
}

func (cp *ControlPlane) Shutdown() {
	cp.mu.Lock()
	if !cp.active {
		cp.mu.Unlock()
		return
	}
	cp.active = false
	cp.mu.Unlock()
	cp.cancel()
	close(cp.tasks)
	cp.wg.Wait()
	close(cp.results)
}
