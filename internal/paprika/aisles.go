package paprika

import (
	"regexp"
	"strings"
)

// normalizeIngredient lowercases, trims, strips leading quantities/units
// ("2 lbs apples" → "apples"), and strips trailing parentheticals ("milk (whole)" → "milk").
func normalizeIngredient(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = leadingQuantityRe.ReplaceAllString(s, "")
	s = trailingParenRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// leadingQuantityRe matches an optional number followed by a unit word at the
// start of the string, e.g. "2 lbs ", "1/2 cup ", "3 cans ".
var leadingQuantityRe = regexp.MustCompile(
	`(?i)^[\d/\s]+\s*` +
		`(oz|ounces?|lbs?|pounds?|g|grams?|kg|` +
		`cups?|tbsps?|tsps?|tablespoons?|teaspoons?|` +
		`mls?|l|liters?|qts?|quarts?|pints?|` +
		`dozens?|cans?|bunch(?:es)?|cloves?|sticks?|` +
		`pkgs?|packages?|bags?|box(?:es)?|jars?|bottles?|` +
		`heads?|slices?|pieces?|servings?|stalks?|sprigs?|` +
		`handfuls?|pinch(?:es)?|dash(?:es)?)\b\.?\s+`,
)

var trailingParenRe = regexp.MustCompile(`\s*\(.*\)\s*$`)

// singularPluralVariants returns alternate forms of s to broaden ingredient matching.
func singularPluralVariants(s string) []string {
	var out []string
	// Remove plural suffixes.
	if strings.HasSuffix(s, "ies") && len(s) > 3 {
		out = append(out, s[:len(s)-3]+"y") // berries → berry
	}
	if strings.HasSuffix(s, "es") && len(s) > 2 {
		base := s[:len(s)-2]
		// Only strip "es" if what remains looks like a real word (≥3 chars).
		if len(base) >= 3 {
			out = append(out, base) // tomatoes → tomat... only if ≥3
		}
	}
	if strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") && len(s) > 1 {
		out = append(out, s[:len(s)-1]) // apples → apple
	}
	// Add plural suffixes.
	out = append(out, s+"s") // apple → apples
	if strings.HasSuffix(s, "o") || strings.HasSuffix(s, "x") ||
		strings.HasSuffix(s, "ch") || strings.HasSuffix(s, "sh") {
		out = append(out, s+"es") // tomato → tomatoes, box → boxes
	}
	return out
}

// wordMatch reports whether keyword (possibly multi-word) appears as a
// contiguous whole-word sequence within name.
func wordMatch(name, keyword string) bool {
	nameWords := strings.Fields(name)
	kwWords := strings.Fields(keyword)
	if len(kwWords) == 0 || len(nameWords) < len(kwWords) {
		return false
	}
	for i := 0; i <= len(nameWords)-len(kwWords); i++ {
		ok := true
		for j, kw := range kwWords {
			if nameWords[i+j] != kw {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// LookupIngredientAisle returns the best aisle name for the ingredient and the
// reason it was chosen: "learned" (from the user's Paprika history), "default"
// (from the built-in keyword table), or "" (no match → Miscellaneous).
//
// Lookup tiers:
//  1. Exact match in the user's groceryingredients table.
//  2. Singular/plural variants of the normalized name.
//  3. Built-in keyword table (defaultAisleKeywords in aisles_default.go).
//
// Tier 3 names are verified against the user's actual aisle list; if the user
// does not have an aisle with that name the entry is skipped (aisles are never
// invented).
func (c *Cache) LookupIngredientAisle(name string) (aisleName, reason string) {
	normalized := normalizeIngredient(name)

	c.mu.RLock()
	aisles := make([]GroceryAisle, len(c.aisles))
	copy(aisles, c.aisles)
	ingredients := make([]GroceryIngredient, len(c.ingredients))
	copy(ingredients, c.ingredients)
	c.mu.RUnlock()

	uidToName := func(uid string) string {
		for _, a := range aisles {
			if a.UID == uid {
				return a.Name
			}
		}
		return ""
	}

	resolveAisleName := func(candidate string) (string, bool) {
		lower := strings.ToLower(candidate)
		for _, a := range aisles {
			if strings.ToLower(a.Name) == lower {
				return a.Name, true
			}
		}
		return "", false
	}

	// Tier 1: exact match.
	for _, ing := range ingredients {
		if strings.ToLower(ing.Name) == normalized {
			if n := uidToName(ing.AisleUID); n != "" {
				return n, "learned"
			}
		}
	}

	// Tier 2: singular/plural variants.
	for _, variant := range singularPluralVariants(normalized) {
		for _, ing := range ingredients {
			if strings.ToLower(ing.Name) == variant {
				if n := uidToName(ing.AisleUID); n != "" {
					return n, "learned"
				}
			}
		}
	}

	// Tier 3: built-in keyword table.
	for _, kw := range defaultAisleKeywords {
		if wordMatch(normalized, kw.keyword) {
			if n, ok := resolveAisleName(kw.aisle); ok {
				return n, "default"
			}
		}
	}

	return "", ""
}
