package paprika

import (
	"regexp"
	"strings"
)

// normalizeIngredient lowercases, trims, strips leading articles ("a", "an"),
// strips leading quantities/units ("2 lbs apples" → "apples"), and strips
// trailing parentheticals ("milk (whole)" → "milk").
func normalizeIngredient(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = leadingArticleRe.ReplaceAllString(s, "")
	s = leadingQuantityRe.ReplaceAllString(s, "")
	s = trailingParenRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

var leadingArticleRe = regexp.MustCompile(`(?i)^(?:a|an)\s+`)

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

// formModifier maps a leading word or phrase to candidate aisle names (tried in
// order; first one present in the user's aisle list wins). A nil aisles slice
// means "no override — fall through to ingredient keywords" (used for "fresh").
type formModifier struct {
	prefix string
	aisles []string
}

// formModifiers is checked before the keyword table. A leading form word
// ("canned", "frozen", etc.) overrides ingredient-based aisle lookup.
// Longer prefixes are listed before shorter ones to avoid premature matches.
var formModifiers = []formModifier{
	{"can of", []string{"Canned Goods", "Pantry", "Canned"}},
	{"tin of", []string{"Canned Goods", "Pantry", "Canned"}},
	{"jar of", []string{"Canned Goods", "Pantry", "Canned"}},
	{"canned", []string{"Canned Goods", "Pantry", "Canned"}},
	{"tinned", []string{"Canned Goods", "Pantry", "Canned"}},
	{"jarred", []string{"Canned Goods", "Pantry", "Canned"}},
	{"frozen", []string{"Frozen"}},
	{"dried",  []string{"Pantry", "Baking", "Dry Goods"}},
	{"dry",    []string{"Pantry", "Baking", "Dry Goods"}},
	{"fresh",  nil}, // no override — fall through to ingredient keywords
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
//  3. Form modifier check: "canned X" → Canned Goods, "frozen X" → Frozen, etc.
//     If the modifier's aisle can't be resolved (even via aliases), falls through
//     to the keyword table rather than returning no match.
//  4. Built-in keyword table (defaultAisleKeywords in aisles_default.go),
//     longest keyword match wins.
//
// Aisle names are resolved via aisleAliases so that e.g. "Canned & Jarred"
// matches the canonical "Canned Goods". No aisle is ever invented.
func (c *Cache) LookupIngredientAisle(name string) (aisleName, reason string) {
	normalized := normalizeIngredient(name)

	c.mu.RLock()
	aisles := make([]GroceryAisle, len(c.aisles))
	copy(aisles, c.aisles)
	ingredients := make([]GroceryIngredient, len(c.ingredients))
	copy(ingredients, c.ingredients)
	c.mu.RUnlock()

	// stage is set before each return and emitted by the deferred log.
	var stage string
	defer func() {
		if aisleName != "" {
			c.logger.Debug("aisle lookup", "ingredient", name, "stage", stage, "aisle", aisleName)
		} else {
			c.logger.Debug("aisle lookup", "ingredient", name, "stage", "none")
		}
	}()

	uidToName := func(uid string) string {
		for _, a := range aisles {
			if a.UID == uid {
				return a.Name
			}
		}
		return ""
	}

	// resolveAisleName looks up candidate in the user's aisle list (case-insensitive),
	// then tries each alias from aisleAliases so that e.g. "Canned Goods" also
	// matches a user aisle named "Canned & Jarred".
	resolveAisleName := func(candidate string) (string, bool) {
		lower := strings.ToLower(candidate)
		for _, a := range aisles {
			if strings.ToLower(a.Name) == lower {
				return a.Name, true
			}
		}
		for _, alias := range aisleAliases[candidate] {
			aliasLower := strings.ToLower(alias)
			for _, a := range aisles {
				if strings.ToLower(a.Name) == aliasLower {
					return a.Name, true
				}
			}
		}
		return "", false
	}

	// Tier 1: exact match.
	for _, ing := range ingredients {
		if strings.ToLower(ing.Name) == normalized {
			if n := uidToName(ing.AisleUID); n != "" {
				stage = "learned"
				return n, "learned"
			}
		}
	}

	// Tier 2: singular/plural variants.
	for _, variant := range singularPluralVariants(normalized) {
		for _, ing := range ingredients {
			if strings.ToLower(ing.Name) == variant {
				if n := uidToName(ing.AisleUID); n != "" {
					stage = "learned-variant"
					return n, "learned"
				}
			}
		}
	}

	// Form modifier check: "canned X" → Canned Goods, "frozen X" → Frozen, etc.
	// Runs before the keyword table. "fresh" explicitly falls through (nil aisles).
	// If the modifier's aisles can't be resolved, fall through to keyword table.
	for _, fm := range formModifiers {
		if normalized != fm.prefix && !strings.HasPrefix(normalized, fm.prefix+" ") {
			continue
		}
		if fm.aisles == nil {
			break // "fresh" — fall through to keyword table
		}
		for _, candidate := range fm.aisles {
			if n, ok := resolveAisleName(candidate); ok {
				stage = "modifier"
				return n, "default"
			}
		}
		// Modifier matched but none of its aisles exist in the user's list —
		// fall through to keyword table rather than returning no match.
		c.logger.Debug("aisle modifier unresolved — falling through to keyword table",
			"ingredient", name, "modifier", fm.prefix)
		break
	}

	// Tier 3: built-in keyword table — longest keyword match wins so that
	// "corn tortillas" beats "corn", "coconut milk" beats "milk", etc.
	// Try the normalized name and all singular/plural variants so that
	// "apples" matches the keyword "apple".
	tier3Candidates := append([]string{normalized}, singularPluralVariants(normalized)...)
	type kwHit struct {
		aisle  string
		keyLen int
	}
	var best *kwHit
	for _, kw := range defaultAisleKeywords {
		for _, candidate := range tier3Candidates {
			if wordMatch(candidate, kw.keyword) {
				if n, ok := resolveAisleName(kw.aisle); ok {
					if best == nil || len(kw.keyword) > best.keyLen {
						best = &kwHit{aisle: n, keyLen: len(kw.keyword)}
					}
				}
				break
			}
		}
	}
	if best != nil {
		stage = "keyword"
		return best.aisle, "default"
	}

	return "", ""
}
