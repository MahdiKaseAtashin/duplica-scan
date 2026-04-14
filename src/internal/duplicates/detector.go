package duplicates

import (
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"cleanpulse/src/internal/model"
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

type DetectOptions struct {
	HashWorkers int
	MatchMode   MatchMode
}

type MatchMode string

const (
	MatchModeContent     MatchMode = "content"
	MatchModeName        MatchMode = "name"
	MatchModeNameContent MatchMode = "name+content"
	MatchModeSize        MatchMode = "size"
)

// Detect finds duplicate files by grouping by size first, then by hash.
func Detect(files []model.FileMeta, hashFn HashFunc, onProgress ProgressCallback) ([]Group, []error) {
	return DetectWithOptions(files, hashFn, onProgress, DetectOptions{HashWorkers: 1, MatchMode: MatchModeContent})
}

// DetectWithOptions finds duplicate files using a bounded hashing worker pool.
func DetectWithOptions(files []model.FileMeta, hashFn HashFunc, onProgress ProgressCallback, options DetectOptions) ([]Group, []error) {
	if options.HashWorkers < 1 {
		options.HashWorkers = 1
	}
	if options.MatchMode == "" {
		options.MatchMode = MatchModeContent
	}

	switch options.MatchMode {
	case MatchModeName:
		return detectByName(files), []error{}
	case MatchModeSize:
		return detectBySize(files), []error{}
	}

	sizeBuckets := make(map[int64][]model.FileMeta, len(files))
	for _, file := range files {
		sizeBuckets[file.Size] = append(sizeBuckets[file.Size], file)
	}

	candidates := make([]model.FileMeta, 0, len(files))
	totalToHash := int64(0)
	for _, bucket := range sizeBuckets {
		if len(bucket) > 1 {
			candidates = append(candidates, bucket...)
			totalToHash += int64(len(bucket))
		}
	}

	if len(candidates) == 0 {
		return []Group{}, []error{}
	}

	type hashResult struct {
		file model.FileMeta
		hash string
		err  error
	}

	jobs := make(chan model.FileMeta, options.HashWorkers*2)
	results := make(chan hashResult, options.HashWorkers*2)

	var wg sync.WaitGroup
	for i := 0; i < options.HashWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				hash, err := hashFn(file.Path)
				results <- hashResult{file: file, hash: hash, err: err}
			}
		}()
	}

	go func() {
		for _, file := range candidates {
			jobs <- file
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	hashedFiles := int64(0)
	allErrors := make([]error, 0, 32)
	groupedBySizeAndHash := make(map[int64]map[string][]model.FileMeta, len(sizeBuckets))

	for result := range results {
		newCount := atomic.AddInt64(&hashedFiles, 1)
		if onProgress != nil {
			onProgress(Progress{
				HashedFiles: newCount,
				TotalToHash: totalToHash,
				CurrentPath: result.file.Path,
			})
		}

		if result.err != nil {
			allErrors = append(allErrors, result.err)
			log.Printf("hashing error on %s: %v", result.file.Path, result.err)
			continue
		}

		byHash, ok := groupedBySizeAndHash[result.file.Size]
		if !ok {
			byHash = make(map[string][]model.FileMeta)
			groupedBySizeAndHash[result.file.Size] = byHash
		}
		byHash[result.hash] = append(byHash[result.hash], result.file)
	}

	finalGroups := make([]Group, 0, 16)
	for size, byHash := range groupedBySizeAndHash {
		for hash, filesWithSameHash := range byHash {
			if options.MatchMode == MatchModeNameContent {
				filesWithSameHash = filterByMatchingName(filesWithSameHash)
			}
			if len(filesWithSameHash) < 2 {
				continue
			}
			finalGroups = append(finalGroups, Group{
				Hash:  hash,
				Size:  size,
				Files: filesWithSameHash,
			})
		}
	}

	return finalGroups, allErrors
}

func detectByName(files []model.FileMeta) []Group {
	byName := make(map[string][]model.FileMeta)
	for _, file := range files {
		key := strings.ToLower(strings.TrimSpace(file.Name))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(file.Path))
		}
		byName[key] = append(byName[key], file)
	}
	out := make([]Group, 0, 16)
	for name, bucket := range byName {
		if len(bucket) < 2 {
			continue
		}
		size := int64(0)
		if len(bucket) > 0 {
			size = bucket[0].Size
		}
		out = append(out, Group{Hash: "name:" + name, Size: size, Files: bucket})
	}
	return out
}

func detectBySize(files []model.FileMeta) []Group {
	bySize := make(map[int64][]model.FileMeta)
	for _, file := range files {
		bySize[file.Size] = append(bySize[file.Size], file)
	}
	out := make([]Group, 0, 16)
	for size, bucket := range bySize {
		if len(bucket) < 2 {
			continue
		}
		out = append(out, Group{Hash: "size", Size: size, Files: bucket})
	}
	return out
}

func filterByMatchingName(files []model.FileMeta) []model.FileMeta {
	if len(files) < 2 {
		return files
	}
	byName := make(map[string][]model.FileMeta)
	for _, file := range files {
		name := strings.ToLower(strings.TrimSpace(file.Name))
		byName[name] = append(byName[name], file)
	}
	out := make([]model.FileMeta, 0, len(files))
	for _, bucket := range byName {
		if len(bucket) > 1 {
			out = append(out, bucket...)
		}
	}
	return out
}
