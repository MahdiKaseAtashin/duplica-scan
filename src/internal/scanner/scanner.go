package scanner

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"cleanpulse/src/internal/model"
)

// Progress provides live information while files are enumerated.
type Progress struct {
	FilesSeen int64
	BytesSeen int64
	Current   string
}

// Summary captures results and non-fatal scanner issues.
type Summary struct {
	Files  []model.FileMeta
	Errors []error
}

type ProgressCallback func(Progress)

type ScanOptions struct {
	ExcludeExtensions map[string]struct{}
	ExcludeDirs       map[string]struct{}
	MinSizeBytes      int64
	MaxSizeBytes      int64
}

// Scan recursively enumerates all files in root and ignores inaccessible paths.
func Scan(root string, onProgress ProgressCallback) (Summary, error) {
	return ScanWithOptions(root, onProgress, ScanOptions{})
}

// ScanWithOptions recursively enumerates files with exclusion filters.
func ScanWithOptions(root string, onProgress ProgressCallback, options ScanOptions) (Summary, error) {
	info, err := os.Stat(root)
	if err != nil {
		return Summary{}, err
	}
	if !info.IsDir() {
		return Summary{}, errors.New("scan root must be a directory")
	}

	summary := Summary{
		Files:  make([]model.FileMeta, 0, 4096),
		Errors: make([]error, 0, 64),
	}

	var filesSeen int64
	var bytesSeen int64

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			summary.Errors = append(summary.Errors, walkErr)
			log.Printf("permission or walk error on %s: %v", path, walkErr)
			return nil
		}

		if d.IsDir() {
			if shouldSkipDir(path, d.Name(), root, options.ExcludeDirs) {
				return fs.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			summary.Errors = append(summary.Errors, err)
			log.Printf("metadata read error on %s: %v", path, err)
			return nil
		}

		if shouldSkipExtension(info.Name(), options.ExcludeExtensions) {
			return nil
		}
		if !sizeWithinRange(info.Size(), options.MinSizeBytes, options.MaxSizeBytes) {
			return nil
		}

		filesSeen++
		bytesSeen += info.Size()

		fileMeta := model.FileMeta{
			Name:       info.Name(),
			Path:       path,
			Size:       info.Size(),
			ModifiedAt: info.ModTime(),
		}
		summary.Files = append(summary.Files, fileMeta)

		if onProgress != nil {
			onProgress(Progress{
				FilesSeen: filesSeen,
				BytesSeen: bytesSeen,
				Current:   path,
			})
		}

		return nil
	})

	if walkErr != nil {
		return Summary{}, walkErr
	}

	return summary, nil
}

func shouldSkipDir(path string, name string, root string, excluded map[string]struct{}) bool {
	if len(excluded) == 0 {
		return false
	}
	if path == root {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	_, exists := excluded[normalized]
	return exists
}

func shouldSkipExtension(name string, excluded map[string]struct{}) bool {
	if len(excluded) == 0 {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return false
	}
	_, exists := excluded[ext]
	return exists
}

func sizeWithinRange(size int64, minBytes int64, maxBytes int64) bool {
	if minBytes > 0 && size < minBytes {
		return false
	}
	if maxBytes > 0 && size > maxBytes {
		return false
	}
	return true
}
