package scanner

import (
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"duplica-scan/src/internal/model"
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

// Scan recursively enumerates all files in root and ignores inaccessible paths.
func Scan(root string, onProgress ProgressCallback) (Summary, error) {
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
			return nil
		}

		info, err := d.Info()
		if err != nil {
			summary.Errors = append(summary.Errors, err)
			log.Printf("metadata read error on %s: %v", path, err)
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
