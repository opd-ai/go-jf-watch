// Package storage provides persistent storage operations using BoltDB
// for metadata and queue management in go-jf-watch.
//
// Design Philosophy:
// - BoltDB chosen for ACID properties and embedded nature
// - Bucket organization mirrors application domains
// - Key patterns use composite keys for efficient queries
// - All operations are atomic and error-safe
package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"time"

	"go.etcd.io/bbolt"
	"github.com/opd-ai/go-jf-watch/pkg/config"
)

// Bucket names following the design specified in PLAN.md
var (
	bucketDownloads = []byte("downloads") // Downloaded items index
	bucketQueue     = []byte("queue")     // Active download queue
	bucketMetadata  = []byte("metadata")  // Media metadata cache
	bucketConfig    = []byte("config")    // Runtime configuration
	bucketStats     = []byte("stats")     // Usage statistics
)

// Manager handles all BoltDB operations with proper error handling and logging.
// It provides a clean interface for storage operations while abstracting
// the underlying BoltDB complexity.
type Manager struct {
	db     *bbolt.DB
	logger *slog.Logger
	config *config.CacheConfig
}

// DownloadRecord represents a completed download entry in the database.
// Key pattern: {media-type}:{jellyfin-id}
type DownloadRecord struct {
	ID           string    `json:"id"`
	MediaType    string    `json:"media_type"`    // movie, episode, series
	JellyfinID   string    `json:"jellyfin_id"`
	LocalPath    string    `json:"local_path"`
	Size         int64     `json:"size"`
	DownloadedAt time.Time `json:"downloaded_at"`
	LastAccessed time.Time `json:"last_accessed"`
	Priority     int       `json:"priority"`
	Checksum     string    `json:"checksum,omitempty"`
}

// QueueItem represents an active download queue entry.
// Key pattern: {priority}:{timestamp}:{id}
type QueueItem struct {
	ID           string    `json:"id"`
	MediaID      string    `json:"media_id"`
	Priority     int       `json:"priority"`
	URL          string    `json:"url"`
	LocalPath    string    `json:"local_path"`
	Size         int64     `json:"size"`
	Status       string    `json:"status"`        // pending, downloading, completed, failed
	Progress     float64   `json:"progress"`      // 0.0 to 1.0
	CreatedAt    time.Time `json:"created_at"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	ErrorMessage string    `json:"error_message,omitempty"`
	RetryCount   int       `json:"retry_count"`
}

// MediaMetadata represents cached Jellyfin media metadata.
// Key pattern: meta:{jellyfin-id}
type MediaMetadata struct {
	ID           string                 `json:"id"`
	JellyfinID   string                 `json:"jellyfin_id"`
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	SeriesID     string                 `json:"series_id,omitempty"`
	SeasonNumber int                    `json:"season_number,omitempty"`
	EpisodeNumber int                   `json:"episode_number,omitempty"`
	Overview     string                 `json:"overview,omitempty"`
	Genres       []string               `json:"genres,omitempty"`
	Size         int64                  `json:"size"`
	Container    string                 `json:"container"`
	LastSynced   time.Time              `json:"last_synced"`
	ExtraData    map[string]interface{} `json:"extra_data,omitempty"`
}

// StorageStats represents usage statistics for monitoring and capacity management.
type StorageStats struct {
	TotalDownloads   int           `json:"total_downloads"`
	TotalSize        int64         `json:"total_size_bytes"`
	DownloadsByType  map[string]int `json:"downloads_by_type"`
	OldestDownload   time.Time     `json:"oldest_download"`
	NewestDownload   time.Time     `json:"newest_download"`
	LastUpdated      time.Time     `json:"last_updated"`
}

// CacheStats represents cache statistics for system monitoring.
type CacheStats struct {
	TotalSizeBytes int64     `json:"total_size_bytes"`
	TotalItems     int       `json:"total_items"`
	LastSync       time.Time `json:"last_sync"`
	Size           int64     `json:"size"`           // Alias for TotalSizeBytes
	ItemCount      int       `json:"item_count"`     // Alias for TotalItems
}

// EpisodeInfo contains basic information about a TV episode.
// Used by predictor to find next episodes in a series.
type EpisodeInfo struct {
	ID      string `json:"id"`
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
	Name    string `json:"name"`
}

// ViewingSession represents a media viewing session for prediction analysis.
// This would be populated by syncing with Jellyfin playback activity.
type ViewingSession struct {
	MediaID      string    `json:"media_id"`
	MediaType    string    `json:"media_type"`    // "movie", "episode"
	SeriesID     string    `json:"series_id,omitempty"`
	Season       int       `json:"season,omitempty"`
	Episode      int       `json:"episode,omitempty"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	Duration     int64     `json:"duration"`      // Total duration in seconds
	WatchedTime  int64     `json:"watched_time"`  // Time actually watched
	Completed    bool      `json:"completed"`     // Watched >85% of content
	DeviceType   string    `json:"device_type,omitempty"`
	QualityLevel string    `json:"quality_level,omitempty"`
}

// NewManager creates a new storage manager with the given configuration.
// It initializes the BoltDB database and creates necessary buckets.
func NewManager(cfg *config.CacheConfig, logger *slog.Logger) (*Manager, error) {
	dbPath := filepath.Join(cfg.Directory, "go-jf-watch.db")
	
	db, err := bbolt.Open(dbPath, 0600, &bbolt.Options{
		Timeout: 1 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}

	manager := &Manager{
		db:     db,
		logger: logger,
		config: cfg,
	}

	// Initialize buckets
	if err := manager.initializeBuckets(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}

	logger.Info("Storage manager initialized", 
		"db_path", dbPath,
		"metadata_store", cfg.MetadataStore)

	return manager, nil
}

// initializeBuckets creates all required buckets if they don't exist.
func (m *Manager) initializeBuckets() error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		buckets := [][]byte{
			bucketDownloads,
			bucketQueue,
			bucketMetadata,
			bucketConfig,
			bucketStats,
		}

		for _, bucket := range buckets {
			if _, err := tx.CreateBucketIfNotExists(bucket); err != nil {
				return fmt.Errorf("failed to create bucket %s: %w", string(bucket), err)
			}
		}

		return nil
	})
}

// Close closes the database connection gracefully.
func (m *Manager) Close() error {
	m.logger.Info("Closing storage manager")
	return m.db.Close()
}

// AddDownloadRecord adds a completed download to the downloads bucket.
func (m *Manager) AddDownloadRecord(record *DownloadRecord) error {
	if record.ID == "" || record.JellyfinID == "" {
		return fmt.Errorf("download record must have ID and JellyfinID")
	}

	key := fmt.Sprintf("%s:%s", record.MediaType, record.JellyfinID)
	
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketDownloads)
		
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("failed to marshal download record: %w", err)
		}

		if err := bucket.Put([]byte(key), data); err != nil {
			return fmt.Errorf("failed to store download record: %w", err)
		}

		m.logger.Debug("Download record added",
			"key", key,
			"media_type", record.MediaType,
			"size_bytes", record.Size)

		return nil
	})
}

// GetDownloadRecord retrieves a download record by media type and Jellyfin ID.
func (m *Manager) GetDownloadRecord(mediaType, jellyfinID string) (*DownloadRecord, error) {
	key := fmt.Sprintf("%s:%s", mediaType, jellyfinID)
	
	var record DownloadRecord
	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketDownloads)
		data := bucket.Get([]byte(key))
		
		if data == nil {
			return fmt.Errorf("download record not found")
		}

		return json.Unmarshal(data, &record)
	})

	if err != nil {
		return nil, err
	}

	return &record, nil
}

// ListDownloadRecords returns all download records, optionally filtered by media type.
func (m *Manager) ListDownloadRecords(mediaType string) ([]*DownloadRecord, error) {
	var records []*DownloadRecord

	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketDownloads)
		
		return bucket.ForEach(func(k, v []byte) error {
			// Filter by media type if specified
			if mediaType != "" {
				keyStr := string(k)
				if len(keyStr) < len(mediaType)+1 || keyStr[:len(mediaType)] != mediaType {
					return nil // Skip this record
				}
			}

			var record DownloadRecord
			if err := json.Unmarshal(v, &record); err != nil {
				m.logger.Warn("Failed to unmarshal download record",
					"key", string(k),
					"error", err)
				return nil // Continue iteration, don't fail completely
			}

			records = append(records, &record)
			return nil
		})
	})

	return records, err
}

// AddQueueItem adds an item to the download queue.
func (m *Manager) AddQueueItem(item *QueueItem) error {
	if item.ID == "" || item.MediaID == "" {
		return fmt.Errorf("queue item must have ID and MediaID")
	}

	// Key pattern: {priority}:{timestamp}:{id} for efficient priority ordering
	key := fmt.Sprintf("%03d:%d:%s", item.Priority, item.CreatedAt.Unix(), item.ID)
	
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketQueue)
		
		data, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("failed to marshal queue item: %w", err)
		}

		if err := bucket.Put([]byte(key), data); err != nil {
			return fmt.Errorf("failed to store queue item: %w", err)
		}

		m.logger.Debug("Queue item added",
			"key", key,
			"priority", item.Priority,
			"status", item.Status)

		return nil
	})
}

// GetQueueItems returns queue items, ordered by priority and creation time.
func (m *Manager) GetQueueItems(status string) ([]*QueueItem, error) {
	var items []*QueueItem

	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketQueue)
		
		cursor := bucket.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var item QueueItem
			if err := json.Unmarshal(v, &item); err != nil {
				m.logger.Warn("Failed to unmarshal queue item",
					"key", string(k),
					"error", err)
				continue
			}

			// Filter by status if specified
			if status != "" && item.Status != status {
				continue
			}

			items = append(items, &item)
		}

		return nil
	})

	return items, err
}

// UpdateQueueItemStatus updates the status and progress of a queue item.
func (m *Manager) UpdateQueueItemStatus(itemID string, status string, progress float64, errorMsg string) error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketQueue)
		
		// Find the item by scanning for the ID in the key
		cursor := bucket.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var item QueueItem
			if err := json.Unmarshal(v, &item); err != nil {
				continue
			}

			if item.ID == itemID {
				// Update the item
				item.Status = status
				item.Progress = progress
				item.ErrorMessage = errorMsg
				
				if status == "downloading" && item.StartedAt.IsZero() {
					item.StartedAt = time.Now()
				}
				if status == "completed" || status == "failed" {
					item.CompletedAt = time.Now()
				}

				data, err := json.Marshal(&item)
				if err != nil {
					return fmt.Errorf("failed to marshal updated queue item: %w", err)
				}

				return bucket.Put(k, data)
			}
		}

		return fmt.Errorf("queue item with ID %s not found", itemID)
	})
}

// RemoveQueueItem removes an item from the download queue.
func (m *Manager) RemoveQueueItem(itemID string) error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketQueue)
		
		// Find and delete the item by scanning for the ID in the key
		cursor := bucket.Cursor()
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var item QueueItem
			if err := json.Unmarshal(v, &item); err != nil {
				continue
			}

			if item.ID == itemID {
				return bucket.Delete(k)
			}
		}

		return fmt.Errorf("queue item with ID %s not found", itemID)
	})
}

// AddMediaMetadata stores metadata for a media item.
func (m *Manager) AddMediaMetadata(metadata *MediaMetadata) error {
	if metadata.JellyfinID == "" {
		return fmt.Errorf("media metadata must have JellyfinID")
	}

	key := fmt.Sprintf("meta:%s", metadata.JellyfinID)
	
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketMetadata)
		
		data, err := json.Marshal(metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal media metadata: %w", err)
		}

		return bucket.Put([]byte(key), data)
	})
}

// GetStorageStats calculates and returns current storage statistics.
func (m *Manager) GetStorageStats() (*StorageStats, error) {
	stats := &StorageStats{
		DownloadsByType: make(map[string]int),
		LastUpdated:     time.Now(),
	}

	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketDownloads)
		
		return bucket.ForEach(func(k, v []byte) error {
			var record DownloadRecord
			if err := json.Unmarshal(v, &record); err != nil {
				return nil // Continue on marshal errors
			}

			stats.TotalDownloads++
			stats.TotalSize += record.Size
			stats.DownloadsByType[record.MediaType]++

			if stats.OldestDownload.IsZero() || record.DownloadedAt.Before(stats.OldestDownload) {
				stats.OldestDownload = record.DownloadedAt
			}
			if record.DownloadedAt.After(stats.NewestDownload) {
				stats.NewestDownload = record.DownloadedAt
			}

			return nil
		})
	})

	return stats, err
}

// GetMediaMetadata retrieves cached metadata for a media item.
// Used by the predictor to get series/episode information for predictions.
func (m *Manager) GetMediaMetadata(mediaID string) (*MediaMetadata, error) {
	var metadata MediaMetadata
	
	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketMetadata)
		if bucket == nil {
			return fmt.Errorf("metadata bucket not found")
		}

		key := fmt.Sprintf("meta:%s", mediaID)
		data := bucket.Get([]byte(key))
		if data == nil {
			return fmt.Errorf("metadata not found for media ID: %s", mediaID)
		}

		return json.Unmarshal(data, &metadata)
	})

	if err != nil {
		m.logger.Error("Failed to get media metadata", "media_id", mediaID, "error", err)
		return nil, err
	}

	return &metadata, nil
}

// GetSeriesEpisodes returns all episodes for a series and season.
// Used by predictor to find next episodes in sequence.
func (m *Manager) GetSeriesEpisodes(seriesID string, season int) ([]EpisodeInfo, error) {
	var episodes []EpisodeInfo
	
	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketMetadata)
		if bucket == nil {
			return fmt.Errorf("metadata bucket not found")
		}

		// Iterate through metadata to find episodes of this series/season
		cursor := bucket.Cursor()
		prefix := []byte("meta:")
		
		for k, v := cursor.Seek(prefix); k != nil && len(k) >= len(prefix); k, v = cursor.Next() {
			if !bytes.HasPrefix(k, prefix) {
				break
			}

			var metadata MediaMetadata
			if err := json.Unmarshal(v, &metadata); err != nil {
				continue // Skip invalid metadata
			}

			if metadata.Type == "episode" && 
			   metadata.SeriesID == seriesID && 
			   metadata.SeasonNumber == season {
				episodes = append(episodes, EpisodeInfo{
					ID:      metadata.ID,
					Season:  metadata.SeasonNumber,
					Episode: metadata.EpisodeNumber,
					Name:    metadata.Name,
				})
			}
		}

		return nil
	})

	if err != nil {
		m.logger.Error("Failed to get series episodes", 
			"series_id", seriesID, "season", season, "error", err)
		return nil, err
	}

	// Sort episodes by episode number
	sort.Slice(episodes, func(i, j int) bool {
		return episodes[i].Episode < episodes[j].Episode
	})

	return episodes, nil
}

// IsMediaCached checks if a media item is already downloaded and cached.
// Used by predictor to avoid queuing already cached content.
func (m *Manager) IsMediaCached(mediaID string) (bool, error) {
	var exists bool
	
	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketDownloads)
		if bucket == nil {
			return nil // No downloads yet
		}

		// Check for any download record with this media ID
		cursor := bucket.Cursor()
		
		for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
			var record DownloadRecord
			if err := json.Unmarshal(v, &record); err != nil {
				continue
			}

			if record.ID == mediaID || record.JellyfinID == mediaID {
				exists = true
				break
			}
		}

		return nil
	})

	return exists, err
}

// GetViewingHistory returns user viewing history for prediction analysis.
// This would be populated by syncing with Jellyfin viewing activity.
func (m *Manager) GetViewingHistory(userID string, days int) ([]ViewingSession, error) {
	var sessions []ViewingSession
	
	err := m.db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketStats)
		if bucket == nil {
			return nil // No viewing history yet
		}

		// Get viewing history for user (this would be stored by Jellyfin sync)
		key := fmt.Sprintf("history:%s", userID)
		data := bucket.Get([]byte(key))
		if data == nil {
			return nil // No history for this user
		}

		var allSessions []ViewingSession
		if err := json.Unmarshal(data, &allSessions); err != nil {
			return err
		}

		// Filter to last N days
		cutoff := time.Now().AddDate(0, 0, -days)
		for _, session := range allSessions {
			if session.StartTime.After(cutoff) {
				sessions = append(sessions, session)
			}
		}

		return nil
	})

	if err != nil {
		m.logger.Error("Failed to get viewing history", 
			"user_id", userID, "days", days, "error", err)
		return nil, err
	}

	return sessions, nil
}

// StoreViewingSession adds a viewing session to the history.
// Called when user starts/completes watching content.
func (m *Manager) StoreViewingSession(userID string, session ViewingSession) error {
	return m.db.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(bucketStats)
		if err != nil {
			return fmt.Errorf("failed to create stats bucket: %w", err)
		}

		// Get existing history
		key := fmt.Sprintf("history:%s", userID)
		var sessions []ViewingSession
		
		if data := bucket.Get([]byte(key)); data != nil {
			if err := json.Unmarshal(data, &sessions); err != nil {
				m.logger.Warn("Failed to unmarshal existing history", "error", err)
				sessions = []ViewingSession{} // Start fresh on error
			}
		}

		// Add new session
		sessions = append(sessions, session)

		// Keep only last 1000 sessions to prevent unbounded growth
		if len(sessions) > 1000 {
			sessions = sessions[len(sessions)-1000:]
		}

		// Store updated history
		data, err := json.Marshal(sessions)
		if err != nil {
			return fmt.Errorf("failed to marshal viewing history: %w", err)
		}

		return bucket.Put([]byte(key), data)
	})
}

// GetCacheStats returns cache statistics for system monitoring.
func (m *Manager) GetCacheStats() (*CacheStats, error) {
	var stats CacheStats
	
	err := m.db.View(func(tx *bbolt.Tx) error {
		// Get downloads bucket to calculate statistics
		bucket := tx.Bucket(bucketDownloads)
		if bucket == nil {
			// No downloads yet, return zero stats
			return nil
		}
		
		var totalSize int64
		var itemCount int
		
		// Iterate through all download records
		bucket.ForEach(func(k, v []byte) error {
			var record DownloadRecord
			if err := json.Unmarshal(v, &record); err != nil {
				m.logger.Warn("Failed to unmarshal download record", "key", string(k), "error", err)
				return nil // Continue iteration
			}
			
			totalSize += record.Size
			itemCount++
			return nil
		})
		
		stats.TotalSizeBytes = totalSize
		stats.TotalItems = itemCount
		stats.Size = totalSize // Alias
		stats.ItemCount = itemCount // Alias
		stats.LastSync = time.Now() // TODO: Track actual last sync time
		
		return nil
	})
	
	if err != nil {
		return nil, fmt.Errorf("failed to get cache stats: %w", err)
	}
	
	return &stats, nil
}