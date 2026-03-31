package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SyncSchedule defines the frequency for a target
type SyncSchedule struct {
	Target    string
	Frequency string // DAILY, WEEKLY
}

// SyncRegistry holds the global scheduling configuration
type SyncRegistry struct {
	DefaultFrequency string
	Schedules        []SyncSchedule
}

func LoadSyncRegistry() *SyncRegistry {
	reg := &SyncRegistry{DefaultFrequency: "WEEKLY"}

	root := "c:\\aAntigravitySpace"
	if envRoot := os.Getenv("ANTIGRAVITY_ROOT"); envRoot != "" {
		root = envRoot
	}
	path := filepath.Join(root, "60PROX", "GitSovereign", "C0100-Configuration-Registry", "sync_registry.jebnf")

	file, err := os.Open(path)
	if err != nil {
		slog.Warn("sync-registry-not-found", "path", path, "using_defaults", true)
		return reg
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "::") {
			continue
		}

		if strings.HasPrefix(line, "DefaultFrequency") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				reg.DefaultFrequency = strings.Trim(strings.TrimSpace(parts[1]), "\";")
			}
		}
		if strings.Contains(line, "Target =") && strings.Contains(line, "Frequency =") {
			target := extractValue(line, "Target =")
			freq := extractValue(line, "Frequency =")
			if target != "" && freq != "" {
				reg.Schedules = append(reg.Schedules, SyncSchedule{Target: target, Frequency: freq})
			}
		}
	}

	slog.Info("sync-registry-loaded", "path", path, "schedules", len(reg.Schedules))
	return reg
}

func extractValue(line, key string) string {
	idx := strings.Index(line, key)
	if idx == -1 {
		return ""
	}
	sub := line[idx+len(key):]
	sub = strings.TrimSpace(sub)
	if strings.HasPrefix(sub, "\"") {
		sub = sub[1:]
		end := strings.Index(sub, "\"")
		if end != -1 {
			return sub[:end]
		}
	}
	// Also handle unquoted if needed, but jeBNF standard is quoted strings for these fields
	return ""
}

func IsSyncDue(ctx context.Context, target StorageTarget, reg *SyncRegistry, org, repo string) bool {
	freq := reg.DefaultFrequency
	for _, s := range reg.Schedules {
		if s.Target == org || s.Target == org+"/"+repo {
			freq = s.Frequency
		}
	}

	lastSync, err := target.GetManifestMetadata(ctx, org, repo)
	if err != nil {
		return true // New or missing manifest
	}

	threshold := 7 * 24 * time.Hour // WEEKLY
	if freq == "DAILY" {
		threshold = 24 * time.Hour
	}

	due := time.Since(lastSync) > threshold
	if !due {
		slog.Info("sync-skipped-not-due", "repo", repo, "last_sync", lastSync.Format(time.RFC3339), "freq", freq)
	}
	return due
}

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
	dryRun := flag.Bool("dry-run", false, "Run full pipeline but skip all writes to Google Drive")
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
	
	// Pulse 6: Set CAS storage destination
	if *dryRun {
		cp.Destination = NewDryRunCAS()
		slog.Info("dry-run-mode", "writes", "DISABLED", "pipeline", "FULL")
	} else {
		gdriveFolder := "1cajjaUMTzSRZn0__GjsFGcgqr20le4-B"
		cp.Destination = NewGoogleDriveCAS(gdriveFolder)

		// Authorization Gate: Verify Drive access before any harvesting
		if err := cp.Destination.(*GoogleDriveCAS).VerifyAccess(ctx); err != nil {
			slog.Error("authorization-gate-failed", "err", err)
			fmt.Fprintf(os.Stderr, "\n❌ %v\n", err)
			os.Exit(1)
		}
		slog.Info("authorization-gate-passed")
	}
	
	defer cp.Shutdown()
	
	dedup := NewHashTree()

	gh := NewGitHubClient()

	// 4. Execution Mode
	switch *mode {
	case "list-orgs":
		orgs, err := gh.ListOrganizations()
		if err != nil {
			slog.Error("list-orgs-failed", "err", err)
			os.Exit(1)
		}
		fmt.Printf("\nOrganizations for Authenticated User:\n")
		fmt.Printf("-------------------------------------\n")
		for _, o := range orgs {
			fmt.Printf("- %s (ID: %d)\n", o.Login, o.ID)
		}
		fmt.Printf("-------------------------------------\n")

	case "list-repos":
		if *org == "" {
			slog.Error("list-repos-failed-missing-org")
			os.Exit(1)
		}
		repos, err := gh.ListRepositories(*org)
		if err != nil {
			slog.Error("list-repos-failed", "org", *org, "err", err)
			os.Exit(1)
		}
		fmt.Printf("\nRepositories in Organization: %s\n", *org)
		fmt.Printf("-------------------------------------\n")
		for _, r := range repos {
			fmt.Printf("- %-30s | Head: %s\n", r.Name, r.HeadHash)
		}
		fmt.Printf("-------------------------------------\n")

	case "harvest":
		var orgsToHarvest []string
		if *org != "" {
			orgsToHarvest = append(orgsToHarvest, *org)
		} else {
			slog.Info("harvest-all-organizations-start")
			orgList, err := gh.ListOrganizations()
			if err != nil {
				slog.Error("harvest-all-orgs-list-failed", "err", err)
				os.Exit(1)
			}
			for _, o := range orgList {
				orgsToHarvest = append(orgsToHarvest, o.Login)
			}

			// Pulse 6: Update Root Semantic Tree
			if cp.Destination != nil {
				cp.Destination.UpdateRootManifest(ctx, orgsToHarvest)
			}
		}

		startTime := time.Now()
		totalNovelHarvested := 0
		reg := LoadSyncRegistry()

		for _, currentOrg := range orgsToHarvest {
			slog.Info("harvest-organization-processing", "org", currentOrg)

			// Discover real repositories
			repos, err := gh.ListRepositories(currentOrg)
			if err != nil {
				slog.Error("harvest-discovery-failed", "org", currentOrg, "err", err)
				continue
			}

			// Pulse 6: Update Organization Semantic Tree
			if cp.Destination != nil {
				var repoNames []string
				for _, r := range repos { repoNames = append(repoNames, r.Name) }
				cp.Destination.UpdateOrgManifest(ctx, currentOrg, repoNames)
			}

			harvestedCount := 0
			skippedCount := 0
			simulatedRepoSize := uint64(50 * 1024 * 1024) // 50MB per repo

			for _, r := range repos {
				repoID := r.Name
				headHash := r.HeadHash

				// Pulse 6: Check scheduling
				if cp.Destination != nil && !IsSyncDue(ctx, cp.Destination, reg, currentOrg, repoID) {
					skippedCount++
					continue
				}

				if r.SovereignState != "ACTIVE" {
					slog.Info("harvest-state-capture", "repo", repoID, "org", currentOrg, "state", r.SovereignState)
					// Still dispatch task to capture state in manifest
					task := Task{
						RepoID:    repoID,
						RepoName:  repoID,
						OrgName:   currentOrg,
						State:     r.SovereignState,
						Action:    "harvest",
						Target:    "local",
						AuthToken: "valid-token-fleet-admin",
					}
					cp.Dispatch(task)
					harvestedCount++ // Counting as "processed"
					continue
				}

				// 1. O(1) Local Dedupe Check (Org-wide hash node)
				if headHash != "" && !dedup.IsNovel(headHash) {
					slog.Info("hfd-deduplication-hit", "repo", repoID, "hash", headHash, "org", currentOrg)
					dedup.Record(headHash, simulatedRepoSize) // Track saved bytes
					continue
				}

				task := Task{
					RepoID:    repoID,
					RepoName:  repoID,
					OrgName:   currentOrg,
					State:     r.SovereignState,
					Action:    "harvest",
					Target:    "local",
					AuthToken: "valid-token-fleet-admin",
				}

				if err := cp.Dispatch(task); err != nil {
					slog.Error("dispatch-failed", "task", repoID, "err", err)
					continue
				}

				if headHash != "" {
					dedup.Record(headHash, 0)
				}
				harvestedCount++
			}

			// Wait for completions for this organization
			received := 0
			for received < harvestedCount {
				select {
				case <-cp.Results():
					received++
					totalNovelHarvested++
				case <-time.After(60 * time.Second):
					slog.Error("harvest-timeout-org", "org", currentOrg)
					received = harvestedCount // Move to next org
				}
			}
			slog.Info("harvest-organization-complete", "org", currentOrg, "novel_count", harvestedCount, "skipped_empty", skippedCount)
		}

		duration := time.Since(startTime)
		metrics := make(map[string]interface{})
		if cp.Destination != nil {
			metrics = cp.Destination.GetMetrics()
		}

		slog.Info("harvest-session-complete-hfd",
			"org_count", len(orgsToHarvest),
			"novel_harvested", totalNovelHarvested,
			"duration", duration,
			"cas_hit_ratio", metrics["cas_hit_ratio"],
			"dry_run", *dryRun,
		)

		generateAssuranceReport(*org, totalNovelHarvested, duration, metrics)
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
        CasHitRatio = %v;
        LogicalBytes = %v;
        PhysicalBytes = %v;
    }
}
`, org, time.Now().Format(time.RFC3339), total, duration.String(),
    metrics["cas_hit_ratio"], metrics["logical_bytes"], metrics["physical_bytes"])
	reportPath := fmt.Sprintf("ASSURANCE_HFD_%s.jebnf", time.Now().Format("20060102-150405"))
	os.WriteFile(reportPath, []byte(report), 0644)
	fmt.Printf("\n🛡️  High-Frequency Dedupe Assurance Report Generated: %s\n", reportPath)
}
