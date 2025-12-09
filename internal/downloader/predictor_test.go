package downloader

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	
	"github.com/opd-ai/go-jf-watch/internal/storage"
	"github.com/opd-ai/go-jf-watch/pkg/config"
)

func TestNewPredictor(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during tests
	}))

	cfg := &config.PredictionConfig{
		Enabled:       true,
		SyncInterval:  4 * time.Hour,
		HistoryDays:   30,
		MinConfidence: 0.7,
	}

	tmpDir := t.TempDir()
	storageManager, err := storage.NewManager(tmpDir, 1000, logger)
	require.NoError(t, err)
	defer storageManager.Close()

	predictor := NewPredictor(storageManager, cfg, logger)

	assert.NotNil(t, predictor)
	assert.Equal(t, storageManager, predictor.storage)
	assert.Equal(t, cfg, predictor.config)
	assert.Equal(t, logger, predictor.logger)
	assert.NotNil(t, predictor.viewingHistory)
	assert.NotNil(t, predictor.preferences)
}

func TestOnPlaybackStart(t *testing.T) {
	tests := []struct {
		name      string
		mediaID   string
		setupMeta func(*storage.Manager) error
		wantErr   bool
	}{
		{
			name:    "successful episode playback",
			mediaID: "test-episode-123",
			setupMeta: func(sm *storage.Manager) error {
				metadata := &storage.MediaMetadata{
					ID:            "test-episode-123",
					JellyfinID:    "test-episode-123",
					Name:          "Test Episode",
					Type:          "episode",
					SeriesID:      "test-series-456",
					SeasonNumber:  1,
					EpisodeNumber: 5,
				}
				return sm.StoreMediaMetadata(metadata)
			},
			wantErr: false,
		},
		{
			name:    "successful movie playback",
			mediaID: "test-movie-789",
			setupMeta: func(sm *storage.Manager) error {
				metadata := &storage.MediaMetadata{
					ID:         "test-movie-789",
					JellyfinID: "test-movie-789",
					Name:       "Test Movie",
					Type:       "movie",
				}
				return sm.StoreMediaMetadata(metadata)
			},
			wantErr: false,
		},
		{
			name:      "metadata not found",
			mediaID:   "nonexistent-media",
			setupMeta: func(sm *storage.Manager) error { return nil },
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelError,
			}))

			predCfg := &config.PredictionConfig{
				Enabled:       true,
				SyncInterval:  4 * time.Hour,
				HistoryDays:   30,
				MinConfidence: 0.7,
			}

			tmpDir := t.TempDir()
			storageCfg := &config.CacheConfig{
				Directory:     tmpDir,
				MetadataStore: "boltdb",
			}
			storageManager, err := storage.NewManager(storageCfg, logger)
			require.NoError(t, err)
			defer storageManager.Close()

			// Setup test metadata
			err = tt.setupMeta(storageManager)
			require.NoError(t, err)

			predictor := NewPredictor(storageManager, predCfg, logger)
			ctx := context.Background()

			err = predictor.OnPlaybackStart(ctx, tt.mediaID)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify viewing session was recorded
				assert.Len(t, predictor.viewingHistory, 1)
				session := predictor.viewingHistory[0]
				assert.Equal(t, tt.mediaID, session.MediaID)
				assert.False(t, session.StartTime.IsZero())
			}
		})
	}
}

func TestPredictContinueWatching(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	cfg := &config.PredictionConfig{
		Enabled:       true,
		SyncInterval:  4 * time.Hour,
		HistoryDays:   30,
		MinConfidence: 0.5, // Lower threshold for testing
	}

	tmpDir := t.TempDir()
	storageCfg := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}
	storageManager, err := storage.NewManager(storageCfg, logger)
	require.NoError(t, err)
	defer storageManager.Close()

	predictor := NewPredictor(storageManager, cfg, logger)

	// Create viewing history for a series
	now := time.Now()
	predictor.viewingHistory = []ViewingSession{
		{
			MediaID:   "episode-S01E01",
			MediaType: "episode",
			SeriesID:  "series-123",
			Season:    1,
			Episode:   1,
			StartTime: now.AddDate(0, 0, -2),
			EndTime:   now.AddDate(0, 0, -2).Add(45 * time.Minute),
			Completed: true,
		},
		{
			MediaID:   "episode-S01E02",
			MediaType: "episode",
			SeriesID:  "series-123",
			Season:    1,
			Episode:   2,
			StartTime: now.AddDate(0, 0, -1),
			EndTime:   now.AddDate(0, 0, -1).Add(45 * time.Minute),
			Completed: true,
		},
	}

	predictions := predictor.predictContinueWatching()

	assert.Len(t, predictions, 1)
	prediction := predictions[0]
	assert.Equal(t, 1, prediction.Priority)
	assert.Equal(t, "series-123", prediction.SeriesID)
	assert.Equal(t, 1, prediction.Season)
	assert.Equal(t, 3, prediction.Episode)
	assert.Equal(t, "episode", prediction.MediaType)
	assert.Contains(t, prediction.Reason, "Next episode in partially watched series")
	assert.True(t, prediction.Confidence > 0)
}

func TestAnalyzeWatchingPatterns(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	cfg := &config.PredictionConfig{
		Enabled:       true,
		SyncInterval:  4 * time.Hour,
		HistoryDays:   30,
		MinConfidence: 0.7,
	}

	tmpDir := t.TempDir()
	storageCfg := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}
	storageManager, err := storage.NewManager(storageCfg, logger)
	require.NoError(t, err)
	defer storageManager.Close()

	predictor := NewPredictor(storageManager, cfg, logger)

	// Create viewing history showing binge-watching pattern
	now := time.Now()
	sameDay := now.Truncate(24 * time.Hour)
	
	predictor.viewingHistory = []ViewingSession{
		// Same day, same series - indicates binge watching
		{
			MediaID:   "episode-S01E01",
			MediaType: "episode",
			SeriesID:  "series-123",
			Season:    1,
			Episode:   1,
			StartTime: sameDay.Add(19 * time.Hour),
			EndTime:   sameDay.Add(19*time.Hour + 45*time.Minute),
			Completed: true,
		},
		{
			MediaID:   "episode-S01E02",
			MediaType: "episode",
			SeriesID:  "series-123",
			Season:    1,
			Episode:   2,
			StartTime: sameDay.Add(20 * time.Hour),
			EndTime:   sameDay.Add(20*time.Hour + 45*time.Minute),
			Completed: true,
		},
		{
			MediaID:   "episode-S01E03",
			MediaType: "episode",
			SeriesID:  "series-123",
			Season:    1,
			Episode:   3,
			StartTime: sameDay.Add(21 * time.Hour),
			EndTime:   sameDay.Add(21*time.Hour + 45*time.Minute),
			Completed: true,
		},
	}

	predictor.analyzeWatchingPatterns()

	patterns := predictor.preferences.WatchingPatterns
	
	// Should detect binge watching (3 episodes same day = 100% binge rate)
	assert.True(t, patterns.PrefersBingeWatching)
	
	// Should detect typical viewing time (19-21 hours)
	assert.Contains(t, patterns.PreferredStartTimes, 19)
	assert.Contains(t, patterns.PreferredStartTimes, 20)
	assert.Contains(t, patterns.PreferredStartTimes, 21)
	
	// Average session duration should be around 45 minutes
	expectedDuration := 45 * time.Minute
	assert.True(t, patterns.AverageSessionDuration >= expectedDuration-time.Minute)
	assert.True(t, patterns.AverageSessionDuration <= expectedDuration+time.Minute)
}

func TestCalculateMetrics(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	cfg := &config.PredictionConfig{
		Enabled:       true,
		SyncInterval:  4 * time.Hour,
		HistoryDays:   30,
		MinConfidence: 0.7,
	}

	tmpDir := t.TempDir()
	storageCfg := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}
	storageManager, err := storage.NewManager(storageCfg, logger)
	require.NoError(t, err)
	defer storageManager.Close()

	predictor := NewPredictor(storageManager, cfg, logger)

	// Create viewing history with mix of completed and incomplete sessions
	now := time.Now()
	day1 := now.Truncate(24 * time.Hour)
	day2 := day1.AddDate(0, 0, 1)
	
	predictor.viewingHistory = []ViewingSession{
		// Day 1: 2 episodes of series A
		{
			MediaID:   "episode-A-S01E01",
			MediaType: "episode",
			SeriesID:  "series-A",
			StartTime: day1.Add(20 * time.Hour),
			Completed: true,
		},
		{
			MediaID:   "episode-A-S01E02",
			MediaType: "episode",
			SeriesID:  "series-A",
			StartTime: day1.Add(21 * time.Hour),
			Completed: true,
		},
		// Day 2: 1 episode of series A (incomplete)
		{
			MediaID:   "episode-A-S01E03",
			MediaType: "episode",
			SeriesID:  "series-A",
			StartTime: day2.Add(20 * time.Hour),
			Completed: false,
		},
		// Day 2: 3 episodes of series B
		{
			MediaID:   "episode-B-S01E01",
			MediaType: "episode",
			SeriesID:  "series-B",
			StartTime: day2.Add(19 * time.Hour),
			Completed: true,
		},
		{
			MediaID:   "episode-B-S01E02",
			MediaType: "episode",
			SeriesID:  "series-B",
			StartTime: day2.Add(19*time.Hour + 45*time.Minute),
			Completed: true,
		},
		{
			MediaID:   "episode-B-S01E03",
			MediaType: "episode",
			SeriesID:  "series-B",
			StartTime: day2.Add(20*time.Hour + 30*time.Minute),
			Completed: true,
		},
	}

	predictor.calculateMetrics()

	// Completion rate: 5 completed out of 6 total = ~83%
	expectedCompletionRate := 5.0 / 6.0
	assert.InDelta(t, expectedCompletionRate, predictor.preferences.CompletionRate, 0.01)

	// Binge rate calculation:
	// Series A: 3 episodes over 2 days = 1.5 episodes/day
	// Series B: 3 episodes over 1 day = 3.0 episodes/day
	// Average: (1.5 + 3.0) / 2 = 2.25 episodes/day
	expectedBingeRate := 2.25
	assert.InDelta(t, expectedBingeRate, predictor.preferences.SeriesBingeRate, 0.1)
}

func TestCalculateContinueConfidence(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	cfg := &config.PredictionConfig{
		Enabled:       true,
		SyncInterval:  4 * time.Hour,
		HistoryDays:   30,
		MinConfidence: 0.7,
	}

	tmpDir := t.TempDir()
	storageCfg := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}
	storageManager, err := storage.NewManager(storageCfg, logger)
	require.NoError(t, err)
	defer storageManager.Close()

	predictor := NewPredictor(storageManager, cfg, logger)

	tests := []struct {
		name               string
		progress           ViewingProgress
		expectedConfidence float64
		tolerance          float64
	}{
		{
			name: "recent watching, high completion",
			progress: ViewingProgress{
				SeriesID:          "series-1",
				LastWatched:       time.Now().AddDate(0, 0, -2), // 2 days ago
				TotalWatched:      5,
				CompletedEpisodes: 5, // 100% completion
			},
			expectedConfidence: 1.0, // Should cap at 1.0
			tolerance:          0.0,
		},
		{
			name: "recent watching, medium completion",
			progress: ViewingProgress{
				SeriesID:          "series-2",
				LastWatched:       time.Now().AddDate(0, 0, -5), // 5 days ago
				TotalWatched:      4,
				CompletedEpisodes: 3, // 75% completion
			},
			expectedConfidence: 0.95, // 0.5 + 0.3 (recent) + 0.15 (completion) = 0.95
			tolerance:          0.05,
		},
		{
			name: "old watching, low completion",
			progress: ViewingProgress{
				SeriesID:          "series-3",
				LastWatched:       time.Now().AddDate(0, 0, -45), // 45 days ago
				TotalWatched:      2,
				CompletedEpisodes: 1, // 50% completion
			},
			expectedConfidence: 0.6, // 0.5 + 0.0 (old) + 0.1 (completion) = 0.6
			tolerance:          0.1,
		},
		{
			name: "many episodes watched",
			progress: ViewingProgress{
				SeriesID:          "series-4",
				LastWatched:       time.Now().AddDate(0, 0, -10), // 10 days ago
				TotalWatched:      8,                             // > 3 episodes
				CompletedEpisodes: 6,                             // 75% completion
			},
			expectedConfidence: 0.85, // 0.5 + 0.2 (medium recent) + 0.15 (completion) + 0.1 (many episodes) = 0.95, but capped
			tolerance:          0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			confidence := predictor.calculateContinueConfidence(tt.progress)
			assert.InDelta(t, tt.expectedConfidence, confidence, tt.tolerance)
			assert.True(t, confidence <= 1.0, "Confidence should not exceed 1.0")
			assert.True(t, confidence >= 0.0, "Confidence should not be negative")
		})
	}
}

func TestFilterPredictions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	cfg := &config.PredictionConfig{
		Enabled:       true,
		SyncInterval:  4 * time.Hour,
		HistoryDays:   30,
		MinConfidence: 0.7,
	}

	tmpDir := t.TempDir()
	storageCfg := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}
	storageManager, err := storage.NewManager(storageCfg, logger)
	require.NoError(t, err)
	defer storageManager.Close()

	predictor := NewPredictor(storageManager, cfg, logger)

	predictions := []PredictionResult{
		{MediaID: "high-priority-high-conf", Priority: 1, Confidence: 0.9},
		{MediaID: "high-priority-low-conf", Priority: 1, Confidence: 0.6},  // Should be filtered out
		{MediaID: "low-priority-high-conf", Priority: 3, Confidence: 0.8},
		{MediaID: "medium-priority-medium-conf", Priority: 2, Confidence: 0.75},
		{MediaID: "very-low-conf", Priority: 1, Confidence: 0.3}, // Should be filtered out
	}

	filtered := predictor.filterPredictions(predictions)

	// Should filter out predictions below min confidence (0.7)
	assert.Len(t, filtered, 3)

	// Should be sorted by priority, then confidence
	assert.Equal(t, "high-priority-high-conf", filtered[0].MediaID)
	assert.Equal(t, "medium-priority-medium-conf", filtered[1].MediaID)
	assert.Equal(t, "low-priority-high-conf", filtered[2].MediaID)

	// Verify all remaining predictions meet confidence threshold
	for _, pred := range filtered {
		assert.True(t, pred.Confidence >= cfg.MinConfidence)
	}
}

func TestPredictNext_EmptyHistory(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	cfg := &config.PredictionConfig{
		Enabled:       true,
		SyncInterval:  4 * time.Hour,
		HistoryDays:   30,
		MinConfidence: 0.7,
	}

	tmpDir := t.TempDir()
	storageCfg := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}
	storageManager, err := storage.NewManager(storageCfg, logger)
	require.NoError(t, err)
	defer storageManager.Close()

	predictor := NewPredictor(storageManager, cfg, logger)
	ctx := context.Background()

	predictions, err := predictor.PredictNext(ctx, "test-user")

	assert.NoError(t, err)
	assert.Empty(t, predictions) // No history should result in no predictions
}

// Helper function to create test storage manager
func createTestStorage(t *testing.T) *storage.Manager {
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
	
	// Cleanup will be handled by test cleanup
	t.Cleanup(func() {
		storageManager.Close()
	})

	return storageManager
}

// Benchmark prediction performance with realistic data
func BenchmarkPredictNext(b *testing.B) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	cfg := &config.PredictionConfig{
		Enabled:       true,
		SyncInterval:  4 * time.Hour,
		HistoryDays:   30,
		MinConfidence: 0.7,
	}

	tmpDir := b.TempDir()
	storageCfg := &config.CacheConfig{
		Directory:     tmpDir,
		MetadataStore: "boltdb",
	}
	storageManager, err := storage.NewManager(storageCfg, logger)
	require.NoError(b, err)
	defer storageManager.Close()

	predictor := NewPredictor(storageManager, cfg, logger)

	// Create realistic viewing history (100 sessions)
	now := time.Now()
	for i := 0; i < 100; i++ {
		session := ViewingSession{
			MediaID:   fmt.Sprintf("episode-%d", i),
			MediaType: "episode",
			SeriesID:  fmt.Sprintf("series-%d", i/10), // 10 episodes per series
			Season:    1,
			Episode:   (i % 10) + 1,
			StartTime: now.AddDate(0, 0, -i/3), // Spread over ~30 days
			EndTime:   now.AddDate(0, 0, -i/3).Add(45 * time.Minute),
			Completed: i%4 != 0, // 75% completion rate
		}
		predictor.viewingHistory = append(predictor.viewingHistory, session)
	}

	ctx := context.Background()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := predictor.PredictNext(ctx, "test-user")
		require.NoError(b, err)
	}
}