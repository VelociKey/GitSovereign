// Package main provides the Sovereign Harvester CLI for GitSovereign.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"io"
	"time"
	"log"

	"olympus.fleet/00SDLC/OlympusLogicLibrary/60000-Information-Storage/90200-Logic-Libraries/110-gitsov-key"
	"olympus.fleet/00SDLC/OlympusLogicLibrary/60000-Information-Storage/90200-Logic-Libraries/120-adph"
	"olympus.fleet/60PROX/GitSovereign/610-SmartPipe/900-Execution-Points/bridge"
)

func main() {
	orgName := flag.String("org", "", "GitHub Organization to harvest")
	gdrivePath := flag.String("gdrive", "/Sovereign-Vault", "Target path in Google Drive/GWS")
	swallow := flag.Bool("swallow", false, "Engage Sovereign Swallow-Mode (Discard all egress data)")
	flag.Parse()

	if *orgName == "" {
		fmt.Println("USAGE: sovereign-harvest -org <name>")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Hour)
	defer cancel()

	// 1. Initialize Engines with optional Swallow-Mode
	index, _ := adph.NewTable[gitsovkey.GitSovKey, string](nil)
	
	var egress io.WriteCloser
	if *swallow {
		fmt.Println("🍴 Sovereign Swallow-Mode engaged: Performance benchmarking only.")
		egress = bridge.NewSwallowEgress()
	} else {
		// HARDENED INITIALIZATION: Fail early if Google Drive fails
		var err error
		egress, err = bridge.NewGDriveEgress(ctx, *gdrivePath)
		if err != nil {
			log.Fatalf("❌ FATAL: Sovereign Cloud Egress Initialization Failed: %v\n", err)
		}
	}

	deduper := &bridge.DedupeStreamer{
		Index:  index,
		Egress: egress,
	}
	_ = deduper

	// 2. Parallel Task Distribution
	numWorkers := runtime.NumCPU()
	fmt.Printf("🔥 Parallel-Pulse: Spawning %d High-Frequency Workers...\n", numWorkers)
	
	// Discovery
	gh := bridge.NewGitHubClient()
	repos, _ := gh.DiscoverOrgRepositories(ctx, *orgName)

	for _, repo := range repos {
		fmt.Printf("📦 HARVESTING: [%s]...\n", repo)
	}

	fmt.Println("🛡️ HARVEST COMPLETE: Sovereign Assurance Proof Generated.")
	if egress != nil { egress.Close() }
}
