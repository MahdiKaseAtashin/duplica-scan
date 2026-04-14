package report

import (
	"os"
	"path/filepath"
	"testing"

	"cleanpulse/src/internal/duplicates"
	"cleanpulse/src/internal/model"
)

func TestExportCSV(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "report.csv")
	groups := sampleGroups()

	if err := Export(groups, FormatCSV, out); err != nil {
		t.Fatalf("export csv failed: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected csv file: %v", err)
	}
}

func TestExportJSON(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "report.json")
	groups := sampleGroups()

	if err := Export(groups, FormatJSON, out); err != nil {
		t.Fatalf("export json failed: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected json file: %v", err)
	}
}

func TestExportRejectsInvalidFormat(t *testing.T) {
	err := Export(sampleGroups(), "xml", "report.xml")
	if err == nil {
		t.Fatal("expected invalid format error")
	}
}

func sampleGroups() []duplicates.Group {
	return []duplicates.Group{
		{
			Hash: "abc",
			Size: 10,
			Files: []model.FileMeta{
				{Name: "a.txt", Path: "/tmp/a.txt", Size: 10},
				{Name: "b.txt", Path: "/tmp/b.txt", Size: 10},
			},
		},
	}
}
