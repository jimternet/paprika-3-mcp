package paprika

import (
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"github.com/jimternet/paprika-3-mcp/internal/aisles"
)

// NormalizeIngredient exposes the ingredient normalizer for unit tests.
// It now delegates to aisles.NormalizeForLookup on the quantity-stripped name.
func NormalizeIngredient(name string) string {
	_, cleanName, _ := ExtractQuantity(name)
	return aisles.NormalizeForLookup(cleanName)
}

// SetAislesAndIngredients injects aisle/ingredient fixtures into the cache
// without going through a network Refresh. For tests only.
func (c *Cache) SetAislesAndIngredients(aisles []GroceryAisle, ingredients []GroceryIngredient) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.aisles = aisles
	c.ingredients = ingredients
}

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
