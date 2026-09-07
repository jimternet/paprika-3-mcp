package paprika

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type cachedRecipe struct {
	Hash   string  `json:"hash"`
	Recipe *Recipe `json:"recipe"`
}

// cacheSnapshot is the on-disk format. The Recipes field replaced the old flat
// map format (pre-v0.2 snapshots); Load migrates old snapshots transparently.
type cacheSnapshot struct {
	Recipes     map[string]cachedRecipe `json:"recipes"`
	Aisles      []GroceryAisle          `json:"aisles,omitempty"`
	Ingredients []GroceryIngredient     `json:"ingredients,omitempty"`
}

// Cache is a hash-keyed, disk-persisted recipe cache. It is safe for concurrent use.
type Cache struct {
	mu          sync.RWMutex
	refreshMu   sync.Mutex // serialises concurrent Refresh calls
	recipes     map[string]cachedRecipe // keyed by UID, uppercase
	aisles      []GroceryAisle          // ordered by OrderFlag
	ingredients []GroceryIngredient     // user's learned ingredient→aisle table
	client      *Client
	path        string // path to persisted snapshot
	logger      *slog.Logger
	lastSync    time.Time
	firstFill   atomic.Bool // true after an empty Load; cleared after first Refresh
	filling     atomic.Bool // true while the first Refresh is running
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
// A missing file is not an error. If the cache is empty after loading,
// the first call to Refresh will use concurrent fetching.
// Old flat-map snapshots (pre-v0.2) are migrated transparently.
func (c *Cache) Load() error {
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			c.firstFill.Store(true)
			return nil
		}
		return err
	}

	var snap cacheSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}

	// Migrate old flat-map format: {"UID": {hash, recipe}, ...}
	if snap.Recipes == nil {
		var old map[string]cachedRecipe
		if err := json.Unmarshal(data, &old); err == nil && len(old) > 0 {
			snap.Recipes = old
		}
	}

	c.mu.Lock()
	c.recipes = snap.Recipes
	if c.recipes == nil {
		c.recipes = make(map[string]cachedRecipe)
	}
	c.aisles = snap.Aisles
	c.ingredients = snap.Ingredients
	empty := len(c.recipes) == 0
	c.mu.Unlock()

	if empty {
		c.firstFill.Store(true)
	}
	return nil
}

// IsFilling reports whether the initial (first-fill) Refresh is still running.
// Tools should return whatever is cached and append a note when this is true.
func (c *Cache) IsFilling() bool {
	return c.filling.Load()
}

// save atomically writes the cache contents to disk. Must be called with mu held (write lock).
func (c *Cache) save() error {
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.Marshal(cacheSnapshot{
		Recipes:     c.recipes,
		Aisles:      c.aisles,
		Ingredients: c.ingredients,
	})
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
// On the first call after an empty Load it uses 3 concurrent GetRecipe workers
// so a large collection fills in under a minute. Steady-state refreshes are
// sequential. Progress is persisted every 25 fetches so a mid-fill kill loses
// at most 25 recipes' worth of work on the next restart.
func (c *Cache) Refresh(ctx context.Context) error {
	if c.client == nil {
		return errors.New("cache has no client configured")
	}

	// Serialise concurrent callers; background ticker + manual refresh_recipes
	// must not overlap.
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	isFirst := c.firstFill.Load()
	if isFirst {
		c.filling.Store(true)
		defer func() {
			c.filling.Store(false)
			c.firstFill.Store(false)
		}()
	}

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

	c.logger.Info("cache refresh started", "stale", len(stale), "first_fill", isFirst)

	var fetched, removed, failed int32

	// processResult writes one fetched recipe into the cache and saves
	// incrementally every 25 successful fetches.
	processResult := func(uid string, recipe *Recipe, fetchErr error) {
		if fetchErr != nil {
			c.logger.Warn("failed to fetch recipe", "uid", uid, "error", fetchErr)
			atomic.AddInt32(&failed, 1)
			return
		}
		c.mu.Lock()
		if recipe.InTrash {
			delete(c.recipes, uid)
			atomic.AddInt32(&removed, 1)
		} else {
			c.recipes[uid] = cachedRecipe{Hash: recipe.Hash, Recipe: recipe}
			n := atomic.AddInt32(&fetched, 1)
			if n%25 == 0 {
				if err := c.save(); err != nil {
					c.logger.Error("incremental cache save failed", "error", err)
				}
			}
		}
		c.mu.Unlock()
	}

	if isFirst && len(stale) > 0 {
		// First fill: 3 concurrent workers share the rate limiter so network
		// latency is pipelined while requests are still paced at 4 req/s.
		const workers = 3
		uidCh := make(chan string, len(stale))
		for _, uid := range stale {
			uidCh <- uid
		}
		close(uidCh)

		var wg sync.WaitGroup
		for range workers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for uid := range uidCh {
					r, err := c.client.GetRecipe(ctx, uid)
					processResult(uid, r, err)
				}
			}()
		}
		wg.Wait()
	} else {
		// Steady-state: sequential; the rate limiter already paces requests.
		for _, uid := range stale {
			r, err := c.client.GetRecipe(ctx, uid)
			processResult(uid, r, err)
		}
	}

	// Fetch aisles and ingredients; non-fatal so a missing endpoint (e.g. in
	// tests or older API versions) does not abort the recipe sync.
	if aisleResp, err := c.client.GetGroceryAisles(ctx); err != nil {
		c.logger.Warn("failed to fetch grocery aisles; keeping cached values", "error", err)
	} else {
		c.mu.Lock()
		c.aisles = aisleResp.Result
		c.mu.Unlock()
		names := make([]string, len(aisleResp.Result))
		for i, a := range aisleResp.Result {
			names[i] = a.Name
		}
		c.logger.Info("grocery aisles synced", "count", len(aisleResp.Result), "names", strings.Join(names, ", "))
	}
	if ingResp, err := c.client.GetGroceryIngredients(ctx); err != nil {
		c.logger.Warn("failed to fetch grocery ingredients; keeping cached values", "error", err)
	} else {
		c.mu.Lock()
		c.ingredients = ingResp.Result
		c.mu.Unlock()
	}

	// Prune UIDs that no longer appear in the server list.
	c.mu.Lock()
	for uid := range c.recipes {
		if !seen[uid] {
			delete(c.recipes, uid)
			atomic.AddInt32(&removed, 1)
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

// Aisles returns the user's grocery aisles sorted by OrderFlag.
func (c *Cache) Aisles() []GroceryAisle {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]GroceryAisle, len(c.aisles))
	copy(out, c.aisles)
	sort.Slice(out, func(i, j int) bool { return out[i].OrderFlag < out[j].OrderFlag })
	return out
}

// AisleByUID returns the aisle with the given UID, or false if not found.
func (c *Cache) AisleByUID(uid string) (GroceryAisle, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, a := range c.aisles {
		if a.UID == uid {
			return a, true
		}
	}
	return GroceryAisle{}, false
}

// AisleByName returns the aisle whose name matches (case-insensitive), or false.
func (c *Cache) AisleByName(name string) (GroceryAisle, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, a := range c.aisles {
		if strings.ToLower(a.Name) == lower {
			return a, true
		}
	}
	return GroceryAisle{}, false
}
