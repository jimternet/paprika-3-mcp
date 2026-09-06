package paprika

import (
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/time/rate"
)

// NewClientFromHTTPClient creates a Client from a pre-configured *http.Client,
// bypassing the login flow. Intended only for unit tests.
func NewClientFromHTTPClient(httpClient *http.Client, logger *slog.Logger, rateLimitInterval time.Duration) *Client {
	if rateLimitInterval <= 0 {
		rateLimitInterval = defaultRateLimitInterval
	}
	l := logger
	if l == nil {
		l = slog.Default()
	}
	return &Client{
		client:            httpClient,
		logger:            l,
		rateLimitInterval: rateLimitInterval,
		limiter:           rate.NewLimiter(rate.Every(rateLimitInterval), 1),
	}
}
