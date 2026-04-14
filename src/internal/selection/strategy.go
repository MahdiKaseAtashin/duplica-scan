package selection

import (
	"errors"
	"sort"
	"strings"

	"cleanpulse/src/internal/duplicates"
	"cleanpulse/src/internal/model"
)

const (
	StrategyManual = ""
	StrategyNewest = "newest"
	StrategyOldest = "oldest"
)

func NormalizeStrategy(raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case StrategyManual, StrategyNewest, StrategyOldest:
		return v, nil
	default:
		return "", errors.New("invalid auto-select strategy (allowed: newest, oldest)")
	}
}

// AutoSelect returns files to delete while keeping exactly one file per duplicate group.
func AutoSelect(groups []duplicates.Group, strategy string) []string {
	selected := make([]string, 0, 128)
	seen := make(map[string]struct{}, 256)

	for _, group := range groups {
		if len(group.Files) < 2 {
			continue
		}

		files := append([]model.FileMeta(nil), group.Files...)
		sort.Slice(files, func(i, j int) bool {
			if files[i].ModifiedAt.Equal(files[j].ModifiedAt) {
				return files[i].Path < files[j].Path
			}
			if strategy == StrategyNewest {
				return files[i].ModifiedAt.After(files[j].ModifiedAt)
			}
			return files[i].ModifiedAt.Before(files[j].ModifiedAt)
		})

		// Keep the first (newest/oldest based on sorting), delete the rest.
		for _, f := range files[1:] {
			if _, exists := seen[f.Path]; exists {
				continue
			}
			seen[f.Path] = struct{}{}
			selected = append(selected, f.Path)
		}
	}

	return selected
}
