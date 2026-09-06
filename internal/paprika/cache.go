package paprika

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type cachedRecipe struct {
	Hash   string  `json:"hash"`
	Recipe *Recipe `json:"recipe"`
}

// Cache is a hash-keyed, disk-persisted recipe cache. It is safe for concurrent use.
type Cache struct {
	mu       sync.RWMutex
	recipes  map[string]cachedRecipe // keyed by UID, uppercase
	client   *Client
	path     string // path to persisted snapshot
	logger   *slog.Logger
	lastSync time.Time
}

// NewCache creates a new Cache backed by client, persisted to path.
func NewCache(client *Client, path string, logger *slog.Logger) *Cache {
	l := logger
	if l == nil {
		l = slog.Default()
	}
	return &Cache{
		recipes: make(map[string]cachedRecipe),
		client:  client,
		path:    path,
		logger:  l,
	}
}

// Load reads the persisted snapshot from disk into the cache.
// A missing file is not an error.
func (c *Cache) Load() error {
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var recipes map[string]cachedRecipe
	if err := json.Unmarshal(data, &recipes); err != nil {
		return err
	}

	c.mu.Lock()
	c.recipes = recipes
	c.mu.Unlock()
	return nil
}

// save atomically writes the cache contents to disk. Must be called with mu held (write lock).
func (c *Cache) save() error {
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.Marshal(c.recipes)
	if err != nil {
		return err
	}

	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}

	return os.Rename(tmp, c.path)
}

// Refresh synchronises the cache with the Paprika server.
// It lists all recipes, fetches any that are new or have changed hashes,
// prunes deleted entries, then persists the updated cache.
func (c *Cache) Refresh(ctx context.Context) error {
	start := time.Now()

	list, err := c.client.ListRecipes(ctx)
	if err != nil {
		return err
	}

	// Build a seen set and determine which UIDs need fetching.
	seen := make(map[string]bool, len(list.Result))
	var stale []string

	c.mu.RLock()
	for _, item := range list.Result {
		uid := strings.ToUpper(item.UID)
		seen[uid] = true
		cached, ok := c.recipes[uid]
		if !ok || cached.Hash != item.Hash {
			stale = append(stale, uid)
		}
	}
	c.mu.RUnlock()

	var fetched, removed, failed int

	// Fetch stale recipes sequentially; the client rate limiter paces us.
	for _, uid := range stale {
		recipe, err := c.client.GetRecipe(ctx, uid)
		if err != nil {
			c.logger.Error("failed to fetch recipe", "uid", uid, "error", err)
			failed++
			continue
		}

		c.mu.Lock()
		if recipe.InTrash {
			delete(c.recipes, uid)
			removed++
		} else {
			c.recipes[uid] = cachedRecipe{Hash: recipe.Hash, Recipe: recipe}
			fetched++
		}
		c.mu.Unlock()
	}

	// Prune UIDs that no longer appear in the server list.
	c.mu.Lock()
	for uid := range c.recipes {
		if !seen[uid] {
			delete(c.recipes, uid)
			removed++
		}
	}
	c.lastSync = time.Now()
	if err := c.save(); err != nil {
		c.logger.Error("failed to persist cache", "error", err)
	}
	totalCount := len(c.recipes)
	c.mu.Unlock()

	c.logger.Info("cache refresh complete",
		"total", totalCount,
		"fetched", fetched,
		"removed", removed,
		"failed", failed,
		"duration", time.Since(start),
	)

	return nil
}

// Get returns the recipe with the given UID (case-insensitive) and true,
// or nil and false if not present.
func (c *Cache) Get(uid string) (*Recipe, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.recipes[strings.ToUpper(uid)]
	if !ok {
		return nil, false
	}
	return entry.Recipe, true
}

// List returns all non-trashed recipes sorted by name.
func (c *Cache) List() []*Recipe {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]*Recipe, 0, len(c.recipes))
	for _, entry := range c.recipes {
		if entry.Recipe != nil && !entry.Recipe.InTrash {
			out = append(out, entry.Recipe)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})

	return out
}

// Search returns recipes whose Name+Ingredients+Description contain every
// whitespace-separated word in query (case-insensitive), sorted by name.
// limit caps the result count; 0 means unlimited.
func (c *Cache) Search(query string, limit int) []*Recipe {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return nil
	}

	all := c.List() // already sorted, already excludes InTrash

	var out []*Recipe
	for _, r := range all {
		text := strings.ToLower(r.Name + " " + r.Ingredients + " " + r.Description)
		match := true
		for _, w := range words {
			if !strings.Contains(text, w) {
				match = false
				break
			}
		}
		if match {
			out = append(out, r)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}

	return out
}

// Put stores or replaces the recipe in the cache (write-through after SaveRecipe).
func (c *Cache) Put(r *Recipe) {
	if r == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	uid := strings.ToUpper(r.UID)
	c.recipes[uid] = cachedRecipe{Hash: r.Hash, Recipe: r}
}

// Remove deletes the entry for uid from the cache (after DeleteRecipe).
func (c *Cache) Remove(uid string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.recipes, strings.ToUpper(uid))
}

// Stats returns the current count and last sync time.
func (c *Cache) Stats() (count int, lastSync time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.recipes), c.lastSync
}
