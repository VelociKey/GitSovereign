package main

import (
        "context"
        "io"
        "testing"
        "time"
)

type MockSource struct{}

func (m *MockSource) StreamRepository(repo string, w io.Writer) (int64, error) {
        time.Sleep(10 * time.Millisecond)
        data := []byte("mock repository data")
        _, err := w.Write(data)
        return int64(len(data)), err
}

func (m *MockSource) FetchComponent(repo, component string) ([]ComponentFile, error) {
        return []ComponentFile{{ID: "mock_file", Data: []byte("mock component data")}}, nil
}

func TestControlPlane_BPS_Telemetry(t *testing.T) {
        ctx := context.Background()
        cp := NewControlPlane(ctx, 2, nil)
        cp.Source = &MockSource{}
        defer cp.Shutdown()

        task := Task{
                RepoID:   "telemetry-test",
                RepoName: "GitSovereign",
                Action:   "harvest",
                State:    "ACTIVE",
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
