package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

// handleVideoStream serves video files with HTTP Range support for seeking.
// Supports partial content requests (206) for efficient video streaming and seeking.
// Falls back to Jellyfin server if the file is not cached locally.
func (s *Server) handleVideoStream(w http.ResponseWriter, r *http.Request) {
	mediaID := chi.URLParam(r, "id")
	if mediaID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Media ID is required", nil)
		return
	}

	s.logger.Debug("Video stream request",
		"media_id", mediaID,
		"range", r.Header.Get("Range"),
		"user_agent", r.UserAgent())

	// Trigger playback prediction for Priority 0 download and next episode queuing
	// Only trigger on initial request (not range requests for seeking)
	if r.Header.Get("Range") == "" {
		go func() {
			ctx := context.Background()
			if err := s.predictor.OnPlaybackStart(ctx, mediaID); err != nil {
				s.logger.Warn("Failed to trigger playback prediction",
					"media_id", mediaID,
					"error", err)
			}
		}()
	}

	// Check if file exists in cache
	cachedItem, err := s.storage.GetDownload(mediaID)
	if err != nil {
		s.logger.Warn("Media not found in cache", "media_id", mediaID, "error", err)
		s.handleFallbackStream(w, r, mediaID)
		return
	}

	// Verify file exists on disk
	if _, err := os.Stat(cachedItem.LocalPath); os.IsNotExist(err) {
		s.logger.Warn("Cached file not found on disk", "media_id", mediaID, "path", cachedItem.LocalPath)
		s.handleFallbackStream(w, r, mediaID)
		return
	}

	// Serve the cached file with range support
	s.serveVideoFile(w, r, cachedItem.LocalPath, cachedItem.ContentType)
}

// serveVideoFile serves a video file with HTTP Range support.
// Uses http.ServeContent for robust range handling including multipart ranges.
func (s *Server) serveVideoFile(w http.ResponseWriter, r *http.Request, filePath, contentType string) {
	// Open file
	file, err := os.Open(filePath)
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to open video file", err)
		return
	}
	defer file.Close()

	// Get file info
	fileInfo, err := file.Stat()
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, "Failed to get file info", err)
		return
	}

	// Detect content type if not provided
	if contentType == "" {
		contentType = s.detectContentType(filePath, file)
		// Reset file position after detection
		file.Seek(0, io.SeekStart)
	}
	w.Header().Set("Content-Type", contentType)

	// Use http.ServeContent for robust Range request handling
	// This handles single ranges, multipart ranges, and proper caching headers
	http.ServeContent(w, r, filepath.Base(filePath), fileInfo.ModTime(), file)
}

// handleFallbackStream handles streaming from Jellyfin server when file is not cached.
// Proxies the request to the original Jellyfin server while preserving headers.
func (s *Server) handleFallbackStream(w http.ResponseWriter, r *http.Request, mediaID string) {
	s.logger.Info("Streaming uncached media from Jellyfin server", "media_id", mediaID)

	// Get stream URL from Jellyfin client
	streamURL, err := s.jellyfinClient.GetStreamURL(mediaID)
	if err != nil {
		s.logger.Error("Failed to get stream URL from Jellyfin",
			"media_id", mediaID, "error", err)
		s.writeErrorResponse(w, http.StatusInternalServerError,
			"Failed to get stream URL", err)
		return
	}

	// Create proxy request to Jellyfin server
	proxyReq, err := http.NewRequestWithContext(r.Context(), "GET", streamURL, nil)
	if err != nil {
		s.logger.Error("Failed to create proxy request", "error", err)
		s.writeErrorResponse(w, http.StatusInternalServerError,
			"Failed to create proxy request", err)
		return
	}

	// Copy relevant headers from original request
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		proxyReq.Header.Set("Range", rangeHeader)
	}
	if userAgent := r.Header.Get("User-Agent"); userAgent != "" {
		proxyReq.Header.Set("User-Agent", userAgent)
	}

	// Make request to Jellyfin server
	client := &http.Client{
		Timeout: 0, // No timeout for streaming
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		s.logger.Error("Failed to proxy request to Jellyfin",
			"media_id", mediaID, "error", err)
		s.writeErrorResponse(w, http.StatusBadGateway,
			"Failed to stream from Jellyfin server", err)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set status code
	w.WriteHeader(resp.StatusCode)

	// Stream the response body
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		s.logger.Error("Error streaming from Jellyfin",
			"media_id", mediaID, "error", err)
		// Can't write error response as we've already started streaming
		return
	}

	s.logger.Debug("Successfully streamed uncached media from Jellyfin",
		"media_id", mediaID, "status", resp.StatusCode)
}

// detectContentType detects the MIME type of a video file.
// Uses file extension and content sniffing for accurate detection.
func (s *Server) detectContentType(filePath string, file *os.File) string {
	// Try to detect from file extension first
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".wmv":
		return "video/x-ms-wmv"
	case ".flv":
		return "video/x-flv"
	case ".webm":
		return "video/webm"
	case ".m4v":
		return "video/x-m4v"
	}

	// Fall back to content sniffing
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil {
		return "application/octet-stream"
	}

	// Reset file position
	file.Seek(0, io.SeekStart)

	contentType := http.DetectContentType(buffer[:n])
	if strings.HasPrefix(contentType, "video/") {
		return contentType
	}

	// Default for unknown video files
	return "video/mp4"
}

// Range represents a byte range for HTTP Range requests.
type Range struct {
	start, end int64
}

// parseRangeHeader parses HTTP Range header and returns list of byte ranges.
// Supports standard "bytes=start-end" format with validation.
func (s *Server) parseRangeHeader(rangeHeader string, fileSize int64) ([]Range, error) {
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return nil, fmt.Errorf("unsupported range unit")
	}

	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	rangeParts := strings.Split(rangeSpec, ",")

	var ranges []Range

	for _, part := range rangeParts {
		part = strings.TrimSpace(part)

		if strings.Contains(part, "-") {
			rangeBounds := strings.SplitN(part, "-", 2)
			startStr, endStr := rangeBounds[0], rangeBounds[1]

			var start, end int64
			var err error

			if startStr == "" {
				// Suffix range: -500 (last 500 bytes)
				if endStr == "" {
					return nil, fmt.Errorf("invalid range format")
				}
				suffix, err := strconv.ParseInt(endStr, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid range end: %v", err)
				}
				start = fileSize - suffix
				if start < 0 {
					start = 0
				}
				end = fileSize - 1
			} else {
				// Start specified
				start, err = strconv.ParseInt(startStr, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("invalid range start: %v", err)
				}

				if endStr == "" {
					// Range from start to end of file
					end = fileSize - 1
				} else {
					end, err = strconv.ParseInt(endStr, 10, 64)
					if err != nil {
						return nil, fmt.Errorf("invalid range end: %v", err)
					}
				}
			}

			// Validate range
			if start < 0 || end < 0 || start > end || start >= fileSize {
				return nil, fmt.Errorf("invalid range bounds")
			}

			// Clamp end to file size
			if end >= fileSize {
				end = fileSize - 1
			}

			ranges = append(ranges, Range{start: start, end: end})
		} else {
			return nil, fmt.Errorf("invalid range format")
		}
	}

	return ranges, nil
}
