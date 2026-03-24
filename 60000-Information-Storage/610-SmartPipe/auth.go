package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// UserProfile represents an authenticated and authorized Firehorse identity.
type UserProfile struct {
	UID    string
	Email  string
	Groups []string
}

// IdentityService handles the "Identity-Plus" Shield logic.
type IdentityService struct {
	WorkstationURL string
	HTTPClient     *http.Client
}

// NewIdentityService initializes the security layer.
func NewIdentityService(workstationURL string) *IdentityService {
	return &IdentityService{
		WorkstationURL: workstationURL,
		HTTPClient:     &http.Client{Timeout: 5 * time.Second},
	}
}

// Authorize verifies the token and checks corporate group membership (PBAC).
func (s *IdentityService) Authorize(ctx context.Context, token string) (*UserProfile, error) {
	l := slog.With("op", "authorize_identity")

	// 1. Validate Token (Simulated Firebase/SPIFFE validation)
	if token == "" {
		l.Warn("auth-rejection-missing-token")
		return nil, fmt.Errorf("missing authentication token")
	}

	if token == "expired-token" {
		l.Warn("auth-rejection-token-expired")
		return nil, fmt.Errorf("session token expired")
	}

	// 2. Resolve Profile from Token
	user := &UserProfile{
		UID:   "uid-" + token[:5], // Mock derivation
		Email: "developer@sovereign.fleet",
	}

	// 3. PBAC: Workstation Group Delegate Call
	groups, err := s.resolveWorkstationGroups(ctx, user.UID, token)
	if err != nil {
		l.Error("auth-error-workstation-delegate-failed", "error", err, "uid", user.UID)
		return nil, fmt.Errorf("failed to verify workstation groups: %w", err)
	}

	user.Groups = groups

	// 4. Policy Check: Must have Firehorse-Harvest-Active group
	authorized := false
	for _, g := range groups {
		if g == "Firehorse-Harvest-Active" {
			authorized = true
			break
		}
	}

	if !authorized {
		l.Warn("auth-rejection-insufficient-privileges", "uid", user.UID, "groups", groups)
		return nil, fmt.Errorf("user not authorized for harvest operations")
	}

	l.Info("auth-success", "uid", user.UID, "email", user.Email, "groups", groups)
	return user, nil
}

// resolveWorkstationGroups simulates the PBAC call to the corporate workstation.
func (s *IdentityService) resolveWorkstationGroups(ctx context.Context, uid, token string) ([]string, error) {
	// In production, this would be a real POST request to s.WorkstationURL
	// For simulation, we return groups based on token content.
	
	if strings.Contains(token, "unauthorized") {
		return []string{"Guest"}, nil
	}

	return []string{"Sovereign-Admin", "Firehorse-Harvest-Active"}, nil
}
