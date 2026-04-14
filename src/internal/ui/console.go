package ui

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"cleanpulse/src/internal/duplicates"
	"cleanpulse/src/internal/scanner"
)

type Console struct {
	lastScanUpdate time.Time
	lastHashUpdate time.Time
	reader         *bufio.Reader
}

func NewConsole() *Console {
	return &Console{
		reader: bufio.NewReader(os.Stdin),
	}
}

func (c *Console) PrintStage(title string) {
	fmt.Printf("\n== %s ==\n", title)
}

func (c *Console) PrintSummaryLine(text string) {
	fmt.Println(text)
}

func (c *Console) OnScanProgress(p scanner.Progress) {
	if time.Since(c.lastScanUpdate) < 150*time.Millisecond {
		return
	}
	c.lastScanUpdate = time.Now()
	fmt.Printf("\r[scan] files: %-8d size: %-10s current: %s", p.FilesSeen, formatBytes(p.BytesSeen), trimPath(p.Current, 60))
}

func (c *Console) OnHashProgress(p duplicates.Progress) {
	if time.Since(c.lastHashUpdate) < 100*time.Millisecond {
		return
	}
	c.lastHashUpdate = time.Now()

	percent := float64(0)
	if p.TotalToHash > 0 {
		percent = (float64(p.HashedFiles) / float64(p.TotalToHash)) * 100
	}

	fmt.Printf("\r[hash] %6.2f%% (%d/%d) current: %s", percent, p.HashedFiles, p.TotalToHash, trimPath(p.CurrentPath, 60))
}

func (c *Console) PrintDuplicateGroups(groups []duplicates.Group) {
	fmt.Println()
	fmt.Println()
	fmt.Println("Duplicate Groups (sorted by reclaimable size):")
	if len(groups) == 0 {
		fmt.Println("- No duplicates found.")
		return
	}

	sorted := append([]duplicates.Group(nil), groups...)
	sort.Slice(sorted, func(i, j int) bool {
		leftReclaimable := int64(len(sorted[i].Files)-1) * sorted[i].Size
		rightReclaimable := int64(len(sorted[j].Files)-1) * sorted[j].Size
		if leftReclaimable == rightReclaimable {
			return sorted[i].Size > sorted[j].Size
		}
		return leftReclaimable > rightReclaimable
	})

	for i, group := range sorted {
		reclaimable := int64(len(group.Files)-1) * group.Size
		fmt.Printf("\nGroup %d | files: %d | each: %s | reclaimable: %s | hash: %s\n", i+1, len(group.Files), formatBytes(group.Size), formatBytes(reclaimable), group.Hash)
		for _, file := range group.Files {
			fmt.Printf("  - name: %s\n", file.Name)
			fmt.Printf("    path: %s\n", file.Path)
			fmt.Printf("    size: %s\n", formatBytes(file.Size))
			fmt.Printf("    modified: %s\n", file.ModifiedAt.Format(time.RFC3339))
		}
	}
}

func (c *Console) CollectDeletionSelection(groups []duplicates.Group) []string {
	selected := make([]string, 0, 128)
	seenPaths := make(map[string]struct{}, 256)

	fmt.Println()
	fmt.Println("Selection mode: for each group, enter file numbers to delete (comma-separated).")
	fmt.Println("Leave blank to skip a group.")

	for i, group := range groups {
		fmt.Printf("\nGroup %d (size: %s)\n", i+1, formatBytes(group.Size))
		for idx, file := range group.Files {
			fmt.Printf("  [%d] %s\n", idx+1, file.Path)
		}
		fmt.Print("Delete indexes: ")

		input, err := c.reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Input error: %v. Skipping group.\n", err)
			continue
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		parts := strings.Split(input, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			n, err := strconv.Atoi(part)
			if err != nil || n < 1 || n > len(group.Files) {
				fmt.Printf("Ignoring invalid index: %q\n", part)
				continue
			}
			path := group.Files[n-1].Path
			if _, exists := seenPaths[path]; exists {
				continue
			}
			seenPaths[path] = struct{}{}
			selected = append(selected, path)
		}
	}

	return selected
}

func (c *Console) ConfirmDeletion(count int) bool {
	if count == 0 {
		return false
	}

	return c.ConfirmDeletionWithPreview(nil, count, 0, false)
}

func (c *Console) ConfirmDeletionWithPreview(selected []string, count int, totalBytes int64, dryRun bool) bool {
	if count == 0 {
		return false
	}

	action := "delete"
	if dryRun {
		action = "simulate deletion of"
	}
	fmt.Printf("\nAction preview: %s %d file(s), total size: %s\n", action, count, formatBytes(totalBytes))
	if len(selected) > 0 {
		fmt.Println("Sample targets:")
		previewCount := len(selected)
		if previewCount > 5 {
			previewCount = 5
		}
		for i := 0; i < previewCount; i++ {
			fmt.Printf("  - %s\n", selected[i])
		}
	}

	token := fmt.Sprintf("DELETE %d FILES", count)
	fmt.Printf("Type %q to confirm: ", token)
	input, err := c.reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Input error: %v\n", err)
		return false
	}
	return strings.TrimSpace(input) == token
}

func (c *Console) PrintDeletionResults(resultsCount int, failedCount int, dryRun bool) {
	if dryRun {
		fmt.Printf("\nDry run complete. %d selected file(s) would be deleted.\n", resultsCount)
		return
	}
	fmt.Printf("\nDeletion complete. Success: %d, Failed: %d\n", resultsCount-failedCount, failedCount)
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func trimPath(path string, max int) string {
	if len(path) <= max {
		return path
	}
	if max < 8 {
		return path[:max]
	}
	// Keep this separator-agnostic for Windows, macOS, and Linux paths.
	return "..." + strings.TrimLeft(path[len(path)-max+3:], `/\`)
}
