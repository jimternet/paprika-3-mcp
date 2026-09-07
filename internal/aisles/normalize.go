package aisles

import (
	"regexp"
	"strings"
)

// NormalizeForLookup prepares a (quantity-stripped) cleanName for history and keyword matching.
// Lowercases, strips trailing parentheticals, strips text after first comma.
func NormalizeForLookup(cleanName string) string {
	s := strings.ToLower(strings.TrimSpace(cleanName))
	s = trailingParenRe.ReplaceAllString(s, "")
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

var trailingParenRe = regexp.MustCompile(`\s*\(.*\)\s*$`)

// NormalizeAisleName normalizes an aisle name for comparison:
// lowercase, strip commas and "&", remove stop-words (and/goods/foods/products/supplies),
// collapse whitespace, singularize the last word.
func NormalizeAisleName(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, ",", " ")
	s = strings.ReplaceAll(s, "&", " ")
	words := strings.Fields(s)
	var out []string
	for _, w := range words {
		switch w {
		case "and", "goods", "foods", "products", "supplies":
			// skip
		default:
			out = append(out, w)
		}
	}
	if len(out) > 0 {
		out[len(out)-1] = singularize(out[len(out)-1])
	}
	return strings.Join(out, " ")
}

// singularize returns the singular form using simple heuristics.
func singularize(w string) string {
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 3:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "ves") && len(w) > 3:
		return w[:len(w)-3] + "f"
	case strings.HasSuffix(w, "es") && len(w) > 3 && !strings.HasSuffix(w, "oes"):
		base := w[:len(w)-2]
		if len(base) >= 3 {
			return base
		}
		return w[:len(w)-1]
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 2:
		return w[:len(w)-1]
	}
	return w
}

// singularPluralVariants returns alternate forms (plural and singular) for broader matching.
func singularPluralVariants(s string) []string {
	var out []string
	if strings.HasSuffix(s, "ies") && len(s) > 3 {
		out = append(out, s[:len(s)-3]+"y")
	}
	if strings.HasSuffix(s, "es") && len(s) > 2 {
		base := s[:len(s)-2]
		if len(base) >= 3 {
			out = append(out, base)
		}
	}
	if strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") && len(s) > 1 {
		out = append(out, s[:len(s)-1])
	}
	out = append(out, s+"s")
	if strings.HasSuffix(s, "o") || strings.HasSuffix(s, "x") ||
		strings.HasSuffix(s, "ch") || strings.HasSuffix(s, "sh") {
		out = append(out, s+"es")
	}
	return out
}

// aisleSynonyms groups normalized aisle names that are equivalent across common Paprika naming variants.
var aisleSynonyms = [][]string{
	// Produce
	{"produce", "fruit vegetable", "fruit veg"},
	// Pasta, Rice and Beans
	{"pasta rice bean", "dry", "pantry", "grain", "dry grain"},
	// Breads and Cereals
	{"bread cereal", "bread", "bakery bread"},
	// Canned and Jar Goods
	{"canned jar", "canned jarred", "canned"},
	// Spices and Seasonings
	{"spice seasoning", "spice", "seasoning"},
	// Sauces and Condiments
	{"sauce condiment", "condiment"},
	// Oils and Dressings
	{"oil dressing", "oil"},
	// Beer, Wine and Spirits
	{"beer wine spirit", "alcohol", "liquor", "beer wine"},
	// Health and Beauty
	{"health beauty", "personal care", "pharmacy"},
	// Home and Garden
	{"home garden", "hardware"},
	// Cleaning Supplies
	{"cleaning supply", "household", "cleaning"},
	// International Cuisine
	{"international cuisine", "international", "ethnic", "world cuisine"},
}
