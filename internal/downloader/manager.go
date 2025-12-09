// Package downloader implements the download manager with worker pool pattern
// for concurrent media downloading with priority queuing and rate limiting.
//
// Architecture follows the worker pool pattern specified in PLAN.md:
// - Manager orchestrates workers and manages queue state
// - Workers handle concurrent download execution (3-5 goroutines)
// - Priority-based download scheduling using channels
// - Rate limiting using golang.org/x/time/rate
package downloader

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"github.com/natefinch/atomic"
	"github.com/schollz/progressbar/v3"
	
	"github.com/opd-ai/go-jf-watch/internal/storage"
	"github.com/opd-ai/go-jf-watch/pkg/config"
)

// Manager orchestrates workers and manages download queue state.
// It implements the worker pool pattern with priority-based scheduling.
type Manager struct {
	workers          int
	jobs             chan *DownloadJob
	results          chan *DownloadResult
	limiter          *rate.Limiter
	storage          *storage.Manager
	logger           *slog.Logger
	config           *config.DownloadConfig
	progressReporter ProgressReporter
	
	// Worker management
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	running    bool
	mu         sync.RWMutex
}

// DownloadJob represents a download task with priority and metadata.
type DownloadJob struct {
	ID          string
	MediaID     string
	Priority    int
	URL         string
	LocalPath   string
	Size        int64
	Checksum    string
	RetryCount  int
	CreatedAt   time.Time
}

// DownloadResult contains the outcome of a download job.
type DownloadResult struct {
	Job         *DownloadJob
	Success     bool
	BytesRead   int64
	Duration    time.Duration
	Error       error
	CompletedAt time.Time
}

// ProgressCallback is called during download to report progress.
type ProgressCallback func(jobID string, downloaded, total int64)

// ProgressReporter interface for sending progress updates (e.g., to WebSocket clients)
type ProgressReporter interface {
	BroadcastProgress(mediaID, status, message string, progress float64)
}

// New creates a new download manager with the specified configuration.
// It initializes the worker pool but doesn't start workers until Start() is called.
func New(cfg *config.DownloadConfig, storage *storage.Manager, logger *slog.Logger) *Manager {
	// Create rate limiter based on configuration
	// Convert Mbps to bytes per second with burst allowance
	bytesPerSecond := rate.Limit(cfg.RateLimitMbps * 1024 * 1024 / 8)
	burstSize := int(bytesPerSecond * 5) // 5 second burst
	
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Manager{
		workers: cfg.Workers,
		jobs:    make(chan *DownloadJob, cfg.Workers*2), // Buffer for efficiency
		results: make(chan *DownloadResult, cfg.Workers*2),
		limiter: rate.NewLimiter(bytesPerSecond, burstSize),
		storage: storage,
		logger:  logger,
		config:  cfg,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// SetProgressReporter sets the progress reporter for WebSocket updates
func (m *Manager) SetProgressReporter(reporter ProgressReporter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.progressReporter = reporter
}

// Start begins processing downloads with the configured number of workers.
// Returns an error if the manager is already running.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.running {
		return fmt.Errorf("download manager is already running")
	}
	
	m.ctx, m.cancel = context.WithCancel(ctx)
	
	m.logger.Info("Starting download manager",
		"workers", m.workers,
		"rate_limit_mbps", m.config.RateLimitMbps)
	
	// Start worker goroutines
	for i := 0; i < m.workers; i++ {
		m.wg.Add(1)
		go m.worker(i)
	}
	
	// Start result processor
	m.wg.Add(1)
	go m.resultProcessor()
	
	// Start queue processor (loads jobs from storage)
	m.wg.Add(1)
	go m.queueProcessor()
	
	m.running = true
	return nil
}

// Stop gracefully shuts down the download manager.
// It waits for current downloads to complete but stops accepting new jobs.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if !m.running {
		return nil
	}
	
	m.logger.Info("Stopping download manager")
	
	// Signal shutdown
	m.cancel()
	
	// Close job channel to stop accepting new jobs
	close(m.jobs)
	
	// Wait for all workers to complete
	m.wg.Wait()
	
	// Close results channel
	close(m.results)
	
	m.running = false
	m.logger.Info("Download manager stopped")
	
	return nil
}

// AddJob adds a download job to the queue with the specified priority.
// Lower priority numbers have higher precedence (0 = highest priority).
func (m *Manager) AddJob(job *DownloadJob) error {
	// Store job in persistent queue
	queueItem := &storage.QueueItem{
		ID:        job.ID,
		MediaID:   job.MediaID,
		Priority:  job.Priority,
		URL:       job.URL,
		LocalPath: job.LocalPath,
		CreatedAt: job.CreatedAt,
		Status:    "queued",
	}
	
	if err := m.storage.AddQueueItem(queueItem); err != nil {
		return fmt.Errorf("failed to add job to storage queue: %w", err)
	}
	
	m.logger.Debug("Added download job to queue",
		"job_id", job.ID,
		"media_id", job.MediaID,
		"priority", job.Priority,
		"url", job.URL)
	
	// Try to send to worker channel if there's space
	select {
	case m.jobs <- job:
		// Job sent to worker immediately
	default:
		// Channel full, job will be picked up by queue processor
		m.logger.Debug("Job channel full, job queued in storage",
			"job_id", job.ID)
	}
	
	return nil
}

// queueProcessor continuously loads jobs from storage into the worker channel.
func (m *Manager) queueProcessor() {
	defer m.wg.Done()
	
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.loadJobsFromQueue()
		}
	}
}

// loadJobsFromQueue loads queued jobs from storage into the worker channel.
func (m *Manager) loadJobsFromQueue() {
	queueItem, err := m.storage.GetNextQueueItem()
	if err != nil || queueItem == nil {
		return // No queued items or error
	}
	
	job := &DownloadJob{
		ID:        queueItem.ID,
		MediaID:   queueItem.MediaID,
		Priority:  queueItem.Priority,
		URL:       queueItem.URL,
		LocalPath: queueItem.LocalPath,
		CreatedAt: queueItem.CreatedAt,
	}
	
	select {
	case m.jobs <- job:
		// Update status to downloading
		queueItem.Status = "downloading"
		now := time.Now()
		queueItem.StartedAt = &now
		
		if err := m.storage.UpdateQueueItem(queueItem); err != nil {
			m.logger.Error("Failed to update queue item status",
				"job_id", job.ID, "error", err)
		}
	case <-m.ctx.Done():
		return
	default:
		// Channel full, try again later
	}
}

// worker processes download jobs from the jobs channel.
func (m *Manager) worker(id int) {
	defer m.wg.Done()
	
	m.logger.Debug("Starting download worker", "worker_id", id)
	
	for {
		select {
		case job, ok := <-m.jobs:
			if !ok {
				m.logger.Debug("Jobs channel closed, worker exiting", "worker_id", id)
				return
			}
			
			result := m.processJob(job)
			
			select {
			case m.results <- result:
			case <-m.ctx.Done():
				return
			}
			
		case <-m.ctx.Done():
			m.logger.Debug("Worker shutting down", "worker_id", id)
			return
		}
	}
}

// processJob handles the actual download of a single job.
func (m *Manager) processJob(job *DownloadJob) *DownloadResult {
	start := time.Now()
	result := &DownloadResult{
		Job:         job,
		CompletedAt: time.Now(),
	}
	
	m.logger.Info("Starting download",
		"job_id", job.ID,
		"media_id", job.MediaID,
		"url", job.URL,
		"local_path", job.LocalPath)
		
	// Report download start
	m.reportProgress(job.MediaID, 0, "downloading", "Download started")
	
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(job.LocalPath), 0755); err != nil {
		result.Error = fmt.Errorf("failed to create directory: %w", err)
		return result
	}
	
	// Create HTTP request
	req, err := http.NewRequestWithContext(m.ctx, "GET", job.URL, nil)
	if err != nil {
		result.Error = fmt.Errorf("failed to create request: %w", err)
		return result
	}
	
	// Add range support for resume capability (future enhancement)
	client := &http.Client{
		Timeout: 30 * time.Minute, // Long timeout for large files
	}
	
	resp, err := client.Do(req)
	if err != nil {
		result.Error = fmt.Errorf("failed to make request: %w", err)
		return result
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		m.reportProgress(job.MediaID, 0, "failed", fmt.Sprintf("HTTP error: %d", resp.StatusCode))
		return result
	}
	
	// Get content length for progress tracking
	contentLength := resp.ContentLength
	if contentLength > 0 {
		job.Size = contentLength
	}
	
	// Create progress bar for this download
	bar := progressbar.DefaultBytes(
		contentLength,
		fmt.Sprintf("Downloading %s", filepath.Base(job.LocalPath)),
	)
	
	// Create rate-limited reader (bypass for Priority 0 - currently playing)
	var dataReader io.Reader
	if job.Priority == 0 {
		// Priority 0 (currently playing) gets full bandwidth
		m.logger.Debug("Using full bandwidth for Priority 0 download", "job_id", job.ID)
		dataReader = resp.Body
	} else {
		// All other priorities use rate limiting
		dataReader = m.createRateLimitedReader(resp.Body)
	}
	
	// Wrap with progress tracking
	progressReader := io.TeeReader(dataReader, bar)
	
	// Use atomic write to ensure file integrity
	err = atomic.WriteFile(job.LocalPath, progressReader)
	if err != nil {
		result.Error = fmt.Errorf("failed to write file: %w", err)
		return result
	}
	
	// Calculate final stats
	result.Success = true
	result.Duration = time.Since(start)
	result.BytesRead = contentLength
	
	m.logger.Info("Download completed successfully",
		"job_id", job.ID,
		"media_id", job.MediaID,
		"duration", result.Duration,
		"bytes", result.BytesRead)
	
	// Report download completion
	m.reportProgress(job.MediaID, 100, "completed", "Download completed successfully")
	
	return result
}

// createRateLimitedReader wraps an io.Reader with rate limiting.
func (m *Manager) createRateLimitedReader(r io.Reader) io.Reader {
	// Get current rate limit based on time and configuration
	currentLimit := m.getCurrentRateLimit()
	
	return &rateLimitedReader{
		reader:  r,
		limiter: currentLimit,
		ctx:     m.ctx,
	}
}

// rateLimitedReader implements io.Reader with rate limiting.
type rateLimitedReader struct {
	reader  io.Reader
	limiter *rate.Limiter
	ctx     context.Context
}

func (r *rateLimitedReader) Read(buf []byte) (int, error) {
	// Wait for rate limiter permission
	if err := r.limiter.WaitN(r.ctx, len(buf)); err != nil {
		return 0, err
	}
	
	return r.reader.Read(buf)
}

// getCurrentRateLimit returns the appropriate rate limiter based on current time and configuration
func (m *Manager) getCurrentRateLimit() *rate.Limiter {
	// Check if we're in peak hours
	if m.isCurrentlyPeakHours() {
		// During peak hours, use reduced bandwidth
		peakBandwidth := float64(m.config.RateLimitMbps) * float64(m.config.RateLimitSchedule.PeakLimitPercent) / 100.0
		bytesPerSecond := rate.Limit(peakBandwidth * 1024 * 1024 / 8)
		burstSize := int(bytesPerSecond * 5) // 5 second burst
		
		m.logger.Debug("Using peak hours rate limit", 
			"peak_bandwidth_mbps", peakBandwidth,
			"peak_limit_percent", m.config.RateLimitSchedule.PeakLimitPercent)
		
		return rate.NewLimiter(bytesPerSecond, burstSize)
	}
	
	// Outside peak hours, use full bandwidth
	return m.limiter
}

// isCurrentlyPeakHours checks if the current time falls within configured peak hours
func (m *Manager) isCurrentlyPeakHours() bool {
	if m.config.RateLimitSchedule.PeakHours == "" {
		return false // No peak hours configured
	}
	
	// Parse peak hours format "HH:MM-HH:MM"
	peakStart, peakEnd, err := parsePeakHours(m.config.RateLimitSchedule.PeakHours)
	if err != nil {
		m.logger.Warn("Invalid peak hours format, ignoring peak hour limits", 
			"peak_hours", m.config.RateLimitSchedule.PeakHours, 
			"error", err)
		return false
	}
	
	now := time.Now()
	currentTime := now.Hour()*100 + now.Minute() // Convert to HHMM format
	
	// Handle case where peak hours span midnight
	if peakStart > peakEnd {
		return currentTime >= peakStart || currentTime <= peakEnd
	}
	
	return currentTime >= peakStart && currentTime <= peakEnd
}

// parsePeakHours parses a time range string like "06:00-23:00" into start and end times in HHMM format
func parsePeakHours(peakHours string) (int, int, error) {
	// Expected format: "06:00-23:00"
	parts := strings.Split(peakHours, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid format, expected HH:MM-HH:MM")
	}
	
	startTime, err := parseTimeToHHMM(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid start time: %w", err)
	}
	
	endTime, err := parseTimeToHHMM(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid end time: %w", err)
	}
	
	return startTime, endTime, nil
}

// parseTimeToHHMM converts a time string like "06:00" to HHMM integer format (600)
func parseTimeToHHMM(timeStr string) (int, error) {
	timeParts := strings.Split(timeStr, ":")
	if len(timeParts) != 2 {
		return 0, fmt.Errorf("invalid time format, expected HH:MM")
	}
	
	hour, err := strconv.Atoi(timeParts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("invalid hour: %s", timeParts[0])
	}
	
	minute, err := strconv.Atoi(timeParts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("invalid minute: %s", timeParts[1])
	}
	
	return hour*100 + minute, nil
}

// reportProgress sends a progress update via the progress reporter (WebSocket)
func (m *Manager) reportProgress(mediaID string, progress float64, status, message string) {
	if m.progressReporter != nil {
		m.progressReporter.BroadcastProgress(mediaID, status, message, progress)
	}
}

// resultProcessor handles completed download results.
func (m *Manager) resultProcessor() {
	defer m.wg.Done()
	
	for {
		select {
		case result, ok := <-m.results:
			if !ok {
				m.logger.Debug("Results channel closed, processor exiting")
				return
			}
			
			m.handleResult(result)
			
		case <-m.ctx.Done():
			m.logger.Debug("Result processor shutting down")
			return
		}
	}
}

// handleResult processes a completed download result.
func (m *Manager) handleResult(result *DownloadResult) {
	job := result.Job
	
	if result.Success {
		// Add to downloads bucket
		downloadRecord := &storage.DownloadRecord{
			ID:           job.ID,
			MediaType:    "unknown", // TODO: Extract from job metadata
			JellyfinID:   job.MediaID,
			LocalPath:    job.LocalPath,
			Size:         result.BytesRead,
			DownloadedAt: result.CompletedAt,
			LastAccessed: result.CompletedAt,
			Status:       "completed",
		}
		
		if err := m.storage.AddDownloadRecord(downloadRecord); err != nil {
			m.logger.Error("Failed to store download record",
				"job_id", job.ID, "error", err)
		}
		
		// Remove from queue
		if err := m.storage.RemoveQueueItem(job.ID); err != nil {
			m.logger.Error("Failed to remove completed job from queue",
				"job_id", job.ID, "error", err)
		}
		
	} else {
		// Handle failed download
		m.logger.Error("Download failed",
			"job_id", job.ID,
			"media_id", job.MediaID,
			"error", result.Error,
			"retry_count", job.RetryCount)
		
		// Update queue item with error and potentially retry
		if job.RetryCount < m.config.RetryAttempts {
			// Schedule retry
			job.RetryCount++
			retryDelay := time.Duration(job.RetryCount) * m.config.RetryDelay
			
			m.logger.Info("Scheduling download retry",
				"job_id", job.ID,
				"retry_count", job.RetryCount,
				"delay", retryDelay)
			
			// TODO: Implement exponential backoff retry scheduling
			// For now, just update the queue item status
			queueItem := &storage.QueueItem{
				ID:           job.ID,
				MediaID:      job.MediaID,
				Priority:     job.Priority,
				URL:          job.URL,
				LocalPath:    job.LocalPath,
				CreatedAt:    job.CreatedAt,
				Status:       "queued", // Reset to queued for retry
				RetryCount:   job.RetryCount,
				ErrorMessage: result.Error.Error(),
			}
			
			if err := m.storage.UpdateQueueItem(queueItem); err != nil {
				m.logger.Error("Failed to update queue item for retry",
					"job_id", job.ID, "error", err)
			}
		} else {
			// Max retries exceeded, mark as failed
			queueItem := &storage.QueueItem{
				ID:           job.ID,
				MediaID:      job.MediaID,
				Priority:     job.Priority,
				URL:          job.URL,
				LocalPath:    job.LocalPath,
				CreatedAt:    job.CreatedAt,
				Status:       "failed",
				RetryCount:   job.RetryCount,
				ErrorMessage: result.Error.Error(),
			}
			
			if err := m.storage.UpdateQueueItem(queueItem); err != nil {
				m.logger.Error("Failed to update failed queue item",
					"job_id", job.ID, "error", err)
			}
		}
	}
}

// QueueDownload adds a media item to the download queue with specified priority.
// This is the primary interface for the prediction engine to queue downloads.
func (m *Manager) QueueDownload(ctx context.Context, mediaID string, priority int) error {
	m.mu.RLock()
	running := m.running
	m.mu.RUnlock()
	
	if !running {
		return fmt.Errorf("download manager is not running")
	}
	
	// Create download job for the media item
	// Note: URL and other details would need to be fetched from Jellyfin API
	job := &DownloadJob{
		ID:        fmt.Sprintf("%s-%d", mediaID, time.Now().Unix()),
		MediaID:   mediaID,
		Priority:  priority,
		CreatedAt: time.Now(),
		// URL and LocalPath would be populated by Jellyfin API integration
	}
	
	m.logger.Debug("Queuing download", 
		"media_id", mediaID,
		"priority", priority,
		"job_id", job.ID)
	
	return m.AddJob(job)
}

// QueueStats contains statistics about the download queue and activity.
type QueueStats struct {
	QueueSize       int `json:"queue_size"`
	ActiveDownloads int `json:"active_downloads"`
	CompletedToday  int `json:"completed_today"`
	FailedToday     int `json:"failed_today"`
}

// GetQueueStats returns current download queue statistics for monitoring.
func (m *Manager) GetQueueStats() QueueStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	// Get queue sizes from storage
	queueSizes, err := m.storage.GetQueueSize()
	if err != nil {
		m.logger.Warn("Failed to get queue sizes", "error", err)
		queueSizes = make(map[int]int)
	}
	
	// Calculate total queue size
	totalQueue := 0
	for _, size := range queueSizes {
		totalQueue += size
	}
	
	// For now, return basic statistics
	// In a full implementation, we'd track completed/failed counts
	return QueueStats{
		QueueSize:       totalQueue,
		ActiveDownloads: len(m.jobs), // Approximate active downloads
		CompletedToday:  0, // Would track in storage
		FailedToday:     0, // Would track in storage
	}
}

// GetQueueItems returns all queue items from storage.
func (m *Manager) GetQueueItems() ([]*storage.QueueItem, error) {
	// Get all queue items regardless of status
	return m.storage.GetQueueItems("")
}

// GetStatus returns the current status of the download manager.
func (m *Manager) GetStatus() (map[string]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	queueSizes, err := m.storage.GetQueueSize()
	if err != nil {
		return nil, fmt.Errorf("failed to get queue sizes: %w", err)
	}
	
	return map[string]interface{}{
		"running":     m.running,
		"workers":     m.workers,
		"queue_sizes": queueSizes,
		"rate_limit":  m.config.RateLimitMbps,
	}, nil
}