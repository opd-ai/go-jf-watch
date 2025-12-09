// Package jellyfin provides a wrapper around the Jellyfin HTTP API
// with enhanced authentication, error handling, and session management.
package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/opd-ai/go-jf-watch/pkg/config"
)

// Client wraps HTTP client with Jellyfin-specific functionality.
// It provides automatic retry logic, session management, and structured logging.
type Client struct {
	config *config.JellyfinConfig
	logger *slog.Logger

	// HTTP client for API calls
	httpClient *http.Client

	// Session management
	sessionToken string
	tokenExpiry  time.Time
	connected    bool
}

// SystemInfo represents Jellyfin system information response
type SystemInfo struct {
	ServerName      string `json:"ServerName"`
	Version         string `json:"Version"`
	OperatingSystem string `json:"OperatingSystem"`
	ID              string `json:"Id"`
}

// New creates a new Jellyfin client wrapper with the provided configuration.
// It initializes the client but does not perform authentication until Connect is called.
func New(cfg *config.JellyfinConfig, logger *slog.Logger) *Client {
	return &Client{
		config: cfg,
		logger: logger,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Connect establishes a connection to the Jellyfin server and authenticates.
// It attempts authentication with the API key and validates the connection.
func (c *Client) Connect(ctx context.Context) error {
	c.logger.Info("Connecting to Jellyfin server",
		"server_url", c.config.ServerURL,
		"user_id", c.config.UserID)

	// Validate configuration
	if c.config.ServerURL == "" {
		return fmt.Errorf("server URL is empty")
	}

	if c.config.APIKey == "" {
		return fmt.Errorf("API key is empty")
	}

	if c.config.UserID == "" {
		return fmt.Errorf("user ID is empty")
	}

	if c.httpClient == nil {
		return fmt.Errorf("HTTP client not initialized")
	}

	// Test connection by getting server info
	_, err := c.getSystemInfo(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to jellyfin server: %w", err)
	}

	c.connected = true
	c.logger.Info("Successfully connected to Jellyfin server")

	return nil
}

// TestConnection validates the connection to Jellyfin without side effects.
// Returns an error if the server is unreachable or authentication fails.
func (c *Client) TestConnection(ctx context.Context) error {
	c.logger.Debug("Testing Jellyfin server connection")

	if c.httpClient == nil {
		return fmt.Errorf("HTTP client not initialized")
	}

	if c.config.ServerURL == "" {
		return fmt.Errorf("server URL not configured")
	}

	if c.config.APIKey == "" {
		return fmt.Errorf("API key not configured")
	}

	// Test connection with real API call
	_, err := c.getSystemInfo(ctx)
	if err != nil {
		return fmt.Errorf("jellyfin connection test failed: %w", err)
	}

	c.logger.Info("Jellyfin connection test successful")
	return nil
}

// getSystemInfo makes an HTTP request to get system information
func (c *Client) getSystemInfo(ctx context.Context) (*SystemInfo, error) {
	url := fmt.Sprintf("%s/System/Info", c.config.ServerURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("X-Emby-Token", c.config.APIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var sysInfo SystemInfo
	if err := json.NewDecoder(resp.Body).Decode(&sysInfo); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &sysInfo, nil
}

// IsConnected returns true if the client has an active session.
func (c *Client) IsConnected() bool {
	return c.connected && c.httpClient != nil
}

// Disconnect closes the connection and clears the session.
func (c *Client) Disconnect() {
	c.logger.Info("Disconnecting from Jellyfin server")
	c.sessionToken = ""
	c.tokenExpiry = time.Time{}
	c.connected = false
}

// GetServerInfo returns basic information about the Jellyfin server.
// This is useful for validating the connection and server compatibility.
func (c *Client) GetServerInfo(ctx context.Context) (*ServerInfo, error) {
	c.logger.Debug("Fetching Jellyfin server information")

	if c.httpClient == nil {
		return nil, fmt.Errorf("HTTP client not initialized")
	}

	// Get system info from Jellyfin API
	sysInfo, err := c.getSystemInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get server info: %w", err)
	}

	return &ServerInfo{
		Name:            sysInfo.ServerName,
		Version:         sysInfo.Version,
		OperatingSystem: sysInfo.OperatingSystem,
		ID:              sysInfo.ID,
	}, nil
}

// ServerInfo contains basic information about the Jellyfin server.
type ServerInfo struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	OperatingSystem string `json:"operating_system"`
	ID              string `json:"id"`
}

// GetStreamURL constructs the direct stream URL for a media item.
// Returns the URL that can be used to proxy/redirect streaming requests.
func (c *Client) GetStreamURL(mediaID string) (string, error) {
	if c.config.ServerURL == "" {
		return "", fmt.Errorf("server URL not configured")
	}

	if c.config.APIKey == "" {
		return "", fmt.Errorf("API key not configured")
	}

	// Construct direct stream URL for Jellyfin
	// Format: {server}/Videos/{id}/stream?Static=true&api_key={key}
	streamURL := fmt.Sprintf("%s/Videos/%s/stream?Static=true&api_key=%s",
		c.config.ServerURL, mediaID, c.config.APIKey)

	c.logger.Debug("Generated stream URL for media",
		"media_id", mediaID,
		"url", streamURL)

	return streamURL, nil
}
