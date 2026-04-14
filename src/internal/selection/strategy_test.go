package selection

import (
	"testing"
	"time"

	"cleanpulse/src/internal/duplicates"
	"cleanpulse/src/internal/model"
)

func TestAutoSelectNewest(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	t1 := t0.Add(2 * time.Hour)
	t2 := t0.Add(4 * time.Hour)

	groups := []duplicates.Group{
		{
			Hash: "h1",
			Size: 10,
			Files: []model.FileMeta{
				{Path: "a", ModifiedAt: t0},
				{Path: "b", ModifiedAt: t2}, // keep
				{Path: "c", ModifiedAt: t1},
			},
		},
	}

	got := AutoSelect(groups, StrategyNewest)
	if len(got) != 2 {
		t.Fatalf("expected 2 selected files, got %d", len(got))
	}
	if got[0] == "b" || got[1] == "b" {
		t.Fatalf("newest file should be kept, selected: %v", got)
	}
}

func TestAutoSelectOldest(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	t1 := t0.Add(2 * time.Hour)

	groups := []duplicates.Group{
		{
			Hash: "h1",
			Size: 10,
			Files: []model.FileMeta{
				{Path: "a", ModifiedAt: t1},
				{Path: "b", ModifiedAt: t0}, // keep
			},
		},
	}

	got := AutoSelect(groups, StrategyOldest)
	if len(got) != 1 {
		t.Fatalf("expected 1 selected file, got %d", len(got))
	}
	if got[0] != "a" {
		t.Fatalf("expected to delete newer file a, got %v", got)
	}
}
