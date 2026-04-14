package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"cleanpulse/src/internal/buildinfo"
	"cleanpulse/src/internal/cleanup"
	"cleanpulse/src/internal/duplicates"
	"cleanpulse/src/internal/hash"
	"cleanpulse/src/internal/report"
	"cleanpulse/src/internal/scanner"
	"cleanpulse/src/internal/selection"
	"cleanpulse/src/internal/ui"
)

func main() {
	rootPath := flag.String("path", "", "Directory (or drive root) to scan (required)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	dryRun := flag.Bool("dry-run", false, "Dry run mode: report duplicates without deletion")
	hashWorkers := flag.Int("hash-workers", runtime.NumCPU(), "Number of concurrent hashing workers")
	excludeExts := flag.String("exclude-exts", "", "Comma-separated file extensions to skip (example: .log,.tmp)")
	excludeDirs := flag.String("exclude-dirs", "", "Comma-separated directory names to skip (example: node_modules,.git)")
	minSizeBytes := flag.Int64("min-size-bytes", 0, "Minimum file size in bytes to include (0 disables)")
	maxSizeBytes := flag.Int64("max-size-bytes", 0, "Maximum file size in bytes to include (0 disables)")
	exportFormat := flag.String("export-format", "", "Export format: csv or json (optional)")
	exportPath := flag.String("export-path", "", "Output path for exported report (optional)")
	autoSelect := flag.String("auto-select", "", "Auto deletion selection strategy: newest or oldest (optional)")
	matchMode := flag.String("match-mode", string(duplicates.MatchModeContent), "Duplicate matching mode: content|name|name+content|size")
	deleteMode := flag.String("delete-mode", string(cleanup.DeletionModeDelete), "Delete mode: delete|quarantine")
	quarantineDir := flag.String("quarantine-dir", "", "Optional quarantine folder when delete-mode=quarantine")
	flag.Parse()
	if *showVersion {
		fmt.Printf("cleanpulse %s\n", buildinfo.Version)
		return
	}

	if *rootPath == "" {
		fmt.Println("Usage: cleanpulse -path <directory_or_drive_root> [-dry-run=true]")
		os.Exit(1)
	}
	strategy, err := selection.NormalizeStrategy(*autoSelect)
	if err != nil {
		log.Fatal(err)
	}

	console := ui.NewConsole()
	start := time.Now()
	filterOptions := scanner.ScanOptions{
		ExcludeExtensions: parseExtensions(*excludeExts),
		ExcludeDirs:       parseNames(*excludeDirs),
		MinSizeBytes:      *minSizeBytes,
		MaxSizeBytes:      *maxSizeBytes,
	}

	if filterOptions.MaxSizeBytes > 0 && filterOptions.MinSizeBytes > filterOptions.MaxSizeBytes {
		log.Fatalf("invalid size filter: min-size-bytes (%d) cannot be greater than max-size-bytes (%d)", filterOptions.MinSizeBytes, filterOptions.MaxSizeBytes)
	}

	console.PrintStage("Stage 1/4: Scan Setup")
	console.PrintSummaryLine(fmt.Sprintf("Path: %s", *rootPath))
	console.PrintSummaryLine(fmt.Sprintf("Dry run: %t", *dryRun))
	console.PrintSummaryLine(fmt.Sprintf("Hash workers: %d", *hashWorkers))
	console.PrintSummaryLine(fmt.Sprintf("Match mode: %s", *matchMode))
	console.PrintSummaryLine(fmt.Sprintf("Delete mode: %s", *deleteMode))

	console.PrintStage("Stage 2/4: Discover and Hash")
	fmt.Printf("Scanning: %s\n", *rootPath)
	scanSummary, err := scanner.ScanWithOptions(*rootPath, console.OnScanProgress, filterOptions)
	if err != nil {
		log.Fatalf("scan failed: %v", err)
	}
	fmt.Println()

	if *hashWorkers < 1 {
		log.Printf("invalid hash-workers value %d; defaulting to 1", *hashWorkers)
		*hashWorkers = 1
	}

	console.PrintSummaryLine(fmt.Sprintf("Collected %d files. Hashing candidate files...", len(scanSummary.Files)))
	groups, hashErrors := duplicates.DetectWithOptions(
		scanSummary.Files,
		hash.SHA256File,
		console.OnHashProgress,
		duplicates.DetectOptions{
			HashWorkers: *hashWorkers,
			MatchMode:   duplicates.MatchMode(strings.TrimSpace(strings.ToLower(*matchMode))),
		},
	)
	fmt.Println()

	console.PrintStage("Stage 3/4: Review Duplicates")
	console.PrintDuplicateGroups(groups)

	if strings.TrimSpace(*exportFormat) != "" {
		path := strings.TrimSpace(*exportPath)
		if path == "" {
			path = defaultExportPath(*exportFormat)
		}
		if err := report.Export(groups, *exportFormat, path); err != nil {
			log.Printf("report export failed: %v", err)
		} else {
			fmt.Printf("Report exported: %s\n", path)
		}
	}

	console.PrintStage("Stage 4/4: Action Summary")
	fmt.Printf("Dry run mode: %t\n", *dryRun)
	fmt.Printf("Duplicate groups found: %d\n", len(groups))
	fmt.Printf("Scanner non-fatal errors: %d | Hash non-fatal errors: %d\n", len(scanSummary.Errors), len(hashErrors))
	fmt.Printf("Completed in %s\n", time.Since(start).Round(time.Millisecond))

	if len(groups) == 0 {
		return
	}

	selected := make([]string, 0, 128)
	if strategy == selection.StrategyManual {
		selected = console.CollectDeletionSelection(groups)
	} else {
		selected = selection.AutoSelect(groups, strategy)
		fmt.Printf("Auto-select strategy %q picked %d file(s) for deletion.\n", strategy, len(selected))
	}
	if len(selected) == 0 {
		fmt.Println("No files selected for deletion.")
		return
	}

	sort.Strings(selected)
	totalBytes := int64(0)
	selectedSet := make(map[string]struct{}, len(selected))
	for _, path := range selected {
		selectedSet[path] = struct{}{}
	}
	for _, group := range groups {
		for _, file := range group.Files {
			if _, ok := selectedSet[file.Path]; ok {
				totalBytes += file.Size
			}
		}
	}

	if !console.ConfirmDeletionWithPreview(selected, len(selected), totalBytes, *dryRun) {
		fmt.Println("Deletion canceled by user.")
		return
	}

	results := cleanup.DeleteFilesWithOptions(selected, cleanup.DeleteOptions{
		DryRun:        *dryRun,
		Mode:          cleanup.DeletionMode(strings.TrimSpace(strings.ToLower(*deleteMode))),
		QuarantineDir: strings.TrimSpace(*quarantineDir),
	})
	failures := 0
	for _, result := range results {
		if result.Err != nil {
			failures++
			fmt.Printf("Failed: %s (%v)\n", result.Path, result.Err)
		}
	}
	console.PrintDeletionResults(len(results), failures, *dryRun)
}

func defaultExportPath(format string) string {
	base := fmt.Sprintf("duplicate-report-%s", time.Now().Format("20060102-150405"))
	switch strings.ToLower(strings.TrimSpace(format)) {
	case report.FormatCSV:
		return filepath.Join(".", "reports", base+".csv")
	case report.FormatJSON:
		return filepath.Join(".", "reports", base+".json")
	default:
		return filepath.Join(".", "reports", base+".txt")
	}
}

func parseExtensions(raw string) map[string]struct{} {
	return parseCSVSet(raw, func(v string) string {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			return ""
		}
		if !strings.HasPrefix(v, ".") {
			return "." + v
		}
		return v
	})
}

func parseNames(raw string) map[string]struct{} {
	return parseCSVSet(raw, func(v string) string {
		return strings.ToLower(strings.TrimSpace(v))
	})
}

func parseCSVSet(raw string, normalizer func(string) string) map[string]struct{} {
	result := make(map[string]struct{})
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return result
	}
	for _, part := range strings.Split(raw, ",") {
		value := normalizer(part)
		if value == "" {
			continue
		}
		result[value] = struct{}{}
	}
	return result
}
