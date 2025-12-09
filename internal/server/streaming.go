package server

import (
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
// Handles partial content requests for efficient streaming and seeking.
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

	fileSize := fileInfo.Size()
	
	// Set content type
	if contentType == "" {
		contentType = s.detectContentType(filePath, file)
	}
	w.Header().Set("Content-Type", contentType)
	
	// Set headers for video streaming
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))

	// Handle Range requests
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		// No range request, serve entire file
		w.Header().Set("Content-Length", strconv.FormatInt(fileSize, 10))
		w.WriteHeader(http.StatusOK)
		
		if r.Method != "HEAD" {
			io.Copy(w, file)
		}
		return
	}

	// Parse Range header
	ranges, err := s.parseRangeHeader(rangeHeader, fileSize)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", fileSize))
		s.writeErrorResponse(w, http.StatusRequestedRangeNotSatisfiable, "Invalid range", err)
		return
	}

	if len(ranges) != 1 {
		// Multiple ranges not supported for simplicity
		s.writeErrorResponse(w, http.StatusRequestedRangeNotSatisfiable, "Multiple ranges not supported", nil)
		return
	}

	// Serve single range
	start, end := ranges[0].start, ranges[0].end
	contentLength := end - start + 1

	// Set partial content headers
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
	w.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	w.WriteHeader(http.StatusPartialContent)

	if r.Method == "HEAD" {
		return
	}

	// Seek to start position and copy range
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		s.logger.Error("Failed to seek in video file", "error", err)
		return
	}

	// Copy only the requested range
	io.CopyN(w, file, contentLength)
}

// handleFallbackStream handles streaming from Jellyfin server when file is not cached.
// Proxies the request to the original Jellyfin server while preserving headers.
func (s *Server) handleFallbackStream(w http.ResponseWriter, r *http.Request, mediaID string) {
	// TODO: Implement fallback to Jellyfin server
	// This requires integration with the Jellyfin client to get stream URL
	
	s.logger.Info("Fallback streaming not yet implemented", "media_id", mediaID)
	s.writeErrorResponse(w, http.StatusNotFound, "Media not available", nil)
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