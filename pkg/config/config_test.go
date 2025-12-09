package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name        string
		configYAML  string
		wantError   bool
		errorMatch  string
	}{
		{
			name: "valid minimal config",
			configYAML: `
jellyfin:
  server_url: "https://jellyfin.example.com"
  api_key: "test-api-key"
  user_id: "test-user-id"
`,
			wantError: false,
		},
		{
			name: "complete valid config",
			configYAML: `
jellyfin:
  server_url: "https://jellyfin.example.com"
  api_key: "test-api-key"
  user_id: "test-user-id"
  timeout: "45s"
  retry_attempts: 5

cache:
  directory: "./test-cache"
  max_size_gb: 100
  eviction_threshold: 0.9
  metadata_store: "boltdb"

download:
  workers: 5
  rate_limit_mbps: 20
  rate_limit_schedule:
    peak_hours: "08:00-22:00"
    peak_limit_percent: 50
  auto_download_current: true
  auto_download_next: false
  auto_download_count: 3

server:
  port: 9090
  host: "127.0.0.1"
  read_timeout: "30s"
  write_timeout: "30s"
  enable_compression: false

prediction:
  enabled: false
  sync_interval: "6h"
  history_days: 60
  min_confidence: 0.8

logging:
  level: "debug"
  format: "text"
  max_size_mb: 50

ui:
  theme: "dark"
  language: "en"
  video_quality_preference: "1080p"
`,
			wantError: false,
		},
		{
			name: "missing required jellyfin config",
			configYAML: `
cache:
  directory: "./test-cache"
`,
			wantError: true,
			errorMatch: "server_url is required",
		},
		{
			name: "invalid server URL",
			configYAML: `
jellyfin:
  server_url: "invalid-url"
  api_key: "test-api-key"
  user_id: "test-user-id"
`,
			wantError: true,
			errorMatch: "server_url must start with http",
		},
		{
			name: "invalid port",
			configYAML: `
jellyfin:
  server_url: "https://jellyfin.example.com"
  api_key: "test-api-key"
  user_id: "test-user-id"

server:
  port: 70000
`,
			wantError: true,
			errorMatch: "port must be between 1 and 65535",
		},
		{
			name: "invalid peak hours format",
			configYAML: `
jellyfin:
  server_url: "https://jellyfin.example.com"
  api_key: "test-api-key"
  user_id: "test-user-id"

download:
  rate_limit_schedule:
    peak_hours: "invalid-format"
`,
			wantError: true,
			errorMatch: "must be in format HH:MM-HH:MM",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary config file
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			
			if err := os.WriteFile(configPath, []byte(tt.configYAML), 0644); err != nil {
				t.Fatalf("Failed to write test config: %v", err)
			}

			// Load configuration
			cfg, err := Load(configPath)

			if tt.wantError {
				if err == nil {
					t.Fatal("Expected error but got none")
				}
				if tt.errorMatch != "" && !contains([]string{err.Error()}, tt.errorMatch) {
					t.Fatalf("Expected error containing '%s', got: %v", tt.errorMatch, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if cfg == nil {
				t.Fatal("Config is nil")
			}

			// Verify required fields are set
			if cfg.Jellyfin.ServerURL == "" {
				t.Error("ServerURL should not be empty")
			}
			if cfg.Jellyfin.APIKey == "" {
				t.Error("APIKey should not be empty")
			}
			if cfg.Jellyfin.UserID == "" {
				t.Error("UserID should not be empty")
			}
		})
	}
}

func TestApplyDefaults(t *testing.T) {
	config := &Config{}
	applyDefaults(config)

	// Test that defaults are applied correctly
	if config.Jellyfin.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", config.Jellyfin.Timeout)
	}

	if config.Cache.Directory != "./cache" {
		t.Errorf("Expected cache directory './cache', got %s", config.Cache.Directory)
	}

	if config.Cache.MaxSizeGB != 500 {
		t.Errorf("Expected max size 500GB, got %d", config.Cache.MaxSizeGB)
	}

	if config.Download.Workers != 3 {
		t.Errorf("Expected 3 workers, got %d", config.Download.Workers)
	}

	if config.Server.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", config.Server.Port)
	}

	if config.Logging.Level != "info" {
		t.Errorf("Expected log level 'info', got %s", config.Logging.Level)
	}
}

func TestLoggingConfigGetLogLevel(t *testing.T) {
	tests := []struct {
		level    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"invalid", slog.LevelInfo}, // Should default to info
		{"", slog.LevelInfo},        // Should default to info
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			cfg := LoggingConfig{Level: tt.level}
			if got := cfg.GetLogLevel(); got != tt.expected {
				t.Errorf("GetLogLevel() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCacheConfigCreateCacheDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	
	config := CacheConfig{
		Directory:     filepath.Join(tmpDir, "cache"),
		TempDirectory: filepath.Join(tmpDir, "cache", "temp"),
	}

	err := config.CreateCacheDirectories()
	if err != nil {
		t.Fatalf("CreateCacheDirectories failed: %v", err)
	}

	// Verify directories were created
	expectedDirs := []string{
		config.Directory,
		config.TempDirectory,
		filepath.Join(config.Directory, "movies"),
		filepath.Join(config.Directory, "series"),
	}

	for _, dir := range expectedDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("Directory %s was not created", dir)
		}
	}
}

func TestValidateJellyfin(t *testing.T) {
	tests := []struct {
		name        string
		config      JellyfinConfig
		wantError   bool
		errorMatch  string
	}{
		{
			name: "valid config",
			config: JellyfinConfig{
				ServerURL:     "https://jellyfin.example.com",
				APIKey:        "test-api-key",
				UserID:        "test-user-id",
				RetryAttempts: 3,
			},
			wantError: false,
		},
		{
			name: "missing server URL",
			config: JellyfinConfig{
				APIKey: "test-api-key",
				UserID: "test-user-id",
			},
			wantError:  true,
			errorMatch: "server_url is required",
		},
		{
			name: "invalid server URL",
			config: JellyfinConfig{
				ServerURL: "invalid-url",
				APIKey:    "test-api-key",
				UserID:    "test-user-id",
			},
			wantError:  true,
			errorMatch: "server_url must start with http",
		},
		{
			name: "missing API key",
			config: JellyfinConfig{
				ServerURL: "https://jellyfin.example.com",
				UserID:    "test-user-id",
			},
			wantError:  true,
			errorMatch: "api_key is required",
		},
		{
			name: "missing user ID",
			config: JellyfinConfig{
				ServerURL: "https://jellyfin.example.com",
				APIKey:    "test-api-key",
			},
			wantError:  true,
			errorMatch: "user_id is required",
		},
		{
			name: "invalid retry attempts",
			config: JellyfinConfig{
				ServerURL:     "https://jellyfin.example.com",
				APIKey:        "test-api-key",
				UserID:        "test-user-id",
				RetryAttempts: 15,
			},
			wantError:  true,
			errorMatch: "retry_attempts must be between 0 and 10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJellyfin(&tt.config)

			if tt.wantError {
				if err == nil {
					t.Fatal("Expected error but got none")
				}
				if tt.errorMatch != "" && !contains([]string{err.Error()}, tt.errorMatch) {
					t.Fatalf("Expected error containing '%s', got: %v", tt.errorMatch, err)
				}
			} else if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

func TestValidatePeakHours(t *testing.T) {
	tests := []struct {
		peakHours string
		wantError bool
	}{
		{"06:00-23:00", false},
		{"00:00-23:59", false},
		{"12:30-14:45", false},
		{"invalid", true},
		{"6:00-23:00", true},     // Invalid hour format
		{"06:60-23:00", true},    // Invalid minutes
		{"24:00-25:00", true},    // Invalid hours
		{"06:00", true},          // Missing end time
		{"06:00-", true},         // Incomplete format
		{"", true},               // Empty string
	}

	for _, tt := range tests {
		t.Run(tt.peakHours, func(t *testing.T) {
			err := validatePeakHours(tt.peakHours)
			if (err != nil) != tt.wantError {
				t.Errorf("validatePeakHours(%q) error = %v, wantError %v", tt.peakHours, err, tt.wantError)
			}
		})
	}
}

func TestValidateCache(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		config     CacheConfig
		wantError  bool
		errorMatch string
	}{
		{
			name: "valid config",
			config: CacheConfig{
				Directory:         tmpDir,
				MaxSizeGB:         100,
				EvictionThreshold: 0.85,
				MetadataStore:     "boltdb",
			},
			wantError: false,
		},
		{
			name: "empty directory",
			config: CacheConfig{
				MaxSizeGB:         100,
				EvictionThreshold: 0.85,
				MetadataStore:     "boltdb",
			},
			wantError:  true,
			errorMatch: "directory is required",
		},
		{
			name: "invalid max size",
			config: CacheConfig{
				Directory:         tmpDir,
				MaxSizeGB:         -1,
				EvictionThreshold: 0.85,
				MetadataStore:     "boltdb",
			},
			wantError:  true,
			errorMatch: "max_size_gb must be positive",
		},
		{
			name: "invalid eviction threshold",
			config: CacheConfig{
				Directory:         tmpDir,
				MaxSizeGB:         100,
				EvictionThreshold: 1.5,
				MetadataStore:     "boltdb",
			},
			wantError:  true,
			errorMatch: "eviction_threshold must be between 0 and 1",
		},
		{
			name: "invalid metadata store",
			config: CacheConfig{
				Directory:         tmpDir,
				MaxSizeGB:         100,
				EvictionThreshold: 0.85,
				MetadataStore:     "invalid",
			},
			wantError:  true,
			errorMatch: "metadata_store must be one of",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCache(&tt.config)

			if tt.wantError {
				if err == nil {
					t.Fatal("Expected error but got none")
				}
				if tt.errorMatch != "" && !contains([]string{err.Error()}, tt.errorMatch) {
					t.Fatalf("Expected error containing '%s', got: %v", tt.errorMatch, err)
				}
			} else if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}