package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// validate performs comprehensive validation of the configuration.
// Returns an error describing the first validation failure found.
func validate(config *Config) error {
	if err := validateJellyfin(&config.Jellyfin); err != nil {
		return fmt.Errorf("jellyfin config: %w", err)
	}

	if err := validateCache(&config.Cache); err != nil {
		return fmt.Errorf("cache config: %w", err)
	}

	if err := validateDownload(&config.Download); err != nil {
		return fmt.Errorf("download config: %w", err)
	}

	if err := validateServer(&config.Server); err != nil {
		return fmt.Errorf("server config: %w", err)
	}

	if err := validatePrediction(&config.Prediction); err != nil {
		return fmt.Errorf("prediction config: %w", err)
	}

	if err := validateLogging(&config.Logging); err != nil {
		return fmt.Errorf("logging config: %w", err)
	}

	if err := validateUI(&config.UI); err != nil {
		return fmt.Errorf("ui config: %w", err)
	}

	return nil
}

// validateJellyfin validates Jellyfin server configuration.
func validateJellyfin(config *JellyfinConfig) error {
	if config.ServerURL == "" {
		return fmt.Errorf("server_url is required")
	}

	if !strings.HasPrefix(config.ServerURL, "http://") && !strings.HasPrefix(config.ServerURL, "https://") {
		return fmt.Errorf("server_url must start with http:// or https://")
	}

	if config.APIKey == "" {
		return fmt.Errorf("api_key is required")
	}

	if config.UserID == "" {
		return fmt.Errorf("user_id is required")
	}

	if config.RetryAttempts < 0 || config.RetryAttempts > 10 {
		return fmt.Errorf("retry_attempts must be between 0 and 10")
	}

	return nil
}

// validateCache validates cache configuration and directory permissions.
func validateCache(config *CacheConfig) error {
	if config.Directory == "" {
		return fmt.Errorf("directory is required")
	}

	// Check if directory exists or can be created
	if err := os.MkdirAll(config.Directory, 0755); err != nil {
		return fmt.Errorf("cannot create cache directory %s: %w", config.Directory, err)
	}

	// Check write permissions
	testFile := filepath.Join(config.Directory, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("cache directory %s is not writable: %w", config.Directory, err)
	}
	os.Remove(testFile) // Clean up test file

	if config.MaxSizeGB <= 0 {
		return fmt.Errorf("max_size_gb must be positive")
	}

	if config.EvictionThreshold <= 0 || config.EvictionThreshold > 1 {
		return fmt.Errorf("eviction_threshold must be between 0 and 1")
	}

	validStores := []string{"boltdb", "flatfile"}
	if !contains(validStores, config.MetadataStore) {
		return fmt.Errorf("metadata_store must be one of: %s", strings.Join(validStores, ", "))
	}

	return nil
}

// validateDownload validates download configuration.
func validateDownload(config *DownloadConfig) error {
	if config.Workers <= 0 || config.Workers > 10 {
		return fmt.Errorf("workers must be between 1 and 10")
	}

	if config.RateLimitMbps <= 0 {
		return fmt.Errorf("rate_limit_mbps must be positive")
	}

	if err := validatePeakHours(config.RateLimitSchedule.PeakHours); err != nil {
		return fmt.Errorf("peak_hours format invalid: %w", err)
	}

	if config.RateLimitSchedule.PeakLimitPercent <= 0 || config.RateLimitSchedule.PeakLimitPercent > 100 {
		return fmt.Errorf("peak_limit_percent must be between 1 and 100")
	}

	if config.AutoDownloadCount < 0 || config.AutoDownloadCount > 10 {
		return fmt.Errorf("auto_download_count must be between 0 and 10")
	}

	if config.RetryAttempts < 0 || config.RetryAttempts > 20 {
		return fmt.Errorf("retry_attempts must be between 0 and 20")
	}

	if config.RetryDelay < 100*time.Millisecond || config.RetryDelay > 60*time.Second {
		return fmt.Errorf("retry_delay must be between 100ms and 60s")
	}

	return nil
}

// validateServer validates HTTP server configuration.
func validateServer(config *ServerConfig) error {
	if config.Port <= 0 || config.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}

	if config.Host == "" {
		return fmt.Errorf("host cannot be empty")
	}

	return nil
}

// validatePrediction validates prediction configuration.
func validatePrediction(config *PredictionConfig) error {
	if config.HistoryDays <= 0 || config.HistoryDays > 365 {
		return fmt.Errorf("history_days must be between 1 and 365")
	}

	if config.MinConfidence < 0 || config.MinConfidence > 1 {
		return fmt.Errorf("min_confidence must be between 0 and 1")
	}

	return nil
}

// validateLogging validates logging configuration.
func validateLogging(config *LoggingConfig) error {
	validLevels := []string{"debug", "info", "warn", "error"}
	if !contains(validLevels, config.Level) {
		return fmt.Errorf("level must be one of: %s", strings.Join(validLevels, ", "))
	}

	validFormats := []string{"json", "text"}
	if !contains(validFormats, config.Format) {
		return fmt.Errorf("format must be one of: %s", strings.Join(validFormats, ", "))
	}

	if config.File != "" {
		// Check if log directory exists or can be created
		logDir := filepath.Dir(config.File)
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return fmt.Errorf("cannot create log directory %s: %w", logDir, err)
		}
	}

	if config.MaxSizeMB <= 0 {
		return fmt.Errorf("max_size_mb must be positive")
	}

	return nil
}

// validateUI validates user interface configuration.
func validateUI(config *UIConfig) error {
	validThemes := []string{"light", "dark", "auto"}
	if !contains(validThemes, config.Theme) {
		return fmt.Errorf("theme must be one of: %s", strings.Join(validThemes, ", "))
	}

	validQualities := []string{"original", "1080p", "720p", "480p"}
	if !contains(validQualities, config.VideoQualityPreference) {
		return fmt.Errorf("video_quality_preference must be one of: %s", strings.Join(validQualities, ", "))
	}

	return nil
}

// validatePeakHours validates the peak hours format (HH:MM-HH:MM).
func validatePeakHours(peakHours string) error {
	// Empty string is valid - disables peak hours feature
	if peakHours == "" {
		return nil
	}

	// Expected format: "06:00-23:00"
	pattern := `^([0-1][0-9]|2[0-3]):[0-5][0-9]-([0-1][0-9]|2[0-3]):[0-5][0-9]$`
	matched, err := regexp.MatchString(pattern, peakHours)
	if err != nil {
		return fmt.Errorf("regex compilation failed: %w", err)
	}

	if !matched {
		return fmt.Errorf("must be in format HH:MM-HH:MM (e.g., 06:00-23:00) or empty to disable")
	}

	return nil
}

// contains checks if a slice contains a specific string.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
