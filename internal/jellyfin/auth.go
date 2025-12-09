package jellyfin

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// AuthManager handles authentication and session management for Jellyfin.
// It provides automatic token refresh and fallback to API key authentication.
type AuthManager struct {
	client       *Client
	logger       *slog.Logger
	sessionToken string
	tokenExpiry  time.Time
}

// NewAuthManager creates a new authentication manager for the given client.
func NewAuthManager(client *Client, logger *slog.Logger) *AuthManager {
	return &AuthManager{
		client: client,
		logger: logger,
	}
}

// Authenticate performs initial authentication with the Jellyfin server.
// It tries to authenticate using the API key and establishes a session.
func (a *AuthManager) Authenticate(ctx context.Context) error {
	a.logger.Info("Authenticating with Jellyfin server")

	// Validate that we have the necessary credentials
	if a.client.config.APIKey == "" {
		return fmt.Errorf("API key is required for authentication")
	}

	if a.client.config.UserID == "" {
		return fmt.Errorf("user ID is required for authentication")
	}

	if a.client.httpClient == nil {
		return fmt.Errorf("jellyfin client not initialized")
	}

	// Set API key for authentication
	a.sessionToken = a.client.config.APIKey

	// Verify authentication by testing connection
	if err := a.client.TestConnection(ctx); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	// For API key auth, we don't get a session token, but we mark as authenticated
	a.tokenExpiry = time.Now().Add(24 * time.Hour) // Assume API keys don't expire quickly
	a.client.sessionToken = a.sessionToken
	a.client.tokenExpiry = a.tokenExpiry

	a.logger.Info("Authentication successful")

	return nil
}

// RefreshToken refreshes the authentication token if it's close to expiry.
// Returns true if the token was refreshed, false if refresh wasn't needed.
func (a *AuthManager) RefreshToken(ctx context.Context) (bool, error) {
	// Check if token needs refresh (within 1 hour of expiry)
	if time.Until(a.tokenExpiry) > time.Hour {
		return false, nil
	}

	a.logger.Debug("Refreshing authentication token")

	// For API key authentication, we just need to verify the key is still valid
	if a.client.httpClient == nil {
		return false, fmt.Errorf("jellyfin client not initialized")
	}

	// Test if API key is still valid
	if err := a.client.TestConnection(ctx); err != nil {
		return false, fmt.Errorf("API key validation failed: %w", err)
	}

	// Extend token expiry
	oldExpiry := a.tokenExpiry
	a.tokenExpiry = time.Now().Add(24 * time.Hour)
	a.client.tokenExpiry = a.tokenExpiry

	a.logger.Info("Token refreshed successfully",
		"old_expiry", oldExpiry.Format(time.RFC3339),
		"new_expiry", a.tokenExpiry.Format(time.RFC3339))

	return true, nil
}

// IsTokenValid returns true if the current token is valid and not expired.
func (a *AuthManager) IsTokenValid() bool {
	return a.sessionToken != "" && time.Now().Before(a.tokenExpiry)
}

// GetAuthHeaders returns the authentication headers for API requests.
// It automatically refreshes the token if necessary.
func (a *AuthManager) GetAuthHeaders(ctx context.Context) (map[string]string, error) {
	// Try to refresh token if needed
	if _, err := a.RefreshToken(ctx); err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	if !a.IsTokenValid() {
		return nil, fmt.Errorf("no valid authentication token available")
	}

	// TODO: Return actual headers when jellyfin-go is available
	// For now, return mock headers
	return map[string]string{
		"Authorization": fmt.Sprintf("Bearer %s", a.sessionToken),
		"X-Emby-Token":  a.client.config.APIKey,
	}, nil
}

// Logout invalidates the current session and clears stored credentials.
func (a *AuthManager) Logout(ctx context.Context) error {
	a.logger.Info("Logging out from Jellyfin server")

	// TODO: Implement actual logout when jellyfin-go is available
	// For now, just clear local session data

	a.sessionToken = ""
	a.tokenExpiry = time.Time{}
	a.client.sessionToken = ""
	a.client.tokenExpiry = time.Time{}

	a.logger.Info("Logout successful")
	return nil
}
