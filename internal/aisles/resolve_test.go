package aisles_test

import (
	"testing"

	"github.com/jimternet/paprika-3-mcp/internal/aisles"
	"github.com/jimternet/paprika-3-mcp/internal/paprika"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// liveAisles uses Paprika stock aisle names for the resolver tests.
var testLiveAisles = []aisles.LiveAisle{
	{Name: "Produce", UID: "a-produce"},
	{Name: "Dairy", UID: "a-dairy"},
	{Name: "Meat", UID: "a-meat"},
	{Name: "Seafood", UID: "a-seafood"},
	{Name: "Bakery", UID: "a-bakery"},
	{Name: "Baking Goods", UID: "a-baking"},
	{Name: "Breads and Cereals", UID: "a-breads"},
	{Name: "Frozen Foods", UID: "a-frozen"},
	{Name: "Canned and Jar Goods", UID: "a-canned"},
	{Name: "Pasta, Rice and Beans", UID: "a-pantry"},
	{Name: "Oils and Dressings", UID: "a-oils"},
	{Name: "Sauces and Condiments", UID: "a-condiments"},
	{Name: "Spices and Seasonings", UID: "a-spices"},
	{Name: "Snacks", UID: "a-snacks"},
	{Name: "Beverages", UID: "a-beverages"},
	{Name: "Miscellaneous", UID: "a-misc"},
}

func loadTestConfig(t *testing.T) *aisles.Config {
	t.Helper()
	cfg, warnings, err := aisles.Load("")
	require.NoError(t, err, "Load embedded config")
	for _, w := range warnings {
		t.Logf("config warning: %s", w)
	}
	return cfg
}

func TestResolve(t *testing.T) {
	cfg := loadTestConfig(t)

	// History fixture: kale → Frozen Foods (to test history stage override)
	kaleHistory := []aisles.HistoryItem{
		{NormalizedName: "kale", AisleName: "Frozen Foods", AisleUID: "a-frozen"},
	}

	cases := []struct {
		name      string
		raw       string
		wantName  string // expected Result.Name (normalized)
		wantQty   string // expected Result.Quantity
		wantAisle string // expected Result.AisleName
		wantStage string // "history", "modifier", "keyword", or "no match"
		history   []aisles.HistoryItem
	}{
		{
			name:      "apples keyword",
			raw:       "apples",
			wantName:  "apples",
			wantQty:   "",
			wantAisle: "Produce",
			wantStage: "keyword",
		},
		{
			name:      "2 lbs apples quantity stripped keyword",
			raw:       "2 lbs apples",
			wantName:  "apples",
			wantQty:   "2 lbs",
			wantAisle: "Produce",
			wantStage: "keyword",
		},
		{
			name:      "canned corn modifier",
			raw:       "canned corn",
			wantName:  "canned corn",
			wantQty:   "",
			wantAisle: "Canned and Jar Goods",
			wantStage: "modifier",
		},
		{
			name:      "corn keyword",
			raw:       "corn",
			wantName:  "corn",
			wantQty:   "",
			wantAisle: "Produce",
			wantStage: "keyword",
		},
		{
			name:      "frozen corn modifier",
			raw:       "frozen corn",
			wantName:  "frozen corn",
			wantQty:   "",
			wantAisle: "Frozen Foods",
			wantStage: "modifier",
		},
		{
			name:      "corn tortillas keyword",
			raw:       "corn tortillas",
			wantName:  "corn tortillas",
			wantQty:   "",
			wantAisle: "Breads and Cereals",
			wantStage: "keyword",
		},
		{
			name:      "a can of black beans modifier",
			raw:       "a can of black beans",
			wantName:  "black beans",
			wantQty:   "1 can",
			wantAisle: "Canned and Jar Goods",
			wantStage: "modifier",
		},
		{
			name:      "black beans keyword",
			raw:       "black beans",
			wantName:  "black beans",
			wantQty:   "",
			wantAisle: "Pasta, Rice and Beans",
			wantStage: "keyword",
		},
		{
			name:      "1 (15 oz) can chickpeas modifier",
			raw:       "1 (15 oz) can chickpeas",
			wantName:  "chickpeas",
			wantQty:   "1 (15 oz) can",
			wantAisle: "Canned and Jar Goods",
			wantStage: "modifier",
		},
		{
			// cannellini beans: "cannellini" and "beans" are NOT modifiers
			name:      "cannellini beans keyword no modifier",
			raw:       "cannellini beans",
			wantName:  "cannellini beans",
			wantQty:   "",
			wantAisle: "Pasta, Rice and Beans",
			wantStage: "keyword",
		},
		{
			name:      "coconut milk keyword",
			raw:       "coconut milk",
			wantName:  "coconut milk",
			wantQty:   "",
			wantAisle: "Canned and Jar Goods",
			wantStage: "keyword",
		},
		{
			name:      "mixed nuts keyword",
			raw:       "mixed nuts",
			wantName:  "mixed nuts",
			wantQty:   "",
			wantAisle: "Snacks",
			wantStage: "keyword",
		},
		{
			name:      "cilantro chopped comma strip",
			raw:       "cilantro, chopped",
			wantName:  "cilantro",
			wantQty:   "",
			wantAisle: "Produce",
			wantStage: "keyword",
		},
		{
			name:      "3 cloves garlic quantity stripped",
			raw:       "3 cloves garlic",
			wantName:  "garlic",
			wantQty:   "3 cloves",
			wantAisle: "Produce",
			wantStage: "keyword",
		},
		{
			name:      "salt and pepper keyword",
			raw:       "salt and pepper",
			wantName:  "salt and pepper",
			wantQty:   "",
			wantAisle: "Spices and Seasonings",
			wantStage: "keyword",
		},
		{
			// frozen pizza: raw has "frozen" modifier → Frozen Foods
			name:      "frozen pizza modifier",
			raw:       "frozen pizza",
			wantName:  "frozen pizza",
			wantQty:   "",
			wantAisle: "Frozen Foods",
			wantStage: "modifier",
		},
		{
			name:      "unknown widget no match",
			raw:       "unknown widget",
			wantName:  "unknown widget",
			wantQty:   "",
			wantAisle: "",
			wantStage: "no match",
		},
		{
			name:      "tomatoes keyword via variant",
			raw:       "tomatoes",
			wantQty:   "",
			wantAisle: "Produce",
			wantStage: "keyword",
		},
		{
			name:      "kale from history",
			raw:       "kale",
			wantName:  "kale",
			wantQty:   "",
			wantAisle: "Frozen Foods",
			wantStage: "history",
			history:   kaleHistory,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			qty, cleanName, _ := paprika.ExtractQuantity(tc.raw)
			result := aisles.Resolve(tc.raw, cleanName, qty, cfg, tc.history, testLiveAisles)

			if tc.wantName != "" {
				assert.Equal(t, tc.wantName, result.Name, "Name")
			}
			assert.Equal(t, tc.wantQty, result.Quantity, "Quantity")
			assert.Equal(t, tc.wantAisle, result.AisleName, "AisleName")
			assert.Equal(t, tc.wantStage, result.Stage, "Stage")
		})
	}
}

func TestResolveNilConfig(t *testing.T) {
	qty, cleanName, _ := paprika.ExtractQuantity("apple")
	result := aisles.Resolve("apple", cleanName, qty, nil, nil, testLiveAisles)
	assert.Equal(t, "no match", result.Stage)
	assert.Equal(t, "", result.AisleName)
}

func TestResolveNoAisles(t *testing.T) {
	cfg := loadTestConfig(t)
	qty, cleanName, _ := paprika.ExtractQuantity("apple")
	result := aisles.Resolve("apple", cleanName, qty, cfg, nil, nil)
	assert.Equal(t, "no match", result.Stage)
	assert.Equal(t, "", result.AisleName)
}
