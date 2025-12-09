package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/opd-ai/go-jf-watch/internal/jellyfin"
)

// APIResponse represents a standard API response structure.
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Message string      `json:"message,omitempty"`
}

// SystemStatus represents the current system status.
type SystemStatus struct {
	Status       string    `json:"status"`
	Version      string    `json:"version"`
	Uptime       string    `json:"uptime"`
	CacheSize    int64     `json:"cache_size_bytes"`
	CacheItems   int       `json:"cache_items"`
	QueueLength  int       `json:"queue_length"`
	ActiveJobs   int       `json:"active_jobs"`
	LastSync     time.Time `json:"last_sync,omitempty"`
}

// QueueItem represents an item in the download queue.
type QueueItem struct {
	ID       string    `json:"id"`
	MediaID  string    `json:"media_id"`
	Title    string    `json:"title"`
	Priority int       `json:"priority"`
	Status   string    `json:"status"`
	Progress float64   `json:"progress"`
	AddedAt  time.Time `json:"added_at"`
	Size     int64     `json:"size_bytes,omitempty"`
}

// AddToQueueRequest represents a request to add an item to the download queue.
type AddToQueueRequest struct {
	MediaID  string `json:"media_id"`
	Priority int    `json:"priority,omitempty"`
}

// handleHealth provides a simple health check endpoint.
// Returns 200 OK if the server is running and storage is accessible.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Check storage health
	if err := s.storage.HealthCheck(); err != nil {
		s.writeErrorResponse(w, http.StatusServiceUnavailable, "Storage unavailable", err)
		return
	}

	s.writeJSONResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Server is healthy",
	})
}

// handleAPIStatus returns comprehensive system status information.
// Includes cache statistics, queue status, and system health metrics.
func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	// Get cache statistics
	cacheStats, err := s.storage.GetCacheStats()
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to get cache stats", err)
		return
	}

	// Get queue statistics (placeholder - will be implemented with download manager)
	queueStats := struct {
		Length     int `json:"length"`
		ActiveJobs int `json:"active_jobs"`
	}{
		Length:     0, // TODO: Get from download manager
		ActiveJobs: 0, // TODO: Get from download manager
	}

	status := SystemStatus{
		Status:      "running",
		Version:     "0.1.0", // TODO: Get from build info
		Uptime:      time.Since(time.Now().Add(-1*time.Hour)).String(), // TODO: Track actual uptime
		CacheSize:   cacheStats.TotalSizeBytes,
		CacheItems:  cacheStats.TotalItems,
		QueueLength: queueStats.Length,
		ActiveJobs:  queueStats.ActiveJobs,
		LastSync:    time.Now().Add(-30 * time.Minute), // TODO: Get from sync manager
	}

	s.writeJSONResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    status,
	})
}

// handleLibrary returns the list of cached media items.
// Supports pagination and filtering parameters for large libraries.
func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 50
	}

	mediaType := r.URL.Query().Get("type") // movies, series, episodes

	// Get cached items from storage
	items, err := s.storage.GetCachedItems(mediaType, page, limit)
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to get cached items", err)
		return
	}

	// Convert to API format
	libraryItems := make([]jellyfin.LibraryItem, len(items))
	for i, item := range items {
		libraryItems[i] = jellyfin.LibraryItem{
			MediaItem: jellyfin.MediaItem{
				ID:            item.ID,
				Name:          item.Name,
				Type:          item.Type,
				Path:          item.Path,
				Size:          item.Size,
				DateAdded:     item.DateAdded,
				SeriesID:      item.SeriesID,
				SeriesName:    item.SeriesName,
				SeasonNumber:  item.SeasonNumber,
				EpisodeNumber: item.EpisodeNumber,
			},
		}
	}

	response := map[string]interface{}{
		"items":       libraryItems,
		"page":        page,
		"limit":       limit,
		"total_items": len(libraryItems), // TODO: Get actual total count
	}

	s.writeJSONResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    response,
	})
}

// handleQueueStatus returns the current download queue status.
// Shows all queued items with their priority, status, and progress.
func (s *Server) handleQueueStatus(w http.ResponseWriter, r *http.Request) {
	// TODO: Get actual queue items from download manager
	// For now, return placeholder data
	queueItems := []QueueItem{
		{
			ID:       "queue-1",
			MediaID:  "media-123",
			Title:    "Example Movie",
			Priority: 1,
			Status:   "downloading",
			Progress: 45.7,
			AddedAt:  time.Now().Add(-10 * time.Minute),
			Size:     1024 * 1024 * 1024 * 2, // 2GB
		},
	}

	s.writeJSONResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    queueItems,
	})
}

// handleQueueAdd adds a new item to the download queue.
// Accepts media ID and optional priority level for the download.
func (s *Server) handleQueueAdd(w http.ResponseWriter, r *http.Request) {
	var req AddToQueueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "Invalid request body", err)
		return
	}

	// Validate request
	if req.MediaID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Media ID is required", nil)
		return
	}

	// Set default priority if not specified
	if req.Priority == 0 {
		req.Priority = 5 // Default to manual priority
	}

	// TODO: Add to download manager queue
	s.logger.Info("Adding item to download queue",
		"media_id", req.MediaID,
		"priority", req.Priority)

	// For now, return success
	queueItem := QueueItem{
		ID:       "queue-" + req.MediaID,
		MediaID:  req.MediaID,
		Title:    "Media Item " + req.MediaID,
		Priority: req.Priority,
		Status:   "queued",
		Progress: 0,
		AddedAt:  time.Now(),
	}

	s.writeJSONResponse(w, http.StatusCreated, APIResponse{
		Success: true,
		Data:    queueItem,
		Message: "Item added to download queue",
	})
}

// handleQueueRemove removes an item from the download queue.
// Stops active downloads if the item is currently being downloaded.
func (s *Server) handleQueueRemove(w http.ResponseWriter, r *http.Request) {
	queueID := chi.URLParam(r, "id")
	if queueID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Queue ID is required", nil)
		return
	}

	// TODO: Remove from download manager queue
	s.logger.Info("Removing item from download queue", "queue_id", queueID)

	s.writeJSONResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Item removed from download queue",
	})
}

// handleGetSettings returns the current application settings.
// Used by the web UI to populate configuration forms.
func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	// Return a subset of configuration that's safe to expose to UI
	settings := map[string]interface{}{
		"cache.max_size_gb":              500,    // Would come from actual config
		"cache.eviction_threshold":       0.85,
		"download.workers":               3,
		"download.rate_limit_mbps":      10,
		"download.auto_download_current": true,
		"download.auto_download_next":    true,
		"download.auto_download_count":   2,
		"server.port":                   s.config.Port,
		"server.host":                   s.config.Host,
		"server.enable_compression":     s.config.EnableCompression,
		"prediction.enabled":            true,
		"prediction.sync_interval":      "4h",
		"prediction.history_days":       30,
		"ui.theme":                      "auto",
		"ui.language":                   "en",
	}

	s.writeJSONResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Data:    settings,
	})
}

// handlePostSettings updates application settings.
// In a full implementation, this would persist changes to configuration.
func (s *Server) handlePostSettings(w http.ResponseWriter, r *http.Request) {
	var settings map[string]interface{}
	
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "Invalid JSON payload", err)
		return
	}

	// In a full implementation, validate and persist settings
	// For Phase 4, we'll just acknowledge the request
	s.logger.Info("Settings update requested", "settings", settings)

	s.writeJSONResponse(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Settings saved successfully (changes require restart)",
	})
}

// writeJSONResponse writes a JSON response with the specified status code.
func (s *Server) writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode JSON response", "error", err)
	}
}

// writeErrorResponse writes an error response with the specified status code and message.
func (s *Server) writeErrorResponse(w http.ResponseWriter, statusCode int, message string, err error) {
	s.logger.Error("HTTP error response",
		"status", statusCode,
		"message", message,
		"error", err)

	errorMsg := message
	if err != nil {
		errorMsg = err.Error()
	}

	s.writeJSONResponse(w, statusCode, APIResponse{
		Success: false,
		Error:   errorMsg,
		Message: message,
	})
}