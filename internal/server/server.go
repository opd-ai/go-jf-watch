// Package server provides HTTP server functionality for go-jf-watch.
// It implements a REST API for cache management and video streaming with
// HTTP Range support for seeking. The server uses chi/v5 for routing with
// CORS support for development and WebSocket connections for real-time updates.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/opd-ai/go-jf-watch/internal/storage"
	"github.com/opd-ai/go-jf-watch/pkg/config"
)

// Server represents the HTTP server for go-jf-watch.
// It provides REST API endpoints, video streaming, and WebSocket connections
// for real-time download progress updates.
type Server struct {
	config      *config.ServerConfig
	logger      *slog.Logger
	storage     *storage.Manager
	httpServer  *http.Server
	router      chi.Router
}

// New creates a new HTTP server instance with the provided configuration.
// The server is configured with middleware for logging, CORS, and request recovery.
func New(cfg *config.ServerConfig, storage *storage.Manager, logger *slog.Logger) *Server {
	s := &Server{
		config:  cfg,
		logger:  logger,
		storage: storage,
	}

	// Create router with middleware
	s.router = chi.NewRouter()
	s.setupMiddleware()
	s.setupRoutes()

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      s.router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// setupMiddleware configures the middleware stack for the router.
// Includes request ID, logging, recovery, compression, and CORS support.
func (s *Server) setupMiddleware() {
	// Basic middleware
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(s.loggingMiddleware())
	s.router.Use(middleware.Recoverer)

	// Compression middleware if enabled
	if s.config.EnableCompression {
		s.router.Use(middleware.Compress(5))
	}

	// CORS configuration for development
	s.router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"}, // Configure appropriately for production
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Set timeout for requests
	s.router.Use(middleware.Timeout(30 * time.Second))
}

// setupRoutes configures all HTTP routes for the server.
// Defines REST API endpoints, static file serving, and WebSocket endpoints.
func (s *Server) setupRoutes() {
	// Health check endpoint
	s.router.Get("/health", s.handleHealth)

	// API routes
	s.router.Route("/api", func(r chi.Router) {
		r.Get("/status", s.handleAPIStatus)
		r.Get("/library", s.handleLibrary)
		r.Route("/queue", func(r chi.Router) {
			r.Get("/", s.handleQueueStatus)
			r.Post("/add", s.handleQueueAdd)
			r.Delete("/{id}", s.handleQueueRemove)
		})
	})

	// Video streaming endpoint with Range support
	s.router.Get("/stream/{id}", s.handleVideoStream)

	// WebSocket endpoint for real-time updates
	s.router.Get("/ws/progress", s.handleWebSocket)

	// Static file serving (placeholder for Phase 4)
	s.router.Get("/*", s.handleStaticFiles)
}

// Start starts the HTTP server in a goroutine.
// Returns immediately and the server runs until Stop is called or context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	s.logger.Info("Starting HTTP server", 
		"address", s.httpServer.Addr,
		"read_timeout", s.config.ReadTimeout,
		"write_timeout", s.config.WriteTimeout)

	// Start server in goroutine
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP server error", "error", err)
		}
	}()

	// Wait for context cancellation
	<-ctx.Done()
	return s.Stop()
}

// Stop gracefully shuts down the HTTP server.
// Waits up to 30 seconds for active connections to complete.
func (s *Server) Stop() error {
	s.logger.Info("Stopping HTTP server")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.logger.Error("Error shutting down HTTP server", "error", err)
		return err
	}

	s.logger.Info("HTTP server stopped successfully")
	return nil
}

// loggingMiddleware creates a structured logging middleware for HTTP requests.
// Logs request method, path, status code, duration, and client IP.
func (s *Server) loggingMiddleware() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			
			// Wrap response writer to capture status code
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			
			// Process request
			next.ServeHTTP(ww, r)
			
			// Log request
			s.logger.Info("HTTP request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start),
				"ip", r.RemoteAddr,
				"user_agent", r.UserAgent(),
			)
		})
	}
}