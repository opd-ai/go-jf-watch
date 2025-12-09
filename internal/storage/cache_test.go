package storage

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opd-ai/go-jf-watch/pkg/config"
)

func TestCacheManagerGetCacheSize(t *testing.T) {
	tempDir := t.TempDir()
	cacheManager := createTestCacheManager(t, tempDir)

	// Create test cache structure
	movieDir := filepath.Join(tempDir, "movies", "movie-1")
	seriesDir := filepath.Join(tempDir, "series", "series-1", "S01E01")

	os.MkdirAll(movieDir, 0755)
	os.MkdirAll(seriesDir, 0755)

	// Create test files
	movieFile := filepath.Join(movieDir, "movie.mkv")
	episodeFile := filepath.Join(seriesDir, "episode.mkv")
	metadataFile := filepath.Join(movieDir, ".meta.json")

	// Write files with known sizes
	movieData := make([]byte, 1024*1024)  // 1MB
	episodeData := make([]byte, 512*1024) // 512KB
	metadataData := []byte(`{"test": "metadata"}`)

	if err := os.WriteFile(movieFile, movieData, 0644); err != nil {
		t.Fatalf("Failed to write movie file: %v", err)
	}
	if err := os.WriteFile(episodeFile, episodeData, 0644); err != nil {
		t.Fatalf("Failed to write episode file: %v", err)
	}
	if err := os.WriteFile(metadataFile, metadataData, 0644); err != nil {
		t.Fatalf("Failed to write metadata file: %v", err)
	}

	size, err := cacheManager.GetCacheSize()
	if err != nil {
		t.Fatalf("Failed to get cache size: %v", err)
	}

	expectedSize := int64(len(movieData) + len(episodeData)) // Metadata files are excluded
	if size != expectedSize {
		t.Errorf("Expected cache size %d, got %d", expectedSize, size)
	}
}

func TestCacheManagerGetCacheUtilization(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.CacheConfig{
		Directory: tempDir,
		MaxSizeGB: 1, // 1GB max
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	storage := createTestManager(t, tempDir)
	defer storage.Close()

	cacheManager := NewCacheManager(cfg, storage, logger)

	// Create a test file (100MB)
	movieDir := filepath.Join(tempDir, "movies", "movie-1")
	os.MkdirAll(movieDir, 0755)

	movieFile := filepath.Join(movieDir, "movie.mkv")
	movieData := make([]byte, 100*1024*1024) // 100MB
	if err := os.WriteFile(movieFile, movieData, 0644); err != nil {
		t.Fatalf("Failed to write movie file: %v", err)
	}

	utilization, err := cacheManager.GetCacheUtilization()
	if err != nil {
		t.Fatalf("Failed to get cache utilization: %v", err)
	}

	// Should be approximately 0.1 (100MB / 1GB)
	expectedUtilization := 100.0 / 1024.0 // 100MB out of 1GB
	if utilization < expectedUtilization-0.01 || utilization > expectedUtilization+0.01 {
		t.Errorf("Expected utilization around %.3f, got %.3f", expectedUtilization, utilization)
	}
}

func TestCacheManagerNeedsCleanup(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.CacheConfig{
		Directory:         tempDir,
		MaxSizeGB:         1,   // 1GB max
		EvictionThreshold: 0.8, // 80% threshold
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	storage := createTestManager(t, tempDir)
	defer storage.Close()

	cacheManager := NewCacheManager(cfg, storage, logger)

	// Initially should not need cleanup
	needs, err := cacheManager.NeedsCleanup()
	if err != nil {
		t.Fatalf("Failed to check cleanup need: %v", err)
	}
	if needs {
		t.Error("Empty cache should not need cleanup")
	}

	// Create a large file that exceeds threshold
	movieDir := filepath.Join(tempDir, "movies", "movie-1")
	os.MkdirAll(movieDir, 0755)

	movieFile := filepath.Join(movieDir, "movie.mkv")
	movieData := make([]byte, 900*1024*1024) // 900MB (90% of 1GB)
	if err := os.WriteFile(movieFile, movieData, 0644); err != nil {
		t.Fatalf("Failed to write large movie file: %v", err)
	}

	// Now should need cleanup
	needs, err = cacheManager.NeedsCleanup()
	if err != nil {
		t.Fatalf("Failed to check cleanup need after adding large file: %v", err)
	}
	if !needs {
		t.Error("Cache exceeding threshold should need cleanup")
	}
}

func TestGetMediaPath(t *testing.T) {
	tempDir := t.TempDir()
	cacheManager := createTestCacheManager(t, tempDir)

	tests := []struct {
		name       string
		mediaType  string
		jellyfinID string
		seasonNum  int
		episodeNum int
		filename   string
		expected   string
	}{
		{
			name:       "movie path",
			mediaType:  "movie",
			jellyfinID: "movie-123",
			filename:   "movie.mkv",
			expected:   filepath.Join(tempDir, "movies", "movie-123", "movie.mkv"),
		},
		{
			name:       "episode path",
			mediaType:  "episode",
			jellyfinID: "series-456",
			seasonNum:  1,
			episodeNum: 5,
			filename:   "episode.mkv",
			expected:   filepath.Join(tempDir, "series", "series-456", "S01E05", "episode.mkv"),
		},
		{
			name:       "other media type",
			mediaType:  "music",
			jellyfinID: "album-789",
			filename:   "song.mp3",
			expected:   filepath.Join(tempDir, "music", "album-789", "song.mp3"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := cacheManager.GetMediaPath(tt.mediaType, tt.jellyfinID, tt.seasonNum, tt.episodeNum, tt.filename)
			if path != tt.expected {
				t.Errorf("Expected path %s, got %s", tt.expected, path)
			}
		})
	}
}

func TestCacheEnsureDirectory(t *testing.T) {
	tempDir := t.TempDir()
	cacheManager := createTestCacheManager(t, tempDir)

	// Test creating movie directory
	err := cacheManager.EnsureDirectory("movie", "movie-123", 0, 0)
	if err != nil {
		t.Fatalf("Failed to ensure movie directory: %v", err)
	}

	expectedDir := filepath.Join(tempDir, "movies", "movie-123")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("Movie directory %s was not created", expectedDir)
	}

	// Test creating episode directory
	err = cacheManager.EnsureDirectory("episode", "series-456", 2, 10)
	if err != nil {
		t.Fatalf("Failed to ensure episode directory: %v", err)
	}

	expectedDir = filepath.Join(tempDir, "series", "series-456", "S02E10")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("Episode directory %s was not created", expectedDir)
	}
}

func TestIsMediaCached(t *testing.T) {
	tempDir := t.TempDir()
	storage := createTestManager(t, tempDir)
	defer storage.Close()

	cacheManager := createTestCacheManagerWithStorage(t, tempDir, storage)

	// Test non-existent media
	cached, path, err := cacheManager.IsMediaCached("movie", "non-existent")
	if err != nil {
		t.Fatalf("Unexpected error checking non-existent media: %v", err)
	}
	if cached {
		t.Error("Non-existent media should not be cached")
	}

	// Add a download record
	moviePath := filepath.Join(tempDir, "movies", "movie-123", "movie.mkv")
	record := &DownloadRecord{
		ID:         "test-movie",
		MediaType:  "movie",
		JellyfinID: "movie-123",
		LocalPath:  moviePath,
		Size:       1024,
	}

	if err := storage.AddDownloadRecord(record); err != nil {
		t.Fatalf("Failed to add download record: %v", err)
	}

	// Create the actual file
	os.MkdirAll(filepath.Dir(moviePath), 0755)
	if err := os.WriteFile(moviePath, []byte("test data"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test existing media
	cached, path, err = cacheManager.IsMediaCached("movie", "movie-123")
	if err != nil {
		t.Fatalf("Unexpected error checking existing media: %v", err)
	}
	if !cached {
		t.Error("Existing media should be cached")
	}
	if path != moviePath {
		t.Errorf("Expected path %s, got %s", moviePath, path)
	}
}

func TestGetEvictionCandidates(t *testing.T) {
	tempDir := t.TempDir()
	storage := createTestManager(t, tempDir)
	defer storage.Close()

	cacheManager := createTestCacheManagerWithStorage(t, tempDir, storage)

	// Create test files and records
	testData := []struct {
		id           string
		mediaType    string
		size         int64
		lastAccessed time.Time
	}{
		{"old-movie", "movie", 1000, time.Now().Add(-48 * time.Hour)},     // Oldest
		{"new-movie", "movie", 2000, time.Now().Add(-1 * time.Hour)},      // Newest
		{"mid-episode", "episode", 1500, time.Now().Add(-24 * time.Hour)}, // Middle
	}

	for _, td := range testData {
		// Create file
		var mediaPath string
		if td.mediaType == "movie" {
			mediaPath = filepath.Join(tempDir, "movies", td.id, "video.mkv")
		} else {
			mediaPath = filepath.Join(tempDir, "series", td.id, "S01E01", "video.mkv")
		}

		os.MkdirAll(filepath.Dir(mediaPath), 0755)
		data := make([]byte, td.size)
		if err := os.WriteFile(mediaPath, data, 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", mediaPath, err)
		}

		// Create download record
		record := &DownloadRecord{
			ID:           td.id,
			MediaType:    td.mediaType,
			JellyfinID:   td.id,
			LocalPath:    mediaPath,
			Size:         td.size,
			LastAccessed: td.lastAccessed,
		}

		if err := storage.AddDownloadRecord(record); err != nil {
			t.Fatalf("Failed to add download record: %v", err)
		}
	}

	// Get eviction candidates
	candidates, err := cacheManager.GetEvictionCandidates(3000) // Target 3KB
	if err != nil {
		t.Fatalf("Failed to get eviction candidates: %v", err)
	}

	if len(candidates) == 0 {
		t.Fatal("Expected eviction candidates but got none")
	}

	// First candidate should be the oldest (highest score)
	if candidates[0].JellyfinID != "old-movie" {
		t.Errorf("Expected first candidate to be old-movie, got %s", candidates[0].JellyfinID)
	}

	// Verify candidates are sorted by score (descending)
	for i := 1; i < len(candidates); i++ {
		if candidates[i-1].Score < candidates[i].Score {
			t.Errorf("Candidates not sorted by score: %f < %f", candidates[i-1].Score, candidates[i].Score)
		}
	}
}

func TestCleanupCache(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.CacheConfig{
		Directory:         tempDir,
		MaxSizeGB:         1,   // 1GB
		EvictionThreshold: 0.5, // 50% for easier testing
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	storage := createTestManager(t, tempDir)
	defer storage.Close()

	cacheManager := NewCacheManager(cfg, storage, logger)

	// Create files that don't exceed threshold
	smallFile := filepath.Join(tempDir, "movies", "small-movie", "video.mkv")
	os.MkdirAll(filepath.Dir(smallFile), 0755)
	smallData := make([]byte, 100*1024*1024) // 100MB
	if err := os.WriteFile(smallFile, smallData, 0644); err != nil {
		t.Fatalf("Failed to create small file: %v", err)
	}

	// Cleanup should not be needed
	err := cacheManager.CleanupCache()
	if err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// File should still exist
	if _, err := os.Stat(smallFile); os.IsNotExist(err) {
		t.Error("Small file was incorrectly removed during cleanup")
	}
}

// Helper functions
func createTestCacheManager(t *testing.T, tempDir string) *CacheManager {
	cfg := &config.CacheConfig{
		Directory:         tempDir,
		MaxSizeGB:         10,
		EvictionThreshold: 0.85,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	storage := createTestManager(t, tempDir)
	t.Cleanup(func() { storage.Close() })

	return NewCacheManager(cfg, storage, logger)
}

func createTestCacheManagerWithStorage(t *testing.T, tempDir string, storage *Manager) *CacheManager {
	cfg := &config.CacheConfig{
		Directory:         tempDir,
		MaxSizeGB:         10,
		EvictionThreshold: 0.85,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	return NewCacheManager(cfg, storage, logger)
}
