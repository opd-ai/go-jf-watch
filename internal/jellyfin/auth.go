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
	
	if a.client.client == nil {\n\t\treturn fmt.Errorf(\"jellyfin client not initialized\")\n\t}\n\n\t// Set API key for authentication\n\ta.client.client.SetAPIKey(a.client.config.APIKey)\n\n\t// Verify authentication by getting user info\n\t_, err := a.client.client.GetUser(a.client.config.UserID)\n\tif err != nil {\n\t\treturn fmt.Errorf(\"authentication failed: %w\", err)\n\t}\n\n\t// For API key auth, we don't get a session token, but we mark as authenticated\n\ta.sessionToken = a.client.config.APIKey // Use API key as session token\n\ta.tokenExpiry = time.Now().Add(24 * time.Hour) // Assume API keys don't expire quickly\n\ta.client.sessionToken = a.sessionToken\n\ta.client.tokenExpiry = a.tokenExpiry\n\t\n\ta.logger.Info(\"Authentication successful\")\n\t\n\treturn nil\n}"

// RefreshToken refreshes the authentication token if it's close to expiry.
// Returns true if the token was refreshed, false if refresh wasn't needed.
func (a *AuthManager) RefreshToken(ctx context.Context) (bool, error) {
	// Check if token needs refresh (within 1 hour of expiry)\n\tif time.Until(a.tokenExpiry) > time.Hour {\n\t\treturn false, nil\n\t}\n\t\n\ta.logger.Debug(\"Refreshing authentication token\")\n\t\n\t// For API key authentication, we just need to verify the key is still valid\n\tif a.client.client == nil {\n\t\treturn false, fmt.Errorf(\"jellyfin client not initialized\")\n\t}\n\t\n\t// Test if API key is still valid\n\t_, err := a.client.client.GetUser(a.client.config.UserID)\n\tif err != nil {\n\t\treturn false, fmt.Errorf(\"API key validation failed: %w\", err)\n\t}\n\t\n\t// Extend token expiry\n\toldExpiry := a.tokenExpiry\n\ta.tokenExpiry = time.Now().Add(24 * time.Hour)\n\ta.client.tokenExpiry = a.tokenExpiry\n\t\n\ta.logger.Info(\"Token refreshed successfully\",\n\t\t\"old_expiry\", oldExpiry.Format(time.RFC3339),\n\t\t\"new_expiry\", a.tokenExpiry.Format(time.RFC3339))\n\t\n\treturn true, nil\n}"

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