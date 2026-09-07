package aisles

import (
	_ "embed"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed default_aisles.yaml
var defaultConfigBytes []byte

type rawConfig struct {
	Aisles    []string            `yaml:"aisles"`
	Modifiers map[string]string   `yaml:"modifiers"`
	Keywords  map[string][]string `yaml:"keywords"`
}

// Config is the fully merged and validated aisle configuration.
type Config struct {
	Aisles    []string      // config-declared aisle names (for validation)
	modifiers map[string]string // lowercase modifier word → config aisle name
	keywords  []keywordEntry    // sorted by len(keyword) descending
}

type keywordEntry struct {
	keyword string
	aisle   string
}

// Load loads and merges the embedded defaults with an optional user config file.
// Missing user file is not an error. Returns warnings for validation issues.
func Load(userPath string) (*Config, []string, error) {
	var base rawConfig
	if err := yaml.Unmarshal(defaultConfigBytes, &base); err != nil {
		return nil, nil, fmt.Errorf("embedded defaults corrupt: %w", err)
	}

	merged := base
	if userPath != "" {
		data, err := os.ReadFile(userPath)
		if err == nil {
			var user rawConfig
			if err := yaml.Unmarshal(data, &user); err != nil {
				return nil, nil, fmt.Errorf("parse %s: %w", userPath, err)
			}
			merged = overlay(base, user)
		} else if !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("read %s: %w", userPath, err)
		}
	}
	return build(merged)
}

// overlay applies user on top of base per the merge rules.
func overlay(base, user rawConfig) rawConfig {
	out := rawConfig{
		Aisles:    base.Aisles,
		Modifiers: make(map[string]string),
		Keywords:  make(map[string][]string),
	}
	if len(user.Aisles) > 0 {
		out.Aisles = user.Aisles
	}
	for k, v := range base.Modifiers {
		out.Modifiers[k] = v
	}
	for k, v := range user.Modifiers {
		out.Modifiers[k] = v
	}
	// Deep-copy base keywords.
	for aisle, kws := range base.Keywords {
		cp := make([]string, len(kws))
		copy(cp, kws)
		out.Keywords[aisle] = cp
	}
	// User keywords: each keyword moves to the user's specified aisle.
	for aisle, kws := range user.Keywords {
		for _, kw := range kws {
			kwLower := strings.ToLower(kw)
			for a, list := range out.Keywords {
				var kept []string
				for _, k := range list {
					if strings.ToLower(k) != kwLower {
						kept = append(kept, k)
					}
				}
				out.Keywords[a] = kept
			}
			out.Keywords[aisle] = append(out.Keywords[aisle], kw)
		}
	}
	return out
}

// build converts a rawConfig into a validated Config.
func build(raw rawConfig) (*Config, []string, error) {
	cfg := &Config{
		Aisles:    raw.Aisles,
		modifiers: make(map[string]string),
	}

	aisleNorms := make(map[string]bool, len(raw.Aisles))
	for _, a := range raw.Aisles {
		aisleNorms[NormalizeAisleName(a)] = true
	}

	hasAisle := func(name string) bool {
		return aisleNorms[NormalizeAisleName(name)]
	}

	var warnings []string

	for mod, aisle := range raw.Modifiers {
		if !hasAisle(aisle) {
			warnings = append(warnings, fmt.Sprintf("modifier %q references unknown aisle %q", mod, aisle))
			continue
		}
		cfg.modifiers[strings.ToLower(strings.TrimSpace(mod))] = aisle
	}

	for aisle, kws := range raw.Keywords {
		if !hasAisle(aisle) {
			warnings = append(warnings, fmt.Sprintf("keyword aisle %q not in aisles list", aisle))
			continue
		}
		for _, kw := range kws {
			cfg.keywords = append(cfg.keywords, keywordEntry{
				keyword: strings.ToLower(strings.TrimSpace(kw)),
				aisle:   aisle,
			})
		}
	}

	sort.Slice(cfg.keywords, func(i, j int) bool {
		return len(cfg.keywords[i].keyword) > len(cfg.keywords[j].keyword)
	})

	return cfg, warnings, nil
}
