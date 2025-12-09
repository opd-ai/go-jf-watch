package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{
			name:    "valid version",
			version: "1.0.0",
			wantErr: false,
		},
		{
			name:    "empty version",
			version: "",
			wantErr: false,
		},
		{
			name:    "version with special characters",
			version: "v1.0.0-beta+build.123",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ui, err := New(tt.version)
			
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, ui)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, ui)
				assert.Equal(t, tt.version, ui.version)
				assert.NotNil(t, ui.staticFS)
				assert.NotNil(t, ui.templates)
			}
		})
	}
}

func TestUI_RegisterRoutes(t *testing.T) {
	ui, err := New("test-version")
	require.NoError(t, err)
	
	router := chi.NewRouter()
	ui.RegisterRoutes(router)
	
	// Test that routes are registered
	// We can't easily test chi route registration directly,
	// but we can test the handlers work
	server := httptest.NewServer(router)
	defer server.Close()
	
	// Test index route
	resp, err := http.Get(server.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()
	
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
}

func TestUI_serveIndex(t *testing.T) {
	ui, err := New("test-version")
	require.NoError(t, err)
	
	tests := []struct {
		name           string
		method         string
		expectedStatus int
		expectedType   string
		checkContent   bool
	}{
		{
			name:           "GET request",
			method:         "GET",
			expectedStatus: http.StatusOK,
			expectedType:   "text/html; charset=utf-8",
			checkContent:   true,
		},
		{
			name:           "POST request should work",
			method:         "POST", 
			expectedStatus: http.StatusOK,
			expectedType:   "text/html; charset=utf-8",
			checkContent:   false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/", nil)
			w := httptest.NewRecorder()
			
			ui.serveIndex(w, req)
			
			resp := w.Result()
			defer resp.Body.Close()
			
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
			assert.Equal(t, tt.expectedType, resp.Header.Get("Content-Type"))
			
			if tt.checkContent {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				
				content := string(body)
				assert.Contains(t, content, "<!DOCTYPE html>")
				assert.Contains(t, content, "Jellyfin Local Cache")
				assert.Contains(t, content, "test-version")
				assert.Contains(t, content, "/static/css/water.css")
				assert.Contains(t, content, "/static/js/app.js")
			}
		})
	}
}

func TestUI_serveStatic(t *testing.T) {
	ui, err := New("test-version")
	require.NoError(t, err)
	
	handler := ui.serveStatic()
	
	tests := []struct {
		name           string
		path           string
		expectedStatus int
		checkContent   bool
		contentCheck   string
	}{
		{
			name:           "CSS file",
			path:           "/css/water.css",
			expectedStatus: http.StatusOK,
			checkContent:   true,
			contentCheck:   ":root",
		},
		{
			name:           "JavaScript file", 
			path:           "/js/app.js",
			expectedStatus: http.StatusOK,
			checkContent:   true,
			contentCheck:   "class JFWatch",
		},
		{
			name:           "non-existent file",
			path:           "/nonexistent.txt",
			expectedStatus: http.StatusNotFound,
			checkContent:   false,
		},
		{
			name:           "directory traversal attempt",
			path:           "/../../../etc/passwd",
			expectedStatus: http.StatusNotFound,
			checkContent:   false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()
			
			handler.ServeHTTP(w, req)
			
			resp := w.Result()
			defer resp.Body.Close()
			
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
			
			if tt.checkContent && tt.expectedStatus == http.StatusOK {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				
				content := string(body)
				assert.Contains(t, content, tt.contentCheck)
			}
		})
	}
}

func TestUI_GetStaticFS(t *testing.T) {
	ui, err := New("test-version")
	require.NoError(t, err)
	
	staticFS := ui.GetStaticFS()
	assert.NotNil(t, staticFS)
	
	// Test that we can read files from the filesystem
	file, err := staticFS.Open("css/water.css")
	require.NoError(t, err)
	defer file.Close()
	
	content, err := io.ReadAll(file)
	require.NoError(t, err)
	assert.NotEmpty(t, content)
	assert.Contains(t, string(content), ":root")
}

func TestUI_GetTemplates(t *testing.T) {
	ui, err := New("test-version")
	require.NoError(t, err)
	
	templates := ui.GetTemplates()
	assert.NotNil(t, templates)
	
	// Test that we can execute the template
	var output strings.Builder
	err = templates.ExecuteTemplate(&output, "index.html", TemplateData{
		Title:   "Test Title",
		Version: "test-version",
	})
	require.NoError(t, err)
	
	content := output.String()
	assert.Contains(t, content, "Test Title")
	assert.Contains(t, content, "test-version")
	assert.Contains(t, content, "<!DOCTYPE html>")
}

func TestTemplateData(t *testing.T) {
	tests := []struct {
		name string
		data TemplateData
	}{
		{
			name: "basic data",
			data: TemplateData{
				Title:   "Test Page",
				Version: "1.0.0",
				Data:    nil,
			},
		},
		{
			name: "with custom data",
			data: TemplateData{
				Title:   "Custom Page",
				Version: "2.0.0",
				Data:    map[string]string{"key": "value"},
			},
		},
	}
	
	ui, err := New("test-version")
	require.NoError(t, err)
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output strings.Builder
			err := ui.templates.ExecuteTemplate(&output, "index.html", tt.data)
			require.NoError(t, err)
			
			content := output.String()
			assert.Contains(t, content, tt.data.Title)
			assert.Contains(t, content, tt.data.Version)
		})
	}
}

func TestUI_Integration(t *testing.T) {
	// Test full integration with chi router
	ui, err := New("integration-test")
	require.NoError(t, err)
	
	router := chi.NewRouter()
	ui.RegisterRoutes(router)
	
	server := httptest.NewServer(router)
	defer server.Close()
	
	tests := []struct {
		name           string
		path           string
		expectedStatus int
		contentCheck   string
	}{
		{
			name:           "index page",
			path:           "/",
			expectedStatus: http.StatusOK,
			contentCheck:   "Jellyfin Local Cache",
		},
		{
			name:           "CSS file",
			path:           "/static/css/water.css",
			expectedStatus: http.StatusOK,
			contentCheck:   ":root",
		},
		{
			name:           "JavaScript file",
			path:           "/static/js/app.js", 
			expectedStatus: http.StatusOK,
			contentCheck:   "JFWatch",
		},
		{
			name:           "missing static file",
			path:           "/static/missing.txt",
			expectedStatus: http.StatusNotFound,
			contentCheck:   "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(server.URL + tt.path)
			require.NoError(t, err)
			defer resp.Body.Close()
			
			assert.Equal(t, tt.expectedStatus, resp.StatusCode)
			
			if tt.expectedStatus == http.StatusOK && tt.contentCheck != "" {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.Contains(t, string(body), tt.contentCheck)
			}
		})
	}
}