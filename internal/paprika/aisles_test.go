package paprika_test

import (
	"testing"
	"time"

	"github.com/soggycactus/paprika-3-mcp/internal/paprika"
	"github.com/stretchr/testify/assert"
)

// fixtureAisles and fixtureIngredients are shared across lookup tests.
var fixtureAisles = []paprika.GroceryAisle{
	{UID: "a-produce", Name: "Produce", OrderFlag: 1},
	{UID: "a-dairy", Name: "Dairy", OrderFlag: 2},
	{UID: "a-meat", Name: "Meat", OrderFlag: 3},
	{UID: "a-frozen", Name: "Frozen", OrderFlag: 4},
	{UID: "a-canned", Name: "Canned Goods", OrderFlag: 5},
	{UID: "a-baking", Name: "Baking", OrderFlag: 6},
	{UID: "a-bakery", Name: "Bakery", OrderFlag: 7},
	{UID: "a-spices", Name: "Spices", OrderFlag: 8},
	{UID: "a-snacks", Name: "Snacks", OrderFlag: 9},
	{UID: "a-beverages", Name: "Beverages", OrderFlag: 10},
}

var fixtureIngredients = []paprika.GroceryIngredient{
	{UID: "i-1", Name: "apples", AisleUID: "a-produce"},
	{UID: "i-2", Name: "milk", AisleUID: "a-dairy"},
	{UID: "i-3", Name: "chicken breast", AisleUID: "a-meat"},
	{UID: "i-4", Name: "cheddar cheese", AisleUID: "a-dairy"},
	{UID: "i-5", Name: "pasta", AisleUID: "a-canned"},
}

// newFixtureCache builds an in-memory cache loaded with the fixture aisles
// and ingredients but no recipes. No network calls are made.
func newFixtureCache(t *testing.T) *paprika.Cache {
	t.Helper()
	dir := t.TempDir()
	c := paprika.NewCache(nil, dir+"/recipes.json", nil)
	c.SetAislesAndIngredients(fixtureAisles, fixtureIngredients)
	return c
}

// ---------------------------------------------------------------------------
// Normalizer tests
// ---------------------------------------------------------------------------

func TestNormalizeIngredient(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"apples", "apples"},
		{"Apples", "apples"},
		{"  Apples  ", "apples"},
		{"2 lbs apples", "apples"},
		{"1/2 cup flour", "flour"},
		{"3 cans diced tomatoes", "diced tomatoes"},
		{"1 bunch cilantro", "cilantro"},
		{"milk (whole)", "milk"},
		{"chicken breast (boneless)", "chicken breast"},
		{"12 oz pasta", "pasta"},
		{"4 tbsp butter", "butter"},
		{"1 tsp salt", "salt"},
		{"2 dozen eggs", "eggs"},
		{"large onion", "large onion"}, // "large" is not a unit
		{"fresh garlic", "fresh garlic"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, paprika.NormalizeIngredient(tc.in))
		})
	}
}

// ---------------------------------------------------------------------------
// Lookup tests
// ---------------------------------------------------------------------------

func TestLookupIngredientAisle(t *testing.T) {
	cache := newFixtureCache(t)

	cases := []struct {
		name       string
		input      string
		wantAisle  string
		wantReason string
	}{
		// Tier 1: exact match (user history)
		{
			name:       "exact match lowercase",
			input:      "apples",
			wantAisle:  "Produce",
			wantReason: "learned",
		},
		{
			name:       "exact match with quantity stripped",
			input:      "2 lbs apples",
			wantAisle:  "Produce",
			wantReason: "learned",
		},
		{
			name:       "exact match uppercase",
			input:      "Apples",
			wantAisle:  "Produce",
			wantReason: "learned",
		},
		{
			name:       "exact match multiword",
			input:      "chicken breast",
			wantAisle:  "Meat",
			wantReason: "learned",
		},
		{
			name:       "exact match with parens stripped",
			input:      "cheddar cheese (shredded)",
			wantAisle:  "Dairy",
			wantReason: "learned",
		},

		// Tier 2: singular/plural variant
		{
			name:       "singular of a plural ingredient",
			input:      "apple",
			wantAisle:  "Produce",
			wantReason: "learned", // "apple" → try "apples" → match
		},

		// Tier 3: default keyword table
		{
			name:       "frozen pizza keyword",
			input:      "frozen pizza",
			wantAisle:  "Frozen",
			wantReason: "default",
		},
		{
			name:       "single frozen keyword",
			input:      "frozen waffles",
			wantAisle:  "Frozen",
			wantReason: "default",
		},
		{
			name:       "flour via baking keyword",
			input:      "all purpose flour",
			wantAisle:  "Baking",
			wantReason: "default",
		},

		// No match
		{
			name:       "unknown widget",
			input:      "unknown widget",
			wantAisle:  "",
			wantReason: "",
		},
		{
			name:       "empty string",
			input:      "",
			wantAisle:  "",
			wantReason: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotAisle, gotReason := cache.LookupIngredientAisle(tc.input)
			assert.Equal(t, tc.wantAisle, gotAisle, "aisle")
			assert.Equal(t, tc.wantReason, gotReason, "reason")
		})
	}
}

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
	aisles := cache.Aisles()
	assert.Len(t, aisles, len(fixtureAisles))
	for i := 1; i < len(aisles); i++ {
		assert.LessOrEqual(t, aisles[i-1].OrderFlag, aisles[i].OrderFlag)
	}
}

// TestLookupNoAisles verifies that lookup returns empty when the user has no
// aisles configured (do not invent aisles).
func TestLookupNoAisles(t *testing.T) {
	dir := t.TempDir()
	cache := paprika.NewCache(nil, dir+"/recipes.json", nil)
	// No aisles set — keyword matches should not produce a result.
	aisle, reason := cache.LookupIngredientAisle("apple")
	assert.Equal(t, "", aisle)
	assert.Equal(t, "", reason)
}

// TestLookupQuantityVariants covers the spec test cases explicitly.
func TestLookupQuantityVariants(t *testing.T) {
	cache := newFixtureCache(t)

	cases := []struct{ in, wantAisle, wantReason string }{
		{"2 lbs apples", "Produce", "learned"},
		{"Apples", "Produce", "learned"},
		{"apple", "Produce", "learned"},
		{"frozen pizza", "Frozen", "default"},
		{"unknown widget", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			gotA, gotR := cache.LookupIngredientAisle(tc.in)
			assert.Equal(t, tc.wantAisle, gotA)
			assert.Equal(t, tc.wantReason, gotR)
		})
	}
}

// Ensure IsFilling does not get stuck true after a refresh with fixture data.
func TestIsFillingClears(t *testing.T) {
	cache := newFixtureCache(t)
	assert.False(t, cache.IsFilling(), "should not be filling when loaded from fixture")
	_ = time.Now() // keep time import used
}
