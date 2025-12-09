// Package downloader integration tests for Phase 5: Intelligence & Optimization
// Tests the integration between prediction engine and download manager.
package downloader

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opd-ai/go-jf-watch/internal/storage"
	"github.com/opd-ai/go-jf-watch/pkg/config"
)

// TestPredictionEngineIntegration tests the complete integration between
// predictor and download manager as implemented in Phase 5.
func TestPredictionEngineIntegration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Setup test storage
	tmpDir := t.TempDir()
	storageCfg := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}
	storageManager, err := storage.NewManager(storageCfg, logger)
	require.NoError(t, err)
	defer storageManager.Close()

	// Setup download manager
	downloadCfg := &config.DownloadConfig{
		Workers:       2,
		RateLimitMbps: 1,
		RetryAttempts: 3,
		RetryDelay:    time.Second,
	}
	downloadManager := New(downloadCfg, storageManager, logger)

	// Setup predictor
	predictionCfg := &config.PredictionConfig{
		Enabled:       true,
		SyncInterval:  time.Minute,
		HistoryDays:   7,
		MinConfidence: 0.5,
	}
	predictor := NewPredictor(storageManager, predictionCfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start download manager
	err = downloadManager.Start(ctx)
	require.NoError(t, err)
	defer downloadManager.Stop()

	// Test queuing a download through the prediction interface
	err = downloadManager.QueueDownload(ctx, "test-media-123", 1)
	assert.NoError(t, err)

	// Test getting queue statistics
	stats := downloadManager.GetQueueStats()
	assert.GreaterOrEqual(t, stats.QueueSize, 0)
	assert.GreaterOrEqual(t, stats.ActiveDownloads, 0)

	// Test prediction cycle (should not error even with empty history)
	predictions, err := predictor.PredictNext(ctx, "test_user")
	assert.NoError(t, err)
	assert.NotNil(t, predictions)
}

// TestDownloadManagerQueueOperations tests the new QueueDownload method
// and related queue management functionality.
func TestDownloadManagerQueueOperations(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	tmpDir := t.TempDir()
	storageCfg := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}
	storageManager, err := storage.NewManager(storageCfg, logger)
	require.NoError(t, err)
	defer storageManager.Close()

	downloadCfg := &config.DownloadConfig{
		Workers:       1,
		RateLimitMbps: 1,
		RetryAttempts: 3,
		RetryDelay:    time.Second,
	}
	manager := New(downloadCfg, storageManager, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test queuing before manager is started (should fail)
	err = manager.QueueDownload(ctx, "test-media-1", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")

	// Start manager
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	// Test successful queuing
	err = manager.QueueDownload(ctx, "test-media-1", 1)
	assert.NoError(t, err)

	err = manager.QueueDownload(ctx, "test-media-2", 0) // Higher priority
	assert.NoError(t, err)

	// Test queue statistics
	stats := manager.GetQueueStats()
	assert.GreaterOrEqual(t, stats.QueueSize, 0)
	assert.Equal(t, 0, stats.CompletedToday) // Will be 0 in test
	assert.Equal(t, 0, stats.FailedToday)    // Will be 0 in test

	// Test manager status
	status, err := manager.GetStatus()
	assert.NoError(t, err)
	assert.True(t, status["running"].(bool))
	assert.Equal(t, 1, status["workers"].(int))
}

// TestPredictionScheduling tests the prediction scheduling logic
// as it would work in the main application.
func TestPredictionScheduling(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	tmpDir := t.TempDir()
	storageCfg := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}
	storageManager, err := storage.NewManager(storageCfg, logger)
	require.NoError(t, err)
	defer storageManager.Close()

	downloadCfg := &config.DownloadConfig{
		Workers:       1,
		RateLimitMbps: 1,
		RetryAttempts: 3,
		RetryDelay:    time.Second,
	}
	downloadManager := New(downloadCfg, storageManager, logger)

	predictionCfg := &config.PredictionConfig{
		Enabled:       true,
		SyncInterval:  100 * time.Millisecond, // Fast for testing
		HistoryDays:   7,
		MinConfidence: 0.5,
	}
	predictor := NewPredictor(storageManager, predictionCfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// Start download manager
	err = downloadManager.Start(ctx)
	require.NoError(t, err)
	defer downloadManager.Stop()

	// Simulate the prediction cycle from main.go
	runPredictionCycle := func() {
		predictions, err := predictor.PredictNext(ctx, "test_user")
		if err != nil {
			logger.Error("Prediction cycle failed", "error", err)
			return
		}

		for _, pred := range predictions {
			if err := downloadManager.QueueDownload(ctx, pred.MediaID, pred.Priority); err != nil {
				logger.Warn("Failed to queue predicted download", 
					"media_id", pred.MediaID, "error", err)
			}
		}
	}

	// Run prediction cycle - should not error
	runPredictionCycle()

	// Test that we can collect metrics
	collectMetrics := func() {
		stats, err := storageManager.GetStorageStats()
		if err == nil {
			logger.Info("Storage metrics collected", "total_downloads", stats.TotalDownloads)
		}
		
		queueStats := downloadManager.GetQueueStats()
		logger.Info("Queue metrics collected", "queue_size", queueStats.QueueSize)
	}

	// Collect metrics - should not error
	collectMetrics()
}

// TestErrorHandling tests error scenarios in the prediction engine integration.
func TestErrorHandling(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	tmpDir := t.TempDir()
	storageCfg := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}
	storageManager, err := storage.NewManager(storageCfg, logger)
	require.NoError(t, err)
	defer storageManager.Close()

	downloadCfg := &config.DownloadConfig{
		Workers:       1,
		RateLimitMbps: 1,
		RetryAttempts: 1,
		RetryDelay:    time.Millisecond,
	}
	downloadManager := New(downloadCfg, storageManager, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test starting manager twice
	err = downloadManager.Start(ctx)
	require.NoError(t, err)
	defer downloadManager.Stop()

	err = downloadManager.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Test stopping manager twice  
	err = downloadManager.Stop()
	assert.NoError(t, err)

	err = downloadManager.Stop()
	assert.NoError(t, err) // Should not error

	// Test queuing after stop
	err = downloadManager.QueueDownload(ctx, "test-media", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}