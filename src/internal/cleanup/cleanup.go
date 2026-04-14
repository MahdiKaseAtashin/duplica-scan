package cleanup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	Path       string
	Deleted    bool
	BackupPath string
	Err        error
}

type DeletionMode string

const (
	DeletionModeDelete     DeletionMode = "delete"
	DeletionModeQuarantine DeletionMode = "quarantine"
)

type DeleteOptions struct {
	DryRun        bool
	Mode          DeletionMode
	QuarantineDir string
}

type QuarantineEntry struct {
	BackupPath   string    `json:"backup_path"`
	OriginalPath string    `json:"original_path"`
	CreatedAt    time.Time `json:"created_at"`
	Source       string    `json:"source"`
}

// DeleteFiles removes selected files unless dryRun is enabled.
func DeleteFiles(paths []string, dryRun bool) []Result {
	return DeleteFilesWithOptions(paths, DeleteOptions{
		DryRun: dryRun,
		Mode:   DeletionModeDelete,
	})
}

func DeleteFilesWithOptions(paths []string, options DeleteOptions) []Result {
	if options.Mode == "" {
		options.Mode = DeletionModeDelete
	}

	results := make([]Result, 0, len(paths))
	for _, path := range paths {
		if options.DryRun {
			results = append(results, Result{
				Path:    path,
				Deleted: false,
				Err:     nil,
			})
			continue
		}

		switch options.Mode {
		case DeletionModeQuarantine:
			backupPath, err := MovePathToQuarantine(path, options.QuarantineDir, "duplicates")
			results = append(results, Result{
				Path:       path,
				Deleted:    err == nil,
				BackupPath: backupPath,
				Err:        err,
			})
		default:
			err := os.Remove(path)
			results = append(results, Result{
				Path:    path,
				Deleted: err == nil,
				Err:     err,
			})
		}
	}
	return results
}

func ResolveQuarantineDir(quarantineDir string) (string, error) {
	baseDir := strings.TrimSpace(quarantineDir)
	if baseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		baseDir = filepath.Join(home, ".duplica-scan", "quarantine")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", err
	}
	return baseDir, nil
}

func MovePathToQuarantine(path string, quarantineDir string, source string) (string, error) {
	baseDir, err := ResolveQuarantineDir(quarantineDir)
	if err != nil {
		return "", err
	}
	name := filepath.Base(path)
	target := filepath.Join(baseDir, fmt.Sprintf("%d-%s-%s", time.Now().UnixNano(), strconv.Itoa(os.Getpid()), name))
	if err := os.Rename(path, target); err != nil {
		return "", err
	}
	entry := QuarantineEntry{
		BackupPath:   target,
		OriginalPath: path,
		CreatedAt:    time.Now(),
		Source:       strings.TrimSpace(source),
	}
	if entry.Source == "" {
		entry.Source = "unknown"
	}
	if err := appendQuarantineEntry(baseDir, entry); err != nil {
		return "", err
	}
	return target, nil
}

func ListQuarantineEntries(quarantineDir string, days int) ([]QuarantineEntry, error) {
	baseDir, err := ResolveQuarantineDir(quarantineDir)
	if err != nil {
		return nil, err
	}
	entries, err := readQuarantineEntries(baseDir)
	if err != nil {
		return nil, err
	}
	if days <= 0 {
		return filterExistingEntries(baseDir, entries)
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	filtered := make([]QuarantineEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.CreatedAt.After(cutoff) || entry.CreatedAt.Equal(cutoff) {
			filtered = append(filtered, entry)
		}
	}
	return filterExistingEntries(baseDir, filtered)
}

func RestoreQuarantineEntry(entry QuarantineEntry) error {
	if strings.TrimSpace(entry.BackupPath) == "" || strings.TrimSpace(entry.OriginalPath) == "" {
		return fmt.Errorf("invalid quarantine entry")
	}
	if _, err := os.Stat(entry.BackupPath); err != nil {
		return err
	}
	if _, err := os.Stat(entry.OriginalPath); err == nil {
		return fmt.Errorf("restore target already exists: %s", entry.OriginalPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(entry.OriginalPath), 0o755); err != nil {
		return err
	}
	if err := os.Rename(entry.BackupPath, entry.OriginalPath); err != nil {
		return err
	}
	baseDir := filepath.Dir(entry.BackupPath)
	entries, err := readQuarantineEntries(baseDir)
	if err == nil {
		updated := make([]QuarantineEntry, 0, len(entries))
		for _, item := range entries {
			if item.BackupPath == entry.BackupPath {
				continue
			}
			updated = append(updated, item)
		}
		_ = writeQuarantineEntries(baseDir, updated)
	}
	return nil
}

func PruneExpiredQuarantine(quarantineDir string, days int) (int, error) {
	if days <= 0 {
		return 0, nil
	}
	baseDir, err := ResolveQuarantineDir(quarantineDir)
	if err != nil {
		return 0, err
	}
	entries, err := readQuarantineEntries(baseDir)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	removed := 0
	kept := make([]QuarantineEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.CreatedAt.Before(cutoff) {
			if err := os.RemoveAll(entry.BackupPath); err != nil && !errorsIsNotExist(err) {
				kept = append(kept, entry)
				continue
			}
			removed++
			continue
		}
		kept = append(kept, entry)
	}
	if err := writeQuarantineEntries(baseDir, kept); err != nil {
		return removed, err
	}
	return removed, nil
}

func appendQuarantineEntry(baseDir string, entry QuarantineEntry) error {
	entries, err := readQuarantineEntries(baseDir)
	if err != nil {
		return err
	}
	entries = append(entries, entry)
	return writeQuarantineEntries(baseDir, entries)
}

func readQuarantineEntries(baseDir string) ([]QuarantineEntry, error) {
	path := filepath.Join(baseDir, "manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if errorsIsNotExist(err) {
			return []QuarantineEntry{}, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return []QuarantineEntry{}, nil
	}
	var entries []QuarantineEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	filtered := make([]QuarantineEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.BackupPath) == "" || strings.TrimSpace(entry.OriginalPath) == "" {
			continue
		}
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = time.Now()
		}
		filtered = append(filtered, entry)
	}
	return filtered, nil
}

func filterExistingEntries(baseDir string, entries []QuarantineEntry) ([]QuarantineEntry, error) {
	filtered := make([]QuarantineEntry, 0, len(entries))
	modified := false
	for _, entry := range entries {
		_, statErr := os.Stat(entry.BackupPath)
		if statErr == nil {
			filtered = append(filtered, entry)
			continue
		}
		if !errorsIsNotExist(statErr) {
			filtered = append(filtered, entry)
			continue
		}
		modified = true
	}
	if modified {
		if err := writeQuarantineEntries(baseDir, filtered); err != nil {
			return nil, err
		}
	}
	return filtered, nil
}

func writeQuarantineEntries(baseDir string, entries []QuarantineEntry) error {
	path := filepath.Join(baseDir, "manifest.json")
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func errorsIsNotExist(err error) bool {
	if err == nil {
		return false
	}
	return os.IsNotExist(err) || errors.Is(err, os.ErrNotExist)
}
