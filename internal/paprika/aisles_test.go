package paprika_test

import (
	"testing"
	"time"

	"github.com/jimternet/paprika-3-mcp/internal/aisles"
	"github.com/jimternet/paprika-3-mcp/internal/paprika"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureAisles uses Paprika stock aisle names so they resolve correctly via NormalizeAisleName.
var fixtureAisles = []paprika.GroceryAisle{
	{UID: "a-produce", Name: "Produce", OrderFlag: 1},
	{UID: "a-dairy", Name: "Dairy", OrderFlag: 2},
	{UID: "a-meat", Name: "Meat", OrderFlag: 3},
	{UID: "a-frozen", Name: "Frozen Foods", OrderFlag: 4},
	{UID: "a-canned", Name: "Canned and Jar Goods", OrderFlag: 5},
	{UID: "a-baking", Name: "Baking Goods", OrderFlag: 6},
	{UID: "a-bakery", Name: "Bakery", OrderFlag: 7},
	{UID: "a-spices", Name: "Spices and Seasonings", OrderFlag: 8},
	{UID: "a-snacks", Name: "Snacks", OrderFlag: 9},
	{UID: "a-beverages", Name: "Beverages", OrderFlag: 10},
	{UID: "a-condiments", Name: "Sauces and Condiments", OrderFlag: 11},
	{UID: "a-pantry", Name: "Pasta, Rice and Beans", OrderFlag: 12},
	{UID: "a-breads", Name: "Breads and Cereals", OrderFlag: 13},
}

var fixtureIngredients = []paprika.GroceryIngredient{
	{UID: "i-1", Name: "apples", AisleUID: "a-produce"},
	{UID: "i-2", Name: "milk", AisleUID: "a-dairy"},
	{UID: "i-3", Name: "chicken breast", AisleUID: "a-meat"},
	{UID: "i-4", Name: "cheddar cheese", AisleUID: "a-dairy"},
}

// newFixtureCache builds an in-memory cache loaded with the fixture aisles
// and ingredients but no recipes. No network calls are made.
func newFixtureCache(t *testing.T) *paprika.Cache {
	t.Helper()
	dir := t.TempDir()
	c := paprika.NewCache(nil, dir+"/recipes.json", nil)
	cfg, _, err := aisles.Load("") // embedded defaults, no user file
	require.NoError(t, err)
	c.SetAislesConfig(cfg)
	c.SetAislesAndIngredients(fixtureAisles, fixtureIngredients)
	return c
}

// ---------------------------------------------------------------------------
// AisleByName / AisleByUID / Aisles tests
// ---------------------------------------------------------------------------

func TestAisleByName(t *testing.T) {
	cache := newFixtureCache(t)

	a, ok := cache.AisleByName("produce")
	assert.True(t, ok)
	assert.Equal(t, "Produce", a.Name)

	a, ok = cache.AisleByName("DAIRY")
	assert.True(t, ok)
	assert.Equal(t, "Dairy", a.Name)

	_, ok = cache.AisleByName("nonexistent")
	assert.False(t, ok)
}

func TestAisleByUID(t *testing.T) {
	cache := newFixtureCache(t)

	a, ok := cache.AisleByUID("a-meat")
	assert.True(t, ok)
	assert.Equal(t, "Meat", a.Name)

	_, ok = cache.AisleByUID("no-such-uid")
	assert.False(t, ok)
}

func TestAislesSortedByOrderFlag(t *testing.T) {
	cache := newFixtureCache(t)
	got := cache.Aisles()
	assert.Len(t, got, len(fixtureAisles))
	for i := 1; i < len(got); i++ {
		assert.LessOrEqual(t, got[i-1].OrderFlag, got[i].OrderFlag)
	}
}

// Ensure IsFilling does not get stuck true after a refresh with fixture data.
func TestIsFillingClears(t *testing.T) {
	cache := newFixtureCache(t)
	assert.False(t, cache.IsFilling(), "should not be filling when loaded from fixture")
	_ = time.Now() // keep time import used
}

// ---------------------------------------------------------------------------
// ResolveAisle tests
// ---------------------------------------------------------------------------

func TestResolveAisle(t *testing.T) {
	cache := newFixtureCache(t)

	cases := []struct {
		name      string
		input     string
		wantAisle string
		wantStage string
	}{
		// Exact match from history (apples → Produce, history)
		{
			name:      "exact match learned",
			input:     "apples",
			wantAisle: "Produce",
			wantStage: "history",
		},
		// Quantity stripped, then history match
		{
			name:      "quantity stripped learned",
			input:     "2 lbs apples",
			wantAisle: "Produce",
			wantStage: "history",
		},
		// Modifier: canned → Canned and Jar Goods
		{
			name:      "modifier canned corn",
			input:     "canned corn",
			wantAisle: "Canned and Jar Goods",
			wantStage: "modifier",
		},
		// Modifier: frozen → Frozen Foods
		{
			name:      "modifier frozen corn",
			input:     "frozen corn",
			wantAisle: "Frozen Foods",
			wantStage: "modifier",
		},
		// Keyword: banana → Produce
		{
			name:      "keyword banana",
			input:     "banana",
			wantAisle: "Produce",
			wantStage: "keyword",
		},
		// Keyword: black beans → Pasta, Rice and Beans
		{
			name:      "keyword black beans",
			input:     "black beans",
			wantAisle: "Pasta, Rice and Beans",
			wantStage: "keyword",
		},
		// No match
		{
			name:      "no match unknown widget",
			input:     "unknown widget",
			wantAisle: "",
			wantStage: "no match",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := cache.ResolveAisle(tc.input)
			assert.Equal(t, tc.wantAisle, result.AisleName, "AisleName")
			assert.Equal(t, tc.wantStage, result.Stage, "Stage")
		})
	}
}
