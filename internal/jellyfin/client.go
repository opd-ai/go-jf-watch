// Package jellyfin provides a wrapper around the Jellyfin Go client
// with enhanced authentication, error handling, and session management.
package jellyfin

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sj14/jellyfin-go"
	"github.com/opd-ai/go-jf-watch/pkg/config"
)

// Client wraps the jellyfin-go client with additional functionality.
// It provides automatic retry logic, session management, and structured logging.
type Client struct {
	config *config.JellyfinConfig
	logger *slog.Logger
	
	// Jellyfin API client
	client *jellyfin.Client
	
	// Session management
	sessionToken string
	tokenExpiry  time.Time
	connected    bool
}

// New creates a new Jellyfin client wrapper with the provided configuration.
// It initializes the client but does not perform authentication until Connect is called.
func New(cfg *config.JellyfinConfig, logger *slog.Logger) *Client {
	// Create jellyfin client with server URL
	jellyfinClient, err := jellyfin.NewClient(cfg.ServerURL)
	if err != nil {
		logger.Error("Failed to create jellyfin client", "error", err)
		// Return client anyway, error will surface during Connect
	}
	
	return &Client{
		config: cfg,
		logger: logger,
		client: jellyfinClient,
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
	
	if c.client == nil {\n\t\treturn fmt.Errorf(\"jellyfin client not initialized\")\n\t}\n\n\t// Set API key for authentication\n\tc.client.SetAPIKey(c.config.APIKey)\n\n\t// Test connection by getting server info\n\t_, err := c.client.GetSystemInfo()\n\tif err != nil {\n\t\treturn fmt.Errorf(\"failed to connect to jellyfin server: %w\", err)\n\t}\n\n\tc.connected = true\n\tc.logger.Info(\"Successfully connected to Jellyfin server\")\n\t\n\treturn nil\n}"

// TestConnection validates the connection to Jellyfin without side effects.
// Returns an error if the server is unreachable or authentication fails.
func (c *Client) TestConnection(ctx context.Context) error {
	c.logger.Debug("Testing Jellyfin server connection")
	
	if c.client == nil {
		return fmt.Errorf("jellyfin client not initialized")
	}
	
	if c.config.ServerURL == "" {
		return fmt.Errorf("server URL not configured")
	}
	
	if c.config.APIKey == "" {
		return fmt.Errorf("API key not configured")
	}
	
	// Test connection with real API call
	c.client.SetAPIKey(c.config.APIKey)
	_, err := c.client.GetSystemInfo()
	if err != nil {
		return fmt.Errorf("jellyfin connection test failed: %w", err)
	}
	
	c.logger.Info("Jellyfin connection test successful")
	return nil
}

// IsConnected returns true if the client has an active session.
func (c *Client) IsConnected() bool {
	return c.connected && c.client != nil
}

// Disconnect closes the connection and clears the session.
func (c *Client) Disconnect() {
	c.logger.Info("Disconnecting from Jellyfin server")
	c.sessionToken = ""
	c.tokenExpiry = time.Time{}
	c.connected = false
	if c.client != nil {
		// Clear API key
		c.client.SetAPIKey("")
	}
}

// GetServerInfo returns basic information about the Jellyfin server.
// This is useful for validating the connection and server compatibility.
func (c *Client) GetServerInfo(ctx context.Context) (*ServerInfo, error) {
	c.logger.Debug("Fetching Jellyfin server information")
	
	if c.client == nil {
		return nil, fmt.Errorf("jellyfin client not initialized")
	}
	
	// Get system info from Jellyfin API
	sysInfo, err := c.client.GetSystemInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to get server info: %w", err)
	}
	
	return &ServerInfo{
		Name:           sysInfo.ServerName,
		Version:        sysInfo.Version,
		OperatingSystem: sysInfo.OperatingSystem,
		ID:             sysInfo.ID,
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