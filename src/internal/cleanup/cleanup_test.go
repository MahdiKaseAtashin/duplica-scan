package cleanup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteFilesDryRunDoesNotDelete(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "sample.txt")
	if err := os.WriteFile(filePath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	results := DeleteFiles([]string{filePath}, true)
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if results[0].Deleted {
		t.Fatalf("expected no deletion in dry run")
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("file should still exist: %v", err)
	}
}

func TestDeleteFilesDeletesWhenNotDryRun(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "delete-me.txt")
	if err := os.WriteFile(filePath, []byte("bye"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	results := DeleteFiles([]string{filePath}, false)
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if !results[0].Deleted {
		t.Fatalf("expected deletion success, err: %v", results[0].Err)
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, got err: %v", err)
	}
}

func TestDeleteFilesWithOptionsQuarantineMovesFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "move-me.txt")
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	quarantineDir := filepath.Join(tmpDir, "quarantine")
	results := DeleteFilesWithOptions([]string{filePath}, DeleteOptions{
		DryRun:        false,
		Mode:          DeletionModeQuarantine,
		QuarantineDir: quarantineDir,
	})
	if len(results) != 1 {
		t.Fatalf("expected one result, got %d", len(results))
	}
	if !results[0].Deleted {
		t.Fatalf("expected quarantine move success, err: %v", results[0].Err)
	}
	if results[0].BackupPath == "" {
		t.Fatalf("expected backup path to be set")
	}
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatalf("expected original file removed, got err: %v", err)
	}
	if _, err := os.Stat(results[0].BackupPath); err != nil {
		t.Fatalf("expected backup file to exist: %v", err)
	}
	entries, err := ListQuarantineEntries(quarantineDir, 7)
	if err != nil {
		t.Fatalf("list quarantine entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one quarantine entry, got %d", len(entries))
	}
	if entries[0].OriginalPath != filePath {
		t.Fatalf("unexpected original path: %s", entries[0].OriginalPath)
	}
}

func TestRestoreQuarantineEntryRestoresFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "restore-me.txt")
	if err := os.WriteFile(filePath, []byte("restore"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	quarantineDir := filepath.Join(tmpDir, "quarantine")
	results := DeleteFilesWithOptions([]string{filePath}, DeleteOptions{
		Mode:          DeletionModeQuarantine,
		QuarantineDir: quarantineDir,
	})
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("failed to quarantine file: %+v", results)
	}
	entries, err := ListQuarantineEntries(quarantineDir, 30)
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(entries))
	}
	if err := RestoreQuarantineEntry(entries[0]); err != nil {
		t.Fatalf("restore entry: %v", err)
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("expected restored file to exist: %v", err)
	}
}

func TestPruneExpiredQuarantineRemovesOldEntries(t *testing.T) {
	tmpDir := t.TempDir()
	quarantineDir := filepath.Join(tmpDir, "quarantine")
	if err := os.MkdirAll(quarantineDir, 0o755); err != nil {
		t.Fatalf("mkdir quarantine: %v", err)
	}
	oldBackup := filepath.Join(quarantineDir, "old.txt")
	if err := os.WriteFile(oldBackup, []byte("old"), 0o644); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	manifest := `[{"backup_path":"` + filepath.ToSlash(oldBackup) + `","original_path":"` + filepath.ToSlash(filepath.Join(tmpDir, "x.txt")) + `","created_at":"` + time.Now().Add(-48*time.Hour).Format(time.RFC3339Nano) + `","source":"cleanup"}]`
	if err := os.WriteFile(filepath.Join(quarantineDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	removed, err := PruneExpiredQuarantine(quarantineDir, 1)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 1 {
		t.Fatalf("expected one removed entry, got %d", removed)
	}
	if _, err := os.Stat(oldBackup); !os.IsNotExist(err) {
		t.Fatalf("expected old backup removed, err: %v", err)
	}
}
