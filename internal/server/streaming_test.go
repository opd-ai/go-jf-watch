package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRangeHeader(t *testing.T) {
	server := createTestServer(t)
	fileSize := int64(1000)

	tests := []struct {
		name        string
		rangeHeader string
		expected    []Range
		expectError bool
	}{
		{
			name:        "simple range",
			rangeHeader: "bytes=0-499",
			expected:    []Range{{start: 0, end: 499}},
			expectError: false,
		},
		{
			name:        "range from start to end",
			rangeHeader: "bytes=500-",
			expected:    []Range{{start: 500, end: 999}},
			expectError: false,
		},
		{
			name:        "suffix range",
			rangeHeader: "bytes=-500",
			expected:    []Range{{start: 500, end: 999}},
			expectError: false,
		},
		{
			name:        "invalid range unit",
			rangeHeader: "items=0-499",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid range format",
			rangeHeader: "bytes=invalid",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "range beyond file size",
			rangeHeader: "bytes=1500-2000",
			expected:    nil,
			expectError: true,
		},
		{
			name:        "invalid range bounds",
			rangeHeader: "bytes=500-200",
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ranges, err := server.parseRangeHeader(tt.rangeHeader, fileSize)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if len(ranges) != len(tt.expected) {
				t.Errorf("Expected %d ranges, got %d", len(tt.expected), len(ranges))
				return
			}

			for i, expectedRange := range tt.expected {
				if ranges[i].start != expectedRange.start || ranges[i].end != expectedRange.end {
					t.Errorf("Range %d: expected %d-%d, got %d-%d",
						i, expectedRange.start, expectedRange.end, ranges[i].start, ranges[i].end)
				}
			}
		})
	}
}

func TestDetectContentType(t *testing.T) {
	server := createTestServer(t)

	tests := []struct {
		filename    string
		expected    string
	}{
		{"video.mp4", "video/mp4"},
		{"movie.mkv", "video/x-matroska"},
		{"film.avi", "video/x-msvideo"},
		{"clip.mov", "video/quicktime"},
		{"video.wmv", "video/x-ms-wmv"},
		{"stream.flv", "video/x-flv"},
		{"web.webm", "video/webm"},
		{"mobile.m4v", "video/x-m4v"},
		{"unknown.xyz", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			// Create a temporary file
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, tt.filename)
			
			// Write some dummy content
			content := []byte("dummy video content for testing")
			if err := os.WriteFile(filePath, content, 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			file, err := os.Open(filePath)
			if err != nil {
				t.Fatalf("Failed to open test file: %v", err)
			}
			defer file.Close()

			contentType := server.detectContentType(filePath, file)
			if contentType != tt.expected {
				t.Errorf("Expected content type %s, got %s", tt.expected, contentType)
			}
		})
	}
}

func TestServeVideoFile(t *testing.T) {
	server := createTestServer(t)

	// Create a test video file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test_video.mp4")
	testContent := make([]byte, 1000) // 1KB test file
	for i := range testContent {
		testContent[i] = byte(i % 256)
	}
	
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name           string
		rangeHeader    string
		expectedStatus int
		expectedLength int
	}{
		{
			name:           "full file request",
			rangeHeader:    "",
			expectedStatus: http.StatusOK,
			expectedLength: 1000,
		},
		{
			name:           "partial content request",
			rangeHeader:    "bytes=0-499",
			expectedStatus: http.StatusPartialContent,
			expectedLength: 500,
		},
		{
			name:           "range from middle to end",
			rangeHeader:    "bytes=500-",
			expectedStatus: http.StatusPartialContent,
			expectedLength: 500,
		},
		{
			name:           "suffix range",
			rangeHeader:    "bytes=-200",
			expectedStatus: http.StatusPartialContent,
			expectedLength: 200,
		},
		{
			name:           "invalid range",
			rangeHeader:    "bytes=2000-3000",
			expectedStatus: http.StatusRequestedRangeNotSatisfiable,
			expectedLength: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.rangeHeader != "" {
				req.Header.Set("Range", tt.rangeHeader)
			}
			
			w := httptest.NewRecorder()

			server.serveVideoFile(w, req, testFile, "video/mp4")

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedStatus == http.StatusOK || tt.expectedStatus == http.StatusPartialContent {
				// Check content type
				contentType := w.Header().Get("Content-Type")
				if contentType != "video/mp4" {
					t.Errorf("Expected content type video/mp4, got %s", contentType)
				}

				// Check Accept-Ranges header
				acceptRanges := w.Header().Get("Accept-Ranges")
				if acceptRanges != "bytes" {
					t.Errorf("Expected Accept-Ranges bytes, got %s", acceptRanges)
				}

				// For successful requests, check content length
				if tt.expectedLength > 0 {
					body := w.Body.Bytes()
					if len(body) != tt.expectedLength {
						t.Errorf("Expected content length %d, got %d", tt.expectedLength, len(body))
					}
				}

				// For range requests, check Content-Range header
				if tt.expectedStatus == http.StatusPartialContent {
					contentRange := w.Header().Get("Content-Range")
					if contentRange == "" {
						t.Error("Expected Content-Range header for partial content")
					}
				}
			}
		})
	}
}

func TestHandleVideoStreamNotFound(t *testing.T) {
	server := createTestServer(t)

	req := httptest.NewRequest("GET", "/stream/nonexistent-media-id", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	// Should return 404 when media is not cached and fallback is not implemented
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestHandleVideoStreamMissingID(t *testing.T) {
	server := createTestServer(t)

	req := httptest.NewRequest("GET", "/stream/", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	// Should return 404 for empty media ID
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestVideoStreamingWithRangeRequests(t *testing.T) {
	server := createTestServer(t)

	// Create a larger test file for more realistic range testing
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "large_video.mp4")
	testContent := make([]byte, 10000) // 10KB test file
	
	// Fill with identifiable pattern
	for i := range testContent {
		testContent[i] = byte(i % 256)
	}
	
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test multiple range requests
	ranges := []string{
		"bytes=0-999",     // First 1KB
		"bytes=5000-5999", // Middle 1KB
		"bytes=9000-",     // Last 1KB
		"bytes=-1000",     // Suffix 1KB
	}

	for i, rangeHeader := range ranges {
		t.Run(fmt.Sprintf("range_%d", i), func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("Range", rangeHeader)
			
			w := httptest.NewRecorder()

			server.serveVideoFile(w, req, testFile, "video/mp4")

			if w.Code != http.StatusPartialContent {
				t.Errorf("Expected status 206, got %d", w.Code)
				return
			}

			// Verify the returned content matches the expected range
			body := w.Body.Bytes()
			if len(body) == 0 {
				t.Error("Expected non-empty response body")
				return
			}

			// For first range (0-999), check that first byte is 0
			if rangeHeader == "bytes=0-999" {
				if len(body) != 1000 {
					t.Errorf("Expected 1000 bytes, got %d", len(body))
				}
				if body[0] != 0 {
					t.Errorf("Expected first byte to be 0, got %d", body[0])
				}
				if body[999] != 231 { // 999 % 256 = 231
					t.Errorf("Expected byte 999 to be 231, got %d", body[999])
				}
			}
		})
	}
}

func TestHEADRequestSupport(t *testing.T) {
	server := createTestServer(t)

	// Create a test video file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test_video.mp4")
	testContent := make([]byte, 1000)
	
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test HEAD request without range
	req := httptest.NewRequest("HEAD", "/test", nil)
	w := httptest.NewRecorder()

	server.serveVideoFile(w, req, testFile, "video/mp4")

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200 for HEAD request, got %d", w.Code)
	}

	// HEAD requests should not have body content
	if w.Body.Len() != 0 {
		t.Errorf("Expected empty body for HEAD request, got %d bytes", w.Body.Len())
	}

	// But should have headers
	contentLength := w.Header().Get("Content-Length")
	if contentLength != "1000" {
		t.Errorf("Expected Content-Length 1000, got %s", contentLength)
	}

	// Test HEAD request with range
	req = httptest.NewRequest("HEAD", "/test", nil)
	req.Header.Set("Range", "bytes=0-499")
	w = httptest.NewRecorder()

	server.serveVideoFile(w, req, testFile, "video/mp4")

	if w.Code != http.StatusPartialContent {
		t.Errorf("Expected status 206 for HEAD range request, got %d", w.Code)
	}

	if w.Body.Len() != 0 {
		t.Errorf("Expected empty body for HEAD request, got %d bytes", w.Body.Len())
	}

	contentRange := w.Header().Get("Content-Range")
	if !strings.Contains(contentRange, "bytes 0-499/1000") {
		t.Errorf("Expected proper Content-Range header, got %s", contentRange)
	}
}