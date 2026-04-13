package duplicates

import (
	"log"

	"duplica-scan/src/internal/model"
)

// Group represents files that share identical content.
type Group struct {
	Hash  string
	Size  int64
	Files []model.FileMeta
}

// Progress captures duplicate detection status.
type Progress struct {
	HashedFiles int64
	TotalToHash int64
	CurrentPath string
}

type HashFunc func(path string) (string, error)
type ProgressCallback func(Progress)

// Detect finds duplicate files by grouping by size first, then by hash.
func Detect(files []model.FileMeta, hashFn HashFunc, onProgress ProgressCallback) ([]Group, []error) {
	sizeBuckets := make(map[int64][]model.FileMeta, len(files))
	for _, file := range files {
		sizeBuckets[file.Size] = append(sizeBuckets[file.Size], file)
	}

	totalToHash := int64(0)
	for _, bucket := range sizeBuckets {
		if len(bucket) > 1 {
			totalToHash += int64(len(bucket))
		}
	}

	hashedFiles := int64(0)
	allErrors := make([]error, 0, 32)
	result := make([]Group, 0, 16)

	for size, bucket := range sizeBuckets {
		if len(bucket) < 2 {
			continue
		}

		hashBuckets := make(map[string][]model.FileMeta, len(bucket))
		for _, file := range bucket {
			hash, err := hashFn(file.Path)
			hashedFiles++

			if onProgress != nil {
				onProgress(Progress{
					HashedFiles: hashedFiles,
					TotalToHash: totalToHash,
					CurrentPath: file.Path,
				})
			}

			if err != nil {
				allErrors = append(allErrors, err)
				log.Printf("hashing error on %s: %v", file.Path, err)
				continue
			}
			hashBuckets[hash] = append(hashBuckets[hash], file)
		}

		for hash, hashBucket := range hashBuckets {
			if len(hashBucket) < 2 {
				continue
			}
			result = append(result, Group{
				Hash:  hash,
				Size:  size,
				Files: hashBucket,
			})
		}
	}

	return result, allErrors
}
