package duplicates

import (
	"errors"
	"testing"

	"cleanpulse/src/internal/model"
)

func TestDetectGroupsBySizeAndHash(t *testing.T) {
	files := []model.FileMeta{
		{Name: "a.txt", Path: "a.txt", Size: 10},
		{Name: "b.txt", Path: "b.txt", Size: 10},
		{Name: "c.txt", Path: "c.txt", Size: 50},
		{Name: "d.txt", Path: "d.txt", Size: 10},
	}

	hashes := map[string]string{
		"a.txt": "same",
		"b.txt": "same",
		"d.txt": "other",
	}

	groups, errs := Detect(files, func(path string) (string, error) {
		v, ok := hashes[path]
		if !ok {
			return "", errors.New("unexpected path")
		}
		return v, nil
	}, nil)

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %d", len(errs))
	}
	if len(groups) != 1 {
		t.Fatalf("expected one duplicate group, got %d", len(groups))
	}
	if len(groups[0].Files) != 2 {
		t.Fatalf("expected two files in group, got %d", len(groups[0].Files))
	}
}

func TestDetectWithOptionsUsesWorkerPool(t *testing.T) {
	files := []model.FileMeta{
		{Name: "a", Path: "a", Size: 10},
		{Name: "b", Path: "b", Size: 10},
	}

	calls := 0
	groups, errs := DetectWithOptions(files, func(path string) (string, error) {
		calls++
		return "same", nil
	}, nil, DetectOptions{HashWorkers: 4})

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %d", len(errs))
	}
	if calls != 2 {
		t.Fatalf("expected 2 hash calls, got %d", calls)
	}
	if len(groups) != 1 {
		t.Fatalf("expected one group, got %d", len(groups))
	}
}

func TestDetectWithOptionsInvalidWorkersDefaultsToOne(t *testing.T) {
	files := []model.FileMeta{
		{Name: "x", Path: "x", Size: 10},
		{Name: "y", Path: "y", Size: 10},
	}

	groups, errs := DetectWithOptions(files, func(path string) (string, error) {
		return "same", nil
	}, nil, DetectOptions{HashWorkers: 0})

	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %d", len(errs))
	}
	if len(groups) != 1 {
		t.Fatalf("expected one group, got %d", len(groups))
	}
}

func TestDetectWithOptionsMatchModeName(t *testing.T) {
	files := []model.FileMeta{
		{Name: "same.txt", Path: "a/same.txt", Size: 10},
		{Name: "same.txt", Path: "b/same.txt", Size: 99},
		{Name: "other.txt", Path: "c/other.txt", Size: 10},
	}
	groups, errs := DetectWithOptions(files, func(path string) (string, error) {
		return "", nil
	}, nil, DetectOptions{HashWorkers: 2, MatchMode: MatchModeName})
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %d", len(errs))
	}
	if len(groups) != 1 {
		t.Fatalf("expected one name-based group, got %d", len(groups))
	}
	if len(groups[0].Files) != 2 {
		t.Fatalf("expected two files in name group, got %d", len(groups[0].Files))
	}
}

func TestDetectWithOptionsMatchModeNameContent(t *testing.T) {
	files := []model.FileMeta{
		{Name: "same.txt", Path: "a/same.txt", Size: 10},
		{Name: "same.txt", Path: "b/same.txt", Size: 10},
		{Name: "diff.txt", Path: "c/diff.txt", Size: 10},
	}
	hashFn := func(path string) (string, error) { return "hash", nil }
	groups, errs := DetectWithOptions(files, hashFn, nil, DetectOptions{HashWorkers: 2, MatchMode: MatchModeNameContent})
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %d", len(errs))
	}
	if len(groups) != 1 {
		t.Fatalf("expected one name+content group, got %d", len(groups))
	}
	if len(groups[0].Files) != 2 {
		t.Fatalf("expected two files in group, got %d", len(groups[0].Files))
	}
}
