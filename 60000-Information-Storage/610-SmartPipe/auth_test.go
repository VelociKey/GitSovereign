package main

import (
	"context"
	"testing"
)

func TestIdentityService_Authorize(t *testing.T) {
	service := NewIdentityService("http://mock-workstation")
	ctx := context.Background()

	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{"ValidAdminToken", "valid-admin-token", false},
		{"ValidDevToken", "valid-dev-token", false},
		{"EmptyToken", "", true},
		{"ExpiredToken", "expired-token", true},
		{"UnauthorizedGroup", "unauthorized-user", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, err := service.Authorize(ctx, tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("Authorize() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && user == nil {
				t.Error("Authorize() returned nil user for valid token")
			}
		})
	}
}

func TestIdentityService_ResolveGroups(t *testing.T) {
	service := NewIdentityService("http://mock-workstation")
	ctx := context.Background()

	groups, err := service.resolveWorkstationGroups(ctx, "uid-123", "valid-token")
	if err != nil {
		t.Fatalf("resolveWorkstationGroups failed: %v", err)
	}

	found := false
	for _, g := range groups {
		if g == "Firehorse-Harvest-Active" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected group 'Firehorse-Harvest-Active' not found in mock response")
	}
}
