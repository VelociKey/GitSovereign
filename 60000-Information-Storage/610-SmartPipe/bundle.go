package main

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
)

// SmartPipe performs a Zero-Disk Harvest by piping git bundle directly into memory
type SmartPipe struct {
	RepoPath string
}

// GenerateBundle streams the git bundle to the provided writer
func (s *SmartPipe) GenerateBundle(w io.Writer) error {
	// Execute 'git bundle create - --all' to stream to stdout
	cmd := exec.Command("git", "bundle", "create", "-", "--all")
	cmd.Dir = s.RepoPath
	cmd.Stdout = w
	
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git bundle failed: %w (stderr: %s)", err, errBuf.String())
	}

	return nil
}

// MemoryRehydrate loads a bundle from an io.Reader (e.g. QUIC stream) and prepares for rehydration
// In Phase 1, we just verify we can read it into a memory-mapped buffer or similar
func (s *SmartPipe) MemoryRehydrate(r io.Reader) ([]byte, error) {
	// For MVP, we read into RAM. 
	// Future optimization: use MMap for Zero-Copy Harvest.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read bundle to RAM failed: %w", err)
	}
	return data, nil
}
