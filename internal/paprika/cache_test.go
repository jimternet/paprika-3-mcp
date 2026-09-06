package paprika_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soggycactus/paprika-3-mcp/internal/paprika"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recipeFixture is a convenience for building test Recipe values.
func recipeFixture(uid, name, hash string) paprika.Recipe {
	return paprika.Recipe{
		UID:         uid,
		Name:        name,
		Hash:        hash,
		Ingredients: "flour, sugar",
		Description: "A tasty treat",
	}
}

// buildListJSON encodes a RecipeList-compatible JSON body from uid/hash pairs.
func buildListJSON(t *testing.T, pairs []struct{ UID, Hash string }) []byte {
	t.Helper()
	type item struct {
		UID  string `json:"uid"`
		Hash string `json:"hash"`
	}
	type envelope struct {
		Result []item `json:"result"`
	}
	items := make([]item, len(pairs))
	for i, p := range pairs {
		items[i] = item{UID: p.UID, Hash: p.Hash}
	}
	data, err := json.Marshal(envelope{Result: items})
	require.NoError(t, err)
	return data
}

// buildGetJSON encodes a GetRecipeResponse-compatible JSON body.
func buildGetJSON(t *testing.T, r paprika.Recipe) []byte {
	t.Helper()
	type envelope struct {
		Result paprika.Recipe `json:"result"`
	}
	data, err := json.Marshal(envelope{Result: r})
	require.NoError(t, err)
	return data
}

// newCacheWithServer constructs a Cache pointing at a test HTTP server.
// The caller must call srv.Close() when done.
func newCacheWithServer(t *testing.T, srv *httptest.Server) *paprika.Cache {
	t.Helper()
	httpClient := &http.Client{Transport: &urlRewriteTransport{base: srv.URL}}
	client := paprika.NewClientFromHTTPClient(httpClient, nil, 1*time.Millisecond)
	dir := t.TempDir()
	return paprika.NewCache(client, dir+"/recipes.json", nil)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestCacheRefreshFetchesAll: empty cache + 3 server recipes → all fetched.
func TestCacheRefreshFetchesAll(t *testing.T) {
	recipes := []paprika.Recipe{
		recipeFixture("UID-1", "Apple Pie", "hash1"),
		recipeFixture("UID-2", "Banana Bread", "hash2"),
		recipeFixture("UID-3", "Carrot Cake", "hash3"),
	}

	listBody := buildListJSON(t, []struct{ UID, Hash string }{
		{"UID-1", "hash1"},
		{"UID-2", "hash2"},
		{"UID-3", "hash3"},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/sync/recipes" {
			w.Write(listBody)
			return
		}
		for _, rec := range recipes {
			if r.URL.Path == "/api/v2/sync/recipe/"+rec.UID+"/" {
				w.Write(buildGetJSON(t, rec))
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cache := newCacheWithServer(t, srv)

	ctx := context.Background()
	require.NoError(t, cache.Refresh(ctx))

	listed := cache.List()
	assert.Len(t, listed, 3)

	count, lastSync := cache.Stats()
	assert.Equal(t, 3, count)
	assert.False(t, lastSync.IsZero())
}

// TestCacheRefreshSkipsUnchanged: second refresh with identical hashes → zero GET /recipe/ calls.
func TestCacheRefreshSkipsUnchanged(t *testing.T) {
	recipe := recipeFixture("UID-1", "Apple Pie", "hash1")
	listBody := buildListJSON(t, []struct{ UID, Hash string }{{"UID-1", "hash1"}})

	var getCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/sync/recipes" {
			w.Write(listBody)
			return
		}
		if r.URL.Path == "/api/v2/sync/recipe/UID-1/" {
			getCalls.Add(1)
			w.Write(buildGetJSON(t, recipe))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cache := newCacheWithServer(t, srv)
	ctx := context.Background()

	// First refresh — should fetch once.
	require.NoError(t, cache.Refresh(ctx))
	assert.Equal(t, int32(1), getCalls.Load())

	// Second refresh — unchanged hash, should skip.
	require.NoError(t, cache.Refresh(ctx))
	assert.Equal(t, int32(1), getCalls.Load(), "no additional GET calls expected on unchanged hash")
}

// TestCacheRefreshFetchesChanged: one hash changes → exactly one GET, body updated.
func TestCacheRefreshFetchesChanged(t *testing.T) {
	original := recipeFixture("UID-1", "Apple Pie", "hash1")
	updated := recipeFixture("UID-1", "Apple Pie Updated", "hash2")

	currentHash := "hash1"
	currentRecipe := original

	var getCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/sync/recipes" {
			w.Write(buildListJSON(t, []struct{ UID, Hash string }{{"UID-1", currentHash}}))
			return
		}
		if r.URL.Path == "/api/v2/sync/recipe/UID-1/" {
			getCalls.Add(1)
			w.Write(buildGetJSON(t, currentRecipe))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cache := newCacheWithServer(t, srv)
	ctx := context.Background()

	require.NoError(t, cache.Refresh(ctx))
	assert.Equal(t, int32(1), getCalls.Load())

	r, ok := cache.Get("UID-1")
	require.True(t, ok)
	assert.Equal(t, "Apple Pie", r.Name)

	// Simulate server-side update.
	currentHash = "hash2"
	currentRecipe = updated

	require.NoError(t, cache.Refresh(ctx))
	assert.Equal(t, int32(2), getCalls.Load(), "should have fetched the changed recipe")

	r, ok = cache.Get("UID-1")
	require.True(t, ok)
	assert.Equal(t, "Apple Pie Updated", r.Name)
}

// TestCacheRefreshPrunesDeleted: UID removed from list response → pruned from cache.
func TestCacheRefreshPrunesDeleted(t *testing.T) {
	r1 := recipeFixture("UID-1", "Apple Pie", "hash1")
	r2 := recipeFixture("UID-2", "Banana Bread", "hash2")

	// Start with both recipes.
	includeSecond := true

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/sync/recipes" {
			pairs := []struct{ UID, Hash string }{{"UID-1", "hash1"}}
			if includeSecond {
				pairs = append(pairs, struct{ UID, Hash string }{"UID-2", "hash2"})
			}
			w.Write(buildListJSON(t, pairs))
			return
		}
		if r.URL.Path == "/api/v2/sync/recipe/UID-1/" {
			w.Write(buildGetJSON(t, r1))
			return
		}
		if r.URL.Path == "/api/v2/sync/recipe/UID-2/" {
			w.Write(buildGetJSON(t, r2))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cache := newCacheWithServer(t, srv)
	ctx := context.Background()

	// Populate both.
	require.NoError(t, cache.Refresh(ctx))
	assert.Len(t, cache.List(), 2)

	// Remove UID-2 from server.
	includeSecond = false
	require.NoError(t, cache.Refresh(ctx))

	listed := cache.List()
	assert.Len(t, listed, 1)
	assert.Equal(t, "Apple Pie", listed[0].Name)

	_, ok := cache.Get("UID-2")
	assert.False(t, ok, "UID-2 should have been pruned")
}

// TestCacheRefreshRemovesTrash: server returns in_trash:true → entry removed from cache.
func TestCacheRefreshRemovesTrash(t *testing.T) {
	live := recipeFixture("UID-1", "Apple Pie", "hash1")
	trashed := recipeFixture("UID-2", "Banana Bread", "hash2-v2")
	trashed.InTrash = true

	currentHash2 := "hash2" // initial hash differs so it will be fetched on second refresh

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/sync/recipes" {
			w.Write(buildListJSON(t, []struct{ UID, Hash string }{
				{"UID-1", "hash1"},
				{"UID-2", currentHash2},
			}))
			return
		}
		if r.URL.Path == "/api/v2/sync/recipe/UID-1/" {
			w.Write(buildGetJSON(t, live))
			return
		}
		if r.URL.Path == "/api/v2/sync/recipe/UID-2/" {
			w.Write(buildGetJSON(t, trashed))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// First refresh: add live recipe to cache only (UID-2 doesn't exist yet → will be fetched).
	cache := newCacheWithServer(t, srv)
	ctx := context.Background()
	require.NoError(t, cache.Refresh(ctx))

	// After initial refresh, UID-2 is in_trash and should already be absent.
	_, ok := cache.Get("UID-2")
	assert.False(t, ok, "trashed recipe should not appear in cache")

	// UID-1 still present.
	_, ok = cache.Get("UID-1")
	assert.True(t, ok)
}

// TestCacheLoadRoundTrip: save via Put, then Load restores the data.
func TestCacheLoadRoundTrip(t *testing.T) {
	// Build a cache with a known recipe, using a no-op server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/sync/recipes" {
			w.Write(buildListJSON(t, nil))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cachePath := dir + "/recipes.json"
	httpClient := &http.Client{Transport: &urlRewriteTransport{base: srv.URL}}
	client := paprika.NewClientFromHTTPClient(httpClient, nil, 1*time.Millisecond)

	// Populate the first cache via Put and trigger a save through Refresh.
	// We use Put directly (it doesn't persist), then use Refresh (empty list) to trigger save
	// of only what we've put. Instead, let's pre-populate by doing a Refresh that returns our recipe.

	recipe := recipeFixture("UID-99", "Zucchini Soup", "hashZ")

	srvWithRecipe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/sync/recipes" {
			w.Write(buildListJSON(t, []struct{ UID, Hash string }{{"UID-99", "hashZ"}}))
			return
		}
		if r.URL.Path == "/api/v2/sync/recipe/UID-99/" {
			w.Write(buildGetJSON(t, recipe))
			return
		}
		http.NotFound(w, r)
	}))
	defer srvWithRecipe.Close()

	httpClient2 := &http.Client{Transport: &urlRewriteTransport{base: srvWithRecipe.URL}}
	client2 := paprika.NewClientFromHTTPClient(httpClient2, nil, 1*time.Millisecond)
	cache1 := paprika.NewCache(client2, cachePath, nil)
	require.NoError(t, cache1.Refresh(context.Background()))

	r, ok := cache1.Get("UID-99")
	require.True(t, ok)
	assert.Equal(t, "Zucchini Soup", r.Name)

	// Now create a second cache pointed at the same path and Load it.
	cache2 := paprika.NewCache(client, cachePath, nil)
	require.NoError(t, cache2.Load())

	r2, ok := cache2.Get("UID-99")
	require.True(t, ok)
	assert.Equal(t, "Zucchini Soup", r2.Name)
	assert.Equal(t, "hashZ", r2.Hash)
}

// TestCacheRefreshContinuesOnError: GetRecipe returns 500 for one UID → refresh
// continues for others, the errored UID is absent, no error returned.
func TestCacheRefreshContinuesOnError(t *testing.T) {
	good := recipeFixture("UID-OK", "Good Recipe", "hashOK")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v2/sync/recipes" {
			w.Write(buildListJSON(t, []struct{ UID, Hash string }{
				{"UID-OK", "hashOK"},
				{"UID-BAD", "hashBAD"},
			}))
			return
		}
		if r.URL.Path == "/api/v2/sync/recipe/UID-OK/" {
			w.Write(buildGetJSON(t, good))
			return
		}
		if r.URL.Path == "/api/v2/sync/recipe/UID-BAD/" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cache := newCacheWithServer(t, srv)
	ctx := context.Background()

	// Refresh should not return an error despite the 500.
	err := cache.Refresh(ctx)
	assert.NoError(t, err, "Refresh should continue on individual fetch error")

	// Good recipe is present.
	r, ok := cache.Get("UID-OK")
	require.True(t, ok)
	assert.Equal(t, "Good Recipe", r.Name)

	// Bad recipe is absent.
	_, ok = cache.Get("UID-BAD")
	assert.False(t, ok, "failed recipe should not appear in cache")
}
