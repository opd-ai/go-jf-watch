package downloader

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/opd-ai/go-jf-watch/internal/storage"
	"github.com/opd-ai/go-jf-watch/pkg/config"
)

func TestNew(t *testing.T) {
	cfg := &config.DownloadConfig{
		Workers:       3,
		RateLimitMbps: 10,
		RetryAttempts: 5,
		RetryDelay:    time.Second,
	}

	tmpDir := t.TempDir()
	storageConfig := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	storage, err := storage.NewManager(storageConfig, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	manager := New(cfg, storage, logger)

	if manager == nil {
		t.Fatal("Expected manager to be non-nil")
	}

	if manager.workers != cfg.Workers {
		t.Errorf("Expected %d workers, got %d", cfg.Workers, manager.workers)
	}

	if manager.config != cfg {
		t.Error("Expected config to be set correctly")
	}

	if manager.storage != storage {
		t.Error("Expected storage to be set correctly")
	}
}

func TestManagerStartStop(t *testing.T) {
	cfg := &config.DownloadConfig{
		Workers:       2,
		RateLimitMbps: 10,
		RetryAttempts: 3,
		RetryDelay:    time.Second,
	}

	tmpDir := t.TempDir()
	storageConfig := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	storage, err := storage.NewManager(storageConfig, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	manager := New(cfg, storage, logger)

	// Test start
	ctx := context.Background()
	err = manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}

	// Check running status
	status, err := manager.GetStatus()
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}

	running, ok := status["running"].(bool)
	if !ok || !running {
		t.Error("Expected manager to be running")
	}

	// Test double start (should fail)
	err = manager.Start(ctx)
	if err == nil {
		t.Error("Expected error when starting already running manager")
	}

	// Test stop
	err = manager.Stop()
	if err != nil {
		t.Fatalf("Failed to stop manager: %v", err)
	}

	// Check stopped status
	status, err = manager.GetStatus()
	if err != nil {
		t.Fatalf("Failed to get status after stop: %v", err)
	}

	running, ok = status["running"].(bool)
	if !ok || running {
		t.Error("Expected manager to be stopped")
	}
}

func TestAddJob(t *testing.T) {
	cfg := &config.DownloadConfig{
		Workers:       1,
		RateLimitMbps: 10,
		RetryAttempts: 3,
		RetryDelay:    time.Second,
	}

	tmpDir := t.TempDir()
	storageConfig := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	storage, err := storage.NewManager(storageConfig, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	manager := New(cfg, storage, logger)

	job := &DownloadJob{
		ID:        "test-job-1",
		MediaID:   "media-123",
		Priority:  1,
		URL:       "https://example.com/test.mkv",
		LocalPath: filepath.Join(tmpDir, "test.mkv"),
		CreatedAt: time.Now(),
	}

	err = manager.AddJob(job)
	if err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Verify job was stored in queue
	queueItems, err := storage.GetQueueItems("queued")
	if err != nil {
		t.Fatalf("Failed to get queue items: %v", err)
	}

	if len(queueItems) == 0 {
		t.Fatal("Expected at least one queue item")
	}

	queueItem := queueItems[0]
	if queueItem.ID != job.ID {
		t.Errorf("Expected job ID %s, got %s", job.ID, queueItem.ID)
	}

	if queueItem.Status != "queued" {
		t.Errorf("Expected status 'queued', got %s", queueItem.Status)
	}
}

func TestDownloadSuccessful(t *testing.T) {
	// Create test server
	testContent := "test file content for download"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testContent)))
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, testContent)
	}))
	defer server.Close()

	cfg := &config.DownloadConfig{
		Workers:       1,
		RateLimitMbps: 100, // High limit for test speed
		RetryAttempts: 3,
		RetryDelay:    100 * time.Millisecond,
	}

	tmpDir := t.TempDir()
	storageConfig := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	storage, err := storage.NewManager(storageConfig, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	manager := New(cfg, storage, logger)

	ctx := context.Background()
	err = manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	// Add download job
	localPath := filepath.Join(tmpDir, "downloaded-test.txt")
	job := &DownloadJob{
		ID:        "test-download-1",
		MediaID:   "media-456",
		Priority:  0, // High priority
		URL:       server.URL,
		LocalPath: localPath,
		CreatedAt: time.Now(),
	}

	err = manager.AddJob(job)
	if err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Wait for download to complete
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var downloadCompleted bool
	for !downloadCompleted {
		select {
		case <-timeout:
			t.Fatal("Download did not complete within timeout")
		case <-ticker.C:
			// Check if file exists
			if _, err := os.Stat(localPath); err == nil {
				downloadCompleted = true
			}
		}
	}

	// Verify file content
	content, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("Expected content %q, got %q", testContent, string(content))
	}

	// Verify download record was created
	record, err := storage.GetDownloadRecord("unknown", job.MediaID)
	if err != nil {
		t.Fatalf("Failed to get download record: %v", err)
	}

	if record.Status != "completed" {
		t.Errorf("Expected status 'completed', got %s", record.Status)
	}

	if record.Size != int64(len(testContent)) {
		t.Errorf("Expected size %d, got %d", len(testContent), record.Size)
	}
}

func TestDownloadRetry(t *testing.T) {
	// Create test server that fails first few requests
	requestCount := 0
	testContent := "retry test content"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount < 3 {
			// Fail first 2 requests
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Succeed on 3rd request
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testContent)))
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, testContent)
	}))
	defer server.Close()

	cfg := &config.DownloadConfig{
		Workers:       1,
		RateLimitMbps: 100,
		RetryAttempts: 5,
		RetryDelay:    100 * time.Millisecond,
	}

	tmpDir := t.TempDir()
	storageConfig := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	storage, err := storage.NewManager(storageConfig, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	manager := New(cfg, storage, logger)

	ctx := context.Background()
	err = manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	localPath := filepath.Join(tmpDir, "retry-test.txt")
	job := &DownloadJob{
		ID:        "test-retry-1",
		MediaID:   "media-retry",
		Priority:  0,
		URL:       server.URL,
		LocalPath: localPath,
		CreatedAt: time.Now(),
	}

	err = manager.AddJob(job)
	if err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Wait for download to eventually succeed
	timeout := time.After(15 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	var downloadCompleted bool
	for !downloadCompleted {
		select {
		case <-timeout:
			t.Fatal("Download with retry did not complete within timeout")
		case <-ticker.C:
			if _, err := os.Stat(localPath); err == nil {
				downloadCompleted = true
			}
		}
	}

	// Verify file content
	content, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("Expected content %q, got %q", testContent, string(content))
	}

	// Verify we made multiple requests (indicating retry)
	if requestCount < 3 {
		t.Errorf("Expected at least 3 requests (with retries), got %d", requestCount)
	}
}

func TestRateLimiting(t *testing.T) {
	// Create test server with large content
	testContent := strings.Repeat("A", 100*1024) // 100KB

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(testContent)))
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, testContent)
	}))
	defer server.Close()

	cfg := &config.DownloadConfig{
		Workers:       1,
		RateLimitMbps: 1, // Very low limit for testing
		RetryAttempts: 3,
		RetryDelay:    100 * time.Millisecond,
	}

	tmpDir := t.TempDir()
	storageConfig := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	storage, err := storage.NewManager(storageConfig, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	manager := New(cfg, storage, logger)

	ctx := context.Background()
	err = manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop()

	localPath := filepath.Join(tmpDir, "rate-limited-test.txt")
	job := &DownloadJob{
		ID:        "test-rate-limit-1",
		MediaID:   "media-rate",
		Priority:  0,
		URL:       server.URL,
		LocalPath: localPath,
		CreatedAt: time.Now(),
	}

	start := time.Now()

	err = manager.AddJob(job)
	if err != nil {
		t.Fatalf("Failed to add job: %v", err)
	}

	// Wait for download to complete
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var downloadCompleted bool
	for !downloadCompleted {
		select {
		case <-timeout:
			t.Fatal("Rate limited download did not complete within timeout")
		case <-ticker.C:
			if _, err := os.Stat(localPath); err == nil {
				downloadCompleted = true
			}
		}
	}

	duration := time.Since(start)

	// With 1 Mbps limit and 100KB file, it should take at least ~800ms
	// Allow some margin for test environment variability
	expectedMinDuration := 500 * time.Millisecond
	if duration < expectedMinDuration {
		t.Errorf("Download completed too quickly (%v), rate limiting may not be working", duration)
	}

	// Verify file was downloaded correctly
	content, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	if len(content) != len(testContent) {
		t.Errorf("Expected content length %d, got %d", len(testContent), len(content))
	}
}

func TestGetStatus(t *testing.T) {
	cfg := &config.DownloadConfig{
		Workers:       3,
		RateLimitMbps: 10,
		RetryAttempts: 5,
		RetryDelay:    time.Second,
	}

	tmpDir := t.TempDir()
	storageConfig := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	storage, err := storage.NewManager(storageConfig, logger)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer storage.Close()

	manager := New(cfg, storage, logger)

	// Test status before starting
	status, err := manager.GetStatus()
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}

	running, ok := status["running"].(bool)
	if !ok || running {
		t.Error("Expected manager to not be running initially")
	}

	workers, ok := status["workers"].(int)
	if !ok || workers != cfg.Workers {
		t.Errorf("Expected %d workers in status, got %v", cfg.Workers, workers)
	}

	rateLimit, ok := status["rate_limit"].(int)
	if !ok || rateLimit != cfg.RateLimitMbps {
		t.Errorf("Expected rate limit %d in status, got %v", cfg.RateLimitMbps, rateLimit)
	}
}
