package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/opd-ai/go-jf-watch/internal/storage"
	"github.com/opd-ai/go-jf-watch/pkg/config"
)

func TestNew(t *testing.T) {
	cfg := &config.ServerConfig{
		Port:              8080,
		Host:              "localhost",
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		EnableCompression: true,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Create temporary storage for testing
	tmpDir := t.TempDir()
	storageManager, err := storage.NewManager(tmpDir, 1000, logger)
	if err != nil {
		t.Fatalf("Failed to create storage manager: %v", err)
	}
	defer storageManager.Close()

	server := New(cfg, storageManager, logger)

	if server == nil {
		t.Fatal("Expected server to be non-nil")
	}

	if server.config != cfg {
		t.Error("Expected config to be set correctly")
	}

	if server.storage != storageManager {
		t.Error("Expected storage to be set correctly")
	}

	if server.httpServer.Addr != "localhost:8080" {
		t.Errorf("Expected server address to be localhost:8080, got %s", server.httpServer.Addr)
	}
}

func TestHealthEndpoint(t *testing.T) {
	server := createTestServer(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response APIResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !response.Success {
		t.Error("Expected success to be true")
	}

	if response.Message != "Server is healthy" {
		t.Errorf("Expected health message, got: %s", response.Message)
	}
}

func TestAPIStatusEndpoint(t *testing.T) {
	server := createTestServer(t)

	req := httptest.NewRequest("GET", "/api/status", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response APIResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !response.Success {
		t.Error("Expected success to be true")
	}

	// Verify status data structure
	statusData, ok := response.Data.(map[string]interface{})
	if !ok {
		t.Fatal("Expected response data to be a map")
	}

	expectedFields := []string{"status", "version", "uptime", "cache_size_bytes", "cache_items", "queue_length", "active_jobs"}
	for _, field := range expectedFields {
		if _, exists := statusData[field]; !exists {
			t.Errorf("Expected status field '%s' to be present", field)
		}
	}
}

func TestLibraryEndpoint(t *testing.T) {
	server := createTestServer(t)

	// Test without parameters
	req := httptest.NewRequest("GET", "/api/library", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response APIResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !response.Success {
		t.Error("Expected success to be true")
	}

	// Test with pagination parameters
	req = httptest.NewRequest("GET", "/api/library?page=1&limit=10&type=movies", nil)
	w = httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestQueueEndpoints(t *testing.T) {
	server := createTestServer(t)

	// Test queue status
	req := httptest.NewRequest("GET", "/api/queue", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Test add to queue
	addRequest := AddToQueueRequest{
		MediaID:  "test-media-123",
		Priority: 3,
	}

	requestBody, _ := json.Marshal(addRequest)
	req = httptest.NewRequest("POST", "/api/queue/add", bytes.NewBuffer(requestBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", w.Code)
	}

	var response APIResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !response.Success {
		t.Error("Expected success to be true")
	}

	// Test remove from queue
	req = httptest.NewRequest("DELETE", "/api/queue/test-queue-id", nil)
	w = httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestQueueAddValidation(t *testing.T) {
	server := createTestServer(t)

	tests := []struct {
		name           string
		requestBody    string
		expectedStatus int
		expectError    bool
	}{
		{
			name:           "valid request",
			requestBody:    `{"media_id": "test-123", "priority": 2}`,
			expectedStatus: http.StatusCreated,
			expectError:    false,
		},
		{
			name:           "missing media ID",
			requestBody:    `{"priority": 2}`,
			expectedStatus: http.StatusBadRequest,
			expectError:    true,
		},
		{
			name:           "invalid JSON",
			requestBody:    `{"media_id": "test-123", "priority":}`,
			expectedStatus: http.StatusBadRequest,
			expectError:    true,
		},
		{
			name:           "default priority",
			requestBody:    `{"media_id": "test-456"}`,
			expectedStatus: http.StatusCreated,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/queue/add", strings.NewReader(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.router.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			var response APIResponse
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if tt.expectError && response.Success {
				t.Error("Expected error response")
			}

			if !tt.expectError && !response.Success {
				t.Errorf("Expected success response, got error: %s", response.Error)
			}
		})
	}
}

func TestStaticFilesEndpoint(t *testing.T) {
	server := createTestServer(t)

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/html" {
		t.Errorf("Expected Content-Type text/html, got %s", contentType)
	}

	body := w.Body.String()
	if !strings.Contains(body, "go-jf-watch") {
		t.Error("Expected response body to contain 'go-jf-watch'")
	}
}

func TestCORSMiddleware(t *testing.T) {
	server := createTestServer(t)

	// Test preflight request
	req := httptest.NewRequest("OPTIONS", "/api/status", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	// Check CORS headers
	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Error("Expected Access-Control-Allow-Origin header")
	}

	if w.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Error("Expected Access-Control-Allow-Methods header")
	}
}

func TestLoggingMiddleware(t *testing.T) {
	server := createTestServer(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	server.router.ServeHTTP(w, req)

	// The middleware should not affect the response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestServerStartStop(t *testing.T) {
	cfg := &config.ServerConfig{
		Port:              0, // Use random port for testing
		Host:              "localhost",
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		EnableCompression: false,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	tmpDir := t.TempDir()
	storageManager, err := storage.NewManager(tmpDir, 1000, logger)
	if err != nil {
		t.Fatalf("Failed to create storage manager: %v", err)
	}
	defer storageManager.Close()

	server := New(cfg, storageManager, logger)

	// Test server stop without starting
	if err := server.Stop(); err != nil {
		// Stop should handle being called without Start
		t.Errorf("Stop() returned error: %v", err)
	}
}

// Helper function to create a test server instance
func createTestServer(t *testing.T) *Server {
	cfg := &config.ServerConfig{
		Port:              8080,
		Host:              "localhost",
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		EnableCompression: true,
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Suppress logs during tests
	}))

	tmpDir := t.TempDir()
	storageManager, err := storage.NewManager(tmpDir, 1000, logger)
	if err != nil {
		t.Fatalf("Failed to create storage manager: %v", err)
	}

	// Cleanup will be handled by test cleanup
	t.Cleanup(func() {
		storageManager.Close()
	})

	return New(cfg, storageManager, logger)
}