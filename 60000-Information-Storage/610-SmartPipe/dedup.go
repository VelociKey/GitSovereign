package main

import (
	"hash/fnv"
	"sync"
	"sync/atomic"
)

// BloomFilter provides a fast O(1) probabilistic membership check.
type BloomFilter struct {
	bits []uint64
	size uint32
}

func NewBloomFilter(size uint32) *BloomFilter {
	return &BloomFilter{
		bits: make([]uint64, (size/64)+1),
		size: size,
	}
}

func (bf *BloomFilter) Add(data string) {
	h := bf.hash(data)
	idx := h % bf.size
	bf.bits[idx/64] |= (1 << (idx % 64))
}

func (bf *BloomFilter) MayContain(data string) bool {
	h := bf.hash(data)
	idx := h % bf.size
	return (bf.bits[idx/64] & (1 << (idx % 64))) != 0
}

func (bf *BloomFilter) hash(data string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(data))
	return h.Sum32()
}

// HashTree tracks unique repository states with high efficiency.
type HashTree struct {
	mu          sync.RWMutex
	hashes      map[string]bool // Key: HeadHash (Org-wide uniqueness)
	filter      *BloomFilter
	hits        uint64
	misses      uint64
	bytesSaved  uint64
}

// NewHashTree initializes the deduplication engine.
func NewHashTree() *HashTree {
	return &HashTree{
		hashes: make(map[string]bool),
		filter: NewBloomFilter(1024 * 1024), // 1M bit filter
	}
}

// IsNovel checks if a hash has been seen before in the organization.
func (h *HashTree) IsNovel(headHash string) bool {
	// 1. O(1) Probabilistic Check
	if !h.filter.MayContain(headHash) {
		atomic.AddUint64(&h.misses, 1)
		return true // Definitely novel
	}

	// 2. Deterministic Check (Thread-Safe)
	h.mu.RLock()
	defer h.mu.RUnlock()
	
	if !h.hashes[headHash] {
		atomic.AddUint64(&h.misses, 1)
		return true
	}

	atomic.AddUint64(&h.hits, 1)
	return false // Redundant
}

// Record updates the dedupe index.
func (h *HashTree) Record(headHash string, sizeBytes uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	
	if h.hashes[headHash] {
		atomic.AddUint64(&h.bytesSaved, sizeBytes)
		return
	}

	h.hashes[headHash] = true
	h.filter.Add(headHash)
}

// GetMetrics returns real-time efficiency data.
func (h *HashTree) GetMetrics() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	hits := atomic.LoadUint64(&h.hits)
	misses := atomic.LoadUint64(&h.misses)
	total := hits + misses
	
	ratio := 0.0
	if total > 0 {
		ratio = float64(hits) / float64(total)
	}

	return map[string]interface{}{
		"hit_ratio":       ratio,
		"hits":            hits,
		"misses":          misses,
		"bytes_saved":     atomic.LoadUint64(&h.bytesSaved),
		"unique_nodes":    len(h.hashes),
	}
}
