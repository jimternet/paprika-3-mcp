package paprika

import (
	"regexp"
	"strings"
)

// extractQtyRe matches a leading quantity phrase. Groups:
// [1] article or number, [2] parenthetical, [3] unit, [4] rest of ingredient.
var extractQtyRe = regexp.MustCompile(
	`(?i)^` +
		`(a|an|\d+(?:\s+\d+)?(?:[./]\d+)?)` +
		`(?:\s+(\([^)]+\)))?` +
		`(?:\s+(cup|cups|tbsp|tsps?|tablespoon|tablespoons|teaspoon|teaspoons|` +
		`lb|lbs|pound|pounds|oz|ounce|ounces|g|gram|grams|kg|` +
		`ml|l|liter|liters|` +
		`bunch|bunches|head|heads|clove|cloves|` +
		`bag|bags|box|boxes|jar|jars|can|cans|bottle|bottles|` +
		`package|packages|pkg|pkgs)\.?` +
		`)?` +
		`(?:\s+of)?` +
		`\s+(.+)$`,
)

// qtyContainerUnits maps plural/singular container unit words to their singular
// form. Container units are preserved in the aisleKey so the aisle modifier
// ("can of X" → Canned Goods) fires correctly in LookupIngredientAisle.
var qtyContainerUnits = map[string]string{
	"bag": "bag", "bags": "bag",
	"box": "box", "boxes": "box",
	"jar": "jar", "jars": "jar",
	"can": "can", "cans": "can",
	"bottle": "bottle", "bottles": "bottle",
	"package": "package", "packages": "package",
	"pkg": "package", "pkgs": "package",
}

// ExtractQuantity parses a leading quantity phrase from an ingredient string.
//
// Returns:
//   - quantity: the extracted phrase ("2 lbs", "1 (15 oz) can", "1"); empty if none found.
//   - cleanName: the ingredient with the quantity phrase removed.
//   - aisleKey: a form of the ingredient suitable for LookupIngredientAisle. For
//     container units (can, jar, bag, …) this is "can of X" so the aisle modifier
//     fires. For measurement units (lbs, cup, …) this is just cleanName.
//
// Articles "a" and "an" are normalised to "1". When no leading quantity phrase is
// found all three values are derived from the trimmed input.
func ExtractQuantity(s string) (quantity, cleanName, aisleKey string) {
	s = strings.TrimSpace(s)
	m := extractQtyRe.FindStringSubmatch(s)
	if m == nil {
		return "", s, s
	}
	numStr, paren, unit, rest := m[1], m[2], m[3], strings.TrimSpace(m[4])

	if strings.EqualFold(numStr, "a") || strings.EqualFold(numStr, "an") {
		numStr = "1"
	}

	var parts []string
	parts = append(parts, numStr)
	if paren != "" {
		parts = append(parts, paren)
	}
	if unit != "" {
		parts = append(parts, unit)
	}
	quantity = strings.Join(parts, " ")
	cleanName = rest

	if singular, ok := qtyContainerUnits[strings.ToLower(unit)]; ok {
		aisleKey = singular + " of " + cleanName
	} else {
		aisleKey = cleanName
	}
	return
}
