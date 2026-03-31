package gitsovkey

import (
	"fmt"
	"lukechampine.com/blake3"
	"strings"
)

// CASKey generates a 256-bit hex-encoded physical storage key using BLAKE3.
// It instantly hashes the in-memory payload without disk I/O.
func CASKey(data []byte) string {
	h := blake3.Sum256(data)
	return fmt.Sprintf("gitsov_blake3_%x", h)
}

// NewCASStream creates a parallelized hashing engine that generates a 256-bit
// BLAKE3 digest on the fly. This is ideal for usage inside an io.TeeReader
// during Zero-Disk network streams (e.g. from GitHub directly to Google Drive).
func NewCASStream() *blake3.Hasher {
	return blake3.New(32, nil)
}

// HashFromStream returns the formatted prefix string once an io stream has
// finished passing through the blake3.Hasher.
func HashFromStream(h *blake3.Hasher) string {
	digest := h.Sum(nil)
	return fmt.Sprintf("gitsov_blake3_%x", digest)
}

// Prefix routes a given BLAKE3 CasKey to its designated 2-character 
// partitioning folder segment on the Google Drive CAS cluster.
// For example: "gitsov_blake3_3db8..." -> "3d"
func Prefix(casKey string) string {
	// Our spec states "gitsov_blake3_{hash}".
	// We want the first 2 characters of the {hash} segment.
	prefix := strings.TrimPrefix(casKey, "gitsov_blake3_")
	if len(prefix) >= 2 {
		return prefix[:2]
	}
	// Fallback/Safety
	return "00"
}
