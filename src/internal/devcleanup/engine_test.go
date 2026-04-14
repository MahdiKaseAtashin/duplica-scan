package devcleanup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRiskLevel(t *testing.T) {
	if got := ParseRiskLevel("safe"); got != RiskSafe {
		t.Fatalf("expected safe risk, got %v", got)
	}
	if got := ParseRiskLevel("moderate"); got != RiskModerate {
		t.Fatalf("expected moderate risk, got %v", got)
	}
	if got := ParseRiskLevel("aggressive"); got != RiskAggressive {
		t.Fatalf("expected aggressive risk, got %v", got)
	}
	if got := ParseRiskLevel("unknown"); got != RiskSafe {
		t.Fatalf("expected unknown fallback to safe, got %v", got)
	}
}

func TestIsSafeCleanupPath(t *testing.T) {
	cases := []struct {
		path string
		safe bool
	}{
		{path: ".", safe: false},
		{path: `C:\`, safe: false},
		{path: `/`, safe: false},
		{path: `/tmp/dev-cache`, safe: true},
		{path: `C:\Users\dev\AppData\Local\Temp`, safe: true},
	}
	for _, tc := range cases {
		if got := isSafeCleanupPath(tc.path); got != tc.safe {
			t.Fatalf("path %q expected safe=%t got=%t", tc.path, tc.safe, got)
		}
	}
}

func TestDiscoverPatternTargets(t *testing.T) {
	root := t.TempDir()
	matchDir := filepath.Join(root, "service-a", "bin")
	otherDir := filepath.Join(root, "service-a", "logs")
	if err := os.MkdirAll(matchDir, 0o755); err != nil {
		t.Fatalf("mkdir match: %v", err)
	}
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}
	filePath := filepath.Join(matchDir, "artifact.dll")
	if err := os.WriteFile(filePath, []byte("abc"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	paths, size, err := discoverPatternTargets([]string{root}, []string{"bin", "obj"}, 0)
	if err != nil {
		t.Fatalf("discover pattern: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("expected 1 path match, got %d", len(paths))
	}
	if size <= 0 {
		t.Fatalf("expected size > 0, got %d", size)
	}
}
