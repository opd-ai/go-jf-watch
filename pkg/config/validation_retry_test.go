package config

import (
	"strings"
	"testing"
	"time"
)

// TestRetryDelayValidation tests the new retry_delay validation
func TestRetryDelayValidation(t *testing.T) {
	tests := []struct {
		name      string
		delay     time.Duration
		wantError bool
	}{
		{
			name:      "Valid: 1 second (default)",
			delay:     1 * time.Second,
			wantError: false,
		},
		{
			name:      "Valid: 100ms (minimum)",
			delay:     100 * time.Millisecond,
			wantError: false,
		},
		{
			name:      "Valid: 60s (maximum)",
			delay:     60 * time.Second,
			wantError: false,
		},
		{
			name:      "Valid: 5 seconds",
			delay:     5 * time.Second,
			wantError: false,
		},
		{
			name:      "Invalid: 0s (too small)",
			delay:     0 * time.Second,
			wantError: true,
		},
		{
			name:      "Invalid: 50ms (below minimum)",
			delay:     50 * time.Millisecond,
			wantError: true,
		},
		{
			name:      "Invalid: 1ms (way too small)",
			delay:     1 * time.Millisecond,
			wantError: true,
		},
		{
			name:      "Invalid: 2 minutes (too large)",
			delay:     2 * time.Minute,
			wantError: true,
		},
		{
			name:      "Invalid: 1 hour (way too large)",
			delay:     1 * time.Hour,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create minimal valid config
			config := &DownloadConfig{
				Workers:           3,
				RateLimitMbps:     10,
				AutoDownloadCount: 2,
				RetryAttempts:     6,
				RetryDelay:        tt.delay,
				RateLimitSchedule: RateLimitScheduleConfig{
					PeakHours:        "06:00-23:00",
					PeakLimitPercent: 25,
				},
			}

			err := validateDownload(config)

			if tt.wantError {
				if err == nil {
					t.Errorf("validateDownload() expected error for delay=%v, got nil", tt.delay)
				} else {
					// Verify error message mentions retry_delay
					if !strings.Contains(err.Error(), "retry_delay") {
						t.Errorf("Error should mention 'retry_delay', got: %v", err)
					}
				}
			} else {
				if err != nil {
					t.Errorf("validateDownload() unexpected error for delay=%v: %v", tt.delay, err)
				}
			}
		})
	}
}
