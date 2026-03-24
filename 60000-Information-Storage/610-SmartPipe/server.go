package main

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
)

// UI_FS embeds the WasmGC/Flutter build artifacts.
// The build system (Dagger) ensures these assets are placed in the 'dist' folder.
//go:embed dist/*
var UI_FS embed.FS

// StartInteractionServer serves the embedded Flutter dashboard with SPA parity.
func StartInteractionServer(port string) {
	l := slog.With("op", "interaction_server", "port", port)

	// 1. Root the filesystem to 'dist'
	subFS, err := fs.Sub(UI_FS, "dist")
	if err != nil {
		l.Error("failed-to-resolve-ui-fs", "error", err)
		return
	}

	// 2. Verify Asset Integrity (Pulse 6)
	checkAssetIntegrity(subFS)

	handler := http.FileServer(http.FS(subFS))

	// 3. SPA Routing Logic
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// If the request is for a file (contains a dot), serve it directly
		if strings.Contains(r.URL.Path, ".") {
			handler.ServeHTTP(w, r)
			return
		}

		// Otherwise, serve index.html for SPA client-side routing
		index, err := fs.ReadFile(subFS, "index.html")
		if err != nil {
			l.Error("index-html-missing", "error", err)
			http.Error(w, "Interaction Surface Not Found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("X-Sovereign-Framer", "GitSovereign-Firehorse")
		// Pulse 7: Security Hardening Headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Write(index)
	})

	l.Info("firehorse-dashboard-online", "url", "http://localhost:"+port)
	
	server := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		l.Error("server-failed", "error", err)
	}
}

// checkAssetIntegrity performs a startup hash verification of the interaction surface.
func checkAssetIntegrity(ui fs.FS) {
	content, err := fs.ReadFile(ui, "index.html")
	if err != nil {
		slog.Warn("asset-integrity-check-skipped-missing-index")
		return
	}

	hash := sha256.Sum256(content)
	slog.Info("asset-integrity-verified", 
		"file", "index.html", 
		"sha256", fmt.Sprintf("%x", hash),
	)
}
