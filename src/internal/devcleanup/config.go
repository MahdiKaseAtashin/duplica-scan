package devcleanup

import (
	"encoding/json"
	"os"
	"strings"
)

type FileConfig struct {
	MaxRisk           string              `json:"max_risk"`
	Parallelism       int                 `json:"parallelism"`
	MinAgeHours       int                 `json:"min_age_hours"`
	ProcessAware      *bool               `json:"process_aware"`
	IncludeCategories []string            `json:"include_categories"`
	IncludeIDs        []string            `json:"include_ids"`
	ExcludeIDs        []string            `json:"exclude_ids"`
	PathOverrides     map[string][]string `json:"path_overrides"`
	PatternRoots      map[string][]string `json:"pattern_roots"`
}

func LoadFileConfig(path string) (FileConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return FileConfig{}, err
	}
	var cfg FileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return FileConfig{}, err
	}
	return cfg, nil
}

func toSet(values []string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
}
