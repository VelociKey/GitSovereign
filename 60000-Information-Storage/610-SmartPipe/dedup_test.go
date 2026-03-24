package main

import (
	"fmt"
	"sync"
	"testing"
)

func TestHashTree_IsNovelAndRecord(t *testing.T) {
	h := NewHashTree()
	hash1 := "hash-v1"
	hash2 := "hash-v2"

	// 1. First time seeing hash1 -> Novel
	if !h.IsNovel(hash1) {
		t.Error("Expected hash1 to be novel")
	}
	h.Record(hash1, 100)

	// 2. Second time seeing hash1 -> Redundant
	if h.IsNovel(hash1) {
		t.Error("Expected hash1 to be redundant")
	}

	// 3. Different repo, same hash -> Redundant (Org-wide dedupe)
	if h.IsNovel(hash1) {
		t.Error("Expected hash1 to be redundant for different repo")
	}

	// 4. Same repo, different hash -> Novel
	if !h.IsNovel(hash2) {
		t.Error("Expected hash2 to be novel")
	}
	h.Record(hash2, 200)
}

func TestHashTree_Metrics(t *testing.T) {
	h := NewHashTree()
	
	// Simulate 10 unique, 90 redundant
	for i := 0; i < 10; i++ {
		hash := fmt.Sprintf("hash-%d", i)
		h.IsNovel(hash)
		h.Record(hash, 1000)
	}
	for i := 0; i < 90; i++ {
		h.IsNovel("hash-0")
		h.Record("hash-0", 1000)
	}

	m := h.GetMetrics()
	if m["hit_ratio"].(float64) != 0.9 {
		t.Errorf("Expected hit ratio 0.9, got %v", m["hit_ratio"])
	}
	if m["bytes_saved"].(uint64) != 90000 {
		t.Errorf("Expected 90000 bytes saved, got %v", m["bytes_saved"])
	}
}

func TestHashTree_Concurrency(t *testing.T) {
	h := NewHashTree()
	wg := sync.WaitGroup{}
	workers := 20
	iterations := 100

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				hash := fmt.Sprintf("worker-%d-hash-%d", id, j)
				h.IsNovel(hash)
				h.Record(hash, 10)
				// Concurrent access to same hash
				h.IsNovel("shared-hash")
				h.Record("shared-hash", 10)
			}
		}(i)
	}
	wg.Wait()

	m := h.GetMetrics()
	expectedUnique := uint64(workers*iterations + 1)
	if m["unique_nodes"].(int) != int(expectedUnique) {
		t.Errorf("Expected %d unique nodes, got %v", expectedUnique, m["unique_nodes"])
	}
}
