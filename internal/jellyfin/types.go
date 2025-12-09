package jellyfin

import "time"

// MediaItem represents a media item from Jellyfin (movie, episode, etc.).
// This struct contains the essential information needed for caching decisions.
type MediaItem struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Type              string    `json:"type"` // Movie, Episode, Series, etc.
	Path              string    `json:"path"`
	Container         string    `json:"container"`
	Size              int64     `json:"size"`
	Bitrate           int       `json:"bitrate"`
	
	// Series information (for episodes)
	SeriesID          string    `json:"series_id,omitempty"`
	SeriesName        string    `json:"series_name,omitempty"`
	SeasonNumber      int       `json:"season_number,omitempty"`
	EpisodeNumber     int       `json:"episode_number,omitempty"`
	
	// Metadata
	Overview          string    `json:"overview,omitempty"`
	Genres            []string  `json:"genres,omitempty"`
	Studios           []string  `json:"studios,omitempty"`
	DateCreated       time.Time `json:"date_created"`
	DateAdded         time.Time `json:"date_added"`
	
	// Playback information
	PlaybackInfo      *PlaybackInfo `json:"playback_info,omitempty"`
	UserData          *UserData     `json:"user_data,omitempty"`
}

// PlaybackInfo contains information needed to stream or download the media.
type PlaybackInfo struct {
	MediaSources      []MediaSource `json:"media_sources"`
	DirectStreamURL   string        `json:"direct_stream_url"`
	RequiresTranscode bool          `json:"requires_transcode"`
	Protocol          string        `json:"protocol"`
}

// MediaSource represents a source file for media content.
type MediaSource struct {
	ID           string         `json:"id"`
	Path         string         `json:"path"`
	Protocol     string         `json:"protocol"`
	Container    string         `json:"container"`
	Size         int64          `json:"size"`
	Bitrate      int            `json:"bitrate"`
	MediaStreams []MediaStream  `json:"media_streams"`
}

// MediaStream represents a video, audio, or subtitle stream.
type MediaStream struct {
	Index           int    `json:"index"`
	Type            string `json:"type"` // Video, Audio, Subtitle
	Codec           string `json:"codec"`
	Language        string `json:"language,omitempty"`
	IsDefault       bool   `json:"is_default"`
	IsForced        bool   `json:"is_forced"`
	Title           string `json:"title,omitempty"`
	
	// Video stream properties
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	AspectRatio     string `json:"aspect_ratio,omitempty"`
	FrameRate       float64 `json:"frame_rate,omitempty"`
	
	// Audio stream properties
	Channels        int    `json:"channels,omitempty"`
	SampleRate      int    `json:"sample_rate,omitempty"`
	
	// Subtitle stream properties
	DeliveryMethod  string `json:"delivery_method,omitempty"`
	External        bool   `json:"external,omitempty"`
}

// UserData contains user-specific information about media items.
type UserData struct {
	PlaybackPositionTicks int64     `json:"playback_position_ticks"`
	PlayCount             int       `json:"play_count"`
	IsFavorite            bool      `json:"is_favorite"`
	Played                bool      `json:"played"`
	LastPlayedDate        time.Time `json:"last_played_date,omitempty"`
}

// LibraryItem represents an item in a Jellyfin library.
type LibraryItem struct {
	MediaItem
	Children []LibraryItem `json:"children,omitempty"` // For series with episodes
}

// ViewingSession represents a user's viewing session for analytics.
type ViewingSession struct {
	ItemID           string    `json:"item_id"`
	UserID           string    `json:"user_id"`
	SessionID        string    `json:"session_id"`
	StartTime        time.Time `json:"start_time"`
	EndTime          time.Time `json:"end_time,omitempty"`
	PlaybackPosition int64     `json:"playback_position"`
	IsPaused         bool      `json:"is_paused"`
	PlayMethod       string    `json:"play_method"` // DirectStream, DirectPlay, Transcode
}

// DownloadRequest represents a request to download media.
type DownloadRequest struct {
	MediaItem   *MediaItem `json:"media_item"`
	Priority    int        `json:"priority"`
	RequestedBy string     `json:"requested_by"` // user, predictor, manual
	CreatedAt   time.Time  `json:"created_at"`
}

// LibraryStats contains statistics about a Jellyfin library.
type LibraryStats struct {
	TotalItems   int           `json:"total_items"`
	TotalSize    int64         `json:"total_size_bytes"`
	ItemsByType  map[string]int `json:"items_by_type"`
	LastSyncTime time.Time     `json:"last_sync_time"`
}