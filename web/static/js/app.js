// go-jf-watch Web UI JavaScript
class JFWatch {
    constructor() {
        this.ws = null;
        this.reconnectInterval = 5000;
        this.currentView = 'library';
        this.init();
    }

    init() {
        this.connectWebSocket();
        this.setupEventListeners();
        this.loadInitialData();
    }

    // WebSocket connection for real-time updates
    connectWebSocket() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${protocol}//${window.location.host}/ws/progress`;
        
        this.ws = new WebSocket(wsUrl);
        
        this.ws.onopen = () => {
            console.log('WebSocket connected');
            this.updateConnectionStatus(true);
        };
        
        this.ws.onmessage = (event) => {
            try {
                const data = JSON.parse(event.data);
                this.handleWebSocketMessage(data);
            } catch (e) {
                console.error('Failed to parse WebSocket message:', e);
            }
        };
        
        this.ws.onclose = () => {
            console.log('WebSocket disconnected');
            this.updateConnectionStatus(false);
            setTimeout(() => this.connectWebSocket(), this.reconnectInterval);
        };
        
        this.ws.onerror = (error) => {
            console.error('WebSocket error:', error);
        };
    }

    handleWebSocketMessage(data) {
        switch (data.type) {
            case 'download_progress':
                this.updateDownloadProgress(data.id, data.progress);
                break;
            case 'download_complete':
                this.markDownloadComplete(data.id);
                break;
            case 'download_error':
                this.markDownloadError(data.id, data.error);
                break;
            case 'queue_update':
                this.refreshQueue();
                break;
            default:
                console.log('Unknown message type:', data.type);
        }
    }

    updateConnectionStatus(connected) {
        const statusEl = document.getElementById('connection-status');
        if (statusEl) {
            statusEl.textContent = connected ? 'Connected' : 'Disconnected';
            statusEl.className = connected ? 'status-cached' : 'status-remote';
        }
    }

    // Event listeners
    setupEventListeners() {
        // Navigation
        document.addEventListener('click', (e) => {
            if (e.target.matches('.nav-link')) {
                e.preventDefault();
                this.showView(e.target.dataset.view);
            }
            
            // Queue actions
            if (e.target.matches('.btn-download')) {
                this.addToQueue(e.target.dataset.id);
            }
            
            if (e.target.matches('.btn-remove')) {
                this.removeFromQueue(e.target.dataset.id);
            }
            
            if (e.target.matches('.btn-pause')) {
                this.pauseDownload(e.target.dataset.id);
            }
            
            if (e.target.matches('.btn-resume')) {
                this.resumeDownload(e.target.dataset.id);
            }
            
            // Video player actions
            if (e.target.matches('.btn-play-local')) {
                this.playVideo(e.target.dataset.id, 'local');
            }
            
            if (e.target.matches('.btn-play-remote')) {
                this.playVideo(e.target.dataset.id, 'remote');
            }
        });

        // Settings form submission
        document.addEventListener('submit', (e) => {
            if (e.target.matches('#settings-form')) {
                e.preventDefault();
                this.saveSettings(new FormData(e.target));
            }
        });

        // Search functionality
        const searchInput = document.getElementById('search-input');
        if (searchInput) {
            let searchTimeout;
            searchInput.addEventListener('input', (e) => {
                clearTimeout(searchTimeout);
                searchTimeout = setTimeout(() => {
                    this.searchLibrary(e.target.value);
                }, 300);
            });
        }
    }

    // View management
    showView(viewName) {
        // Update navigation
        document.querySelectorAll('.nav-link').forEach(link => {
            link.classList.toggle('active', link.dataset.view === viewName);
        });

        // Hide all views
        document.querySelectorAll('[data-view]').forEach(view => {
            view.style.display = 'none';
        });

        // Show selected view
        const targetView = document.querySelector(`[data-view="${viewName}"]`);
        if (targetView) {
            targetView.style.display = 'block';
            this.currentView = viewName;
            
            // Load view-specific data
            switch (viewName) {
                case 'library':
                    this.loadLibrary();
                    break;
                case 'queue':
                    this.loadQueue();
                    break;
                case 'settings':
                    this.loadSettings();
                    break;
            }
        }
    }

    // API calls
    async apiCall(endpoint, options = {}) {
        try {
            const response = await fetch(`/api${endpoint}`, {
                headers: {
                    'Content-Type': 'application/json',
                    ...options.headers
                },
                ...options
            });
            
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }
            
            return await response.json();
        } catch (error) {
            console.error(`API call failed for ${endpoint}:`, error);
            this.showError(`Failed to ${endpoint}: ${error.message}`);
            throw error;
        }
    }

    // Data loading
    async loadInitialData() {
        try {
            const status = await this.apiCall('/status');
            this.updateSystemStatus(status);
        } catch (error) {
            console.error('Failed to load initial data:', error);
        }
    }

    async loadLibrary() {
        try {
            this.showLoading('library-container');
            const library = await this.apiCall('/library');
            this.renderLibrary(library.items || []);
        } catch (error) {
            this.showError('Failed to load library');
        } finally {
            this.hideLoading('library-container');
        }
    }

    async loadQueue() {
        try {
            this.showLoading('queue-container');
            const queue = await this.apiCall('/queue');
            this.renderQueue(queue.items || []);
        } catch (error) {
            this.showError('Failed to load queue');
        } finally {
            this.hideLoading('queue-container');
        }
    }

    async loadSettings() {
        try {
            const settings = await this.apiCall('/settings');
            this.populateSettings(settings);
        } catch (error) {
            this.showError('Failed to load settings');
        }
    }

    // Rendering methods
    renderLibrary(items) {
        const container = document.getElementById('library-grid');
        if (!container) return;

        container.innerHTML = items.map(item => `
            <div class="card" data-id="${item.id}">
                <div class="status-badge status-${item.status}">${item.status}</div>
                <img src="${item.thumbnail || '/static/images/placeholder.jpg'}" 
                     alt="${item.title}" loading="lazy">
                <h3>${item.title}</h3>
                <p>${item.year || ''} ${item.type || ''}</p>
                ${item.status === 'downloading' ? `
                    <div class="progress-bar">
                        <div class="progress-fill" style="width: ${item.progress || 0}%"></div>
                    </div>
                    <p>Downloading: ${item.progress || 0}%</p>
                ` : ''}
                <div class="card-actions">
                    ${item.status === 'cached' ? `
                        <button class="btn-play-local" data-id="${item.id}">Play Local</button>
                    ` : `
                        <button class="btn-play-remote" data-id="${item.id}">Play Remote</button>
                    `}
                    ${item.status === 'remote' ? `
                        <button class="btn-download" data-id="${item.id}">Download</button>
                    ` : ''}
                </div>
            </div>
        `).join('');
    }

    renderQueue(items) {
        const container = document.getElementById('queue-list');
        if (!container) return;

        if (items.length === 0) {
            container.innerHTML = '<p>No items in download queue.</p>';
            return;
        }

        container.innerHTML = items.map(item => `
            <div class="queue-item" data-id="${item.id}">
                <img src="${item.thumbnail || '/static/images/placeholder.jpg'}" 
                     alt="${item.title}">
                <div class="queue-info">
                    <h4>${item.title}</h4>
                    <p>Priority: ${item.priority} | Status: ${item.status}</p>
                    ${item.status === 'downloading' ? `
                        <div class="progress-bar">
                            <div class="progress-fill" style="width: ${item.progress || 0}%"></div>
                        </div>
                        <p>${item.progress || 0}% complete</p>
                    ` : ''}
                </div>
                <div class="queue-actions">
                    ${item.status === 'downloading' ? `
                        <button class="btn-pause" data-id="${item.id}">Pause</button>
                    ` : item.status === 'paused' ? `
                        <button class="btn-resume" data-id="${item.id}">Resume</button>
                    ` : ''}
                    <button class="btn-remove" data-id="${item.id}">Remove</button>
                </div>
            </div>
        `).join('');
    }

    populateSettings(settings) {
        Object.keys(settings).forEach(key => {
            const input = document.querySelector(`[name="${key}"]`);
            if (input) {
                if (input.type === 'checkbox') {
                    input.checked = settings[key];
                } else {
                    input.value = settings[key];
                }
            }
        });
    }

    // Queue management
    async addToQueue(id) {
        try {
            await this.apiCall('/queue/add', {
                method: 'POST',
                body: JSON.stringify({ id: id, priority: 5 })
            });
            this.showSuccess('Added to download queue');
            this.refreshCurrentView();
        } catch (error) {
            this.showError('Failed to add to queue');
        }
    }

    async removeFromQueue(id) {
        try {
            await this.apiCall(`/queue/${id}`, { method: 'DELETE' });
            this.showSuccess('Removed from queue');
            this.refreshCurrentView();
        } catch (error) {
            this.showError('Failed to remove from queue');
        }
    }

    async pauseDownload(id) {
        try {
            await this.apiCall(`/queue/${id}/pause`, { method: 'POST' });
            this.showSuccess('Download paused');
        } catch (error) {
            this.showError('Failed to pause download');
        }
    }

    async resumeDownload(id) {
        try {
            await this.apiCall(`/queue/${id}/resume`, { method: 'POST' });
            this.showSuccess('Download resumed');
        } catch (error) {
            this.showError('Failed to resume download');
        }
    }

    // Settings management
    async saveSettings(formData) {
        try {
            const settings = Object.fromEntries(formData);
            await this.apiCall('/settings', {
                method: 'POST',
                body: JSON.stringify(settings)
            });
            this.showSuccess('Settings saved successfully');
        } catch (error) {
            this.showError('Failed to save settings');
        }
    }

    // Video player
    playVideo(id, source) {
        const url = source === 'local' ? `/stream/${id}` : `/jellyfin/stream/${id}`;
        
        // Initialize or update Video.js player
        if (window.videojs && document.getElementById('video-player')) {
            const player = videojs('video-player');
            player.src({ type: 'video/mp4', src: url });
            player.ready(() => {
                player.play();
            });
            
            // Show video container
            document.getElementById('video-container').style.display = 'block';
        } else {
            // Fallback to direct video element
            window.open(url, '_blank');
        }
    }

    // Progress updates
    updateDownloadProgress(id, progress) {
        const progressBars = document.querySelectorAll(`[data-id="${id}"] .progress-fill`);
        progressBars.forEach(bar => {
            bar.style.width = `${progress}%`;
        });
        
        const progressTexts = document.querySelectorAll(`[data-id="${id}"] .progress-text`);
        progressTexts.forEach(text => {
            text.textContent = `${progress}%`;
        });
    }

    markDownloadComplete(id) {
        // Update UI to reflect completed download
        const cards = document.querySelectorAll(`[data-id="${id}"]`);
        cards.forEach(card => {
            const badge = card.querySelector('.status-badge');
            if (badge) {
                badge.textContent = 'cached';
                badge.className = 'status-badge status-cached';
            }
        });
        
        this.refreshCurrentView();
    }

    markDownloadError(id, error) {
        this.showError(`Download failed for item ${id}: ${error}`);
        this.refreshCurrentView();
    }

    // Search functionality
    async searchLibrary(query) {
        if (!query.trim()) {
            this.loadLibrary();
            return;
        }
        
        try {
            const results = await this.apiCall(`/library?search=${encodeURIComponent(query)}`);
            this.renderLibrary(results.items || []);
        } catch (error) {
            this.showError('Search failed');
        }
    }

    // Utility methods
    refreshCurrentView() {
        switch (this.currentView) {
            case 'library':
                this.loadLibrary();
                break;
            case 'queue':
                this.loadQueue();
                break;
        }
    }

    async refreshQueue() {
        if (this.currentView === 'queue') {
            this.loadQueue();
        }
    }

    updateSystemStatus(status) {
        const elements = {
            'cache-size': status.cache_size_gb || '0',
            'cache-items': status.cache_items || '0',
            'active-downloads': status.active_downloads || '0',
            'queue-size': status.queue_size || '0'
        };
        
        Object.entries(elements).forEach(([id, value]) => {
            const el = document.getElementById(id);
            if (el) el.textContent = value;
        });
    }

    showLoading(containerId) {
        const container = document.getElementById(containerId);
        if (container) {
            container.innerHTML = '<div class="loading">⟳</div> <span>Loading...</span>';
        }
    }

    hideLoading(containerId) {
        // Loading will be replaced by actual content
    }

    showError(message) {
        this.showNotification(message, 'error');
    }

    showSuccess(message) {
        this.showNotification(message, 'success');
    }

    showNotification(message, type) {
        // Simple notification system
        const notification = document.createElement('div');
        notification.className = `notification notification-${type}`;
        notification.textContent = message;
        notification.style.cssText = `
            position: fixed;
            top: 20px;
            right: 20px;
            padding: 1rem;
            border-radius: 4px;
            color: white;
            background: ${type === 'error' ? '#dc3545' : '#28a745'};
            z-index: 1000;
            animation: slideIn 0.3s ease-out;
        `;
        
        document.body.appendChild(notification);
        
        setTimeout(() => {
            notification.remove();
        }, 5000);
    }
}

// Initialize the application when DOM is loaded
document.addEventListener('DOMContentLoaded', () => {
    window.jfWatch = new JFWatch();
});

// Add CSS animation for notifications
if (!document.getElementById('notification-styles')) {
    const style = document.createElement('style');
    style.id = 'notification-styles';
    style.textContent = `
        @keyframes slideIn {
            from {
                transform: translateX(100%);
                opacity: 0;
            }
            to {
                transform: translateX(0);
                opacity: 1;
            }
        }
    `;
    document.head.appendChild(style);
}