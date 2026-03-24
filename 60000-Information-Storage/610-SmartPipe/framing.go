package main

import (
	"fmt"
	"io"
	"time"
)

// SegmentHeader represents a jeBNF-compatible frame for data exfiltration.
type SegmentHeader struct {
	SegmentID string
	RepoID    string
	Offset    int64
	Length    int
	Timestamp time.Time
}

// WriteTo serializes the header into a jeBNF string and writes it to the destination.
func (h *SegmentHeader) WriteTo(w io.Writer) (int64, error) {
	header := fmt.Sprintf("::SegmentHeader::v1 { ID = %q; Repo = %q; Offset = %d; Length = %d; Time = %q }\n",
		h.SegmentID, h.RepoID, h.Offset, h.Length, h.Timestamp.Format(time.RFC3339))
	
	n, err := io.WriteString(w, header)
	return int64(n), err
}

// FramedWriter wraps an io.Writer to automatically inject SegmentHeaders.
type FramedWriter struct {
	Dest   io.Writer
	RepoID string
	Offset int64
}

// Write segments the data and adds jeBNF headers.
func (f *FramedWriter) Write(p []byte) (n int, err error) {
	header := &SegmentHeader{
		SegmentID: fmt.Sprintf("SEG-%d", time.Now().UnixNano()),
		RepoID:    f.RepoID,
		Offset:    f.Offset,
		Length:    len(p),
		Timestamp: time.Now(),
	}

	// 1. Write Header
	_, err = header.WriteTo(f.Dest)
	if err != nil {
		return 0, fmt.Errorf("failed to write jeBNF segment header: %w", err)
	}

	// 2. Write Data
	n, err = f.Dest.Write(p)
	f.Offset += int64(n)
	
	// 3. Write Tail (Optional, for visual separation in logs)
	io.WriteString(f.Dest, "\n::SegmentEnd::\n")
	
	return n, err
}
