# go-jf-watch

A self-hosted web UI that intelligently pre-caches Jellyfin media locally to minimize streaming latency through predictive downloading.

[![Go Version](https://img.shields.io/badge/Go-1.21+-blue.svg)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20Windows%20%7C%20macOS-lightgrey.svg)]()

## Features

- 🚀 **Instant Playback**: Downloads currently watched episodes immediately at full bandwidth
- 🧠 **Smart Prediction**: Automatically queues next episodes based on viewing patterns
- 💾 **Efficient Storage**: Intelligent cache management with configurable limits
- 🌐 **Modern Web UI**: Complete interface with Video.js player and real-time updates
- ⚡ **Minimal Latency**: <1 second startup for cached content
- 🔧 **Single Binary**: Complete deployment with embedded web UI and assets
- 📱 **Responsive Design**: Mobile-first interface using Water.css framework
- 🔄 **Real-time Updates**: WebSocket connections for live download progress
- ⚙️ **Configuration UI**: Web-based settings management with form validation

## Quick Start

### Prerequisites

- Go 1.21+ (for development)
- Access to a Jellyfin server
- 500GB+ available disk space (configurable)

### Installation

#### Option 1: Pre-built Binary (Coming Soon)
```bash
# Download latest release for your platform
wget https://github.com/opd-ai/go-jf-watch/releases/latest/download/go-jf-watch-linux-amd64
chmod +x go-jf-watch-linux-amd64
```

#### Option 2: Build from Source
```bash
git clone https://github.com/opd-ai/go-jf-watch.git
cd go-jf-watch
go build -o go-jf-watch cmd/go-jf-watch/main.go
```

### Configuration

1. Copy the example configuration:
```bash
cp config.example.yaml config.yaml
```

2. Edit `config.yaml` with your Jellyfin server details:
```yaml
jellyfin:
  server_url: "https://your-jellyfin-server.com"
  api_key: "your-jellyfin-api-key"
  user_id: "your-jellyfin-user-id"

cache:
  directory: "./cache"
  max_size_gb: 500

download:
  workers: 3
  rate_limit_mbps: 10
  auto_download_current: true
  auto_download_next: true

server:
  port: 8080
  host: "0.0.0.0"
```

3. Get your Jellyfin API key and user ID:
   - Log into Jellyfin web interface
   - Go to Administration → Dashboard → API Keys
   - Create a new API key
   - Get your user ID from Administration → Users

### Running

```bash
./go-jf-watch
```

Access the web UI at `http://localhost:8080`

## How It Works

### Intelligent Download Strategy

**Priority System:**
- **Priority 0**: Currently playing episode (immediate download at full speed)
- **Priority 1**: Next unwatched episode in series
- **Priority 2**: Following episodes in sequence
- **Priority 3**: New content matching your preferences

### Smart Bandwidth Management

- **Current Episode**: Always uses full bandwidth for instant playbook
- **Peak Hours** (6AM-11PM): Background downloads use 25% bandwidth
- **Off-Peak** (11PM-6AM): Full bandwidth for all downloads
- **Configurable**: Adjust limits based on your network capacity

### Automatic Cache Management

- **Intelligent Eviction**: Removes old content when storage limit reached
- **Protection**: Never evicts currently playing or downloading content
- **Configurable Limits**: Set maximum cache size and cleanup thresholds

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Jellyfin      │    │   go-jf-watch   │    │   Local Cache   │
│   Server        │◄──►│   Monitor       │──►│   Storage       │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                │
                                ▼
                       ┌─────────────────┐
                       │   Web UI        │
                       │   Player        │
                       └─────────────────┘
```

**Components:**
- **API Monitor**: Tracks viewing patterns via Jellyfin API
- **Download Predictor**: Queues likely-next media intelligently  
- **Worker Pool**: Manages concurrent downloads with rate limiting
- **Storage Manager**: Handles cache organization and cleanup
- **Web Server**: Serves local content with fallback to remote

## Configuration Reference

### Complete Configuration Example

```yaml
jellyfin:
  server_url: "https://jellyfin.example.com"
  api_key: "your-jellyfin-api-key" 
  user_id: "jellyfin-user-id"
  timeout: "30s"
  retry_attempts: 3

cache:
  directory: "./cache"
  max_size_gb: 500
  eviction_threshold: 0.85
  metadata_store: "boltdb"
  temp_directory: "./cache/temp"

download:
  workers: 3
  rate_limit_mbps: 10
  rate_limit_schedule:
    peak_hours: "06:00-23:00"
    peak_limit_percent: 25
  auto_download_current: true
  auto_download_next: true
  auto_download_count: 2
  current_episode_priority: true
  retry_attempts: 5
  retry_delay: "1s"

server:
  port: 8080
  host: "0.0.0.0"
  read_timeout: "15s"
  write_timeout: "15s"
  enable_compression: true

prediction:
  enabled: true
  sync_interval: "4h"
  history_days: 30
  min_confidence: 0.7

logging:
  level: "info"
  format: "json"
  file: ""
  max_size_mb: 100

ui:
  theme: "auto"
  language: "en"
  video_quality_preference: "original"
```

### Key Settings Explained

| Setting | Description | Default |
|---------|-------------|---------|
| `cache.max_size_gb` | Maximum cache size before cleanup | 500 |
| `download.workers` | Concurrent download threads | 3 |
| `download.rate_limit_mbps` | Maximum download speed | 10 |
| `download.auto_download_current` | Download current episode immediately | true |
| `server.port` | Web UI port | 8080 |
| `prediction.sync_interval` | How often to check for new content | 4h |

## API Reference

### REST Endpoints

```
GET  /                          # Web UI
GET  /api/library               # Cached library items  
GET  /api/queue                 # Download queue status
POST /api/queue/add             # Add item to download queue
DEL  /api/queue/{id}            # Remove from queue
GET  /stream/{id}               # Local video streaming
GET  /api/status                # System status and stats
```

### WebSocket

```
WS   /ws/progress               # Real-time download progress
```

## Development

### Prerequisites

- Go 1.21+
- Node.js 16+ (for frontend development)
- Air (for hot reload): `go install github.com/cosmtrek/air@latest`

### Development Setup

```bash
# Clone repository
git clone https://github.com/opd-ai/go-jf-watch.git
cd go-jf-watch

# Install dependencies
go mod download

# Run with hot reload
air

# Or build and run manually
go build -o go-jf-watch cmd/go-jf-watch/main.go
./go-jf-watch
```

### Project Structure

```
go-jf-watch/
├── cmd/go-jf-watch/           # Application entrypoint
├── internal/
│   ├── jellyfin/              # Jellyfin API integration
│   ├── downloader/            # Download management
│   ├── storage/               # Storage & metadata
│   ├── server/                # HTTP server & API
│   └── ui/                    # Frontend assets
├── pkg/config/                # Configuration management
├── web/                       # Frontend source files
└── scripts/                   # Build & utility scripts
```

### Building

```bash
# Build for current platform
make build

# Build for all platforms
make build-all

# Run tests
make test

# Clean build artifacts
make clean
```

## Deployment

### Systemd Service (Linux)

Create `/etc/systemd/system/go-jf-watch.service`:

```ini
[Unit]
Description=Jellyfin Local Cache Service
After=network.target

[Service]
Type=simple
User=jellyfin
WorkingDirectory=/opt/go-jf-watch
ExecStart=/opt/go-jf-watch/go-jf-watch
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl enable go-jf-watch
sudo systemctl start go-jf-watch
```

### Docker (Coming Soon)

```dockerfile
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY go-jf-watch .
COPY config.yaml .
EXPOSE 8080
CMD ["./go-jf-watch"]
```

### Reverse Proxy (Optional)

Nginx configuration for external access:

```nginx
server {
    listen 80;
    server_name watch.yourdomain.com;
    
    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
    
    location /ws/ {
        proxy_pass http://localhost:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

## Troubleshooting

### Common Issues

**Connection to Jellyfin fails:**
- Verify `server_url`, `api_key`, and `user_id` in config
- Check network connectivity to Jellyfin server
- Ensure API key has proper permissions

**Downloads not starting:**
- Check available disk space
- Verify download workers setting
- Review logs for rate limiting or network errors

**High memory usage:**
- Reduce number of concurrent workers
- Check cache directory for corruption
- Monitor download progress for stuck transfers

**UI not loading:**
- Verify port 8080 is not blocked by firewall
- Check server logs for startup errors
- Ensure proper file permissions on cache directory

### Debug Mode

Enable detailed logging:

```yaml
logging:
  level: "debug"
  format: "text"
```

### Getting Help

- 📖 [Documentation](https://github.com/opd-ai/go-jf-watch/wiki)
- 🐛 [Report Issues](https://github.com/opd-ai/go-jf-watch/issues)
- 💬 [Discussions](https://github.com/opd-ai/go-jf-watch/discussions)

## Performance & Requirements

### System Requirements

- **CPU**: 1+ cores (benefits from multi-core for downloads)
- **Memory**: 200MB+ RAM (plus buffer cache)
- **Storage**: 500GB+ available space (configurable)
- **Network**: Broadband connection (10+ Mbps recommended)

### Performance Targets

- **Local Playback**: <1 second startup latency
- **UI Response**: <200ms navigation, <2s library loading  
- **Download Efficiency**: 85%+ bandwidth utilization
- **Memory Footprint**: <200MB under normal operation

## Roadmap

### Version 1.0 (Current)
- ✅ Core caching and predictive downloads
- ✅ Web UI with video player
- ✅ Storage management and cleanup
- ✅ Jellyfin API integration

### Version 1.1 (Planned)
- 🔲 Multi-user session support
- 🔲 Subtitle download and caching
- 🔲 Mobile app companion
- 🔲 Advanced analytics and metrics

### Version 2.0 (Future)
- 🔲 Transcoding support
- 🔲 Distributed cache sharing
- 🔲 Machine learning prediction improvements
- 🔲 Plugin system for extensibility

## Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

### Development Process

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Add tests for new functionality
5. Commit your changes (`git commit -m 'Add amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

### Code Style

- Follow standard Go formatting (`gofmt`)
- Write tests for new features
- Update documentation for API changes
- Follow conventional commit messages

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- [Jellyfin](https://jellyfin.org/) - The amazing media server that makes this possible
- [jellyfin-go](https://github.com/sj14/jellyfin-go) - Go client library for Jellyfin API
- [BoltDB](https://github.com/etcd-io/bbolt) - Embedded database for metadata storage
- [Chi](https://github.com/go-chi/chi) - Lightweight HTTP router for Go

---

**Made with ❤️ for the Jellyfin community**
