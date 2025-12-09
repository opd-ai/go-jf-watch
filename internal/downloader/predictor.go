// Package downloader implements viewing pattern analysis and predictive logic
// for intelligent media pre-caching based on user behavior patterns.
//
// The predictor analyzes Jellyfin viewing history to automatically queue
// likely-next media for download, following the priority system:
// - Priority 0: Currently playing (immediate download)
// - Priority 1: Next unwatched episode in active series
// - Priority 2: Following 2-3 episodes in sequence
// - Priority 3: New items matching viewing history
// - Priority 4: Popular items in preferred genres
package downloader

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/opd-ai/go-jf-watch/internal/storage"
	"github.com/opd-ai/go-jf-watch/pkg/config"
)

// Predictor analyzes viewing patterns and predicts next media to download.
// It maintains viewing history and user preferences to make intelligent
// predictions about what content should be pre-cached.
type Predictor struct {
	storage         *storage.Manager
	logger          *slog.Logger
	config          *config.PredictionConfig
	downloadManager DownloadQueuer
	
	// Cached analysis data
	viewingHistory []ViewingSession
	preferences    UserPreferences
	lastSync       time.Time
}

// DownloadQueuer interface for queueing downloads (implemented by Manager)
type DownloadQueuer interface {
	QueueDownload(ctx context.Context, mediaID string, priority int) error
}

// ViewingSession represents a single media viewing session with metadata.
// Used to track user behavior patterns for prediction analysis.
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

// UserPreferences holds analyzed user preferences for prediction.
// Built from viewing history analysis to understand user patterns.
type UserPreferences struct {
	PreferredGenres     []string           `json:"preferred_genres"`
	PreferredLanguages  []string           `json:"preferred_languages"`
	WatchingPatterns    WatchingPattern    `json:"watching_patterns"`
	SeriesBingeRate     float64           `json:"series_binge_rate"`    // Episodes per day
	PreferredViewTimes  []TimeWindow      `json:"preferred_view_times"` // When user watches
	CompletionRate      float64           `json:"completion_rate"`      // % of content finished
	LastUpdated         time.Time         `json:"last_updated"`
}

// WatchingPattern describes user viewing behavior patterns.
type WatchingPattern struct {
	AverageSessionDuration time.Duration `json:"average_session_duration"`
	PrefersBingeWatching   bool         `json:"prefers_binge_watching"`
	TypicalViewingDays     []time.Weekday `json:"typical_viewing_days"`
	PreferredStartTimes    []int         `json:"preferred_start_times"` // Hours 0-23
}

// TimeWindow represents a time period when user typically watches content.
type TimeWindow struct {
	StartHour int `json:"start_hour"` // 0-23
	EndHour   int `json:"end_hour"`   // 0-23
	Frequency int `json:"frequency"`  // How often this window is used
}

// PredictionResult contains a predicted download recommendation.
type PredictionResult struct {
	MediaID     string  `json:"media_id"`
	Priority    int     `json:"priority"`     // 0-5 priority level
	Confidence  float64 `json:"confidence"`   // 0.0-1.0 prediction confidence
	Reason      string  `json:"reason"`       // Human-readable reason
	SeriesID    string  `json:"series_id,omitempty"`
	Season      int     `json:"season,omitempty"`
	Episode     int     `json:"episode,omitempty"`
	MediaType   string  `json:"media_type"`
	EstimatedSize int64 `json:"estimated_size,omitempty"`
}

// NewPredictor creates a new viewing pattern predictor instance.
// Initializes with empty preferences that will be built from viewing history.
func NewPredictor(storage *storage.Manager, config *config.PredictionConfig, logger *slog.Logger) *Predictor {
	return &Predictor{
		storage:        storage,
		logger:         logger,
		config:         config,
		viewingHistory: make([]ViewingSession, 0),
		preferences:    UserPreferences{
			PreferredGenres:    make([]string, 0),
			PreferredLanguages: make([]string, 0),
			PreferredViewTimes: make([]TimeWindow, 0),
		},
	}
}

// SetDownloadManager sets the download manager for queueing predicted downloads
func (p *Predictor) SetDownloadManager(dm DownloadQueuer) {
	p.downloadManager = dm
}

// OnPlaybackStart handles immediate prediction when user starts watching content.
// This triggers Priority 0 (currently playing) download and queues next episode.
func (p *Predictor) OnPlaybackStart(ctx context.Context, mediaID string) error {
	p.logger.Info("Playback started, triggering immediate predictions", 
		"media_id", mediaID)

	// Record viewing session start
	session := ViewingSession{
		MediaID:   mediaID,
		StartTime: time.Now(),
	}

	// Get media metadata from storage to determine type and series info
	metadata, err := p.storage.GetMediaMetadata(mediaID)
	if err != nil {
		p.logger.Warn("Failed to get media metadata for playback prediction",
			"media_id", mediaID, "error", err)
		return fmt.Errorf("failed to get metadata: %w", err)
	}

	session.MediaType = metadata.Type
	session.SeriesID = metadata.SeriesID
	session.Season = metadata.Season
	session.Episode = metadata.Episode

	// Add to viewing history for future analysis
	p.viewingHistory = append(p.viewingHistory, session)

	// If this is a TV series episode, predict next episode(s)
	if session.MediaType == "episode" && session.SeriesID != "" {
		return p.predictNextEpisodes(ctx, session.SeriesID, session.Season, session.Episode)
	}

	return nil
}

// PredictNext analyzes viewing patterns and returns download recommendations.
// This is the main prediction method called periodically for proactive caching.
func (p *Predictor) PredictNext(ctx context.Context, userID string) ([]PredictionResult, error) {
	p.logger.Debug("Starting prediction analysis", "user_id", userID)

	// Refresh viewing history if needed
	if time.Since(p.lastSync) > p.config.SyncInterval {
		if err := p.refreshViewingHistory(ctx, userID); err != nil {
			p.logger.Error("Failed to refresh viewing history", "error", err)
			return nil, fmt.Errorf("failed to refresh history: %w", err)
		}
	}

	// Update user preferences from recent history
	if err := p.updatePreferences(); err != nil {
		p.logger.Warn("Failed to update preferences", "error", err)
	}

	var predictions []PredictionResult

	// Priority 1: Continue watching - next episodes in active series
	continuePredictions := p.predictContinueWatching()
	predictions = append(predictions, continuePredictions...)

	// Priority 2: Up next - following episodes in sequence
	upNextPredictions := p.predictUpNext()
	predictions = append(predictions, upNextPredictions...)

	// Priority 3: Recently added content matching preferences
	recentPredictions := p.predictRecentlyAdded()
	predictions = append(predictions, recentPredictions...)

	// Priority 4: Trending content in preferred genres
	trendingPredictions := p.predictTrending()
	predictions = append(predictions, trendingPredictions...)

	// Filter by confidence threshold and limit results
	predictions = p.filterPredictions(predictions)

	p.logger.Info("Prediction analysis complete", 
		"total_predictions", len(predictions),
		"user_id", userID)

	return predictions, nil
}

// predictNextEpisodes predicts the next episode(s) in a series after playback starts.
// This is called immediately when user starts watching to queue Priority 1 content.
func (p *Predictor) predictNextEpisodes(ctx context.Context, seriesID string, currentSeason, currentEpisode int) error {
	p.logger.Debug("Predicting next episodes", 
		"series_id", seriesID, 
		"season", currentSeason, 
		"episode", currentEpisode)

	// Get series information from storage
	episodes, err := p.storage.GetSeriesEpisodes(seriesID, currentSeason)
	if err != nil {
		return fmt.Errorf("failed to get series episodes: %w", err)
	}

	// Find next episode in current season
	for _, episode := range episodes {
		if episode.Season == currentSeason && episode.Episode == currentEpisode+1 {
			// Check if already downloaded
			cached, err := p.storage.IsMediaCached(episode.ID)
			if err != nil {
				p.logger.Warn("Failed to check cache status", "episode_id", episode.ID)
				continue
			}

			if !cached {
				p.logger.Info("Queueing next episode for download",
					"episode_id", episode.ID,
					"season", episode.Season,
					"episode", episode.Episode)
				
				// Add to download queue with Priority 1
				if p.downloadManager != nil {
					if err := p.downloadManager.QueueDownload(ctx, episode.ID, 1); err != nil {
						p.logger.Error("Failed to queue next episode download",
							"episode_id", episode.ID,
							"error", err)
					} else {
						p.logger.Info("Successfully queued next episode download",
							"episode_id", episode.ID,
							"priority", 1)
					}
				}
			}
			break
		}
	}

	// Also check for next season if at end of current season
	if currentEpisode >= len(episodes) {
		nextSeasonEpisodes, err := p.storage.GetSeriesEpisodes(seriesID, currentSeason+1)
		if err == nil && len(nextSeasonEpisodes) > 0 {
			firstEpisode := nextSeasonEpisodes[0]
			cached, err := p.storage.IsMediaCached(firstEpisode.ID)
			if err == nil && !cached {
				p.logger.Info("Queueing first episode of next season",
					"episode_id", firstEpisode.ID,
					"season", firstEpisode.Season,
					"episode", firstEpisode.Episode)
					
				// Add to download queue with Priority 2 (lower priority than next episode)
				if p.downloadManager != nil {
					if err := p.downloadManager.QueueDownload(ctx, firstEpisode.ID, 2); err != nil {
						p.logger.Error("Failed to queue next season episode download",
							"episode_id", firstEpisode.ID,
							"error", err)
					}
				}
			}
		}
	}

	return nil
}

// predictContinueWatching finds series where user has started but not finished watching.
// Returns Priority 1 predictions for next episodes in partially watched series.
func (p *Predictor) predictContinueWatching() []PredictionResult {
	var predictions []PredictionResult
	
	// Group viewing history by series
	seriesProgress := make(map[string]ViewingProgress)
	
	for _, session := range p.viewingHistory {
		if session.MediaType == "episode" && session.SeriesID != "" {
			progress := seriesProgress[session.SeriesID]
			progress.SeriesID = session.SeriesID
			progress.LastWatched = session.StartTime
			
			// Track latest episode watched
			if session.Season > progress.LastSeason || 
			   (session.Season == progress.LastSeason && session.Episode > progress.LastEpisode) {
				progress.LastSeason = session.Season
				progress.LastEpisode = session.Episode
				progress.LastMediaID = session.MediaID
			}
			
			progress.TotalWatched++
			if session.Completed {
				progress.CompletedEpisodes++
			}
			
			seriesProgress[session.SeriesID] = progress
		}
	}

	// Create predictions for active series (watched within last 30 days)
	cutoff := time.Now().AddDate(0, 0, -30)
	for _, progress := range seriesProgress {
		if progress.LastWatched.After(cutoff) && progress.CompletedEpisodes > 0 {
			confidence := p.calculateContinueConfidence(progress)
			if confidence >= p.config.MinConfidence {
				predictions = append(predictions, PredictionResult{
					MediaID:    fmt.Sprintf("%s_S%02dE%02d", progress.SeriesID, progress.LastSeason, progress.LastEpisode+1),
					Priority:   1,
					Confidence: confidence,
					Reason:     "Next episode in partially watched series",
					SeriesID:   progress.SeriesID,
					Season:     progress.LastSeason,
					Episode:    progress.LastEpisode + 1,
					MediaType:  "episode",
				})
			}
		}
	}

	return predictions
}

// ViewingProgress tracks user progress through a TV series.
type ViewingProgress struct {
	SeriesID          string
	LastSeason        int
	LastEpisode       int
	LastMediaID       string
	LastWatched       time.Time
	TotalWatched      int
	CompletedEpisodes int
}

// predictUpNext predicts following episodes in sequence (Priority 2).
// Based on binge-watching patterns and typical viewing behavior.
func (p *Predictor) predictUpNext() []PredictionResult {
	var predictions []PredictionResult

	// If user shows binge-watching behavior, predict multiple episodes ahead
	if p.preferences.WatchingPatterns.PrefersBingeWatching {
		episodeCount := 2
		if p.preferences.SeriesBingeRate > 3.0 { // More than 3 episodes per day
			episodeCount = 3
		}

		p.logger.Debug("User shows binge-watching pattern, predicting multiple episodes",
			"episode_count", episodeCount,
			"binge_rate", p.preferences.SeriesBingeRate)

		// This would integrate with continue watching predictions
		// to add additional episodes beyond the immediate next one
	}

	return predictions
}

// predictRecentlyAdded suggests new content matching user preferences (Priority 3).
func (p *Predictor) predictRecentlyAdded() []PredictionResult {
	var predictions []PredictionResult

	// Get recently added content from storage
	// Filter by preferred genres and languages
	// Calculate confidence based on genre match and release recency

	return predictions
}

// predictTrending suggests popular content in preferred genres (Priority 4).
func (p *Predictor) predictTrending() []PredictionResult {
	var predictions []PredictionResult

	// This would integrate with Jellyfin popularity metrics
	// Filter by genres user has shown interest in
	// Lower priority as it's speculative

	return predictions
}

// refreshViewingHistory updates viewing history from Jellyfin API or storage.
func (p *Predictor) refreshViewingHistory(ctx context.Context, userID string) error {
	p.logger.Debug("Refreshing viewing history", "user_id", userID)

	// Get viewing history from storage (this would be populated by Jellyfin sync)
	history, err := p.storage.GetViewingHistory(userID, p.config.HistoryDays)
	if err != nil {
		return fmt.Errorf("failed to get viewing history: %w", err)
	}

	p.viewingHistory = history
	p.lastSync = time.Now()

	p.logger.Info("Viewing history refreshed", 
		"sessions_loaded", len(history),
		"user_id", userID)

	return nil
}

// updatePreferences analyzes viewing history to update user preferences.
func (p *Predictor) updatePreferences() error {
	if len(p.viewingHistory) == 0 {
		return nil
	}

	p.logger.Debug("Updating user preferences from viewing history",
		"session_count", len(p.viewingHistory))

	// Analyze viewing patterns
	p.analyzeWatchingPatterns()
	p.analyzeGenrePreferences()
	p.analyzeViewingTimes()
	p.calculateMetrics()

	p.preferences.LastUpdated = time.Now()

	p.logger.Info("User preferences updated",
		"preferred_genres", len(p.preferences.PreferredGenres),
		"completion_rate", p.preferences.CompletionRate,
		"binge_rate", p.preferences.SeriesBingeRate)

	return nil
}

// analyzeWatchingPatterns determines user binge-watching behavior.
func (p *Predictor) analyzeWatchingPatterns() {
	if len(p.viewingHistory) < 2 {
		return
	}

	var totalDuration time.Duration
	var bingeCount int
	dayMap := make(map[time.Weekday]int)

	for i, session := range p.viewingHistory {
		duration := session.EndTime.Sub(session.StartTime)
		totalDuration += duration

		// Track viewing days
		dayMap[session.StartTime.Weekday()]++

		// Check for binge behavior (multiple episodes same day)
		if i > 0 {
			prevSession := p.viewingHistory[i-1]
			if session.SeriesID == prevSession.SeriesID &&
			   session.StartTime.Format("2006-01-02") == prevSession.StartTime.Format("2006-01-02") {
				bingeCount++
			}
		}
	}

	// Update patterns
	p.preferences.WatchingPatterns.AverageSessionDuration = totalDuration / time.Duration(len(p.viewingHistory))
	p.preferences.WatchingPatterns.PrefersBingeWatching = float64(bingeCount)/float64(len(p.viewingHistory)) > 0.3

	// Find most common viewing days
	var days []time.Weekday
	for day, count := range dayMap {
		if count > len(p.viewingHistory)/7 { // More than average
			days = append(days, day)
		}
	}
	p.preferences.WatchingPatterns.TypicalViewingDays = days
}

// analyzeGenrePreferences extracts preferred genres from viewing history.
func (p *Predictor) analyzeGenrePreferences() {
	genreCount := make(map[string]int)

	for _, session := range p.viewingHistory {
		if session.Completed {
			// This would get genre information from metadata
			// For now, we'll simulate with placeholder logic
		}
	}

	// Convert to sorted list of preferred genres
	type genreScore struct {
		genre string
		count int
	}
	
	var scores []genreScore
	for genre, count := range genreCount {
		scores = append(scores, genreScore{genre, count})
	}
	
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].count > scores[j].count
	})

	// Take top genres (up to 5)
	p.preferences.PreferredGenres = make([]string, 0, 5)
	for i, score := range scores {
		if i >= 5 {
			break
		}
		p.preferences.PreferredGenres = append(p.preferences.PreferredGenres, score.genre)
	}
}

// analyzeViewingTimes identifies when user typically watches content.
func (p *Predictor) analyzeViewingTimes() {
	hourCount := make(map[int]int)

	for _, session := range p.viewingHistory {
		hour := session.StartTime.Hour()
		hourCount[hour]++
	}

	// Find peak viewing times
	p.preferences.WatchingPatterns.PreferredStartTimes = make([]int, 0)
	avgCount := len(p.viewingHistory) / 24
	
	for hour, count := range hourCount {
		if count > avgCount*2 { // Significantly above average
			p.preferences.WatchingPatterns.PreferredStartTimes = append(p.preferences.WatchingPatterns.PreferredStartTimes, hour)
		}
	}

	sort.Ints(p.preferences.WatchingPatterns.PreferredStartTimes)
}

// calculateMetrics computes completion rates and binge rates.
func (p *Predictor) calculateMetrics() {
	if len(p.viewingHistory) == 0 {
		return
	}

	completed := 0
	seriesEpisodes := make(map[string][]time.Time)

	for _, session := range p.viewingHistory {
		if session.Completed {
			completed++
		}

		// Track series viewing for binge rate calculation
		if session.SeriesID != "" {
			day := session.StartTime.Truncate(24 * time.Hour)
			seriesEpisodes[session.SeriesID] = append(seriesEpisodes[session.SeriesID], day)
		}
	}

	p.preferences.CompletionRate = float64(completed) / float64(len(p.viewingHistory))

	// Calculate average episodes per day for series
	totalDays := 0
	totalEpisodes := 0
	for _, episodes := range seriesEpisodes {
		daySet := make(map[time.Time]bool)
		for _, day := range episodes {
			daySet[day] = true
		}
		totalDays += len(daySet)
		totalEpisodes += len(episodes)
	}

	if totalDays > 0 {
		p.preferences.SeriesBingeRate = float64(totalEpisodes) / float64(totalDays)
	}
}

// calculateContinueConfidence determines confidence for continuing a series.
func (p *Predictor) calculateContinueConfidence(progress ViewingProgress) float64 {
	confidence := 0.5 // Base confidence

	// Recent activity increases confidence
	daysSince := time.Since(progress.LastWatched).Hours() / 24
	if daysSince < 7 {
		confidence += 0.3
	} else if daysSince < 30 {
		confidence += 0.2
	}

	// Completion rate affects confidence
	completionRate := float64(progress.CompletedEpisodes) / float64(progress.TotalWatched)
	confidence += completionRate * 0.2

	// Multiple episodes watched increases confidence
	if progress.TotalWatched > 3 {
		confidence += 0.1
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// filterPredictions removes low-confidence predictions and limits results.
func (p *Predictor) filterPredictions(predictions []PredictionResult) []PredictionResult {
	// Filter by minimum confidence
	var filtered []PredictionResult
	for _, pred := range predictions {
		if pred.Confidence >= p.config.MinConfidence {
			filtered = append(filtered, pred)
		}
	}

	// Sort by priority, then confidence
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Priority == filtered[j].Priority {
			return filtered[i].Confidence > filtered[j].Confidence
		}
		return filtered[i].Priority < filtered[j].Priority
	})

	// Limit results (don't overwhelm download queue)
	maxResults := 10
	if len(filtered) > maxResults {
		filtered = filtered[:maxResults]
	}

	return filtered
}

// GetLastSyncTime returns the timestamp of the last successful sync operation.
// Used by the status API to provide accurate sync timing information.
func (p *Predictor) GetLastSyncTime() time.Time {
	return p.lastSync
}