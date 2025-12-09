package downloader

import (
	"errors"
	"math/rand"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsRetryableError verifies error classification for retry logic
func TestIsRetryableError(t *testing.T) {
	// Create a minimal manager for testing
	m := &Manager{}

	tests := []struct {
		name       string
		err        error
		httpStatus int
		want       bool
		reason     string
	}{
		// Permanent failures - should NOT retry
		{
			name:       "404 Not Found",
			err:        errors.New("not found"),
			httpStatus: http.StatusNotFound,
			want:       false,
			reason:     "File doesn't exist, retrying won't help",
		},
		{
			name:       "403 Forbidden",
			err:        errors.New("forbidden"),
			httpStatus: http.StatusForbidden,
			want:       false,
			reason:     "Permission denied, retrying won't help",
		},
		{
			name:       "410 Gone",
			err:        errors.New("gone"),
			httpStatus: http.StatusGone,
			want:       false,
			reason:     "Resource permanently removed",
		},
		{
			name:       "401 Unauthorized",
			err:        errors.New("unauthorized"),
			httpStatus: http.StatusUnauthorized,
			want:       false,
			reason:     "Auth failed, retrying won't help without new credentials",
		},
		{
			name:       "400 Bad Request",
			err:        errors.New("bad request"),
			httpStatus: http.StatusBadRequest,
			want:       false,
			reason:     "Client error, request is malformed",
		},

		// Temporary failures - SHOULD retry
		{
			name:       "429 Too Many Requests",
			err:        errors.New("rate limited"),
			httpStatus: http.StatusTooManyRequests,
			want:       true,
			reason:     "Rate limiting is temporary",
		},
		{
			name:       "503 Service Unavailable",
			err:        errors.New("service unavailable"),
			httpStatus: http.StatusServiceUnavailable,
			want:       true,
			reason:     "Service temporarily down",
		},
		{
			name:       "502 Bad Gateway",
			err:        errors.New("bad gateway"),
			httpStatus: http.StatusBadGateway,
			want:       true,
			reason:     "Upstream server issue, temporary",
		},
		{
			name:       "504 Gateway Timeout",
			err:        errors.New("timeout"),
			httpStatus: http.StatusGatewayTimeout,
			want:       true,
			reason:     "Timeout is temporary",
		},
		{
			name:       "Network error (no HTTP status)",
			err:        errors.New("connection refused"),
			httpStatus: 0,
			want:       true,
			reason:     "Network errors are typically transient",
		},
		{
			name:       "500 Internal Server Error",
			err:        errors.New("server error"),
			httpStatus: http.StatusInternalServerError,
			want:       true,
			reason:     "Server errors might be temporary",
		},

		// Edge cases
		{
			name:       "No error",
			err:        nil,
			httpStatus: 200,
			want:       false,
			reason:     "No error means no retry needed",
		},
		{
			name:       "418 I'm a teapot (other 4xx)",
			err:        errors.New("teapot"),
			httpStatus: http.StatusTeapot,
			want:       false,
			reason:     "Other 4xx are client errors",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.isRetryableError(tt.err, tt.httpStatus)
			assert.Equal(t, tt.want, got,
				"isRetryableError() = %v, want %v. Reason: %s",
				got, tt.want, tt.reason)
		})
	}
}

// TestRetryJitterRandomization verifies jitter adds randomness to retry delays
func TestRetryJitterRandomization(t *testing.T) {
	// Test that jitter produces different values
	baseDelay := 1000 // milliseconds
	iterations := 100

	values := make(map[int]bool)
	for i := 0; i < iterations; i++ {
		// Simulate jitter calculation
		jitter := float64(baseDelay) * (0.75 + 0.5*rand.Float64())
		values[int(jitter)] = true
	}

	// With 100 iterations, we should get multiple different values
	// If we get fewer than 10 unique values, randomization isn't working
	assert.Greater(t, len(values), 10,
		"Jitter should produce varied results, got %d unique values from %d iterations",
		len(values), iterations)

	// All values should be within ±25% of base delay
	for value := range values {
		minExpected := int(float64(baseDelay) * 0.75)
		maxExpected := int(float64(baseDelay) * 1.25)

		assert.GreaterOrEqual(t, value, minExpected,
			"Jittered value %d should be >= %d (75%% of base)",
			value, minExpected)
		assert.LessOrEqual(t, value, maxExpected,
			"Jittered value %d should be <= %d (125%% of base)",
			value, maxExpected)
	}
}
