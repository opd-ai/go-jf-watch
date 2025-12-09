package jellyfin

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/opd-ai/go-jf-watch/pkg/config"
)

func TestNew(t *testing.T) {
	cfg := &config.JellyfinConfig{
		ServerURL: "https://jellyfin.example.com",
		APIKey:    "test-api-key",
		UserID:    "test-user-id",
	}
	
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during tests
	}))
	
	client := New(cfg, logger)
	
	if client == nil {
		t.Fatal("Expected client to be non-nil")
	}
	
	if client.config != cfg {
		t.Error("Expected config to be set correctly")
	}
	
	if client.logger != logger {
		t.Error("Expected logger to be set correctly")
	}
}

func TestClientConnect(t *testing.T) {
	tests := []struct {
		name      string
		config    *config.JellyfinConfig
		wantError bool
	}{
		{
			name: "valid config",
			config: &config.JellyfinConfig{
				ServerURL: "https://jellyfin.example.com",
				APIKey:    "test-api-key",
				UserID:    "test-user-id",
			},
			wantError: false,
		},
		{
			name: "empty server URL",
			config: &config.JellyfinConfig{
				ServerURL: "",
				APIKey:    "test-api-key",
				UserID:    "test-user-id",
			},
			wantError: true,
		},
		{
			name: "empty API key",
			config: &config.JellyfinConfig{
				ServerURL: "https://jellyfin.example.com",
				APIKey:    "",
				UserID:    "test-user-id",
			},
			wantError: true,
		},
		{
			name: "empty user ID",
			config: &config.JellyfinConfig{
				ServerURL: "https://jellyfin.example.com",
				APIKey:    "test-api-key",
				UserID:    "",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelError,
			}))
			
			client := New(tt.config, logger)
			ctx := context.Background()
			
			err := client.Connect(ctx)
			
			if tt.wantError && err == nil {
				t.Error("Expected error but got none")
			}
			
			if !tt.wantError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestClientTestConnection(t *testing.T) {
	cfg := &config.JellyfinConfig{
		ServerURL: "https://jellyfin.example.com",
		APIKey:    "test-api-key",
		UserID:    "test-user-id",
	}
	
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	
	client := New(cfg, logger)
	ctx := context.Background()
	
	err := client.TestConnection(ctx)
	if err != nil {
		t.Errorf("TestConnection failed: %v", err)
	}
}

func TestClientGetServerInfo(t *testing.T) {
	cfg := &config.JellyfinConfig{
		ServerURL: "https://jellyfin.example.com",
		APIKey:    "test-api-key",
		UserID:    "test-user-id",
	}
	
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	
	client := New(cfg, logger)
	ctx := context.Background()
	
	serverInfo, err := client.GetServerInfo(ctx)
	if err != nil {
		t.Errorf("GetServerInfo failed: %v", err)
	}
	
	if serverInfo == nil {
		t.Fatal("Expected serverInfo to be non-nil")
	}
	
	if serverInfo.Name == "" {
		t.Error("Expected server name to be set")
	}
	
	if serverInfo.Version == "" {
		t.Error("Expected server version to be set")
	}
}

func TestClientIsConnected(t *testing.T) {
	cfg := &config.JellyfinConfig{
		ServerURL: "https://jellyfin.example.com",
		APIKey:    "test-api-key",
		UserID:    "test-user-id",
	}
	
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	
	client := New(cfg, logger)
	
	// Initially not connected
	if client.IsConnected() {
		t.Error("Expected client to not be connected initially")
	}
	
	// Simulate connection
	client.sessionToken = "test-token"
	client.tokenExpiry = time.Now().Add(1 * time.Hour)
	
	if !client.IsConnected() {
		t.Error("Expected client to be connected after setting token")
	}
	
	// Simulate expired token
	client.tokenExpiry = time.Now().Add(-1 * time.Hour)
	
	if client.IsConnected() {
		t.Error("Expected client to not be connected with expired token")
	}
}

func TestClientDisconnect(t *testing.T) {
	cfg := &config.JellyfinConfig{
		ServerURL: "https://jellyfin.example.com",
		APIKey:    "test-api-key",
		UserID:    "test-user-id",
	}
	
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	
	client := New(cfg, logger)
	
	// Set up a mock session
	client.sessionToken = "test-token"
	client.tokenExpiry = time.Now().Add(1 * time.Hour)
	
	// Verify connected
	if !client.IsConnected() {
		t.Error("Expected client to be connected before disconnect")
	}
	
	// Disconnect
	client.Disconnect()
	
	// Verify disconnected
	if client.IsConnected() {
		t.Error("Expected client to be disconnected after Disconnect()")
	}
	
	if client.sessionToken != "" {
		t.Error("Expected sessionToken to be cleared after disconnect")
	}
}