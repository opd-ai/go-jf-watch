package storage

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opd-ai/go-jf-watch/pkg/config"
)

func TestNewManager(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.CacheConfig{
		Directory:     tempDir,
		MetadataStore: "boltdb",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during tests
	}))

	manager, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer manager.Close()

	if manager == nil {
		t.Fatal("Manager should not be nil")
	}

	// Verify database file was created
	dbPath := filepath.Join(tempDir, "go-jf-watch.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}
}

func TestAddAndGetDownloadRecord(t *testing.T) {
	tempDir := t.TempDir()
	manager := createTestManager(t, tempDir)
	defer manager.Close()

	record := &DownloadRecord{
		ID:           "test-id-1",
		MediaType:    "movie",
		JellyfinID:   "jellyfin-123",
		LocalPath:    "/cache/movies/jellyfin-123/movie.mkv",
		Size:         1024 * 1024 * 500, // 500MB
		DownloadedAt: time.Now(),
		LastAccessed: time.Now(),
		Priority:     1,
		Checksum:     "abcdef123456",
	}

	// Test adding record
	err := manager.AddDownloadRecord(record)
	if err != nil {
		t.Fatalf("Failed to add download record: %v", err)
	}

	// Test retrieving record
	retrieved, err := manager.GetDownloadRecord(record.MediaType, record.JellyfinID)
	if err != nil {
		t.Fatalf("Failed to get download record: %v", err)
	}

	if retrieved.ID != record.ID {
		t.Errorf("Expected ID %s, got %s", record.ID, retrieved.ID)
	}
	if retrieved.MediaType != record.MediaType {
		t.Errorf("Expected MediaType %s, got %s", record.MediaType, retrieved.MediaType)
	}
	if retrieved.JellyfinID != record.JellyfinID {
		t.Errorf("Expected JellyfinID %s, got %s", record.JellyfinID, retrieved.JellyfinID)
	}
	if retrieved.Size != record.Size {
		t.Errorf("Expected Size %d, got %d", record.Size, retrieved.Size)
	}
}

func TestAddDownloadRecordValidation(t *testing.T) {
	tempDir := t.TempDir()
	manager := createTestManager(t, tempDir)
	defer manager.Close()

	tests := []struct {
		name        string
		record      *DownloadRecord
		expectError bool
	}{
		{
			name: "valid record",
			record: &DownloadRecord{
				ID:         "test-1",
				JellyfinID: "jellyfin-1",
				MediaType:  "movie",
			},
			expectError: false,
		},
		{
			name: "missing ID",
			record: &DownloadRecord{
				JellyfinID: "jellyfin-1",
				MediaType:  "movie",
			},
			expectError: true,
		},
		{
			name: "missing JellyfinID",
			record: &DownloadRecord{
				ID:        "test-1",
				MediaType: "movie",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.AddDownloadRecord(tt.record)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestListDownloadRecords(t *testing.T) {
	tempDir := t.TempDir()
	manager := createTestManager(t, tempDir)
	defer manager.Close()

	// Add test records
	records := []*DownloadRecord{
		{
			ID:         "movie-1",
			JellyfinID: "jellyfin-movie-1",
			MediaType:  "movie",
			Size:       1000,
		},
		{
			ID:         "episode-1",
			JellyfinID: "jellyfin-episode-1",
			MediaType:  "episode",
			Size:       2000,
		},
		{
			ID:         "movie-2",
			JellyfinID: "jellyfin-movie-2",
			MediaType:  "movie",
			Size:       3000,
		},
	}

	for _, record := range records {
		if err := manager.AddDownloadRecord(record); err != nil {
			t.Fatalf("Failed to add record: %v", err)
		}
	}

	// Test listing all records
	allRecords, err := manager.ListDownloadRecords("")
	if err != nil {
		t.Fatalf("Failed to list all records: %v", err)
	}
	if len(allRecords) != 3 {
		t.Errorf("Expected 3 records, got %d", len(allRecords))
	}

	// Test filtering by media type
	movieRecords, err := manager.ListDownloadRecords("movie")
	if err != nil {
		t.Fatalf("Failed to list movie records: %v", err)
	}
	if len(movieRecords) != 2 {
		t.Errorf("Expected 2 movie records, got %d", len(movieRecords))
	}

	episodeRecords, err := manager.ListDownloadRecords("episode")
	if err != nil {
		t.Fatalf("Failed to list episode records: %v", err)
	}
	if len(episodeRecords) != 1 {
		t.Errorf("Expected 1 episode record, got %d", len(episodeRecords))
	}
}

func TestQueueOperations(t *testing.T) {
	tempDir := t.TempDir()
	manager := createTestManager(t, tempDir)
	defer manager.Close()

	now := time.Now()
	item := &QueueItem{
		ID:        "queue-test-1",
		MediaID:   "media-123",
		Priority:  1,
		URL:       "http://example.com/media.mkv",
		LocalPath: "/cache/media.mkv",
		Size:      1024 * 1024,
		Status:    "pending",
		Progress:  0.0,
		CreatedAt: now,
	}

	// Test adding queue item
	err := manager.AddQueueItem(item)
	if err != nil {
		t.Fatalf("Failed to add queue item: %v", err)
	}

	// Test getting queue items
	items, err := manager.GetQueueItems("")
	if err != nil {
		t.Fatalf("Failed to get queue items: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("Expected 1 queue item, got %d", len(items))
	}
	if items[0].ID != item.ID {
		t.Errorf("Expected ID %s, got %s", item.ID, items[0].ID)
	}

	// Test filtering by status
	pendingItems, err := manager.GetQueueItems("pending")
	if err != nil {
		t.Fatalf("Failed to get pending items: %v", err)
	}
	if len(pendingItems) != 1 {
		t.Errorf("Expected 1 pending item, got %d", len(pendingItems))
	}

	downloadingItems, err := manager.GetQueueItems("downloading")
	if err != nil {
		t.Fatalf("Failed to get downloading items: %v", err)
	}
	if len(downloadingItems) != 0 {
		t.Errorf("Expected 0 downloading items, got %d", len(downloadingItems))
	}

	// Test updating status
	err = manager.UpdateQueueItemStatus(item.ID, "downloading", 0.5, "")
	if err != nil {
		t.Fatalf("Failed to update queue item status: %v", err)
	}

	// Verify status update
	updatedItems, err := manager.GetQueueItems("downloading")
	if err != nil {
		t.Fatalf("Failed to get updated items: %v", err)
	}
	if len(updatedItems) != 1 {
		t.Fatalf("Expected 1 downloading item, got %d", len(updatedItems))
	}
	if updatedItems[0].Progress != 0.5 {
		t.Errorf("Expected progress 0.5, got %f", updatedItems[0].Progress)
	}

	// Test removing queue item
	err = manager.RemoveQueueItem(item.ID)
	if err != nil {
		t.Fatalf("Failed to remove queue item: %v", err)
	}

	// Verify removal
	finalItems, err := manager.GetQueueItems("")
	if err != nil {
		t.Fatalf("Failed to get final queue items: %v", err)
	}
	if len(finalItems) != 0 {
		t.Errorf("Expected 0 queue items after removal, got %d", len(finalItems))
	}
}

func TestQueueItemValidation(t *testing.T) {
	tempDir := t.TempDir()
	manager := createTestManager(t, tempDir)
	defer manager.Close()

	tests := []struct {
		name        string
		item        *QueueItem
		expectError bool
	}{
		{
			name: "valid item",
			item: &QueueItem{
				ID:        "test-1",
				MediaID:   "media-1",
				CreatedAt: time.Now(),
			},
			expectError: false,
		},
		{
			name: "missing ID",
			item: &QueueItem{
				MediaID:   "media-1",
				CreatedAt: time.Now(),
			},
			expectError: true,
		},
		{
			name: "missing MediaID",
			item: &QueueItem{
				ID:        "test-1",
				CreatedAt: time.Now(),
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.AddQueueItem(tt.item)
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestMediaMetadata(t *testing.T) {
	tempDir := t.TempDir()
	manager := createTestManager(t, tempDir)
	defer manager.Close()

	metadata := &MediaMetadata{
		ID:         "meta-test-1",
		JellyfinID: "jellyfin-meta-1",
		Name:       "Test Movie",
		Type:       "movie",
		Overview:   "A test movie for unit testing",
		Genres:     []string{"Action", "Adventure"},
		Size:       1024 * 1024 * 1024, // 1GB
		Container:  "mkv",
		LastSynced: time.Now(),
		ExtraData:  map[string]interface{}{"test": "value"},
	}

	err := manager.AddMediaMetadata(metadata)
	if err != nil {
		t.Fatalf("Failed to add media metadata: %v", err)
	}

	// Note: We don't have a GetMediaMetadata method in the current implementation,
	// but we can verify it was stored by checking the database directly
	// This would be expanded with a getter method in a real implementation
}

func TestStorageStats(t *testing.T) {
	tempDir := t.TempDir()
	manager := createTestManager(t, tempDir)
	defer manager.Close()

	// Add some test download records
	records := []*DownloadRecord{
		{
			ID:           "stats-movie-1",
			JellyfinID:   "jellyfin-movie-1",
			MediaType:    "movie",
			Size:         1000 * 1024 * 1024, // 1GB
			DownloadedAt: time.Now().Add(-24 * time.Hour),
		},
		{
			ID:           "stats-episode-1",
			JellyfinID:   "jellyfin-episode-1",
			MediaType:    "episode",
			Size:         500 * 1024 * 1024, // 500MB
			DownloadedAt: time.Now().Add(-12 * time.Hour),
		},
		{
			ID:           "stats-movie-2",
			JellyfinID:   "jellyfin-movie-2",
			MediaType:    "movie",
			Size:         800 * 1024 * 1024, // 800MB
			DownloadedAt: time.Now().Add(-6 * time.Hour),
		},
	}

	for _, record := range records {
		if err := manager.AddDownloadRecord(record); err != nil {
			t.Fatalf("Failed to add record for stats test: %v", err)
		}
	}

	stats, err := manager.GetStorageStats()
	if err != nil {
		t.Fatalf("Failed to get storage stats: %v", err)
	}

	if stats.TotalDownloads != 3 {
		t.Errorf("Expected 3 total downloads, got %d", stats.TotalDownloads)
	}

	expectedTotalSize := int64(1000+500+800) * 1024 * 1024
	if stats.TotalSize != expectedTotalSize {
		t.Errorf("Expected total size %d, got %d", expectedTotalSize, stats.TotalSize)
	}

	if stats.DownloadsByType["movie"] != 2 {
		t.Errorf("Expected 2 movie downloads, got %d", stats.DownloadsByType["movie"])
	}

	if stats.DownloadsByType["episode"] != 1 {
		t.Errorf("Expected 1 episode download, got %d", stats.DownloadsByType["episode"])
	}

	if stats.OldestDownload.After(stats.NewestDownload) {
		t.Error("Oldest download should be before newest download")
	}
}

func TestDatabaseConcurrency(t *testing.T) {
	tempDir := t.TempDir()
	manager := createTestManager(t, tempDir)
	defer manager.Close()

	// Test concurrent operations
	done := make(chan bool, 10)

	// Concurrent writers
	for i := 0; i < 5; i++ {
		go func(id int) {
			record := &DownloadRecord{
				ID:         fmt.Sprintf("concurrent-%d", id),
				JellyfinID: fmt.Sprintf("jellyfin-%d", id),
				MediaType:  "movie",
				Size:       int64(id * 1024),
			}
			manager.AddDownloadRecord(record)
			done <- true
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		go func() {
			manager.ListDownloadRecords("")
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all records were added
	records, err := manager.ListDownloadRecords("")
	if err != nil {
		t.Fatalf("Failed to list records after concurrent test: %v", err)
	}

	if len(records) != 5 {
		t.Errorf("Expected 5 records after concurrent operations, got %d", len(records))
	}
}

// Helper function to create a test manager
func createTestManager(t *testing.T, tempDir string) *Manager {
	cfg := &config.CacheConfig{
		Directory:     tempDir,
		MetadataStore: "boltdb",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during tests
	}))

	manager, err := NewManager(cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create test manager: %v", err)
	}

	return manager
}
