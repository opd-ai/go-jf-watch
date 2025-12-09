// Package config provides configuration management for go-jf-watch.
// It uses koanf for flexible configuration loading from YAML files with validation.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config holds the complete configuration for the go-jf-watch application.
// It represents the structure of config.yaml with validation rules for each section.
type Config struct {
	Jellyfin   JellyfinConfig   `koanf:"jellyfin"`
	Cache      CacheConfig      `koanf:"cache"`
	Download   DownloadConfig   `koanf:"download"`
	Server     ServerConfig     `koanf:"server"`
	Prediction PredictionConfig `koanf:"prediction"`
	Logging    LoggingConfig    `koanf:"logging"`
	UI         UIConfig         `koanf:"ui"`
}

// JellyfinConfig contains Jellyfin server connection and authentication settings.
type JellyfinConfig struct {
	ServerURL     string        `koanf:"server_url"`
	APIKey        string        `koanf:"api_key"`
	UserID        string        `koanf:"user_id"`
	Timeout       time.Duration `koanf:"timeout"`
	RetryAttempts int           `koanf:"retry_attempts"`
}

// CacheConfig defines cache storage settings and limits.
type CacheConfig struct {
	Directory         string  `koanf:"directory"`
	MaxSizeGB         int     `koanf:"max_size_gb"`
	EvictionThreshold float64 `koanf:"eviction_threshold"`
	MetadataStore     string  `koanf:"metadata_store"`
	TempDirectory     string  `koanf:"temp_directory"`
}

// DownloadConfig controls download behavior, rate limiting, and scheduling.
type DownloadConfig struct {
	Workers                 int                      `koanf:"workers"`
	RateLimitMbps          int                      `koanf:"rate_limit_mbps"`
	RateLimitSchedule      RateLimitScheduleConfig  `koanf:"rate_limit_schedule"`
	AutoDownloadCurrent    bool                     `koanf:"auto_download_current"`
	AutoDownloadNext       bool                     `koanf:"auto_download_next"`
	AutoDownloadCount      int                      `koanf:"auto_download_count"`
	CurrentEpisodePriority bool                     `koanf:"current_episode_priority"`
	RetryAttempts          int                      `koanf:"retry_attempts"`
	RetryDelay             time.Duration            `koanf:"retry_delay"`
}

// RateLimitScheduleConfig defines peak/off-peak bandwidth scheduling.
type RateLimitScheduleConfig struct {
	PeakHours        string `koanf:"peak_hours"`
	PeakLimitPercent int    `koanf:"peak_limit_percent"`
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Port              int           `koanf:"port"`
	Host              string        `koanf:"host"`
	ReadTimeout       time.Duration `koanf:"read_timeout"`
	WriteTimeout      time.Duration `koanf:"write_timeout"`
	EnableCompression bool          `koanf:"enable_compression"`
}

// PredictionConfig controls predictive download behavior.
type PredictionConfig struct {
	Enabled       bool          `koanf:"enabled"`
	SyncInterval  time.Duration `koanf:"sync_interval"`
	HistoryDays   int           `koanf:"history_days"`
	MinConfidence float64       `koanf:"min_confidence"`
}

// LoggingConfig defines logging behavior and output format.
type LoggingConfig struct {
	Level     string `koanf:"level"`
	Format    string `koanf:"format"`
	File      string `koanf:"file"`
	MaxSizeMB int    `koanf:"max_size_mb"`
}

// UIConfig contains user interface settings.
type UIConfig struct {
	Theme                    string `koanf:"theme"`
	Language                 string `koanf:"language"`
	VideoQualityPreference   string `koanf:"video_quality_preference"`
}

// Load reads configuration from the specified YAML file and applies validation.
// Returns a validated Config struct or an error if loading/validation fails.
func Load(configPath string) (*Config, error) {
	k := koanf.New(".")

	// Load configuration from YAML file
	if err := k.Load(file.Provider(configPath), yaml.Parser()); err != nil {
		return nil, fmt.Errorf("failed to load config from %s: %w", configPath, err)
	}

	var config Config
	if err := k.Unmarshal("", &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Apply defaults for missing values
	applyDefaults(&config)

	// Validate configuration
	if err := validate(&config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &config, nil
}

// applyDefaults sets sensible defaults for configuration values that weren't specified.
func applyDefaults(config *Config) {
	// Jellyfin defaults
	if config.Jellyfin.Timeout == 0 {
		config.Jellyfin.Timeout = 30 * time.Second
	}
	if config.Jellyfin.RetryAttempts == 0 {
		config.Jellyfin.RetryAttempts = 3
	}

	// Cache defaults
	if config.Cache.Directory == "" {
		config.Cache.Directory = "./cache"
	}
	if config.Cache.MaxSizeGB == 0 {
		config.Cache.MaxSizeGB = 500
	}
	if config.Cache.EvictionThreshold == 0 {
		config.Cache.EvictionThreshold = 0.85
	}
	if config.Cache.MetadataStore == "" {
		config.Cache.MetadataStore = "boltdb"
	}
	if config.Cache.TempDirectory == "" {
		config.Cache.TempDirectory = filepath.Join(config.Cache.Directory, "temp")
	}

	// Download defaults
	if config.Download.Workers == 0 {
		config.Download.Workers = 3
	}
	if config.Download.RateLimitMbps == 0 {
		config.Download.RateLimitMbps = 10
	}
	if config.Download.RateLimitSchedule.PeakHours == "" {
		config.Download.RateLimitSchedule.PeakHours = "06:00-23:00"
	}
	if config.Download.RateLimitSchedule.PeakLimitPercent == 0 {
		config.Download.RateLimitSchedule.PeakLimitPercent = 25
	}
	if config.Download.AutoDownloadCount == 0 {
		config.Download.AutoDownloadCount = 2
	}
	if config.Download.RetryAttempts == 0 {
		config.Download.RetryAttempts = 5
	}
	if config.Download.RetryDelay == 0 {
		config.Download.RetryDelay = 1 * time.Second
	}

	// Server defaults
	if config.Server.Port == 0 {
		config.Server.Port = 8080
	}
	if config.Server.Host == "" {
		config.Server.Host = "0.0.0.0"
	}
	if config.Server.ReadTimeout == 0 {
		config.Server.ReadTimeout = 15 * time.Second
	}
	if config.Server.WriteTimeout == 0 {
		config.Server.WriteTimeout = 15 * time.Second
	}

	// Prediction defaults
	if config.Prediction.SyncInterval == 0 {
		config.Prediction.SyncInterval = 4 * time.Hour
	}
	if config.Prediction.HistoryDays == 0 {
		config.Prediction.HistoryDays = 30
	}
	if config.Prediction.MinConfidence == 0 {
		config.Prediction.MinConfidence = 0.7
	}

	// Logging defaults
	if config.Logging.Level == "" {
		config.Logging.Level = "info"
	}
	if config.Logging.Format == "" {
		config.Logging.Format = "json"
	}
	if config.Logging.MaxSizeMB == 0 {
		config.Logging.MaxSizeMB = 100
	}

	// UI defaults
	if config.UI.Theme == "" {
		config.UI.Theme = "auto"
	}
	if config.UI.Language == "" {
		config.UI.Language = "en"
	}
	if config.UI.VideoQualityPreference == "" {
		config.UI.VideoQualityPreference = "original"
	}
}

// GetLogLevel converts the string log level to slog.Level.
// Returns slog.LevelInfo for invalid or unknown levels.
func (c *LoggingConfig) GetLogLevel() slog.Level {
	switch c.Level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// CreateCacheDirectories ensures that cache and temp directories exist.
// Creates directories with appropriate permissions if they don't exist.
func (c *CacheConfig) CreateCacheDirectories() error {
	directories := []string{
		c.Directory,
		c.TempDirectory,
		filepath.Join(c.Directory, "movies"),
		filepath.Join(c.Directory, "series"),
	}

	for _, dir := range directories {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	return nil
}