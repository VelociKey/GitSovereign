package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractValue(t *testing.T) {
	tests := []struct {
		line     string
		key      string
		expected string
	}{
		{`Target = "VelociKey";`, "Target =", "VelociKey"},
		{`Frequency = "DAILY";`, "Frequency =", "DAILY"},
		{`DefaultFrequency = "WEEKLY";`, "DefaultFrequency =", "WEEKLY"},
		{`NoKey = "Value";`, "Target =", ""},
		{`Target = Value;`, "Target =", ""}, // Current implementation expects quotes
	}

	for _, tt := range tests {
		got := extractValue(tt.line, tt.key)
		if got != tt.expected {
			t.Errorf("extractValue(%q, %q) = %q; want %q", tt.line, tt.key, got, tt.expected)
		}
	}
}

func TestLoadSyncRegistry(t *testing.T) {
	// Create a temporary jebnf file
	tmpDir, err := os.MkdirTemp("", "gitsov-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	regPath := filepath.Join(tmpDir, "60PROX", "GitSovereign", "C0100-Configuration-Registry")
	err = os.MkdirAll(regPath, 0755)
	if err != nil {
		t.Fatal(err)
	}

	content := `::Olympus::Firehorse::SyncRegistry::v1 {
    DefaultFrequency = "DAILY";
    Schedules = [
        { Target = "TestOrg/Repo"; Frequency = "WEEKLY"; }
    ];
}`
	err = os.WriteFile(filepath.Join(regPath, "sync_registry.jebnf"), []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Mock ANTIGRAVITY_ROOT
	oldRoot := os.Getenv("ANTIGRAVITY_ROOT")
	os.Setenv("ANTIGRAVITY_ROOT", tmpDir)
	defer os.Setenv("ANTIGRAVITY_ROOT", oldRoot)

	reg := LoadSyncRegistry()

	if reg.DefaultFrequency != "DAILY" {
		t.Errorf("Expected DefaultFrequency DAILY, got %s", reg.DefaultFrequency)
	}

	if len(reg.Schedules) != 1 {
		t.Errorf("Expected 1 schedule, got %d", len(reg.Schedules))
	} else {
		if reg.Schedules[0].Target != "TestOrg/Repo" {
			t.Errorf("Expected Target TestOrg/Repo, got %s", reg.Schedules[0].Target)
		}
		if reg.Schedules[0].Frequency != "WEEKLY" {
			t.Errorf("Expected Frequency WEEKLY, got %s", reg.Schedules[0].Frequency)
		}
	}
}
