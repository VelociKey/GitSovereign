package main

import (
	"context"
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

// ControlPlane manages the parallelism and density of Firehorse operations.
type ControlPlane struct {
	maxWorkers int
	curWorkers int
	tasks      chan Task
	results    chan Result
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex
	active     bool
	throttle   chan struct{} // Backpressure semaphore
	identity   *IdentityService // Pulse 3: Security Layer
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
	bps := float64(bytesProcessed) / duration.Seconds()
	
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

// executeExfiltration simulates the QUIC pipe with jeBNF framing.
func (cp *ControlPlane) executeExfiltration(task Task) (int64, error) {
	// Pulse 2: Wait for backpressure slot
	select {
	case <-cp.throttle:
		defer func() { cp.throttle <- struct{}{} }()
	case <-cp.ctx.Done():
		return 0, cp.ctx.Err()
	}

	// 1. Mock Destination
	dest := io.Discard 
	
	// 2. Initialize jeBNF Framer
	framed := &FramedWriter{
		Dest:   dest,
		RepoID: task.RepoID,
	}

	// 3. Simulate Data Streaming
	var totalBytes int64
	chunks := 5
	chunkSize := 1024 * 1024 // 1MB chunks
	
	for i := 0; i < chunks; i++ {
		data := make([]byte, chunkSize) 
		n, err := framed.Write(data)
		if err != nil {
			return totalBytes, err
		}
		totalBytes += int64(n)
		time.Sleep(10 * time.Millisecond)
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
