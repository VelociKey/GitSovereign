package main

import (
	"context"
	"testing"
	"time"
)


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
