package aisles

import "strings"

// HistoryItem is a learned ingredient→aisle mapping from user's Paprika history.
type HistoryItem struct {
	NormalizedName string // from NormalizeForLookup
	AisleName      string // live aisle name
	AisleUID       string
}

// LiveAisle is a live Paprika grocery aisle.
type LiveAisle struct {
	Name string
	UID  string
}

// Result is the output of Resolve.
type Result struct {
	Name      string // normalized ingredient name (quantity already stripped by caller)
	Quantity  string // extracted quantity phrase, or ""
	AisleName string // matched live aisle name, or ""
	AisleUID  string // matched live aisle UID, or ""
	Stage     string // "history", "modifier", "keyword", or "no match"
}

// Resolve returns the best aisle for an ingredient.
//   - raw: the original user input (used for modifier whole-word matching).
//   - cleanName: quantity-stripped name (from paprika.ExtractQuantity).
//   - qty: extracted quantity string (from paprika.ExtractQuantity).
//   - cfg: merged aisles config (nil → always "no match").
//   - history: pre-normalized learned ingredient→aisle entries.
//   - liveAisles: aisles from user's Paprika account.
func Resolve(raw, cleanName, qty string, cfg *Config, history []HistoryItem, liveAisles []LiveAisle) Result {
	normalized := NormalizeForLookup(cleanName)
	res := Result{Name: normalized, Quantity: qty, Stage: "no match"}

	if cfg == nil || len(liveAisles) == 0 {
		return res
	}

	// Stage 1: History.
	candidates := append([]string{normalized}, singularPluralVariants(normalized)...)
	for _, h := range history {
		for _, c := range candidates {
			if h.NormalizedName == c {
				res.AisleName = h.AisleName
				res.AisleUID = h.AisleUID
				res.Stage = "history"
				return res
			}
		}
	}

	// Stage 2: Modifier (whole-word check on raw input).
	rawWords := strings.Fields(strings.ToLower(raw))
	for _, w := range rawWords {
		if configAisle, ok := cfg.modifiers[w]; ok {
			if la, ok := resolveAisle(configAisle, liveAisles); ok {
				res.AisleName = la.Name
				res.AisleUID = la.UID
				res.Stage = "modifier"
				return res
			}
		}
	}

	// Stage 3: Keyword (longest match first; sorted by len desc).
	for _, ke := range cfg.keywords {
		for _, c := range candidates {
			if wordMatch(c, ke.keyword) {
				if la, ok := resolveAisle(ke.aisle, liveAisles); ok {
					res.AisleName = la.Name
					res.AisleUID = la.UID
					res.Stage = "keyword"
					return res
				}
				break // keyword matched but aisle not available; try next keyword
			}
		}
	}

	return res
}

// resolveAisle finds the live aisle matching configAisle via normalization and synonyms.
func resolveAisle(configAisle string, liveAisles []LiveAisle) (LiveAisle, bool) {
	normConf := NormalizeAisleName(configAisle)
	for _, la := range liveAisles {
		if NormalizeAisleName(la.Name) == normConf {
			return la, true
		}
	}
	// Synonym check.
	for _, group := range aisleSynonyms {
		inGroup := false
		for _, s := range group {
			if s == normConf {
				inGroup = true
				break
			}
		}
		if !inGroup {
			continue
		}
		for _, la := range liveAisles {
			normLive := NormalizeAisleName(la.Name)
			for _, s := range group {
				if s == normLive {
					return la, true
				}
			}
		}
	}
	return LiveAisle{}, false
}

// wordMatch reports whether keyword appears as a contiguous whole-word sequence in name.
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
