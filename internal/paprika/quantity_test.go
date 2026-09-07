package paprika_test

import (
	"testing"

	"github.com/jimternet/paprika-3-mcp/internal/paprika"
	"github.com/stretchr/testify/assert"
)

func TestExtractQuantity(t *testing.T) {
	cases := []struct {
		in           string
		wantQty      string
		wantClean    string
		wantAisleKey string
	}{
		// Bare numbers
		{"2 lbs apples", "2 lbs", "apples", "apples"},
		{"3 cloves garlic", "3 cloves", "garlic", "garlic"},
		{"12 oz pasta", "12 oz", "pasta", "pasta"},
		{"4 tbsp butter", "4 tbsp", "butter", "butter"},
		{"1 tsp salt", "1 tsp", "salt", "salt"},
		{"1/2 cup flour", "1/2 cup", "flour", "flour"},

		// Container units → aisleKey preserves "can of X" etc.
		{"3 cans diced tomatoes", "3 cans", "diced tomatoes", "can of diced tomatoes"},
		{"2 jars salsa", "2 jars", "salsa", "jar of salsa"},
		{"1 bag frozen peas", "1 bag", "frozen peas", "bag of frozen peas"},
		{"2 boxes pasta", "2 boxes", "pasta", "box of pasta"},
		{"1 bottle olive oil", "1 bottle", "olive oil", "bottle of olive oil"},
		{"1 package tofu", "1 package", "tofu", "package of tofu"},
		{"2 pkgs noodles", "2 pkgs", "noodles", "package of noodles"},

		// Article "a" / "an" normalised to "1"
		{"a can of black beans", "1 can", "black beans", "can of black beans"},
		{"an onion", "1", "onion", "onion"},

		// Parenthetical size embedded before unit
		{"1 (15 oz) can chickpeas", "1 (15 oz) can", "chickpeas", "can of chickpeas"},
		{"2 (14 oz) cans diced tomatoes", "2 (14 oz) cans", "diced tomatoes", "can of diced tomatoes"},

		// No quantity — passthrough
		{"apples", "", "apples", "apples"},
		{"fresh garlic", "", "fresh garlic", "fresh garlic"},
		{"large onion", "", "large onion", "large onion"},

		// Mixed fraction (e.g. "1 1/2")
		{"1 1/2 cups milk", "1 1/2 cups", "milk", "milk"},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			qty, clean, aisleKey := paprika.ExtractQuantity(tc.in)
			assert.Equal(t, tc.wantQty, qty, "quantity")
			assert.Equal(t, tc.wantClean, clean, "cleanName")
			assert.Equal(t, tc.wantAisleKey, aisleKey, "aisleKey")
		})
	}
}
