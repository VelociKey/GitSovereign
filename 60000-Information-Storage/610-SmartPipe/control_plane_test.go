package main

import (
	"context"
	"testing"
	"time"
)

func TestControlPlane_BPS_Telemetry(t *testing.T) {
	ctx := context.Background()
	cp := NewControlPlane(ctx, 2, nil)
	defer cp.Shutdown()

	task := Task{
		RepoID:   "telemetry-test",
		RepoName: "GitSovereign",
		Action:   "harvest",
	}

	err := cp.Dispatch(task)
	if err != nil {
		t.Fatalf("Failed to dispatch task: %v", err)
	}

	select {
	case res := <-cp.Results():
		if !res.Success {
			t.Errorf("Task failed: %v", res.Error)
		}
		
		bps, ok := res.Metrics["bps"]
		if !ok || bps <= 0 {
			t.Errorf("Invalid BPS metric: %v", bps)
		}
		
		bytes, ok := res.Metrics["bytes"]
		if !ok || bytes <= 0 {
			t.Errorf("Invalid bytes metric: %v", bytes)
		}
		
		t.Logf("Telemetry Verified: Bytes=%v, BPS=%v", bytes, bps)

	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for task result")
	}
}

func TestControlPlane_DispatchAndExecute(t *testing.T) {
	ctx := context.Background()
	cp := NewControlPlane(ctx, 4, nil)
	defer cp.Shutdown()

	task := Task{
		RepoID:   "repo-1",
		RepoName: "GitSovereign",
		Action:   "harvest",
		Target:   "local",
	}

	err := cp.Dispatch(task)
	if err != nil {
		t.Fatalf("Failed to dispatch task: %v", err)
	}

	select {
	case res := <-cp.Results():
		if !res.Success {
			t.Errorf("Task failed: %v", res.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for task result")
	}
}

func TestControlPlane_Shutdown(t *testing.T) {
	ctx := context.Background()
	cp := NewControlPlane(ctx, 2, nil)
	
	task := Task{RepoID: "task-1"}
	_ = cp.Dispatch(task)

	cp.Shutdown()

	err := cp.Dispatch(Task{RepoID: "task-2"})
	if err == nil {
		t.Error("Expected error dispatching after shutdown")
	}
}

func TestControlPlane_Concurrency(t *testing.T) {
	ctx := context.Background()
	workers := 10
	taskCount := 50
	cp := NewControlPlane(ctx, workers, nil)
	defer cp.Shutdown()

	for i := 0; i < taskCount; i++ {
		_ = cp.Dispatch(Task{RepoID: "multi-task"})
	}

	received := 0
	for received < taskCount {
		select {
		case <-cp.Results():
			received++
		case <-time.After(5 * time.Second):
			t.Fatalf("Timeout: received %d/%d", received, taskCount)
		}
	}
}
