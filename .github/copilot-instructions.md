# Project Overview

`go-jf-watch` is a self-hosted web UI that intelligently pre-caches Jellyfin media locally to minimize streaming latency through predictive downloading. The system implements a caching proxy for Jellyfin media servers, designed for single-user or household deployment on commodity hardware. It monitors user viewing patterns, predictively downloads media to local storage before playback, and serves cached content via a local web interface with automatic storage management.

The architecture follows clean separation of concerns with a Go backend serving an embedded web UI. The system prioritizes currently playing content (Priority 0) with immediate full-bandwidth downloads, while intelligently queuing next episodes and managing storage with LRU eviction policies. Target users are Jellyfin administrators seeking to eliminate buffering and reduce bandwidth usage during peak viewing times.

## Technical Stack

- **Primary Language**: Go 1.21+ (required for `slog` and enhanced `embed` features)
- **HTTP Framework**: `github.com/go-chi/chi/v5` for routing with stdlib `net/http` server
- **Database**: `go.etcd.io/bbolt` for embedded metadata storage (no SQL databases)
- **Configuration**: `github.com/knadh/koanf/v2` with YAML support via `gopkg.in/yaml.v3`
- **Logging**: `log/slog` (stdlib) for structured logging
- **Testing**: `testing` (stdlib) + `github.com/stretchr/testify` for assertions
- **Build/Deploy**: Single binary with embedded assets via `embed` package, Makefile for builds
- **Frontend**: Video.js player with htmx and Water.css, embedded via `//go:embed`
- **External APIs**: Custom HTTP client for Jellyfin server integration with API key authentication

## Code Assistance Guidelines

1. **Prioritize Standard Library**: Use stdlib packages (`net/http`, `encoding/json`, `log/slog`) over external dependencies when performance is adequate. Only add external dependencies when stdlib is insufficient for the specific use case.

2. **Follow Worker Pool Patterns**: Implement concurrent operations using channels and goroutine pools. Download manager should use `chan *DownloadJob` and `chan *DownloadResult` for coordination. Limit concurrent workers to 3-5 goroutines for downloads.

3. **Implement Priority-Based Downloads**: Use priority system where Priority 0 (currently playing) gets immediate full bandwidth, Priority 1 (next episodes) queued automatically, and higher priorities for predictive content. Always protect currently playing/downloading content from eviction.

4. **Use BoltDB Bucket Organization**: Structure BoltDB with buckets: `downloads`, `queue`, `metadata`, `config`, `stats`. Use key patterns like `{media-type}:{jellyfin-id}` for downloads and `{priority}:{timestamp}:{id}` for queue items.

5. **Embed Frontend Assets**: Use `//go:embed web/static/*` and `//go:embed web/templates/*` for asset embedding. Serve static files through chi router with fallback handling for single-page application routing.

6. **Implement HTTP Range Support**: Video streaming must support HTTP Range requests for seeking. Use `http.ServeContent()` with proper Content-Type detection and range handling for cached media files.

7. **Handle Configuration with Validation**: Load configuration using koanf with YAML provider. Implement validation for critical settings like cache directory permissions, Jellyfin connectivity, and storage limits before startup.

## Project Context

- **Domain**: Media streaming optimization and predictive caching for Jellyfin servers. Key concepts include viewing patterns analysis, storage eviction policies (LRU with protection), bandwidth scheduling (peak/off-peak), and download priority queues.

- **Architecture**: Clean architecture with separation: `cmd/` for entrypoint, `internal/` for core logic (jellyfin, downloader, storage, server, ui), `pkg/config/` for shared configuration types, `web/` for frontend source files.

- **Key Directories**: 
  - `internal/jellyfin/`: API client wrapper with authentication and session management
  - `internal/downloader/`: Worker pool pattern with priority queue and rate limiting
  - `internal/storage/`: BoltDB operations and filesystem cache organization
  - `internal/server/`: Chi router with streaming endpoints and WebSocket progress updates

- **Configuration**: Single YAML file with sections for jellyfin (server_url, api_key, user_id), cache (directory, max_size_gb), download (workers, rate_limit_mbps), server (port, host), and logging (level, format).

## Quality Standards

- **Testing Requirements**: Maintain >70% code coverage using Go's built-in testing package. Write table-driven tests for business logic functions (predictors, storage managers). Include integration tests for Jellyfin API interactions and HTTP endpoints using testify/suite.

- **Code Review Criteria**: All public functions must have godoc comments. Use structured logging with slog for all operations. Implement proper error handling with context propagation. Follow Go naming conventions and use gofmt for formatting.

- **Documentation Standards**: Update README.md for user-facing changes. Maintain PLAN.md for architectural decisions. Include configuration examples in config.example.yaml. Document API endpoints in OpenAPI format when adding new REST endpoints.

## Networking Best Practices (for Go projects)

When declaring network variables, always use interface types:
- Never use `net.UDPAddr`, `net.IPAddr`, or `net.TCPAddr`. Use `net.Addr` only instead.
- Never use `net.UDPConn`, use `net.PacketConn` instead
- Never use `net.TCPConn`, use `net.Conn` instead
- Never use `net.UDPListener` or `net.TCPListener`, use `net.Listener` instead
- Never use a type switch or type assertion to convert from an interface type to a concrete type. Use the interface methods instead.

This approach enhances testability and flexibility when working with different network implementations or mocks.

## Jellyfin-Specific Patterns

- **API Client Implementation**: Custom HTTP client that handles retry logic, session management, and rate limiting using stdlib `net/http`. Implement automatic token refresh with fallback to API key authentication.

- **Media Metadata Caching**: Cache Jellyfin library metadata in BoltDB to reduce API calls. Implement periodic sync (default 4h intervals) with conflict resolution for updated media information.

- **Filesystem Organization**: Organize cached media as `cache/movies/{jellyfin-id}/` and `cache/series/{series-id}/S{season:02d}E{episode:02d}/` with original filenames preserved and separate `.meta.json` files for local metadata.

- **Predictive Logic**: Implement viewing pattern analysis based on Jellyfin playback history. Queue next episodes automatically when user starts current episode, with configurable lookahead (default 2 episodes).

## Performance Considerations

- **Memory Management**: Target <200MB memory usage under normal operation. Use streaming for large file operations and implement proper cleanup of goroutines and file handles.

- **Storage Efficiency**: Implement intelligent eviction starting at 85% capacity using LRU with protection for currently playing content. Use atomic file operations via `github.com/natefinch/atomic` for safe concurrent access.

- **Bandwidth Management**: Support configurable rate limiting with peak/off-peak scheduling. Currently playing content always gets full bandwidth regardless of time-based restrictions.