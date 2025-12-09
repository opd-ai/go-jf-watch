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
	workers    int
	jobs       chan *DownloadJob
	results    chan *DownloadResult
	limiter    *rate.Limiter
	storage    *storage.Manager
	logger     *slog.Logger
	config     *config.DownloadConfig
	
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

// Start begins processing downloads with the configured number of workers.
// Returns an error if the manager is already running.
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.running {
		return fmt.Errorf("download manager is already running")
	}
	
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
	
	// Create rate-limited reader
	rateLimitedReader := m.createRateLimitedReader(resp.Body)
	
	// Wrap with progress tracking
	progressReader := io.TeeReader(rateLimitedReader, bar)
	
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
	
	return result
}

// createRateLimitedReader wraps an io.Reader with rate limiting.
func (m *Manager) createRateLimitedReader(r io.Reader) io.Reader {
	return &rateLimitedReader{
		reader:  r,
		limiter: m.limiter,
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