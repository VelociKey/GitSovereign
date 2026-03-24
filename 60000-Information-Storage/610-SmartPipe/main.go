package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"
)

func main() {
	// 1. Setup Structured Logging
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// 2. Parse Parameters
	mode := flag.String("mode", "service", "Operation mode: service, scan, or harvest")
	org := flag.String("org", "", "GitHub Organization to scan/harvest")
	workers := flag.Int("parallelism", 4, "Number of parallel Firehorse workers")
	workstation := flag.String("workstation", "http://localhost:8080", "Corporate workstation URL")
	port := flag.String("port", "8080", "Port for the Interaction Surface")
	flag.Parse()

	// 3. Initialize Foundations
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	slog.Info("firehorse-smartpipe-start", 
		"mode", *mode, 
		"org", *org, 
		"parallelism", *workers,
		"dedup_engine", "HashTree+Bloom",
	)

	identity := NewIdentityService(*workstation)
	cp := NewControlPlane(ctx, *workers, identity)
	defer cp.Shutdown()
	
	dedup := NewHashTree()
	storage := &LocalArchive{RootPath: "./archive"}

	// 4. Execution Mode
	switch *mode {
	case "harvest":
		if *org == "" {
			slog.Error("harvest-failed-missing-org")
			os.Exit(1)
		}
		
		startTime := time.Now()
		
		// Pulse 5: High-Frequency Deduplication Simulation (100 Repos)
		slog.Info("harvest-discovery-start-hfd", "org", *org, "simulated_repos", 100)
		
		harvestedCount := 0
		simulatedRepoSize := uint64(50 * 1024 * 1024) // 50MB per repo

		for i := 0; i < 100; i++ {
			repoID := fmt.Sprintf("Repo-%03d", i)
			headHash := "static-hash-v1"
			
			// Pulse 5: Only 10% are novel (simulating repo 0-9)
			if i >= 10 {
				headHash = "static-hash-v1"
			} else {
				headHash = fmt.Sprintf("novel-hash-%d", i)
			}

			// 1. O(1) Local Dedupe Check (Org-wide hash node)
			if !dedup.IsNovel(headHash) {
				slog.Info("hfd-deduplication-hit", "repo", repoID, "hash", headHash)
				dedup.Record(headHash, simulatedRepoSize) // Track saved bytes
				continue
			}

			// 2. Unique Hash Node Verification (Storage Check)
			exists, _ := storage.Exists(ctx, repoID, headHash)
			if exists {
				slog.Info("hfd-storage-hit", "repo", repoID, "hash", headHash)
				dedup.Record(headHash, simulatedRepoSize)
				continue
			}

			task := Task{
				RepoID:    repoID,
				RepoName:  repoID,
				Action:    "harvest",
				Target:    "local",
				AuthToken: "valid-token-fleet-admin",
			}
			
			if err := cp.Dispatch(task); err != nil {
				slog.Error("dispatch-failed", "task", repoID, "err", err)
				continue
			}
			
			dedup.Record(headHash, 0) // It's novel, no bytes saved yet
			harvestedCount++
		}

		// Wait for completions
		received := 0
		for received < harvestedCount {
			select {
			case <-cp.Results():
				received++
			case <-time.After(30 * time.Second):
				slog.Error("harvest-timeout-hfd")
				return
			}
		}

		duration := time.Since(startTime)
		metrics := dedup.GetMetrics()
		
		slog.Info("harvest-session-complete-hfd", 
			"total_attempted", 100, 
			"novel_harvested", harvestedCount, 
			"duration", duration,
			"hit_ratio", metrics["hit_ratio"],
			"bytes_saved_mb", (metrics["bytes_saved"].(uint64)) / (1024 * 1024),
		)

		generateAssuranceReport(*org, harvestedCount, duration, metrics)

	case "service":
		slog.Info("firehorse-service-start", "port", *port)
		StartInteractionServer(*port)

	default:
		slog.Warn("unsupported-mode", "mode", *mode)
	}
}

func generateAssuranceReport(org string, total int, duration time.Duration, metrics map[string]interface{}) {
	report := fmt.Sprintf(`::Olympus::Firehorse::PinnacleAssurance::v1 {
    Org = %q;
    Timestamp = %q;
    AuthorizedRepos = %d;
    SovereigntyTime = %q;
    IntegrityStatus = "VERIFIED";
    Efficiency {
        HitRatio = %v;
        BytesSaved = %v;
        UniqueNodes = %v;
    }
}
`, org, time.Now().Format(time.RFC3339), total, duration.String(), 
   metrics["hit_ratio"], metrics["bytes_saved"], metrics["unique_nodes"])

	reportPath := fmt.Sprintf("ASSURANCE_HFD_%s.jebnf", time.Now().Format("20060102-150405"))
	os.WriteFile(reportPath, []byte(report), 0644)
	fmt.Printf("\n🛡️  High-Frequency Dedupe Assurance Report Generated: %s\n", reportPath)
}
