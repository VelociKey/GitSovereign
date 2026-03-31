package main

import (
	"context"
	"testing"
	"time"
)

type MockStorage struct {
	LastSync time.Time
}

func (m *MockStorage) PutBlob(ctx context.Context, hash string, data []byte) error { return nil }
func (m *MockStorage) BlobExists(ctx context.Context, hash string) (bool, error)  { return false, nil }
func (m *MockStorage) RecordLogicalBytes(size uint64)                             {}
func (m *MockStorage) GetMetrics() map[string]interface{}                         { return nil }
func (m *MockStorage) UpdateRootManifest(ctx context.Context, orgs []string) error { return nil }
func (m *MockStorage) UpdateOrgManifest(ctx context.Context, org string, repos []string) error {
	return nil
}
func (m *MockStorage) UpdateRepoManifest(ctx context.Context, org, repo, state string, categories map[string][]ManifestEntry) error {
	return nil
}
func (m *MockStorage) GetManifestMetadata(ctx context.Context, org, repo string) (time.Time, error) {
	if m.LastSync.IsZero() {
		return time.Time{}, fmt.Errorf("manifest-not-found")
	}
	return m.LastSync, nil
}

func TestIsSyncDue(t *testing.T) {
	ctx := context.Background()
	reg := &SyncRegistry{
		DefaultFrequency: "WEEKLY",
		Schedules: []SyncSchedule{
			{Target: "DailyOrg", Frequency: "DAILY"},
		},
	}

	now := time.Now()

	tests := []struct {
		name     string
		org      string
		repo     string
		lastSync time.Time
		expected bool
	}{
		{
			name:     "NewRepo_Due",
			org:      "NormalOrg",
			repo:     "NewRepo",
			lastSync: time.Time{}, // Zero time
			expected: true,
		},
		{
			name:     "WeeklyDefault_NotDue",
			org:      "NormalOrg",
			repo:     "Repo1",
			lastSync: now.Add(-3 * 24 * time.Hour),
			expected: false,
		},
		{
			name:     "WeeklyDefault_Due",
			org:      "NormalOrg",
			repo:     "Repo1",
			lastSync: now.Add(-8 * 24 * time.Hour),
			expected: true,
		},
		{
			name:     "DailyOverride_NotDue",
			org:      "DailyOrg",
			repo:     "Repo1",
			lastSync: now.Add(-12 * time.Hour),
			expected: false,
		},
		{
			name:     "DailyOverride_Due",
			org:      "DailyOrg",
			repo:     "Repo1",
			lastSync: now.Add(-25 * time.Hour),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockStorage{LastSync: tt.lastSync}
			got := IsSyncDue(ctx, mock, reg, tt.org, tt.repo)
			if got != tt.expected {
				t.Errorf("IsSyncDue() = %v, want %v", got, tt.expected)
			}
		})
	}
}
