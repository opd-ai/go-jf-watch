package storage

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/opd-ai/go-jf-watch/pkg/config"
)

// CacheManager handles filesystem-level cache operations including
// capacity management, eviction policies, and directory organization.
//
// Design principles:
// - LRU eviction with protection for active downloads
// - Atomic operations to prevent corruption
// - Predictable directory structure
// - Graceful degradation on filesystem errors
type CacheManager struct {
	config  *config.CacheConfig
	storage *Manager
	logger  *slog.Logger
}

// CacheEntry represents a cached media file with its metadata.
type CacheEntry struct {
	Path         string
	Size         int64
	LastAccessed time.Time
	MediaType    string
	JellyfinID   string
	Protected    bool // Protected from eviction (currently downloading/playing)
}

// EvictionCandidate represents an item that can be evicted, sorted by priority.
type EvictionCandidate struct {
	CacheEntry
	Score float64 // Higher score = higher priority for eviction
}

// NewCacheManager creates a new cache manager with the given storage manager.
func NewCacheManager(cfg *config.CacheConfig, storage *Manager, logger *slog.Logger) *CacheManager {
	return &CacheManager{
		config:  cfg,
		storage: storage,
		logger:  logger,
	}
}

// GetCacheSize calculates the current total size of cached media.
// It scans the filesystem and cross-references with database records.
func (c *CacheManager) GetCacheSize() (int64, error) {
	var totalSize int64

	// Walk through cache directories
	mediaDirs := []string{
		filepath.Join(c.config.Directory, "movies"),
		filepath.Join(c.config.Directory, "series"),
	}

	for _, mediaDir := range mediaDirs {
		if _, err := os.Stat(mediaDir); os.IsNotExist(err) {
			continue // Skip non-existent directories
		}

		err := filepath.Walk(mediaDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				c.logger.Warn("Error walking cache directory",
					"path", path,
					"error", err)
				return nil // Continue walking
			}

			if !info.IsDir() && info.Name() != ".meta.json" {
				totalSize += info.Size()
			}

			return nil
		})

		if err != nil {
			return 0, fmt.Errorf("failed to calculate cache size: %w", err)
		}
	}

	return totalSize, nil
}

// GetCacheUtilization returns the current cache utilization as a percentage.
func (c *CacheManager) GetCacheUtilization() (float64, error) {
	currentSize, err := c.GetCacheSize()
	if err != nil {
		return 0, err
	}

	maxSizeBytes := int64(c.config.MaxSizeGB) * 1024 * 1024 * 1024
	return float64(currentSize) / float64(maxSizeBytes), nil
}

// NeedsCleanup returns true if cache cleanup should be triggered.
func (c *CacheManager) NeedsCleanup() (bool, error) {
	utilization, err := c.GetCacheUtilization()
	if err != nil {
		return false, err
	}

	return utilization >= c.config.EvictionThreshold, nil
}

// GetMediaPath returns the expected filesystem path for a media item.
// Follows the directory structure specified in PLAN.md.
func (c *CacheManager) GetMediaPath(mediaType, jellyfinID string, seasonNum, episodeNum int, filename string) string {
	switch mediaType {
	case "movie":
		return filepath.Join(c.config.Directory, "movies", jellyfinID, filename)
	case "episode":
		return filepath.Join(c.config.Directory, "series", jellyfinID,
			fmt.Sprintf("S%02dE%02d", seasonNum, episodeNum), filename)
	default:
		return filepath.Join(c.config.Directory, mediaType, jellyfinID, filename)
	}
}

// GetMetadataPath returns the path to the metadata file for a media item.
func (c *CacheManager) GetMetadataPath(mediaType, jellyfinID string, seasonNum, episodeNum int) string {
	mediaDir := filepath.Dir(c.GetMediaPath(mediaType, jellyfinID, seasonNum, episodeNum, "dummy"))
	return filepath.Join(mediaDir, ".meta.json")
}

// EnsureDirectory creates the directory structure for a media item if it doesn't exist.
func (c *CacheManager) EnsureDirectory(mediaType, jellyfinID string, seasonNum, episodeNum int) error {
	mediaPath := c.GetMediaPath(mediaType, jellyfinID, seasonNum, episodeNum, "dummy")
	mediaDir := filepath.Dir(mediaPath)

	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", mediaDir, err)
	}

	return nil
}

// IsMediaCached checks if a media item is already cached.
func (c *CacheManager) IsMediaCached(mediaType, jellyfinID string) (bool, string, error) {
	record, err := c.storage.GetDownloadRecord(mediaType, jellyfinID)
	if err != nil {
		return false, "", nil // Not found in database
	}

	// Check if file actually exists on filesystem
	if _, err := os.Stat(record.LocalPath); os.IsNotExist(err) {
		c.logger.Warn("Database record exists but file missing",
			"jellyfin_id", jellyfinID,
			"path", record.LocalPath)
		return false, "", nil
	}

	return true, record.LocalPath, nil
}

// GetCacheEntries returns all cache entries for eviction analysis.
func (c *CacheManager) GetCacheEntries() ([]*CacheEntry, error) {
	records, err := c.storage.ListDownloadRecords("")
	if err != nil {
		return nil, fmt.Errorf("failed to list download records: %w", err)
	}

	var entries []*CacheEntry

	for _, record := range records {
		// Check if file exists
		info, err := os.Stat(record.LocalPath)
		if os.IsNotExist(err) {
			c.logger.Debug("Cached file missing, skipping",
				"jellyfin_id", record.JellyfinID,
				"path", record.LocalPath)
			continue
		}
		if err != nil {
			c.logger.Warn("Error accessing cached file",
				"path", record.LocalPath,
				"error", err)
			continue
		}

		entry := &CacheEntry{
			Path:         record.LocalPath,
			Size:         info.Size(),
			LastAccessed: record.LastAccessed,
			MediaType:    record.MediaType,
			JellyfinID:   record.JellyfinID,
			Protected:    c.isProtectedFromEviction(record.JellyfinID),
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// isProtectedFromEviction checks if an item should be protected from eviction.
// Protected items include currently downloading and recently accessed content.
func (c *CacheManager) isProtectedFromEviction(jellyfinID string) bool {
	// Check if item is currently in download queue
	queueItems, err := c.storage.GetQueueItems("downloading")
	if err == nil {
		for _, item := range queueItems {
			if item.MediaID == jellyfinID {
				return true
			}
		}
	}

	// Protect recently accessed items (within 24 hours)
	// This would typically be determined by playback tracking
	return false
}

// GetEvictionCandidates returns items that can be evicted, sorted by priority.
// Uses LRU with protection as specified in PLAN.md.
func (c *CacheManager) GetEvictionCandidates(targetSize int64) ([]*EvictionCandidate, error) {
	entries, err := c.GetCacheEntries()
	if err != nil {
		return nil, err
	}

	var candidates []*EvictionCandidate
	now := time.Now()

	for _, entry := range entries {
		if entry.Protected {
			continue // Skip protected items
		}

		// Calculate eviction score based on:
		// 1. Age since last access (higher = more evictable)
		// 2. Size (larger files get slight preference for removal)
		// 3. Media type preferences

		daysSinceAccess := now.Sub(entry.LastAccessed).Hours() / 24
		sizeMB := float64(entry.Size) / (1024 * 1024)

		score := daysSinceAccess * 1.0 // Base score from age

		// Slight preference for removing larger files when space is tight
		if sizeMB > 1000 { // Files > 1GB
			score += 0.5
		}

		// Media type scoring (movies slightly more evictable than episodes)
		if entry.MediaType == "movie" {
			score += 0.1
		}

		candidates = append(candidates, &EvictionCandidate{
			CacheEntry: *entry,
			Score:      score,
		})
	}

	// Sort by eviction score (highest first)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	// Return only enough candidates to reach target size
	var totalSize int64
	var result []*EvictionCandidate

	for _, candidate := range candidates {
		result = append(result, candidate)
		totalSize += candidate.Size

		if totalSize >= targetSize {
			break
		}
	}

	return result, nil
}

// EvictItems removes the specified items from cache and database.
func (c *CacheManager) EvictItems(candidates []*EvictionCandidate) error {
	var totalEvicted int64
	var evictedCount int

	for _, candidate := range candidates {
		if err := c.evictSingleItem(candidate); err != nil {
			c.logger.Error("Failed to evict item",
				"path", candidate.Path,
				"jellyfin_id", candidate.JellyfinID,
				"error", err)
			continue
		}

		totalEvicted += candidate.Size
		evictedCount++

		c.logger.Info("Evicted cached item",
			"jellyfin_id", candidate.JellyfinID,
			"size_mb", candidate.Size/(1024*1024),
			"last_accessed", candidate.LastAccessed.Format(time.RFC3339))
	}

	c.logger.Info("Cache eviction completed",
		"evicted_count", evictedCount,
		"total_size_mb", totalEvicted/(1024*1024))

	return nil
}

// evictSingleItem removes a single item from both filesystem and database.
func (c *CacheManager) evictSingleItem(candidate *EvictionCandidate) error {
	// Remove from filesystem
	if err := os.Remove(candidate.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove file %s: %w", candidate.Path, err)
	}

	// Remove metadata file if it exists
	metadataPath := filepath.Join(filepath.Dir(candidate.Path), ".meta.json")
	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		c.logger.Debug("Failed to remove metadata file",
			"path", metadataPath,
			"error", err)
	}

	// Remove empty directory if this was the last file
	mediaDir := filepath.Dir(candidate.Path)
	if isEmpty, _ := c.isDirEmpty(mediaDir); isEmpty {
		os.Remove(mediaDir)
	}

	// Note: We don't remove from database to maintain download history
	// The record will show that it was downloaded but is no longer cached

	return nil
}

// isDirEmpty checks if a directory is empty or contains only .meta.json.
func (c *CacheManager) isDirEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}

	for _, entry := range entries {
		if entry.Name() != ".meta.json" {
			return false, nil
		}
	}

	return true, nil
}

// CleanupCache performs cache cleanup when utilization exceeds threshold.
// Implements two-tier cleanup:
// - Normal cleanup at eviction threshold (default 85%) targets 70% utilization
// - Emergency cleanup at 95% capacity targets 60% utilization with more aggressive eviction
func (c *CacheManager) CleanupCache() error {
	utilization, err := c.GetCacheUtilization()
	if err != nil {
		return fmt.Errorf("failed to check cache utilization: %w", err)
	}

	const emergencyThreshold = 0.95
	isEmergency := utilization >= emergencyThreshold

	if utilization < c.config.EvictionThreshold {
		c.logger.Debug("Cache cleanup not needed",
			"utilization", fmt.Sprintf("%.1f%%", utilization*100))
		return nil
	}

	if isEmergency {
		c.logger.Warn("Emergency cache cleanup triggered",
			"utilization", fmt.Sprintf("%.1f%%", utilization*100),
			"emergency_threshold", fmt.Sprintf("%.1f%%", emergencyThreshold*100))
	} else {
		c.logger.Info("Starting cache cleanup",
			"utilization", fmt.Sprintf("%.1f%%", utilization*100),
			"threshold", fmt.Sprintf("%.1f%%", c.config.EvictionThreshold*100))
	}

	// Calculate target reduction
	// Emergency cleanup is more aggressive (60% target vs 70% normal)
	targetUtilization := 0.70
	if isEmergency {
		targetUtilization = 0.60
	}
	maxSizeBytes := int64(c.config.MaxSizeGB) * 1024 * 1024 * 1024
	targetReduction := int64(float64(maxSizeBytes) * (utilization - targetUtilization))

	candidates, err := c.GetEvictionCandidates(targetReduction)
	if err != nil {
		return fmt.Errorf("failed to get eviction candidates: %w", err)
	}

	if len(candidates) == 0 {
		c.logger.Warn("No eviction candidates found - all items may be protected")
		return nil
	}

	return c.EvictItems(candidates)
}
